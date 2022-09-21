package sdk

import (
	"encoding/json"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type DefaultContextDataDeserializer struct {
	ContextDataDeserializer
}

func (d DefaultContextDataDeserializer) Deserialize(bytes []byte) (jsonmodels.ContextData, error) {
	var data jsonmodels.ContextData
	if err := json.Unmarshal(bytes, &data); err != nil {
		return data, err
	} else {
		return data, err
	}
}
