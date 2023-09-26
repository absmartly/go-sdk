package sdk

import (
	"github.com/go-resty/resty/v2"
	"time"
)

type DefaultHttpClientConfig struct {
	ConnectTimeout_           time.Duration
	ConnectionKeepAlive_      time.Duration
	RetryInterval_            time.Duration
	ConnectionRequestTimeout_ time.Duration
	MaxRetries_               int
	MaxConnectionsPerHost_    int
	Logger                    resty.Logger
}

func CreateDefaultHttpClientConfig() DefaultHttpClientConfig {
	var config = DefaultHttpClientConfig{
		ConnectTimeout_:           3000 * time.Millisecond,
		ConnectionKeepAlive_:      30000 * time.Millisecond,
		RetryInterval_:            333 * time.Millisecond,
		ConnectionRequestTimeout_: 1000 * time.Millisecond,
		MaxRetries_:               5,
		MaxConnectionsPerHost_:    500,
		Logger:                    Logger{},
	}
	return config
}
