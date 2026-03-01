package notifier

import (
	"context"
	"time"

	"ethereum-whale-alert/internal/metrics"
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
