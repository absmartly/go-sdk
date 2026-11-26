package sdk

import "github.com/absmartly/go-sdk/sdk/future"

type ContextDataProvider interface {
	GetContextData() *future.Future
}

// ContextPayloadProvider is an optional ContextDataProvider method set: a
// provider that also declares GetContextPayload is asked for the full
// payload, a future of jsonmodels.ContextPayload (or ContextData), when a
// context loads or refreshes its data, so holdouts and switchbacks are
// served. GetContextData keeps resolving to jsonmodels.ContextData.
// DefaultContextDataProvider and Client give the payload themselves but do
// not export GetContextPayload, so a type embedding either does not opt in
// by promotion: one that overrides only GetContextData keeps that override in
// use, and one that wants the payload declares GetContextPayload.
type ContextPayloadProvider interface {
	GetContextPayload() *future.Future
}

// contextPayloadFuture is the future a context loads from provider: the full
// payload from the default provider and client themselves (a value or
// pointer, not a type embedding them) and from a provider declaring
// GetContextPayload, and GetContextData from any other.
func contextPayloadFuture(provider ContextDataProvider) *future.Future {
	switch p := provider.(type) {
	case DefaultContextDataProvider:
		return contextPayloadFuture(p.client_)
	case *DefaultContextDataProvider:
		return contextPayloadFuture(p.client_)
	case Client:
		return p.getContextData(true)
	case *Client:
		return p.getContextData(true)
	case ContextPayloadProvider:
		return p.GetContextPayload()
	}
	return provider.GetContextData()
}
