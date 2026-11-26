package sdk

import (
	"github.com/absmartly/go-sdk/sdk/future"
)

type DefaultContextDataProvider struct {
	ContextDataProvider
	client_ ClientI
}

func CreateDefaultContextDataProvider(client ClientI) DefaultContextDataProvider {
	return DefaultContextDataProvider{client_: client}
}

func (dfc DefaultContextDataProvider) GetContextData() *future.Future {
	return dfc.client_.GetContextData()
}

// GetContextPayload is the client's GetContextPayload when it has one, and
// its GetContextData otherwise.
func (dfc DefaultContextDataProvider) GetContextPayload() *future.Future {
	if payloads, ok := dfc.client_.(ContextPayloadProvider); ok {
		return payloads.GetContextPayload()
	}
	return dfc.client_.GetContextData()
}
