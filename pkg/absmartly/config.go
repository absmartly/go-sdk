package absmartly

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Endpoint    string
	ApiKey      string
	Application string
	Environment string

	BatchSize     uint
	BatchInterval time.Duration

	RefreshInterval time.Duration
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Endpoint:    os.Getenv("ABSMARTLY_ENDPOINT"),
		ApiKey:      os.Getenv("ABSMARTLY_APIKEY"),
		Application: os.Getenv("ABSMARTLY_APPLICATION"),
		Environment: os.Getenv("ABSMARTLY_ENVIRONMENT"),
	}
	if cfg.Endpoint == "" {
		return Config{}, fmt.Errorf("env ABSMARTLY_ENDPOINT is unset or empty")
	}
	if cfg.ApiKey == "" {
		return Config{}, fmt.Errorf("env ABSMARTLY_APIKEY is unset or empty")
	}
	if cfg.Application == "" {
		return Config{}, fmt.Errorf("env ABSMARTLY_APPLICATION is unset or empty")
	}
	if cfg.Environment == "" {
		return Config{}, fmt.Errorf("env ABSMARTLY_ENVIRONMENT is unset or empty")
	}

	return cfg, nil
}

func ConfigFromEnvMust() Config {
	cfg, err := ConfigFromEnv()
	if err != nil {
		panic(err)
	}

	return cfg
}
