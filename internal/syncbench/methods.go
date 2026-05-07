package syncbench

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"ethereum-whale-alert/internal/logbench"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// RunWebSocket subscribes to newHeads via WS and records one BlockEvent per
// header arrival. ObservedAt is captured the instant the header is dequeued
// from the subscription channel — this is the application-visible delivery
// time and is what the rest of the pipeline (whale detection, notifications)
// actually depends on. Returns when ctx is done or the subscription errors.
func RunWebSocket(ctx context.Context, wsURL string, run *Run) error {
	client, err := ethclient.DialContext(ctx, wsURL)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer client.Close()

	headers := make(chan *types.Header, 64)
	sub, err := client.SubscribeNewHead(ctx, headers)
	if err != nil {
		return fmt.Errorf("subscribe newHeads: %w", err)
	}
	defer sub.Unsubscribe()

	seen := make(map[uint64]struct{})
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-sub.Err():
			if err == nil {
				return nil
			}
			return fmt.Errorf("subscription: %w", err)
		case h := <-headers:
			now := time.Now()
			if h == nil {
				continue
			}
			n := h.Number.Uint64()
			if _, dup := seen[n]; dup {
				// Reorg or duplicate delivery — ignore for first-seen accounting.
				continue
			}
			seen[n] = struct{}{}
			run.addEvent(BlockEvent{
				BlockNumber:    n,
				ObservedAt:     now,
				BlockTimestamp: time.Unix(int64(h.Time), 0),
			})
			run.addStats(1, 0, 0)
		}
	}
}

// RunPolling polls eth_blockNumber every interval, using the shared rate
// limiter built into RawRPC. When the head advances, it emits a BlockEvent for
// each block in the gap — all stamped with the same ObservedAt, since polling
// genuinely cannot tell intermediate arrival times apart.
//
// All polls (including idle ones that returned the same head) are accounted in
// run.Stats so the report shows polling cost honestly.
func RunPolling(ctx context.Context, rpc *logbench.RawRPC, interval time.Duration, run *Run) error {
	var lastSeen uint64
	first := true

	tick := func() {
		resp, result, err := rpc.Call(ctx, "eth_blockNumber", []any{})
		now := time.Now()
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("poll error", "interval", interval, "err", err)
			}
			return
		}
		run.addStats(1, resp.BytesIn, resp.BytesOut)

		var hex string
		if err := json.Unmarshal(result, &hex); err != nil {
			return
		}
		n, err := parseHexUint(hex)
		if err != nil {
			return
		}

		if first {
			// First poll defines the baseline — we don't know how long the
			// current head has been there, so skip emitting events for it.
			lastSeen = n
			first = false
			return
		}
		if n <= lastSeen {
			return
		}
		// Emit one event per block in (lastSeen, n]. They share ObservedAt by
		// design — that's the whole reason polling has worse latency than push.
		for b := lastSeen + 1; b <= n; b++ {
			run.addEvent(BlockEvent{BlockNumber: b, ObservedAt: now})
		}
		lastSeen = n
	}

	// Tick immediately so the baseline is captured at t=0 rather than t=interval.
	tick()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick()
		}
	}
}

func parseHexUint(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
}
