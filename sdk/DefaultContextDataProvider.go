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
