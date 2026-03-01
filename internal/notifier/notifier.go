package notifier

import (
	"context"
	"math/big"
	"time"
)

type AlertEvent struct {
	TxHash      string
	BlockNumber *big.Int
	ValueETH    string
	To          string
	Timestamp   time.Time
}

type Notifier interface {
	Notify(ctx context.Context, event AlertEvent) error
}
