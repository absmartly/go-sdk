package sdk

import (
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type ContextDataDeserializer interface {
	Deserialize(bytes []byte) (jsonmodels.ContextData, error)
}
