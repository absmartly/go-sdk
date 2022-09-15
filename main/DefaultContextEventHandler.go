package main

import (
	"github.com/absmartly/go-sdk/main/future"
	"github.com/absmartly/go-sdk/main/jsonmodels"
)

type DefaultContextEventHandler struct {
	ContextEventHandler
	client_ ClientI
}

func CreateDefaultContextEventHandler(client ClientI) DefaultContextEventHandler {
	return DefaultContextEventHandler{client_: client}
}

func (dfc DefaultContextEventHandler) Publish(context Context, event jsonmodels.PublishEvent) *future.Future {
	return dfc.client_.Publish(event)
}
