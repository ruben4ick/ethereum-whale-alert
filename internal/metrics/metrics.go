package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Transaction metrics
	WhaleTxTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "whale_transactions_total",
		Help: "Total number of detected whale transactions.",
	}, []string{"type"})

	WhaleTxValueETH = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "whale_transaction_value_eth",
		Help:    "Distribution of whale transaction values in ETH.",
		Buckets: []float64{100, 500, 1000, 5000, 10000},
	})

	WhaleTxPerBlock = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "whale_transactions_per_block",
		Help: "Number of whale transactions in the last processed block.",
	})

	// Block processing metrics
	BlocksProcessedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "blocks_processed_total",
		Help: "Total number of Ethereum blocks processed.",
	})

	BlockProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "block_processing_duration_seconds",
		Help:    "Time spent processing each block.",
		Buckets: prometheus.DefBuckets,
	})

	BlocksWithWhalesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "blocks_with_whales_total",
		Help: "Total number of blocks that contained at least one whale transaction.",
	})

	// Blockchain metrics
	BlockGasUsed = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ethereum_block_gas_used",
		Help: "Gas used in the last processed block.",
	})

	BlockTransactionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ethereum_block_transactions_total",
		Help: "Total number of transactions across all processed blocks.",
	})

	BlockTransactionsPerBlock = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ethereum_block_transactions_per_block",
		Help: "Number of transactions in the last processed block.",
	})

	ERC20SkippedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "erc20_tokens_skipped_total",
		Help: "Total ERC-20 transfers skipped due to unavailable price.",
	})

	// Notification metrics
	NotificationsSentTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "notifications_sent_total",
		Help: "Total number of notifications successfully sent.",
	}, []string{"channel"})

	NotificationErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "notification_errors_total",
		Help: "Total number of notification errors.",
	}, []string{"channel"})

	NotificationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "notification_duration_seconds",
		Help:    "Time spent sending a notification webhook.",
		Buckets: prometheus.DefBuckets,
	}, []string{"channel"})
)
