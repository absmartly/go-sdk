package sdk

import (
	context2 "context"
	"errors"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"testing"
)

type ClientMock struct {
}

func (c ClientMock) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return 5, nil
	})
}

func (c ClientMock) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, nil
	})
}

func TestContextEventHandlerPublish(t *testing.T) {
	var context = Context{}
	var client = ClientMock{}
	var event = jsonmodels.PublishEvent{}
	var eventHandler = DefaultContextEventHandler{client_: client}
	var result, err = eventHandler.Publish(context, event).Get(context2.Background())
	assertAny(nil, err, t)
	assertAny(nil, result, t)
}

type ClientMockEx struct {
}

func (c ClientMockEx) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return 5, nil
	})
}

func (c ClientMockEx) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, errors.New("FAILED")
	})
}

func TestContextEventHandlerPublishExceptionally(t *testing.T) {
	var context = Context{}
	var client = ClientMockEx{}
	var event = jsonmodels.PublishEvent{}
	var eventHandler = DefaultContextEventHandler{client_: client}
	var result, err = eventHandler.Publish(context, event).Get(context2.Background())
	assertAny(errors.New("FAILED"), err, t)
	assertAny(nil, result, t)
}
