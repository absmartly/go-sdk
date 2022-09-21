package sdk

import (
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type ContextEventHandler interface {
	Publish(context Context, event jsonmodels.PublishEvent) *future.Future
}
