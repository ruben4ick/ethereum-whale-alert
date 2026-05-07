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

// Run starts the WebSocket subscriber and one polling goroutine per configured
// interval, lets them collect events for cfg.Duration, and returns the
// per-method runs. All goroutines share a single rate-limited HTTP client so
// the combined RPS stays within the provider's quota.
func (r *Runner) Run(ctx context.Context) RunOutput {
	rps := r.cfg.HTTPRPS
	if rps <= 0 {
		// One poller per interval, with the shortest interval being typically 1s,
		// so a sane default budget is num_intervals × (1/shortest) plus headroom.
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
