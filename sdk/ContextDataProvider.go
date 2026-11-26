package sdk

import (
	"reflect"

	"github.com/absmartly/go-sdk/sdk/future"
)

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
	if payloads, ok := payloadProviderOf(provider); ok {
		return payloads.GetContextPayload()
	}
	return provider.GetContextData()
}

// payloadProviderOf applies the explicit-opt-in rule to a client or provider
// that embeds the default implementation and overrides GetContextData.
func payloadProviderOf(value interface{}) (ContextPayloadProvider, bool) {
	payloads, ok := value.(ContextPayloadProvider)
	if !ok {
		return nil, false
	}
	var t = reflect.TypeOf(value)
	if embedsType(t, reflect.TypeOf(DefaultContextDataProvider{})) || embedsType(t, reflect.TypeOf(Client{})) {
		return payloads, false
	}
	return payloads, true
}
