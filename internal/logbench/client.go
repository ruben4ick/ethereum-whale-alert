package logbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// RawRPC is a minimal JSON-RPC over HTTP client that records exact request and
// response payload sizes. We deliberately avoid go-ethereum's ethclient/rpc
// here so that BytesIn / BytesOut measurements reflect what actually went over
// the wire (after the provider's JSON serialization, before HTTP gzip).
//
// The rate limiter is denominated in throughput compute units per second,
// matching the way Alchemy enforces CU/sec quotas. Each Call consumes
// throughputCU tokens before issuing the HTTP request, so methods with high
// throughput-CU cost (eth_getBlockReceipts = 500) are spaced out automatically
// without causing 429s, while cheap methods (eth_getLogs = 60) run faster.
type RawRPC struct {
	url     string
	http    *http.Client
	limiter *rate.Limiter // tokens are throughput compute units
	retries int
	id      uint64
}

type RawResponse struct {
	BodyBytes []byte
	BytesOut  int64
	BytesIn   int64
	Latency   time.Duration
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

type rpcEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// maxThroughputCU is the largest single-call throughput-CU cost across all
// methods we benchmark (eth_getBlockReceipts = 500). The token bucket's burst
// must be at least this large, otherwise WaitN(500) on a burst=250 bucket
// returns an immediate error ("Wait(n=500) exceeds limiter's burst 250").
const maxThroughputCU = 500

// NewRawRPC creates a client whose limiter holds throughputCUPS tokens/sec,
// where each token = 1 throughput compute unit. Burst is sized so the most
// expensive single call always fits.
func NewRawRPC(url string, throughputCUPS float64, retries int) *RawRPC {
	burst := int(throughputCUPS)
	if burst < maxThroughputCU {
		burst = maxThroughputCU
	}
	return &RawRPC{
		url:     url,
		http:    &http.Client{Timeout: 60 * time.Second},
		limiter: rate.NewLimiter(rate.Limit(throughputCUPS), burst),
		retries: retries,
	}
}

// Call performs one JSON-RPC request and returns the parsed result bytes plus
// transport-level measurements. Retries on rate-limit / transient errors.
// throughputCU is the method's throughput compute-unit cost; the limiter waits
// for that many tokens before issuing the request.
func (r *RawRPC) Call(ctx context.Context, method string, params []any, throughputCU int) (*RawResponse, json.RawMessage, error) {
	id := atomic.AddUint64(&r.id, 1)
	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("marshal: %w", err)
	}

	if throughputCU < 1 {
		throughputCU = 1
	}

	var lastErr error
	for attempt := 0; attempt <= r.retries; attempt++ {
		if err := r.limiter.WaitN(ctx, throughputCU); err != nil {
			return nil, nil, err
		}

		resp, result, err := r.doOnce(ctx, reqBody)
		if err == nil {
			return resp, result, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return nil, nil, err
		}
		base := time.Duration(500*(1<<attempt)) * time.Millisecond
		if base > 30*time.Second {
			base = 30 * time.Second
		}
		jitter := time.Duration(rand.Int63n(int64(base / 2)))
		select {
		case <-time.After(base + jitter):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return nil, nil, lastErr
}

func (r *RawRPC) doOnce(ctx context.Context, reqBody []byte) (*RawResponse, json.RawMessage, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Disable gzip so BytesIn measures decoded JSON length consistently across
	// providers — gzip on/off would otherwise skew bandwidth comparisons.
	httpReq.Header.Set("Accept-Encoding", "identity")

	start := time.Now()
	httpResp, err := r.http.Do(httpReq)
	if err != nil {
		return nil, nil, err
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	latency := time.Since(start)
	if err != nil {
		return nil, nil, err
	}

	if httpResp.StatusCode >= 400 {
		return nil, nil, fmt.Errorf("http %d: %s", httpResp.StatusCode, truncate(string(body), 200))
	}

	var env rpcEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, nil, fmt.Errorf("decode envelope: %w (body=%s)", err, truncate(string(body), 200))
	}
	if env.Error != nil {
		return nil, nil, env.Error
	}

	return &RawResponse{
		BodyBytes: body,
		BytesOut:  int64(len(reqBody)),
		BytesIn:   int64(len(body)),
		Latency:   latency,
	}, env.Result, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *rpcError
	if errors.As(err, &rpcErr) {
		// Provider-specific rate-limit / capacity errors carry HTTP-ish messages
		// even when delivered as JSON-RPC errors.
		msg := strings.ToLower(rpcErr.Message)
		if strings.Contains(msg, "rate limit") || strings.Contains(msg, "compute units") ||
			strings.Contains(msg, "too many requests") || strings.Contains(msg, "capacity") {
			return true
		}
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "compute units per second"),
		strings.Contains(msg, "429"),
		strings.Contains(msg, "too many requests"),
		strings.Contains(msg, "rate limit"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "eof"):
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}
