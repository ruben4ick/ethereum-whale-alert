package notifier

import (
	"context"
	"math/big"
	"time"

	"ethereum-whale-alert/internal/metrics"
)

type Notifier interface {
	Notify(ctx context.Context, event AlertEvent) error
}

type AlertEvent struct {
	TxHash      string
	BlockNumber *big.Int
	BlockHash   string
	ValueETH    string
	From        string
	To          string
	Type        Type
	Token       string
	TokenAmount string // raw token amount (e.g. "1000000.0000")
	Timestamp   time.Time
	Status      Status
}

type Type string

const (
	TypeNativeETH Type = "native_eth"
	TypeERC20     Type = "erc20"
)

type Status string

const (
	StatusDetected Status = "detected"
	StatusReorged  Status = "reorged"
)

type metricsNotifier struct {
	channel string
	inner   Notifier
}

func WithMetrics(channel string, n Notifier) Notifier {
	return &metricsNotifier{channel: channel, inner: n}
}

func (m *metricsNotifier) Notify(ctx context.Context, event AlertEvent) error {
	start := time.Now()
	err := m.inner.Notify(ctx, event)
	metrics.NotificationDuration.WithLabelValues(m.channel).Observe(time.Since(start).Seconds())

	if err != nil {
		metrics.NotificationErrorsTotal.WithLabelValues(m.channel).Inc()
	} else {
		metrics.NotificationsSentTotal.WithLabelValues(m.channel).Inc()
	}
	return err
}
