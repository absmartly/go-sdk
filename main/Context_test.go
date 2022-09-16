package main

import (
	"github.com/absmartly/go-sdk/main/future"
	"github.com/absmartly/go-sdk/main/internal"
	"github.com/absmartly/go-sdk/main/jsonmodels"
	"io/ioutil"
	"testing"
)

type ClientContextMock struct {
}

func (c ClientContextMock) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return 5, nil
	})
}

func (c ClientContextMock) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, nil
	})
}

var units = map[string]string{
	"session_id": "e791e240fcd3df7d238cfc285f475e8152fcc0ec",
	"user_id":    "123456789",
	"email":      "bleh@absmartly.com"}

var deser = DefaultContextDataDeserializer{}

var data jsonmodels.ContextData
var refreshData jsonmodels.ContextData
var audienceData jsonmodels.ContextData
var audienceStrictData jsonmodels.ContextData

var dataFutureReady *future.Future
var clock internal.Clock
var dataProvider ContextDataProvider
var eventHandler ContextEventHandler
var eventLogger ContextEventLogger
var variableParser DefaultVariableParser
var audienceMatcher AudienceMatcher

func setUp() {
	content, _ := ioutil.ReadFile("../context.json")
	data, _ = deser.Deserialize(content)
	dataFutureReady, _ = future.New()
	clock = internal.FixedClock{Millis_: 1_620_000_000_000}
	var client = ClientDataProviderMock{}
	dataProvider = DefaultContextDataProvider{client_: client}

}

func CreateTestContext(config ContextConfig, dataFuture *future.Future) Context {
	var buff [512]byte
	var block [16]int32
	var st [4]int32
	return CreateContext(clock, config, dataFuture, dataProvider, eventHandler, eventLogger, variableParser, audienceMatcher,
		buff, block, st)
}

func TestConstructorSetsOverrides(t *testing.T) {
	setUp()
	var overrides = map[string]int{"exp_test": 2, "exp_test_1": 1}
	var config = ContextConfig{}
	config.Units_ = units
	config.Overrides_ = overrides

	var context = CreateTestContext(config, dataFutureReady)
	for key, value := range overrides {
		assertAny(value, context.GetOverride(key), t)
	}
}

func TestConstructorSetsCustomAssignments(t *testing.T) {
	setUp()
	var cassignments = map[string]int{"exp_test": 2, "exp_test_1": 1}
	var config = ContextConfig{}
	config.Units_ = units
	config.Cassigmnents_ = cassignments

	var context = CreateTestContext(config, dataFutureReady)
	for key, value := range cassignments {
		assertAny(value, context.GetCustomAssignment(key), t)
	}
}
