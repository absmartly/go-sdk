package sdk

import (
	context2 "context"
	"errors"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"testing"
)

type ClientDataProviderMock struct {
}

func (c ClientDataProviderMock) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return 5, nil
	})
}

func (c ClientDataProviderMock) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, nil
	})
}

func TestDefaultContextDataProvider(t *testing.T) {
	var client = ClientDataProviderMock{}
	var eventHandler = DefaultContextDataProvider{client_: client}
	var result, err = eventHandler.GetContextData().Get(context2.Background())
	assertAny(nil, err, t)
	assertAny(5, result, t)
}

type ClientDataProviderMockEx struct {
}

func (c ClientDataProviderMockEx) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, errors.New("FAILED")
	})
}

func (c ClientDataProviderMockEx) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, nil
	})
}

func TestDefaultContextDataProviderExceptionally(t *testing.T) {
	var client = ClientDataProviderMockEx{}
	var eventHandler = DefaultContextDataProvider{client_: client}
	var result, err = eventHandler.GetContextData().Get(context2.Background())
	assertAny(errors.New("FAILED"), err, t)
	assertAny(nil, result, t)
}
