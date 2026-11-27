package sdk

import "github.com/absmartly/go-sdk/sdk/future"

type ContextDataProvider interface {
	GetContextData() *future.Future
}
