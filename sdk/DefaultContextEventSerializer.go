package sdk

import (
	"encoding/json"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type DefaultContextEventSerializer struct {
	ContextEventSerializer
}

func (d DefaultContextEventSerializer) Serialize(publishEvent jsonmodels.PublishEvent) ([]byte, error) {
	return json.Marshal(publishEvent)
}
