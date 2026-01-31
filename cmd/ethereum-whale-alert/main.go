package main

import (
	"context"
	"log/slog"
	"os"

	"ethereum-whale-alert/internal/app"
)

func main() {
	if err := app.New(&app.Config{}).Run(context.Background()); err != nil {
		slog.Error("application error", "error", err)
		os.Exit(1)
	}
}
