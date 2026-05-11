package main

import (
	"context"
	syncbench2 "ethereum-whale-alert/internal/benchmarks/syncbench"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	GethWSURL     string        `envconfig:"GETH_WS_URL" required:"true"`
	GethHTTPURL   string        `envconfig:"GETH_HTTP_URL" required:"true"`
	Duration      time.Duration `envconfig:"BENCH_DURATION" default:"10m"`
	PollIntervals string        `envconfig:"BENCH_POLL_INTERVALS" default:"1s,3s,6s,12s"`
	HTTPRPS       float64       `envconfig:"BENCH_RPS" default:"25"`
	MaxRetries    int           `envconfig:"BENCH_MAX_RETRIES" default:"5"`
	OutputPrefix  string        `envconfig:"BENCH_OUT" default:"./benchresults/syncbench"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		slog.Error("config error", "error", err)
		os.Exit(1)
	}

	intervals, err := parseIntervals(cfg.PollIntervals)
	if err != nil {
		slog.Error("parse poll intervals", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runner := syncbench2.New(syncbench2.Config{
		WSURL:         cfg.GethWSURL,
		HTTPURL:       cfg.GethHTTPURL,
		Duration:      cfg.Duration,
		PollIntervals: intervals,
		CU:            syncbench2.DefaultAlchemyCU(),
		HTTPRPS:       cfg.HTTPRPS,
		MaxRetries:    cfg.MaxRetries,
	})

	slog.Info("syncbench starting",
		"duration", cfg.Duration,
		"poll_intervals", intervals,
		"ws_url_host", redactHost(cfg.GethWSURL),
		"http_url_host", redactHost(cfg.GethHTTPURL),
	)

	out := runner.Run(ctx)

	csvPath := cfg.OutputPrefix + ".csv"
	jsonPath := cfg.OutputPrefix + ".json"

	if err := os.MkdirAll(filepath.Dir(cfg.OutputPrefix), 0o755); err != nil {
		slog.Error("create output dir", "error", err)
		os.Exit(1)
	}

	summary := syncbench2.BuildSummary(out, syncbench2.DefaultAlchemyCU())
	if err := syncbench2.WriteCSV(csvPath, out); err != nil {
		slog.Error("write csv", "error", err)
		os.Exit(1)
	}
	if err := syncbench2.WriteJSON(jsonPath, summary); err != nil {
		slog.Error("write json", "error", err)
		os.Exit(1)
	}

	syncbench2.PrintComparison(summary)
	slog.Info("artifacts written", "csv", csvPath, "json", jsonPath)
}

func parseIntervals(s string) ([]time.Duration, error) {
	parts := strings.Split(s, ",")
	out := make([]time.Duration, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		d, err := time.ParseDuration(p)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func redactHost(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			return u[:i+3] + rest[:j]
		}
	}
	return u
}
