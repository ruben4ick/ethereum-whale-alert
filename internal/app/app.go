package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"ethereum-whale-alert/internal/client"
	"ethereum-whale-alert/internal/notifier"
	"ethereum-whale-alert/internal/watcher"

	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		slog.Info("metrics server listening")
		if err := http.ListenAndServe(a.cfg.MetricsPort, mux); err != nil {
			slog.Error("metrics server error", "error", err)
		}
	}()

	var notifiers []notifier.Notifier

	if a.cfg.DiscordWebhookURL != "" {
		notifiers = append(notifiers, notifier.WithMetrics("discord", notifier.NewDiscord(a.cfg.DiscordWebhookURL)))
		slog.Info("discord notifier enabled")
	}

	if a.cfg.SlackWebhookURL != "" {
		notifiers = append(notifiers, notifier.WithMetrics("slack", notifier.NewSlack(a.cfg.SlackWebhookURL)))
		slog.Info("slack notifier enabled")
	}

	w := watcher.New(ethereumClient, a.cfg.MinThresholdETH, notifiers...)

	go func() {
		if err := w.Run(ctx); err != nil {
			slog.Error("watcher error", "error", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down...")
	return nil
}
