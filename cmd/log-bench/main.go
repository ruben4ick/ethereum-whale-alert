package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"ethereum-whale-alert/internal/logbench"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	GethHTTPURL string `envconfig:"GETH_HTTP_URL"`
	BlockCount  int    `envconfig:"BENCH_BLOCKS" default:"50"`
	Concurrency int    `envconfig:"BENCH_CONCURRENCY" default:"4"`
	// ThroughputCUPS is the rate-limit budget in throughput compute units per
	// second. Each call consumes its method's throughput-CU cost, so this
	// directly maps to the Alchemy CU/sec quota of your plan.
	// Defaults are set well below the Free plan limit (330 CUPS) to leave room
	// for jitter and avoid 429s.
	ThroughputCUPS float64 `envconfig:"BENCH_CUPS" default:"250"`
	MaxRetries     int     `envconfig:"BENCH_MAX_RETRIES" default:"10"`
	RangeChunk     int     `envconfig:"BENCH_RANGE_CHUNK" default:"10"`
	OutputPrefix   string  `envconfig:"BENCH_OUT" default:"./logbench-results"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		slog.Error("config error", "error", err)
		os.Exit(1)
	}

	rpc := logbench.NewRawRPC(cfg.GethHTTPURL, cfg.ThroughputCUPS, cfg.MaxRetries)

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
		"throughput_cups", cfg.ThroughputCUPS,
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
