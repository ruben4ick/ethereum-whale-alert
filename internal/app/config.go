package app

import "github.com/kelseyhightower/envconfig"

type Config struct {
	GethWSURL         string  `envconfig:"GETH_WS_URL" required:"true"`
	MinThresholdETH   float64 `envconfig:"MIN_THRESHOLD_ETH" default:"0.1"`
	DiscordWebhookURL string  `envconfig:"DISCORD_WEBHOOK_URL"`
	SlackWebhookURL   string  `envconfig:"SLACK_WEBHOOK_URL"`
	MetricsPort       string  `envconfig:"METRICS_PORT" default:"2112"`
}

func (cfg *Config) Process() error {
	return envconfig.Process("", cfg)
}
