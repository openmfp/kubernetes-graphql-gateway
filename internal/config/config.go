package config

import (
	"github.com/vrischmann/envconfig"
)

type Config struct {
	Port       string `envconfig:"default=8080,optional"`
	LogLevel   string `envconfig:"default=INFO,optional"`
	WatchedDir string `envconfig:"default=definitions,required"`
}

// NewFromEnv creates a Config from environment values
func NewFromEnv() (Config, error) {
	appConfig := Config{}
	err := envconfig.Init(&appConfig)
	return appConfig, err
}
