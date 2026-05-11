package main

import (
	"context"
	logbench2 "ethereum-whale-alert/internal/benchmarks/logbench"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	GethHTTPURL    string  `envconfig:"GETH_HTTP_URL"`
	BlockCount     int     `envconfig:"BENCH_BLOCKS" default:"50"`
	Concurrency    int     `envconfig:"BENCH_CONCURRENCY" default:"4"`
	ThroughputCUPS float64 `envconfig:"BENCH_CUPS" default:"250"`
	MaxRetries     int     `envconfig:"BENCH_MAX_RETRIES" default:"10"`
	RangeChunk     int     `envconfig:"BENCH_RANGE_CHUNK" default:"10"`
	OutputPrefix   string  `envconfig:"BENCH_OUT" default:"./benchresults/logbench"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		slog.Error("config error", "error", err)
		os.Exit(1)
	}

	rpc := logbench2.NewRawRPC(cfg.GethHTTPURL, cfg.ThroughputCUPS, cfg.MaxRetries)

	runner := logbench2.New(rpc, logbench2.Config{
		BlockCount:  cfg.BlockCount,
		Concurrency: cfg.Concurrency,
		RangeChunk:  cfg.RangeChunk,
		CU:          logbench2.DefaultAlchemyCU(),
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
		"throughput_cups", cfg.ThroughputCUPS,
		"range_chunk", cfg.RangeChunk,
	)

	start := time.Now()
	out := runner.Run(ctx, blocks)
	slog.Info("bench done", "duration", time.Since(start))

	csvPath := cfg.OutputPrefix + ".csv"
	jsonPath := cfg.OutputPrefix + ".json"

	if err := os.MkdirAll(filepath.Dir(cfg.OutputPrefix), 0o755); err != nil {
		slog.Error("create output dir", "error", err)
		os.Exit(1)
	}

	if err := logbench2.WriteCSV(csvPath, out.Results); err != nil {
		slog.Error("write csv", "error", err)
		os.Exit(1)
	}
	summary := logbench2.BuildSummary(blocks, out.Results, logbench2.DefaultAlchemyCU())
	if err := logbench2.WriteJSON(jsonPath, summary); err != nil {
		slog.Error("write json", "error", err)
		os.Exit(1)
	}

	logbench2.PrintComparison(summary, out.Mismatches)
	slog.Info("artifacts written", "csv", csvPath, "json", jsonPath)
}
