package syncbench

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"ethereum-whale-alert/internal/logbench"
)

type Runner struct {
	cfg Config
}

func New(cfg Config) *Runner { return &Runner{cfg: cfg} }

type RunOutput struct {
	Runs       []*Run
	StartedAt  time.Time
	FinishedAt time.Time
}

func (r *Runner) Run(ctx context.Context) RunOutput {
	rps := r.cfg.HTTPRPS
	if rps <= 0 {
		rps = 25
	}
	rpc := logbench.NewRawRPC(r.cfg.HTTPURL, rps, r.cfg.MaxRetries)

	out := RunOutput{StartedAt: time.Now()}
	out.Runs = append(out.Runs, &Run{Method: MethodWebSocket})
	for _, iv := range r.cfg.PollIntervals {
		out.Runs = append(out.Runs, &Run{Method: MethodPolling, PollInterval: iv})
	}

	benchCtx, cancel := context.WithTimeout(ctx, r.cfg.Duration)
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("starting method", "method", MethodWebSocket)
		if err := RunWebSocket(benchCtx, r.cfg.WSURL, out.Runs[0]); err != nil && benchCtx.Err() == nil {
			slog.Error("ws run", "err", err)
		}
	}()

	for i, iv := range r.cfg.PollIntervals {
		i, iv := i, iv
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("starting method", "method", MethodPolling, "interval", iv)
			if err := RunPolling(benchCtx, rpc, iv, out.Runs[i+1]); err != nil && benchCtx.Err() == nil {
				slog.Error("polling run", "interval", iv, "err", err)
			}
		}()
	}

	wg.Wait()
	out.FinishedAt = time.Now()
	return out
}
