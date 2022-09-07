package main

import (
	"github.com/absmartly/go-sdk/main/jsonmodels"
)

type ContextDataDeserializer interface {
	Deserialize(bytes []byte) (jsonmodels.ContextData, error)
}
