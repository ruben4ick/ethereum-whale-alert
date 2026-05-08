package watcher

import (
	"context"
	"log/slog"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var decimalsSelector = crypto.Keccak256([]byte("decimals()"))[:4]

type tokenInfo struct {
	decimals uint8
	known    bool
}

func (w *Watcher) tokenDecimals(ctx context.Context, token common.Address) (uint8, bool) {
	if cached, ok := w.decimalsCache[token]; ok {
		return cached.decimals, cached.known
	}

	out, err := w.client.CallContract(ctx, token, decimalsSelector)
	if err != nil || len(out) < 32 {
		slog.Debug("decimals lookup failed", "token", token.Hex(), "err", err)
		w.decimalsCache[token] = tokenInfo{known: false}
		return 0, false
	}

	dec := out[31]
	w.decimalsCache[token] = tokenInfo{decimals: dec, known: true}
	return dec, true
}

func scaleByDecimals(amount *big.Int, decimals uint8) *big.Float {
	divisor := new(big.Float).SetInt(
		new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil),
	)
	return new(big.Float).Quo(new(big.Float).SetInt(amount), divisor)
}
