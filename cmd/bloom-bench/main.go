package main

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"math/rand"
	"os"
	"strings"
	"time"

	"ethereum-whale-alert/internal/bloombench"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/kelseyhightower/envconfig"
	"golang.org/x/time/rate"
)

type Config struct {
	GethURL           string  `envconfig:"GETH_WS_URL" required:"true"`
	BlockCount        int     `envconfig:"BENCH_BLOCKS" default:"1000"`
	Concurrency       int     `envconfig:"BENCH_CONCURRENCY" default:"4"`
	RequestsPerSecond float64 `envconfig:"BENCH_RPS" default:"3"`
	MaxRetries        int     `envconfig:"BENCH_MAX_RETRIES" default:"10"`
	OutputPrefix      string  `envconfig:"BENCH_OUT" default:"./bloom-results"`
}

type ethClientAdapter struct {
	c       *ethclient.Client
	limiter *rate.Limiter
	retries int
}

func (a *ethClientAdapter) HeaderByNumber(ctx context.Context, n *big.Int) (*types.Header, error) {
	var h *types.Header
	err := a.do(ctx, func(ctx context.Context) error {
		var err error
		h, err = a.c.HeaderByNumber(ctx, n)
		return err
	})
	return h, err
}

func (a *ethClientAdapter) BlockReceipts(ctx context.Context, n *big.Int) ([]*types.Receipt, error) {
	var rs []*types.Receipt
	bn := rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(n.Int64()))
	err := a.do(ctx, func(ctx context.Context) error {
		var err error
		rs, err = a.c.BlockReceipts(ctx, bn)
		return err
	})
	return rs, err
}

func (a *ethClientAdapter) LatestBlockNumber(ctx context.Context) (uint64, error) {
	var n uint64
	err := a.do(ctx, func(ctx context.Context) error {
		var err error
		n, err = a.c.BlockNumber(ctx)
		return err
	})
	return n, err
}

func (a *ethClientAdapter) do(ctx context.Context, fn func(ctx context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt <= a.retries; attempt++ {
		if err := a.limiter.Wait(ctx); err != nil {
			return err
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryable(err) {
			return err
		}
		base := time.Duration(500*(1<<attempt)) * time.Millisecond
		if base > 30*time.Second {
			base = 30 * time.Second
		}
		jitter := time.Duration(rand.Int63n(int64(base / 2)))
		select {
		case <-time.After(base + jitter):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return lastErr
}

func isRetryable(err error) bool {
	if err == nil {
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

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		slog.Error("config error", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := ethclient.DialContext(ctx, cfg.GethURL)
	if err != nil {
		slog.Error("dial error", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	adapter := &ethClientAdapter{
		c:       client,
		limiter: rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), max(1, int(cfg.RequestsPerSecond))),
		retries: cfg.MaxRetries,
	}

	topics := bloombench.DefaultTopics()
	runner := bloombench.New(adapter, bloombench.Config{
		BlockCount:  cfg.BlockCount,
		Concurrency: cfg.Concurrency,
		Topics:      topics,
	})

	slog.Info("starting bloom bench",
		"blocks", cfg.BlockCount,
		"topics", len(topics),
		"concurrency", cfg.Concurrency,
		"rps", cfg.RequestsPerSecond,
		"max_retries", cfg.MaxRetries,
	)

	start := time.Now()
	results, err := runner.Run(ctx)
	if err != nil {
		slog.Error("bench failed", "error", err)
		os.Exit(1)
	}
	slog.Info("bench done", "duration", time.Since(start))

	csvPath := cfg.OutputPrefix + ".csv"
	jsonPath := cfg.OutputPrefix + ".json"

	if err := bloombench.WriteCSV(csvPath, results, topics); err != nil {
		slog.Error("write csv", "error", err)
		os.Exit(1)
	}

	summary := bloombench.BuildSummary(results, topics)
	if err := bloombench.WriteJSON(jsonPath, summary); err != nil {
		slog.Error("write json", "error", err)
		os.Exit(1)
	}

	bloombench.PrintSummary(summary)
	slog.Info("artifacts written", "csv", csvPath, "json", jsonPath)
}
