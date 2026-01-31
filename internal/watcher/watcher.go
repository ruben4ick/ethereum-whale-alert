package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
)

type Client interface {
	SubscribeNewBlocks(ctx context.Context) (chan *types.Header, error)
	GetBlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
}

type Watcher struct {
	client       Client
	thresholdWei *big.Int
}

func New(client Client, thresholdETH float64) *Watcher {
	thresholdWei := new(big.Float).Mul(
		big.NewFloat(thresholdETH),
		big.NewFloat(1e18),
	)
	wei, _ := thresholdWei.Int(nil)

	return &Watcher{
		client:       client,
		thresholdWei: wei,
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
	block, err := w.client.GetBlockByNumber(ctx, number)
	if err != nil {
		return fmt.Errorf("get a block by number: %w", err)
	}

	for _, tx := range block.Transactions() {
		if tx.Value().Cmp(w.thresholdWei) >= 0 {
			w.logWhaleTransaction(tx, block.Number())
		}
	}

	return nil
}

func (w *Watcher) logWhaleTransaction(tx *types.Transaction, blockNumber *big.Int) {
	ethValue := new(big.Float).Quo(
		new(big.Float).SetInt(tx.Value()),
		big.NewFloat(1e18),
	)

	slog.Info("whale transaction detected",
		"tx_hash", tx.Hash().Hex(),
		"block", blockNumber,
		"value_eth", ethValue.Text('f', 4),
		"to", tx.To(),
	)
}
