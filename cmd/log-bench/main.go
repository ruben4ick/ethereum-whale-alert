package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"ethereum-whale-alert/internal/logbench"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	HTTPURL           string  `envconfig:"BENCH_HTTP_URL"`
	GethWSURL         string  `envconfig:"GETH_WS_URL"` // fallback: derive HTTPURL from this
	BlockCount        int     `envconfig:"BENCH_BLOCKS" default:"100"`
	Concurrency       int     `envconfig:"BENCH_CONCURRENCY" default:"4"`
	RequestsPerSecond float64 `envconfig:"BENCH_RPS" default:"5"`
	MaxRetries        int     `envconfig:"BENCH_MAX_RETRIES" default:"10"`
	RangeChunk        int     `envconfig:"BENCH_RANGE_CHUNK" default:"10"`
	OutputPrefix      string  `envconfig:"BENCH_OUT" default:"./logbench-results"`
}

// deriveHTTP attempts to convert a wss/ws URL into an https/http URL. Works
// for Alchemy-style URLs out of the box. For Infura the path also differs
// (/ws/v3 vs /v3) so a manual BENCH_HTTP_URL is required.
func deriveHTTP(wsURL string) string {
	switch {
	case strings.HasPrefix(wsURL, "wss://"):
		return "https://" + strings.TrimPrefix(wsURL, "wss://")
	case strings.HasPrefix(wsURL, "ws://"):
		return "http://" + strings.TrimPrefix(wsURL, "ws://")
	}
	return wsURL
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		slog.Error("config error", "error", err)
		os.Exit(1)
	}

	httpURL := cfg.HTTPURL
	if httpURL == "" {
		if cfg.GethWSURL == "" {
			slog.Error("either BENCH_HTTP_URL or GETH_WS_URL must be set")
			os.Exit(1)
		}
		httpURL = deriveHTTP(cfg.GethWSURL)
		if strings.Contains(httpURL, "/ws/") {
			slog.Warn("derived HTTP URL still contains '/ws/' — Infura requires manual BENCH_HTTP_URL (e.g., https://mainnet.infura.io/v3/KEY)", "url", httpURL)
		}
	}

	rpc := logbench.NewRawRPC(httpURL, cfg.RequestsPerSecond, cfg.MaxRetries)

	runner := logbench.New(rpc, logbench.Config{
		BlockCount:  cfg.BlockCount,
		Concurrency: cfg.Concurrency,
		RangeChunk:  cfg.RangeChunk,
		CU:          logbench.DefaultAlchemyCU(),
	})

	ctx := context.Background()
	slog.Info("picking block range", "count", cfg.BlockCount)
	blocks, err := runner.PickBlocks(ctx)
	if err != nil {
		slog.Error("pick blocks", "error", err)
		os.Exit(1)
	}
	slog.Info("block range",
		"start", blocks[0],
		"end", blocks[len(blocks)-1],
		"count", len(blocks),
		"concurrency", cfg.Concurrency,
		"rps", cfg.RequestsPerSecond,
		"range_chunk", cfg.RangeChunk,
	)

	start := time.Now()
	out := runner.Run(ctx, blocks)
	slog.Info("bench done", "duration", time.Since(start))

	csvPath := cfg.OutputPrefix + ".csv"
	jsonPath := cfg.OutputPrefix + ".json"

	if err := logbench.WriteCSV(csvPath, out.Results); err != nil {
		slog.Error("write csv", "error", err)
		os.Exit(1)
	}
	summary := logbench.BuildSummary(blocks, out.Results, logbench.DefaultAlchemyCU())
	if err := logbench.WriteJSON(jsonPath, summary); err != nil {
		slog.Error("write json", "error", err)
		os.Exit(1)
	}

	logbench.PrintComparison(summary, out.Mismatches)
	slog.Info("artifacts written", "csv", csvPath, "json", jsonPath)
}
