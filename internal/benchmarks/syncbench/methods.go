package syncbench

import (
	"context"
	"encoding/json"
	"ethereum-whale-alert/internal/benchmarks/logbench"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

func RunWebSocket(ctx context.Context, wsURL string, run *Run) error {
	client, err := ethclient.DialContext(ctx, wsURL)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer client.Close()

	headers := make(chan *types.Header, 64)
	sub, err := client.SubscribeNewHead(ctx, headers)
	if err != nil {
		return fmt.Errorf("subscribe newHeads: %w", err)
	}
	defer sub.Unsubscribe()

	seen := make(map[uint64]struct{})
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-sub.Err():
			if err == nil {
				return nil
			}
			return fmt.Errorf("subscription: %w", err)
		case h := <-headers:
			now := time.Now()
			if h == nil {
				continue
			}
			n := h.Number.Uint64()
			if _, dup := seen[n]; dup {
				continue
			}
			seen[n] = struct{}{}
			run.addEvent(BlockEvent{
				BlockNumber:    n,
				ObservedAt:     now,
				BlockTimestamp: time.Unix(int64(h.Time), 0),
			})
			run.addStats(1, 0, 0)
		}
	}
}

func RunPolling(ctx context.Context, rpc *logbench.RawRPC, interval time.Duration, run *Run) error {
	var lastSeen uint64
	first := true

	tick := func() {
		resp, result, err := rpc.Call(ctx, "eth_blockNumber", []any{}, 10)
		now := time.Now()
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("poll error", "interval", interval, "err", err)
			}
			return
		}
		run.addStats(1, resp.BytesIn, resp.BytesOut)

		var hex string
		if err := json.Unmarshal(result, &hex); err != nil {
			return
		}
		n, err := parseHexUint(hex)
		if err != nil {
			return
		}

		if first {
			lastSeen = n
			first = false
			return
		}
		if n <= lastSeen {
			return
		}
		for b := lastSeen + 1; b <= n; b++ {
			run.addEvent(BlockEvent{BlockNumber: b, ObservedAt: now})
		}
		lastSeen = n
	}

	tick()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick()
		}
	}
}

func parseHexUint(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
}
