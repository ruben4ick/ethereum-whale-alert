package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"ethereum-whale-alert/internal/metrics"
	"ethereum-whale-alert/internal/notifier"

	"github.com/ethereum/go-ethereum/core/types"
)

type Client interface {
	SubscribeNewBlocks(ctx context.Context) (chan *types.Header, error)
	GetBlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
}

type Watcher struct {
	client       Client
	thresholdWei *big.Int
	notifiers    []notifier.Notifier
}

func New(client Client, thresholdETH float64, notifiers ...notifier.Notifier) *Watcher {
	thresholdWei := new(big.Float).Mul(
		big.NewFloat(thresholdETH),
		big.NewFloat(1e18),
	)
	wei, _ := thresholdWei.Int(nil)

	return &Watcher{
		client:       client,
		thresholdWei: wei,
		notifiers:    notifiers,
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	headers, err := w.client.SubscribeNewBlocks(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case header := <-headers:
			if err := w.processBlock(ctx, header.Number); err != nil {
				slog.Error("failed to process block", "block", header.Number, "error", err)
			}
		}
	}
}

func (w *Watcher) processBlock(ctx context.Context, number *big.Int) error {
	start := time.Now()

	block, err := w.client.GetBlockByNumber(ctx, number)
	if err != nil {
		return fmt.Errorf("get a block by number: %w", err)
	}

	blockTime := time.Unix(int64(block.Time()), 0)

	whaleCount := 0
	for _, tx := range block.Transactions() {
		if tx.Value().Cmp(w.thresholdWei) >= 0 {
			valueETH, _ := new(big.Float).Quo(
				new(big.Float).SetInt(tx.Value()),
				big.NewFloat(1e18),
			).Float64()

			metrics.WhaleTxTotal.WithLabelValues("native_eth").Inc()
			metrics.WhaleTxValueETH.Observe(valueETH)
			whaleCount++

			event := w.buildEvent(tx, block.Number(), blockTime)
			w.notify(ctx, event)
		}
	}

	metrics.BlocksProcessedTotal.Inc()
	metrics.BlockProcessingDuration.Observe(time.Since(start).Seconds())
	metrics.WhaleTxPerBlock.Set(float64(whaleCount))

	if whaleCount > 0 {
		metrics.BlocksWithWhalesTotal.Inc()
	}

	return nil
}

func (w *Watcher) buildEvent(tx *types.Transaction, blockNumber *big.Int, blockTime time.Time) notifier.AlertEvent {
	ethValue := new(big.Float).Quo(
		new(big.Float).SetInt(tx.Value()),
		big.NewFloat(1e18),
	)

	to := "contract creation"
	if tx.To() != nil {
		to = tx.To().Hex()
	}

	return notifier.AlertEvent{
		TxHash:      tx.Hash().Hex(),
		BlockNumber: blockNumber,
		ValueETH:    ethValue.Text('f', 4),
		To:          to,
		Timestamp:   blockTime,
	}
}

func (w *Watcher) notify(ctx context.Context, event notifier.AlertEvent) {
	slog.Info("whale transaction detected",
		"tx_hash", event.TxHash,
		"block", event.BlockNumber,
		"value_eth", event.ValueETH,
		"to", event.To,
	)

	for _, n := range w.notifiers {
		if err := n.Notify(ctx, event); err != nil {
			slog.Error("failed to send notification", "error", err)
		}
	}
}
