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

	if abs.AudienceDeserializer_ == nil {
		abs.AudienceDeserializer_ = DefaultAudienceDeserializer{}
	}

	return abs
}

// CreateContext
//buff should be 512 bytes
//block should be 16 bytes
//st should be 4 bytes
//assignBuf should be 12 bytes
func (abs ABSmartly) CreateContext(config ContextConfig) *Context {
	return CreateContext(internal.SystemClockUTC{}, config, abs.ContextDataProvider_.GetContextData(), abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_})
}

// CreateContextWith
//buff should be 512 bytes
//block should be 16 bytes
//st should be 4 bytes
//assignBuf should be 12 bytes
func (abs ABSmartly) CreateContextWith(config ContextConfig, data jsonmodels.ContextData) *Context {
	var ft, done = future.New()
	done(data, nil)
	return CreateContext(internal.SystemClockUTC{}, config, ft, abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_})
}

func (abs ABSmartly) GetContextData() *future.Future {
	return abs.ContextDataProvider_.GetContextData()
}
