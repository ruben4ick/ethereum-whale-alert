package app

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ethereum-whale-alert/internal/client"
)

type App struct {
	cfg *Config
}

func New(cfg *Config) *App {
	return &App{cfg: cfg}
}

func (a *App) Run(ctx context.Context) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := a.cfg.Process(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	slog.Info("whale alert started", "geth_url", a.cfg.GethWSURL)

	ethereumClient, err := client.NewEthereumClient(ctx, a.cfg.GethWSURL)
	if err != nil {
		return err
	}
	defer ethereumClient.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down...")
	return nil
}
