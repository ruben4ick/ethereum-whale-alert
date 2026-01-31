package app

import "github.com/kelseyhightower/envconfig"

type Config struct {
	GethWSURL string `envconfig:"GETH_WS_URL" required:"true"`
}

func (cfg *Config) Process() error {
	return envconfig.Process("", cfg)
}
