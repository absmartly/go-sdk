package main

import (
	"github.com/absmartly/go-sdk/main/future"
	"github.com/absmartly/go-sdk/main/jsonmodels"
)

type ContextEventHandler interface {
	Publish(context Context, event jsonmodels.PublishEvent) *future.Future
}
