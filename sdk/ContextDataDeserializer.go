package sdk

import (
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type ContextDataDeserializer interface {
	Deserialize(bytes []byte) (jsonmodels.ContextData, error)
}

// ContextPayloadDeserializer is an optional ContextDataDeserializer method
// set: a deserializer that also implements DeserializePayload returns the
// full payload, with holdouts and each experiment's holdout and switchback
// fields, and the client delivers that to contexts. DefaultContextDataDeserializer
// implements it.
type ContextPayloadDeserializer interface {
	DeserializePayload(bytes []byte) (jsonmodels.ContextPayload, error)
}
