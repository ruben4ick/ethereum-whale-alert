package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"ethereum-whale-alert/internal/metrics"
	"ethereum-whale-alert/internal/notifier"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var transferEventSig = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

type PriceFetcher interface {
	PriceInETH(ctx context.Context, tokenAddress string) (float64, bool)
}

type Client interface {
	SubscribeNewBlocks(ctx context.Context) (chan *types.Header, error)
	GetBlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
	GetTransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
}

type Config struct {
	ThresholdETH float64
	WatchERC20   bool
}

type Watcher struct {
	client       Client
	thresholdWei *big.Int
	thresholdETH float64
	notifiers    []notifier.Notifier
	watchERC20   bool
	priceFetcher PriceFetcher
}

func New(client Client, cfg Config, pf PriceFetcher, notifiers ...notifier.Notifier) *Watcher {
	thresholdWei := new(big.Float).Mul(
		big.NewFloat(cfg.ThresholdETH),
		big.NewFloat(1e18),
	)
	wei, _ := thresholdWei.Int(nil)

	return &Watcher{
		client:       client,
		thresholdWei: wei,
		thresholdETH: cfg.ThresholdETH,
		notifiers:    notifiers,
		watchERC20:   cfg.WatchERC20,
		priceFetcher: pf,
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
		// Native ETH whale transfers.
		if tx.Value().Cmp(w.thresholdWei) >= 0 {
			whaleCount++
			event := w.buildNativeEvent(tx, block.Number(), blockTime)
			metrics.WhaleTxTotal.WithLabelValues("native_eth").Inc()
			metrics.WhaleTxValueETH.Observe(ethValue(tx.Value()))
			w.notify(ctx, event)
		}

		// ERC-20 Transfer events.
		if w.watchERC20 {
			events := w.checkERC20Transfer(ctx, tx, block.Number(), blockTime)
			for _, event := range events {
				whaleCount++
				w.notify(ctx, event)
			}
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

func (w *Watcher) checkERC20Transfer(ctx context.Context, tx *types.Transaction, blockNumber *big.Int, blockTime time.Time) []notifier.AlertEvent {
	receipt, err := w.client.GetTransactionReceipt(ctx, tx.Hash())
	if err != nil {
		slog.Error("failed to get receipt", "tx", tx.Hash().Hex(), "error", err)
		return nil
	}

	var events []notifier.AlertEvent
	for _, log := range receipt.Logs {
		if len(log.Topics) != 3 || log.Topics[0] != transferEventSig {
			continue
		}

		value := new(big.Int).SetBytes(log.Data)
		from := common.BytesToAddress(log.Topics[1].Bytes())
		to := common.BytesToAddress(log.Topics[2].Bytes())

		tokenAddr := log.Address.Hex()

		valueFloat := new(big.Float).SetInt(value)
		displayValue := new(big.Float).Quo(valueFloat, big.NewFloat(1e18))
		tokenAmount, _ := displayValue.Float64()

		tokenPriceETH, ok := w.priceFetcher.PriceInETH(ctx, tokenAddr)
		if !ok {
			slog.Debug("skipping erc20 transfer: price unavailable", "token", tokenAddr)
			continue
		}

		valueInETH := tokenAmount * tokenPriceETH
		if valueInETH < w.thresholdETH {
			continue
		}

		metrics.WhaleTxTotal.WithLabelValues(string(notifier.TypeERC20)).Inc()
		metrics.WhaleTxValueETH.Observe(valueInETH)

		events = append(events, notifier.AlertEvent{
			TxHash:      tx.Hash().Hex(),
			BlockNumber: blockNumber,
			ValueETH:    fmt.Sprintf("%.4f", valueInETH),
			From:        from.Hex(),
			To:          to.Hex(),
			Type:        notifier.TypeERC20,
			Token:       tokenAddr,
			TokenAmount: displayValue.Text('f', 4),
			Timestamp:   blockTime,
		})
	}
	return events
}

func (w *Watcher) buildNativeEvent(tx *types.Transaction, blockNumber *big.Int, blockTime time.Time) notifier.AlertEvent {
	ethVal := new(big.Float).Quo(
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
		ValueETH:    ethVal.Text('f', 4),
		To:          to,
		Type:        notifier.TypeNativeETH,
		Timestamp:   blockTime,
	}
}

func (w *Watcher) notify(ctx context.Context, event notifier.AlertEvent) {
	slog.Info("whale transaction detected",
		"type", event.Type,
		"tx_hash", event.TxHash,
		"block", event.BlockNumber,
		"value", event.ValueETH,
		"from", event.From,
		"to", event.To,
		"token", event.Token,
	)

	for _, n := range w.notifiers {
		if err := n.Notify(ctx, event); err != nil {
			slog.Error("failed to send notification", "error", err)
		}
	}
}

func ethValue(wei *big.Int) float64 {
	v, _ := new(big.Float).Quo(
		new(big.Float).SetInt(wei),
		big.NewFloat(1e18),
	).Float64()
	return v
}
