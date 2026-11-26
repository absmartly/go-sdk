package sdk

import (
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type ABSmartly struct {
	ContextDataProvider_  ContextDataProvider
	ContextEventHandler_  ContextEventHandler
	ContextEventLogger_   ContextEventLogger
	VariableParser_       VariableParser
	AudienceDeserializer_ AudienceDeserializer
	Client_               ClientI
}

// ABsmartly is an alias of ABSmartly, spelled as the product is.
type ABsmartly = ABSmartly

// clientSystemAttributes is the optional interface a client implements to
// expose the application and environment names the context injects into the
// attribute map that audiences are evaluated against. The default Client
// implements it; a custom ClientI that does not simply contributes no system
// attributes, so the public ClientI interface stays backward compatible.
type clientSystemAttributes interface {
	GetApplication() string
	GetEnvironment() string
}

func Create(config ABSmartlyConfig) ABSmartly {
	var abs = ABSmartly(config)
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
func (abs ABSmartly) CreateContext(config ContextConfig) *Context {
	var application, environment = abs.systemAttributes()
	return createContext(internal.SystemClockUTC{}, config, contextPayloadFuture(abs.ContextDataProvider_), abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_},
		application, environment)
}

// CreateContextWith
func (abs ABSmartly) CreateContextWith(config ContextConfig, data jsonmodels.ContextData) *Context {
	var application, environment = abs.systemAttributes()
	var ft, done = future.New()
	done(data, nil)
	return createContext(internal.SystemClockUTC{}, config, ft, abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_},
		application, environment)
}

// CreateContextWithPayload is CreateContextWith for a full context payload,
// with holdouts and each experiment's holdout and switchback fields.
func (abs ABSmartly) CreateContextWithPayload(config ContextConfig, data jsonmodels.ContextPayload) *Context {
	var application, environment = abs.systemAttributes()
	var ft, done = future.New()
	done(data, nil)
	return createContext(internal.SystemClockUTC{}, config, ft, abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_},
		application, environment)
}

// systemAttributes returns the client's application and environment names,
// which the context evaluates audiences and assignment rules with. They are
// passed to createContext so the context has them before the data future
// delivers its Ready callback: CreateContextWith settles that future first, so
// the callback fires during construction, and a Ready logger that evaluates an
// audience must already see them. The collector adds the same values to every
// exposure server-side, so they are never published as attributes here.
func (abs ABSmartly) systemAttributes() (string, string) {
	if client, ok := abs.Client_.(clientSystemAttributes); ok {
		return client.GetApplication(), client.GetEnvironment()
	}
	return "", ""
}

func (abs ABSmartly) GetContextData() *future.Future {
	return abs.ContextDataProvider_.GetContextData()
}
