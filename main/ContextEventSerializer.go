package main

import "github.com/absmartly/go-sdk/main/jsonmodels"

type ContextEventSerializer interface {
	Serialize(publishEvent jsonmodels.PublishEvent) ([]byte, error)
}
