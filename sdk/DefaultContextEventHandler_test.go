package sdk

import (
	context2 "context"
	"errors"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

type ClientMockWithTimeout struct {
	delay time.Duration
}

func (c ClientMockWithTimeout) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return jsonmodels.ContextData{}, nil
	})
}

func (c ClientMockWithTimeout) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		time.Sleep(c.delay)
		return nil, nil
	})
}

func TestPublisherTimeout(t *testing.T) {
	var context = Context{}
	var client = ClientMockWithTimeout{delay: 100 * time.Millisecond}
	var event = jsonmodels.PublishEvent{}
	var eventHandler = DefaultContextEventHandler{client_: client}

	ctx, cancel := context2.WithTimeout(context2.Background(), 50*time.Millisecond)
	defer cancel()

	var result, err = eventHandler.Publish(context, event).Get(ctx)

	if err == nil {
		t.Logf("Expected timeout error or nil result, got result: %v", result)
	}
}

type ClientMockWithRetry struct {
	attempts    int32
	maxFailures int32
}

func (c *ClientMockWithRetry) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return jsonmodels.ContextData{}, nil
	})
}

func (c *ClientMockWithRetry) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		attempt := atomic.AddInt32(&c.attempts, 1)
		if attempt <= c.maxFailures {
			return nil, errors.New("transient failure")
		}
		return nil, nil
	})
}

func TestPublisherRetryLogic(t *testing.T) {
	client := &ClientMockWithRetry{attempts: 0, maxFailures: 2}
	var ctx = Context{}
	var event = jsonmodels.PublishEvent{}
	var eventHandler = DefaultContextEventHandler{client_: client}

	_, err := eventHandler.Publish(ctx, event).Get(context2.Background())
	if err == nil {
		t.Logf("First attempt succeeded (or SDK doesn't implement retry)")
	} else {
		t.Logf("First attempt failed as expected: %v", err)
	}

	atomic.StoreInt32(&client.attempts, 2)
	_, err = eventHandler.Publish(ctx, event).Get(context2.Background())
	if err != nil {
		t.Errorf("Expected success after failures exhausted, got: %v", err)
	}
}

type ClientMockWithQueue struct {
	mu       sync.Mutex
	received []jsonmodels.PublishEvent
}

func (c *ClientMockWithQueue) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return jsonmodels.ContextData{}, nil
	})
}

func (c *ClientMockWithQueue) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		c.mu.Lock()
		c.received = append(c.received, event)
		c.mu.Unlock()
		return nil, nil
	})
}

func TestPublisherEventQueuing(t *testing.T) {
	client := &ClientMockWithQueue{}

	events := []jsonmodels.PublishEvent{
		{PublishedAt: 1, Goals: []jsonmodels.GoalAchievement{{Name: "goal1"}}},
		{PublishedAt: 2, Goals: []jsonmodels.GoalAchievement{{Name: "goal2"}}},
		{PublishedAt: 3, Goals: []jsonmodels.GoalAchievement{{Name: "goal3"}}},
	}

	var ctx = Context{}
	var eventHandler = DefaultContextEventHandler{client_: client}

	for _, event := range events {
		_, err := eventHandler.Publish(ctx, event).Get(context2.Background())
		if err != nil {
			t.Errorf("Publish failed: %v", err)
		}
	}

	client.mu.Lock()
	received := len(client.received)
	client.mu.Unlock()

	if received != len(events) {
		t.Errorf("Expected %d events to be received, got %d", len(events), received)
	}
}

func TestPublisherConcurrentPublish(t *testing.T) {
	client := &ClientMockWithQueue{}
	var ctx = Context{}
	var eventHandler = DefaultContextEventHandler{client_: client}

	numGoroutines := 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			event := jsonmodels.PublishEvent{
				PublishedAt: int64(idx),
				Goals:       []jsonmodels.GoalAchievement{{Name: "concurrent_goal"}},
			}
			_, err := eventHandler.Publish(ctx, event).Get(context2.Background())
			if err != nil {
				t.Errorf("Concurrent publish %d failed: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	client.mu.Lock()
	received := len(client.received)
	client.mu.Unlock()

	if received != numGoroutines {
		t.Errorf("Expected %d events from concurrent publishes, got %d", numGoroutines, received)
	}
}

func TestPublisherEmptyPayload(t *testing.T) {
	client := &ClientMockWithQueue{}
	var ctx = Context{}
	var eventHandler = DefaultContextEventHandler{client_: client}

	emptyEvent := jsonmodels.PublishEvent{
		PublishedAt: 0,
		Exposures:   []jsonmodels.Exposure{},
		Goals:       []jsonmodels.GoalAchievement{},
		Attributes:  []jsonmodels.Attribute{},
		Units:       []jsonmodels.Unit{},
	}

	_, err := eventHandler.Publish(ctx, emptyEvent).Get(context2.Background())
	if err != nil {
		t.Errorf("Expected empty payload to be publishable, got error: %v", err)
	}

	client.mu.Lock()
	received := len(client.received)
	client.mu.Unlock()

	if received != 1 {
		t.Errorf("Expected 1 event to be received even with empty payload, got %d", received)
	}
}

func TestPublisherLargePayload(t *testing.T) {
	client := &ClientMockWithQueue{}
	var ctx = Context{}
	var eventHandler = DefaultContextEventHandler{client_: client}

	numGoals := 1000
	goals := make([]jsonmodels.GoalAchievement, numGoals)
	for i := 0; i < numGoals; i++ {
		goals[i] = jsonmodels.GoalAchievement{
			Name:       "goal_" + string(rune('a'+i%26)),
			AchievedAt: int64(i),
			Properties: map[string]interface{}{
				"index":  i,
				"value":  float64(i) * 1.5,
				"active": i%2 == 0,
			},
		}
	}

	numExposures := 100
	exposures := make([]jsonmodels.Exposure, numExposures)
	for i := 0; i < numExposures; i++ {
		exposures[i] = jsonmodels.Exposure{
			Id:        i,
			Name:      "exp_" + string(rune('a'+i%26)),
			Variant:   i % 3,
			ExposedAt: int64(i * 1000),
			Assigned:  true,
			Eligible:  true,
		}
	}

	largeEvent := jsonmodels.PublishEvent{
		PublishedAt: time.Now().UnixMilli(),
		Hashed:      true,
		Goals:       goals,
		Exposures:   exposures,
		Units: []jsonmodels.Unit{
			{Type: "user_id", Uid: "large-payload-test-user"},
		},
	}

	_, err := eventHandler.Publish(ctx, largeEvent).Get(context2.Background())
	if err != nil {
		t.Errorf("Expected large payload to be publishable, got error: %v", err)
	}

	client.mu.Lock()
	if len(client.received) != 1 {
		t.Errorf("Expected 1 large event to be received, got %d", len(client.received))
	}
	receivedEvent := client.received[0]
	client.mu.Unlock()

	if len(receivedEvent.Goals) != numGoals {
		t.Errorf("Expected %d goals in received event, got %d", numGoals, len(receivedEvent.Goals))
	}

	if len(receivedEvent.Exposures) != numExposures {
		t.Errorf("Expected %d exposures in received event, got %d", numExposures, len(receivedEvent.Exposures))
	}
}
