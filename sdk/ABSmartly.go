package sdk

import (
	"context"
	"time"

	"github.com/absmartly/go-sdk/pkg/absmartly"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type ABSmartly struct {
	sdk absmartly.SDK
}

// Create the SDK
// Deprecated: switch to v2 SDK absmartly.Create
func Create(config ABSmartlyConfig) ABSmartly {
	cfgOld := config.Client_.GetConfig()
	cfg := absmartly.Config{
		Endpoint:    cfgOld.Endpoint_,
		ApiKey:      cfgOld.ApiKey_,
		Application: cfgOld.Application_,
		Environment: cfgOld.Environment_,
		// These parameters are controlled individually by Context in Old SDK
		// Set to some defaults
		BatchSize:       100,
		BatchInterval:   1 * time.Second,
		RefreshInterval: 30 * time.Second,
	}
	sdk := absmartly.Safe(absmartly.New(context.Background(), cfg))
	abs := ABSmartly{
		sdk: sdk,
	}

	return abs
}

// CreateContext
// Deprecated: switch to v2 SDK absmartly.Create
func (abs ABSmartly) CreateContext(config ContextConfig) *Context {
	uc := abs.sdk.UnitContext(config.Units_)
	return &Context{
		uc: uc,
	}
}

// CreateContextWith
// Deprecated: switch to v2 SDK absmartly.Create, file reading is not yet implemented in v2 SDK.
func (abs ABSmartly) CreateContextWith(config ContextConfig, data jsonmodels.ContextData) *Context {
	return abs.CreateContext(config)
}
