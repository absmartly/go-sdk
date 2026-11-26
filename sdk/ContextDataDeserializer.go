package sdk

import (
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"reflect"
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

// payloadDeserializerOf opts into the full payload only when the configured
// value did not override the legacy Deserialize hook while embedding the
// default deserializer. Such an embedding is a legacy customization, not an
// opt-in to payload dispatch.
func payloadDeserializerOf(value ContextDataDeserializer) (ContextPayloadDeserializer, bool) {
	payloads, ok := value.(ContextPayloadDeserializer)
	if !ok {
		return nil, false
	}
	if isDefaultDeserializerType(reflect.TypeOf(value)) {
		return payloads, true
	}
	if embedsType(reflect.TypeOf(value), reflect.TypeOf(DefaultContextDataDeserializer{})) {
		return payloads, false
	}
	return payloads, true
}

func isDefaultDeserializerType(t reflect.Type) bool {
	return t == reflect.TypeOf(DefaultContextDataDeserializer{}) ||
		(t.Kind() == reflect.Ptr && t.Elem() == reflect.TypeOf(DefaultContextDataDeserializer{}))
}

func embedsType(t, embedded reflect.Type) bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Anonymous && t.Field(i).Type == embedded {
			return true
		}
	}
	return false
}

func methodPointer(t reflect.Type, name string) uintptr {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	method, ok := t.MethodByName(name)
	if !ok {
		return 0
	}
	return method.Func.Pointer()
}
