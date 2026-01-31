package app

import "github.com/kelseyhightower/envconfig"

type Config struct {
	GethWSURL       string  `envconfig:"GETH_WS_URL" required:"true"`
	MinThresholdETH float64 `envconfig:"MIN_THRESHOLD_ETH" default:"0.1"`
}

func (cfg *Config) Process() error {
	return envconfig.Process("", cfg)
}
