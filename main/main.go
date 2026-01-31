package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ethereum-whale-alert/internal/app"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg := &app.Config{}
	if err := cfg.Process(); err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	slog.Info("whale alert started", "geth_url", cfg.GethWSURL)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down...")
}
