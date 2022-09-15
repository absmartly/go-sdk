package main

import (
	"github.com/absmartly/go-sdk/main/future"
	"github.com/absmartly/go-sdk/main/jsonmodels"
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

func (abs ABSmartly) CreateContext(config ContextConfig) Context {
	return CreateContext(config, abs.ContextDataProvider_.GetContextData(), abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_})
}

func (abs ABSmartly) CreateContextWith(config ContextConfig, data jsonmodels.ContextData) Context {
	var future = future.Call(func() (future.Value, error) {
		return &data, nil
	})
	return CreateContext(config, future, abs.ContextDataProvider_,
		abs.ContextEventHandler_, abs.ContextEventLogger_, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_})
}

func (abs ABSmartly) GetContextData() *future.Future {
	return abs.ContextDataProvider_.GetContextData()
}
