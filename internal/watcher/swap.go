package watcher

import (
	"context"
	"errors"
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

var swapEventSig = crypto.Keccak256Hash([]byte("Swap(address,uint256,uint256,uint256,uint256,address)"))

var (
	token0Selector = crypto.Keccak256([]byte("token0()"))[:4]
	token1Selector = crypto.Keccak256([]byte("token1()"))[:4]
)

type poolPair struct {
	token0 common.Address
	token1 common.Address
	known  bool
}

func (w *Watcher) checkSwaps(ctx context.Context, tx *types.Transaction, blockNumber *big.Int, blockHash common.Hash, blockTime time.Time) []notifier.AlertEvent {
	receipt, err := w.client.GetTransactionReceipt(ctx, tx.Hash())
	if err != nil || receipt == nil {
		return nil
	}

	var events []notifier.AlertEvent
	for _, log := range receipt.Logs {
		if len(log.Topics) != 3 || log.Topics[0] != swapEventSig {
			continue
		}
		if len(log.Data) != 128 {
			continue // not a Uniswap V2 Swap (4 × uint256 = 128 bytes)
		}

		amount0In := new(big.Int).SetBytes(log.Data[0:32])
		amount1In := new(big.Int).SetBytes(log.Data[32:64])
		amount0Out := new(big.Int).SetBytes(log.Data[64:96])
		amount1Out := new(big.Int).SetBytes(log.Data[96:128])

		recipient := common.BytesToAddress(log.Topics[2].Bytes())
		pool := log.Address

		pair, ok := w.resolvePool(ctx, pool)
		if !ok {
			continue
		}

		var tokenIn, tokenOut common.Address
		var amountIn, amountOut *big.Int
		switch {
		case amount0In.Sign() > 0 && amount1Out.Sign() > 0:
			tokenIn, amountIn = pair.token0, amount0In
			tokenOut, amountOut = pair.token1, amount1Out
		case amount1In.Sign() > 0 && amount0Out.Sign() > 0:
			tokenIn, amountIn = pair.token1, amount1In
			tokenOut, amountOut = pair.token0, amount0Out
		default:
			continue
		}

		valueInETH, ok := w.swapValueETH(ctx, tokenIn, amountIn)
		if !ok {
			metrics.ERC20SkippedTotal.Inc()
			continue
		}
		if valueInETH < w.thresholdETH {
			continue
		}

		amountInLabel := formatTokenAmount(amountIn)
		amountOutLabel := formatTokenAmount(amountOut)
		summary := fmt.Sprintf("%s %s → %s %s",
			amountInLabel, shortAddr(tokenIn),
			amountOutLabel, shortAddr(tokenOut),
		)

		metrics.WhaleTxTotal.WithLabelValues(string(notifier.TypeSwap)).Inc()
		metrics.WhaleTxValueETH.Observe(valueInETH)

		events = append(events, notifier.AlertEvent{
			TxHash:      tx.Hash().Hex(),
			BlockNumber: blockNumber,
			BlockHash:   blockHash.Hex(),
			ValueETH:    fmt.Sprintf("%.4f", valueInETH),
			From:        pool.Hex(),
			To:          recipient.Hex(),
			Type:        notifier.TypeSwap,
			Token:       tokenIn.Hex(),
			TokenAmount: summary,
			Timestamp:   blockTime,
			Status:      notifier.StatusDetected,
		})
	}
	return events
}

// swapValueETH computes the ETH-equivalent value of `amount` of `token`
// using the cached price feed. Token amount is assumed to use 18 decimals
// — for high-precision USD computation a per-token decimals lookup would
// be needed, but for whale-threshold filtering this approximation is fine.
func (w *Watcher) swapValueETH(ctx context.Context, token common.Address, amount *big.Int) (float64, bool) {
	priceETH, ok := w.priceFetcher.PriceInETH(ctx, token.Hex())
	if !ok {
		return 0, false
	}

	amountFloat := new(big.Float).SetInt(amount)
	scaled, _ := new(big.Float).Quo(amountFloat, big.NewFloat(1e18)).Float64()
	return scaled * priceETH, true
}

func (w *Watcher) resolvePool(ctx context.Context, pool common.Address) (poolPair, bool) {
	if cached, ok := w.poolCache[pool]; ok {
		return cached, cached.known
	}

	token0, err0 := w.callAddress(ctx, pool, token0Selector)
	token1, err1 := w.callAddress(ctx, pool, token1Selector)
	if err0 != nil || err1 != nil {
		slog.Debug("pool token resolve failed", "pool", pool.Hex(), "err0", err0, "err1", err1)
		w.poolCache[pool] = poolPair{known: false}
		return poolPair{}, false
	}

	pair := poolPair{token0: token0, token1: token1, known: true}
	w.poolCache[pool] = pair
	return pair, true
}

func (w *Watcher) callAddress(ctx context.Context, to common.Address, selector []byte) (common.Address, error) {
	out, err := w.client.CallContract(ctx, to, selector)
	if err != nil {
		return common.Address{}, err
	}
	if len(out) < 32 {
		return common.Address{}, errors.New("short response")
	}
	return common.BytesToAddress(out[12:32]), nil
}

func formatTokenAmount(amount *big.Int) string {
	scaled := new(big.Float).Quo(new(big.Float).SetInt(amount), big.NewFloat(1e18))
	return scaled.Text('f', 4)
}

func shortAddr(a common.Address) string {
	hex := a.Hex()
	if len(hex) < 10 {
		return hex
	}
	return hex[:6] + "…" + hex[len(hex)-4:]
}
