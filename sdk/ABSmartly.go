package sdk

import (
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type ABsmartly struct {
	ContextDataProvider_ ContextDataProvider
	ContextPublisher_    ContextEventHandler
	// Deprecated: Use ContextPublisher_ instead.
	ContextEventHandler_  ContextEventHandler
	ContextEventLogger_   ContextEventLogger
	VariableParser_       VariableParser
	AudienceDeserializer_ AudienceDeserializer
	Client_               ClientI
}

type ABSmartly = ABsmartly

// clientSystemAttributes is the optional interface a client implements to
// expose the application and environment names the context injects into the
// attribute map that audiences are evaluated against. The default Client
// implements it; a custom ClientI that does not simply contributes no system
// attributes, so the public ClientI interface stays backward compatible.
type clientSystemAttributes interface {
	GetApplication() string
	GetEnvironment() string
}

func Create(config ABsmartlyConfig) ABsmartly {
	var abs = ABsmartly(config)
	if abs.ContextPublisher_ != nil {
		abs.ContextEventHandler_ = abs.ContextPublisher_
	}
	if abs.ContextDataProvider_ == nil {
		abs.ContextDataProvider_ = DefaultContextDataProvider{client_: abs.Client_}
	}

	if abs.ContextEventHandler_ == nil {
		abs.ContextEventHandler_ = DefaultContextEventHandler{client_: abs.Client_}
	}

	if abs.VariableParser_ == nil {
		abs.VariableParser_ = DefaultVariableParser{}
	}

	if abs.AudienceDeserializer_ == nil {
		abs.AudienceDeserializer_ = DefaultAudienceDeserializer{}
	}

	return abs
}

// CreateContext
func (abs ABsmartly) CreateContext(config ContextConfig) *Context {
	var cntx = CreateContext(internal.SystemClockUTC{}, config, abs.ContextDataProvider_.GetContextData(), abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_})
	abs.setSystemAttributes(cntx)
	return cntx
}

// CreateContextWith
func (abs ABsmartly) CreateContextWith(config ContextConfig, data jsonmodels.ContextData) *Context {
	var ft, done = future.New()
	done(data, nil)
	var cntx = CreateContext(internal.SystemClockUTC{}, config, ft, abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_})
	abs.setSystemAttributes(cntx)
	return cntx
}

// setSystemAttributes copies the application and environment names from the
// client onto the context, so audiences and rule conditions can target them.
// The collector adds the same values to every exposure server-side, so they
// are never published as attributes here.
func (abs ABsmartly) setSystemAttributes(cntx *Context) {
	if client, ok := abs.Client_.(clientSystemAttributes); ok {
		cntx.Application_ = client.GetApplication()
		cntx.Environment_ = client.GetEnvironment()
	}
}

func (abs ABsmartly) GetContextData() *future.Future {
	return abs.ContextDataProvider_.GetContextData()
}
