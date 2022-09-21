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

func (abs ABSmartly) CreateContext(config ContextConfig, buff [512]byte, block [16]int32, st [4]int32) *Context {
	return CreateContext(internal.SystemClockUTC{}, config, abs.ContextDataProvider_.GetContextData(), abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_},
		buff, block, st)
}

func (abs ABSmartly) CreateContextWith(config ContextConfig, data jsonmodels.ContextData, buff [512]byte, block [16]int32, st [4]int32) *Context {
	var ft, done = future.New()
	done(&data, nil)
	return CreateContext(internal.SystemClockUTC{}, config, ft, abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_},
		buff, block, st)
}

func (abs ABSmartly) GetContextData() *future.Future {
	return abs.ContextDataProvider_.GetContextData()
}
