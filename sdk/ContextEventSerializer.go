package sdk

import "github.com/absmartly/go-sdk/sdk/jsonmodels"

type ContextEventSerializer interface {
	Serialize(publishEvent jsonmodels.PublishEvent) ([]byte, error)
}
