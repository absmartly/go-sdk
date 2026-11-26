package sdk

import "github.com/absmartly/go-sdk/sdk/future"

type ContextDataProvider interface {
	GetContextData() *future.Future
}

// ContextPayloadProvider is an optional ContextDataProvider method set: a
// provider that also implements GetContextPayload is asked for the full
// payload, a future of jsonmodels.ContextPayload (or ContextData), when a
// context loads or refreshes its data, so holdouts and switchbacks are
// served. GetContextData keeps resolving to jsonmodels.ContextData.
// DefaultContextDataProvider implements it.
type ContextPayloadProvider interface {
	GetContextPayload() *future.Future
}

// contextPayloadFuture asks provider for the full payload when it can give
// it, and for the ContextData otherwise.
func contextPayloadFuture(provider ContextDataProvider) *future.Future {
	if payloads, ok := provider.(ContextPayloadProvider); ok {
		return payloads.GetContextPayload()
	}
	return provider.GetContextData()
}
