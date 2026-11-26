package sdk

import (
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type ContextDataDeserializer interface {
	Deserialize(bytes []byte) (jsonmodels.ContextData, error)
}

// ContextPayloadDeserializer is an optional ContextDataDeserializer method
// set: a deserializer that also declares DeserializePayload returns the full
// payload, with holdouts and each experiment's holdout and switchback fields,
// and the client delivers that to contexts. DefaultContextDataDeserializer
// decodes the full payload itself but does not export DeserializePayload, so
// a type embedding it does not opt in by promotion: one that overrides only
// Deserialize keeps that override in use, and one that wants the payload
// declares DeserializePayload, for example with DeserializeContextPayload.
type ContextPayloadDeserializer interface {
	DeserializePayload(bytes []byte) (jsonmodels.ContextPayload, error)
}

// payloadDecoderOf is how d decodes the full payload: the default
// deserializer itself (a value or pointer, not a type embedding it), or a
// deserializer declaring DeserializePayload. It reports false for one with
// only Deserialize.
func payloadDecoderOf(d ContextDataDeserializer) (func([]byte) (jsonmodels.ContextPayload, error), bool) {
	switch d := d.(type) {
	case DefaultContextDataDeserializer, *DefaultContextDataDeserializer:
		return DeserializeContextPayload, true
	case ContextPayloadDeserializer:
		return d.DeserializePayload, true
	}
	return nil, false
}
