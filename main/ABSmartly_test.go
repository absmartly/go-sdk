package main

import (
	"context"
	"github.com/absmartly/go-sdk/main/future"
	"github.com/absmartly/go-sdk/main/jsonmodels"
	"testing"
)

var contextData = jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{{Id: 5}}}

type ClientABSMock struct {
}

func (c ClientABSMock) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return &contextData, nil
	})
}

func (c ClientABSMock) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, nil
	})
}

func TestCreateContext(t *testing.T) {
	var config = ABSmartlyConfig{Client_: ClientABSMock{}}
	var abs = Create(config)
	var contextConfig = ContextConfig{Units_: map[string]string{"user_id": "1234567"}}
	var buff [512]byte
	var block [16]int32
	var st [4]int32
	var result = abs.CreateContext(contextConfig, buff, block, st)
	assertAny(true, result.ReadyFuture_ != nil, t)
	assertAny(true, result.Cassignments_ != nil, t)
}

func TestContextWith(t *testing.T) {

	var config = ABSmartlyConfig{Client_: ClientABSMock{}}
	var abs = Create(config)
	var contextConfig = ContextConfig{Units_: map[string]string{"user_id": "1234567"}}
	var buff [512]byte
	var block [16]int32
	var st [4]int32
	var result = abs.CreateContextWith(contextConfig, contextData, buff, block, st)
	assertAny(true, result.ReadyFuture_ != nil, t)
	assertAny(true, result.Cassignments_ != nil, t)
	assertAny(map[string]string{"user_id": "1234567"}, result.Units_, t)
}

func TestGetContext(t *testing.T) {

	var config = ABSmartlyConfig{Client_: ClientABSMock{}, ContextDataProvider_: ClientABSMock{}}
	var abs = Create(config)
	var result, err = abs.GetContextData().Get(context.Background())
	assertAny(nil, err, t)
	assertAny(&contextData, result, t)
}
