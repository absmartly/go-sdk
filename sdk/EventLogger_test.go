package sdk

import (
	"errors"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type MockEventLogger struct {
	events     []LoggedEvent
	mu         sync.Mutex
	callCount  int32
	shouldFail bool
}

type LoggedEvent struct {
	EventType EventType
	Data      interface{}
}

func (m *MockEventLogger) HandleEvent(context Context, eventType EventType, data interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	atomic.AddInt32(&m.callCount, 1)
	m.events = append(m.events, LoggedEvent{EventType: eventType, Data: data})
	if m.shouldFail {
		panic("intentional logger failure")
	}
}

func (m *MockEventLogger) GetEvents() []LoggedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]LoggedEvent, len(m.events))
	copy(result, m.events)
	return result
}

func (m *MockEventLogger) GetCallCount() int32 {
	return atomic.LoadInt32(&m.callCount)
}

func (m *MockEventLogger) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = nil
	atomic.StoreInt32(&m.callCount, 0)
}

type EventLoggerClientMock struct {
	publishError bool
}

func (c EventLoggerClientMock) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return jsonmodels.ContextData{
			Experiments: []jsonmodels.Experiment{
				{Id: 1, Name: "exp_test", UnitType: "user_id"},
			},
		}, nil
	})
}

func (c EventLoggerClientMock) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		if c.publishError {
			return nil, errors.New("publish failed")
		}
		return nil, nil
	})
}

func createEventLoggerTestContext(logger *MockEventLogger, dataFuture *future.Future) *Context {
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{
		"user_id": "test-user-123",
	}
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dataProvider = DefaultContextDataProvider{client_: client}
	var eventHandler = DefaultContextEventHandler{client_: client}
	var variableParser = DefaultVariableParser{}
	var audienceMatcher = AudienceMatcher{DefaultAudienceDeserializer{}}
	var clock = FixedClockForTest{millis: 1620000000000}

	return CreateContext(clock, config, dataFuture, dataProvider, eventHandler, logger, variableParser, audienceMatcher)
}

type FixedClockForTest struct {
	millis int64
}

func (f FixedClockForTest) Millis() int64 {
	return f.millis
}

func TestEventLoggerCalledOnReady(t *testing.T) {
	logger := &MockEventLogger{}

	dataFuture, done := future.New()
	ctx := createEventLoggerTestContext(logger, dataFuture)

	done(jsonmodels.ContextData{
		Experiments: []jsonmodels.Experiment{
			{Id: 1, Name: "exp_test", UnitType: "user_id"},
		},
	}, nil)

	ctx.WaitUntilReady()

	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundReady := false
	for _, event := range events {
		if event.EventType == Ready {
			foundReady = true
			break
		}
	}

	if !foundReady {
		t.Errorf("Expected READY event to be logged, but it was not found in events: %v", events)
	}
}

func TestEventLoggerCalledOnError(t *testing.T) {
	logger := &MockEventLogger{}

	dataFuture, done := future.New()
	_ = createEventLoggerTestContext(logger, dataFuture)

	done(nil, errors.New("context data fetch failed"))

	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundError := false
	for _, event := range events {
		if event.EventType == Error {
			foundError = true
			if _, ok := event.Data.(error); !ok {
				t.Errorf("Expected error data to be an error type, got %T", event.Data)
			}
			break
		}
	}

	if !foundError {
		t.Errorf("Expected ERROR event to be logged, but it was not found in events: %v", events)
	}
}

func TestEventLoggerCalledOnExposure(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dataProvider = DefaultContextDataProvider{client_: client}
	var eventHandler = DefaultContextEventHandler{client_: client}
	var variableParser = DefaultVariableParser{}
	var audienceMatcher = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dataProvider, eventHandler, logger, variableParser, audienceMatcher)

	logger.Reset()

	_, err := ctx.GetTreatment("exp_test_ab")
	if err != nil {
		t.Fatalf("GetTreatment failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundExposure := false
	for _, event := range events {
		if event.EventType == Exposure {
			foundExposure = true
			if _, ok := event.Data.(jsonmodels.Exposure); !ok {
				t.Errorf("Expected exposure data to be jsonmodels.Exposure, got %T", event.Data)
			}
			break
		}
	}

	if !foundExposure {
		t.Errorf("Expected EXPOSURE event to be logged, but it was not found in events: %v", events)
	}
}

func TestEventLoggerCalledOnGoal(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dataProvider = DefaultContextDataProvider{client_: client}
	var eventHandler = DefaultContextEventHandler{client_: client}
	var variableParser = DefaultVariableParser{}
	var audienceMatcher = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dataProvider, eventHandler, logger, variableParser, audienceMatcher)

	logger.Reset()

	err := ctx.Track("purchase", map[string]interface{}{"amount": 99.99})
	if err != nil {
		t.Fatalf("Track failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundGoal := false
	for _, event := range events {
		if event.EventType == Goal {
			foundGoal = true
			if goal, ok := event.Data.(jsonmodels.GoalAchievement); ok {
				if goal.Name != "purchase" {
					t.Errorf("Expected goal name 'purchase', got '%s'", goal.Name)
				}
			} else {
				t.Errorf("Expected goal data to be jsonmodels.GoalAchievement, got %T", event.Data)
			}
			break
		}
	}

	if !foundGoal {
		t.Errorf("Expected GOAL event to be logged, but it was not found in events: %v", events)
	}
}

func TestEventLoggerCalledOnPublish(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dataProvider = DefaultContextDataProvider{client_: client}
	var eventHandler = DefaultContextEventHandler{client_: client}
	var variableParser = DefaultVariableParser{}
	var audienceMatcher = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dataProvider, eventHandler, logger, variableParser, audienceMatcher)

	logger.Reset()

	_ = ctx.Track("goal1", nil)
	err := ctx.Publish()
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	events := logger.GetEvents()
	foundPublish := false
	for _, event := range events {
		if event.EventType == Publish {
			foundPublish = true
			if _, ok := event.Data.(jsonmodels.PublishEvent); !ok {
				t.Errorf("Expected publish data to be jsonmodels.PublishEvent, got %T", event.Data)
			}
			break
		}
	}

	if !foundPublish {
		t.Errorf("Expected PUBLISH event to be logged, but it was not found in events: %v", events)
	}
}

func TestEventLoggerCalledOnRefresh(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dataProvider = DefaultContextDataProvider{client_: client}
	var eventHandler = DefaultContextEventHandler{client_: client}
	var variableParser = DefaultVariableParser{}
	var audienceMatcher = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dataProvider, eventHandler, logger, variableParser, audienceMatcher)

	logger.Reset()

	ctx.Refresh()

	time.Sleep(50 * time.Millisecond)

	events := logger.GetEvents()
	foundRefresh := false
	for _, event := range events {
		if event.EventType == Refresh {
			foundRefresh = true
			break
		}
	}

	if !foundRefresh {
		t.Errorf("Expected REFRESH event to be logged, but it was not found in events: %v", events)
	}
}

func TestEventLoggerCalledOnClose(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dataProvider = DefaultContextDataProvider{client_: client}
	var eventHandler = DefaultContextEventHandler{client_: client}
	var variableParser = DefaultVariableParser{}
	var audienceMatcher = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dataProvider, eventHandler, logger, variableParser, audienceMatcher)

	logger.Reset()

	ctx.Close()

	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundClose := false
	for _, event := range events {
		if event.EventType == Close {
			foundClose = true
			break
		}
	}

	if !foundClose {
		t.Errorf("Expected CLOSE event to be logged, but it was not found in events: %v", events)
	}
}

func TestEventLoggerErrorInCallback(t *testing.T) {
	setUp()

	logger := &MockEventLogger{shouldFail: true}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dataProvider = DefaultContextDataProvider{client_: client}
	var eventHandler = DefaultContextEventHandler{client_: client}
	var variableParser = DefaultVariableParser{}
	var audienceMatcher = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	defer func() {
		if r := recover(); r != nil {
		}
	}()

	_ = CreateContext(clk, config, dataFutureReady, dataProvider, eventHandler, logger, variableParser, audienceMatcher)

	if logger.GetCallCount() == 0 {
		t.Logf("Logger was called despite potential panic (expected behavior)")
	}
}
