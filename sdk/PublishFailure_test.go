package sdk

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// heldPublisher is a ContextEventHandler whose publish results the test
// settles by hand, so a test can fail a publish or keep one in flight.
type heldPublisher struct {
	mu      sync.Mutex
	events  []jsonmodels.PublishEvent
	settles []future.SetResultFunc
}

func (p *heldPublisher) Publish(context Context, event jsonmodels.PublishEvent) *future.Future {
	var result, done = future.New()
	p.mu.Lock()
	p.events = append(p.events, event)
	p.settles = append(p.settles, done)
	p.mu.Unlock()
	return result
}

func (p *heldPublisher) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

func (p *heldPublisher) event(t *testing.T, i int) jsonmodels.PublishEvent {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if i >= len(p.events) {
		t.Fatalf("publisher called %d times, want at least %d", len(p.events), i+1)
	}
	return p.events[i]
}

func (p *heldPublisher) settle(t *testing.T, i int, err error) {
	t.Helper()
	p.mu.Lock()
	if i >= len(p.settles) {
		p.mu.Unlock()
		t.Fatalf("publisher called %d times, want at least %d", len(p.settles), i+1)
	}
	var done = p.settles[i]
	p.mu.Unlock()
	done(nil, err)
}

func createHeldPublisherContext(t *testing.T, publisher *heldPublisher, logger *MockEventLogger) *Context {
	t.Helper()
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.PublishDelay_ = -1
	config.RefreshInterval_ = 0
	var el ContextEventLogger
	if logger != nil {
		config.EventLogger_ = logger
		el = logger
	}
	var ctx = createTrackedContext(clock, config, dataFutureReady, dataProvider, publisher, el, variableParser, audienceMatcher)
	if !ctx.IsReady() || ctx.IsFailed() {
		t.Fatalf("context not ready: ready=%v failed=%v", ctx.IsReady(), ctx.IsFailed())
	}
	return ctx
}

func getWithTimeout(t *testing.T, fut *future.Future) error {
	t.Helper()
	var ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := fut.Get(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timed out waiting for publish")
	}
	return err
}

func goalNames(goals []jsonmodels.GoalAchievement) []string {
	var names = make([]string, 0, len(goals))
	for _, goal := range goals {
		names = append(names, goal.Name)
	}
	return names
}

// Cross-SDK scenario 193: a failed publish must keep its events pending, and
// the next publish must send them again.
func TestPublishFailureKeepsEventsPending(t *testing.T) {
	var publisher = &heldPublisher{}
	var ctx = createHeldPublisherContext(t, publisher, nil)

	variant, err := ctx.GetTreatment("exp_test_ab")
	assertAny(nil, err, t)
	assertAny(1, variant, t)
	assertAny(nil, ctx.Track("goal1", map[string]interface{}{"amount": 125}), t)
	assertAny(int32(2), ctx.GetPendingCount(), t)

	var publishFailed = errors.New("Publish failed")
	fut, err := ctx.PublishAsync()
	assertAny(nil, err, t)
	publisher.settle(t, 0, publishFailed)
	assertAny(publishFailed, getWithTimeout(t, fut), t)

	assertAny(int32(2), ctx.GetPendingCount(), t)
	assertAny(1, len(ctx.Exposures_), t)
	assertAny([]string{"goal1"}, goalNames(ctx.Achievements_), t)

	var failed = publisher.event(t, 0)
	fut, err = ctx.PublishAsync()
	assertAny(nil, err, t)
	assertAny(2, publisher.calls(), t)
	publisher.settle(t, 1, nil)
	assertAny(nil, getWithTimeout(t, fut), t)

	var retried = publisher.event(t, 1)
	assertAny(failed.Exposures, retried.Exposures, t)
	assertAny(failed.Goals, retried.Goals, t)
	assertAny(int32(0), ctx.GetPendingCount(), t)
	assertAny(0, len(ctx.Exposures_), t)
	assertAny(0, len(ctx.Achievements_), t)

	// The exposure was queued once and sent once more after the failure; a
	// repeated treatment call must not queue it again.
	_, _ = ctx.GetTreatment("exp_test_ab")
	assertAny(int32(0), ctx.GetPendingCount(), t)
}

func TestPublishFailureRequeuesBatchAheadOfEventsRecordedInFlight(t *testing.T) {
	var publisher = &heldPublisher{}
	var ctx = createHeldPublisherContext(t, publisher, nil)

	assertAny(nil, ctx.Track("goal1", nil), t)
	fut, err := ctx.PublishAsync()
	assertAny(nil, err, t)

	// The publish is in flight: its batch has left the queue.
	assertAny(int32(0), ctx.GetPendingCount(), t)
	assertAny(nil, ctx.Track("goal2", nil), t)
	assertAny(int32(1), ctx.GetPendingCount(), t)

	publisher.settle(t, 0, errors.New("Publish failed"))
	if getWithTimeout(t, fut) == nil {
		t.Fatal("expected the publish to fail")
	}

	assertAny(int32(2), ctx.GetPendingCount(), t)
	assertAny([]string{"goal1", "goal2"}, goalNames(ctx.Achievements_), t)

	fut, err = ctx.PublishAsync()
	assertAny(nil, err, t)
	publisher.settle(t, 1, nil)
	assertAny(nil, getWithTimeout(t, fut), t)
	assertAny([]string{"goal1", "goal2"}, goalNames(publisher.event(t, 1).Goals), t)
	assertAny(int32(0), ctx.GetPendingCount(), t)
}

func TestPublishFailureDoesNotChangeTheFailedEvent(t *testing.T) {
	var publisher = &heldPublisher{}
	var ctx = createHeldPublisherContext(t, publisher, nil)

	assertAny(nil, ctx.Track("goal1", nil), t)
	fut, _ := ctx.PublishAsync()
	assertAny(nil, ctx.Track("goal2", nil), t)
	publisher.settle(t, 0, errors.New("Publish failed"))
	getWithTimeout(t, fut)

	// The publisher can keep the event it received, so requeueing must not
	// write into the slices that event holds.
	assertAny(nil, ctx.Track("goal3", nil), t)
	assertAny([]string{"goal1"}, goalNames(publisher.event(t, 0).Goals), t)
	assertAny([]string{"goal1", "goal2", "goal3"}, goalNames(ctx.Achievements_), t)
}

func TestPublishFailureLogsErrorAndNoPublishEvent(t *testing.T) {
	var publisher = &heldPublisher{}
	var logger = &MockEventLogger{}
	var ctx = createHeldPublisherContext(t, publisher, logger)

	assertAny(nil, ctx.Track("goal1", nil), t)
	logger.Reset()

	var publishFailed = errors.New("Publish failed")
	fut, _ := ctx.PublishAsync()
	publisher.settle(t, 0, publishFailed)
	getWithTimeout(t, fut)

	var events = logger.GetEvents()
	if len(events) != 1 || events[0].EventType != Error || events[0].Data != publishFailed {
		t.Errorf("got logged events %#v, want one Error event with the publish error", events)
	}
}

func TestPublishOnFailedContextStillDiscardsEvents(t *testing.T) {
	setUp()
	var publisher = &heldPublisher{}
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.PublishDelay_ = -1
	config.RefreshInterval_ = 0
	var ctx = createTrackedContext(clock, config, dataFutureFailed, dataProvider, publisher, nil, variableParser, audienceMatcher)
	assertAny(true, ctx.IsFailed(), t)

	assertAny(nil, ctx.Track("goal1", nil), t)
	assertAny(int32(1), ctx.GetPendingCount(), t)
	assertAny(nil, ctx.Publish(), t)
	assertAny(int32(0), ctx.GetPendingCount(), t)
	assertAny(0, publisher.calls(), t)
}

// A close that fails to publish must leave the context open and its events
// pending, so a later Close or Publish can send them.
func TestFailedCloseKeepsContextOpenForRetry(t *testing.T) {
	var publisher = &heldPublisher{}
	var ctx = createHeldPublisherContext(t, publisher, nil)

	assertAny(nil, ctx.Track("goal1", nil), t)

	fut, err := ctx.CloseAsync()
	assertAny(nil, err, t)
	var publishFailed = errors.New("Publish failed")
	publisher.settle(t, 0, publishFailed)
	assertAny(publishFailed, getWithTimeout(t, fut), t)

	assertAny(false, ctx.IsClosed(), t)
	assertAny(false, ctx.IsClosing(), t)
	assertAny(int32(1), ctx.GetPendingCount(), t)

	fut, err = ctx.CloseAsync()
	assertAny(nil, err, t)
	assertAny(2, publisher.calls(), t)
	publisher.settle(t, 1, nil)
	assertAny(nil, getWithTimeout(t, fut), t)
	assertAny(true, ctx.IsClosed(), t)
	assertAny(int32(0), ctx.GetPendingCount(), t)
}

// A publish that is already in flight when Close is called must hold the close
// open: if it fails, its events are requeued and the context must still accept a retry.
func TestCloseWaitsForInFlightPublishThatFails(t *testing.T) {
	var publisher = &heldPublisher{}
	var ctx = createHeldPublisherContext(t, publisher, nil)

	assertAny(nil, ctx.Track("goal1", nil), t)
	inflight, err := ctx.PublishAsync()
	assertAny(nil, err, t)

	closing, err := ctx.CloseAsync()
	assertAny(nil, err, t)
	assertAny(false, ctx.IsClosed(), t)

	var publishFailed = errors.New("Publish failed")
	publisher.settle(t, 0, publishFailed)
	assertAny(publishFailed, getWithTimeout(t, inflight), t)
	assertAny(publishFailed, getWithTimeout(t, closing), t)

	assertAny(false, ctx.IsClosed(), t)
	assertAny(int32(1), ctx.GetPendingCount(), t)
	assertAny(nil, ctx.Publish2ForTest(t, publisher, 1), t)
}

// A failed close must bring back the automatic refresh that Close stopped.
func TestFailedCloseRestoresRefreshTimer(t *testing.T) {
	var publisher = &heldPublisher{}
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.PublishDelay_ = -1
	config.RefreshInterval_ = 5000
	var ctx = createTrackedContext(clock, config, dataFutureReady, dataProvider, publisher, nil, variableParser, audienceMatcher)
	defer ctx.ClearRefreshTimer()

	assertAny(nil, ctx.Track("goal1", nil), t)
	closing, err := ctx.CloseAsync()
	assertAny(nil, err, t)
	publisher.settle(t, 0, errors.New("Publish failed"))
	_ = getWithTimeout(t, closing)

	ctx.StateLock_.Lock()
	var restored = ctx.RefreshTimer_ != nil
	ctx.StateLock_.Unlock()
	assertAny(true, restored, t)
}

func (c *Context) Publish2ForTest(t *testing.T, publisher *heldPublisher, i int) error {
	t.Helper()
	fut, err := c.PublishAsync()
	if err != nil {
		return err
	}
	publisher.settle(t, i, nil)
	return getWithTimeout(t, fut)
}

// Concurrent RefreshAsync callers must all get the future of the active
// refresh and must not return before it settles.
func TestConcurrentRefreshAsyncShareActiveFuture(t *testing.T) {
	setUp()
	var slow, settle = future.New()
	var provider = DefaultContextDataProvider{client_: slowRefreshClient{fut: slow}}
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var ctx = createTrackedContext(clock, config, dataFutureReady, provider, eventHandler, nil, variableParser, audienceMatcher)

	var results = make([]*future.Future, 16)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = ctx.RefreshAsync()
		}(i)
	}
	wg.Wait()
	for _, f := range results {
		if f.Ready() {
			t.Fatal("RefreshAsync returned a settled future before the refresh finished")
		}
		if f != results[0] {
			t.Fatal("callers got different futures for the same active refresh")
		}
	}
	settle(data, nil)
	assertAny(nil, getWithTimeout(t, results[0]), t)
}

type slowRefreshClient struct {
	ClientContextMock
	fut *future.Future
}

func (c slowRefreshClient) GetContextData() *future.Future { return c.fut }

// Close racing with a publish that has just drained the queue must not
// close the context before that publish settles.
func TestCloseRacingPublishNeverStrandsFailedBatch(t *testing.T) {
	for i := 0; i < 200; i++ {
		var publisher = &heldPublisher{}
		var ctx = createHeldPublisherContext(t, publisher, nil)
		assertAny(nil, ctx.Track("goal1", nil), t)

		var wg sync.WaitGroup
		var closing *future.Future
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = ctx.PublishAsync() }()
		go func() { defer wg.Done(); closing, _ = ctx.CloseAsync() }()
		wg.Wait()

		if publisher.calls() > 0 {
			publisher.settle(t, 0, errors.New("Publish failed"))
		}
		_, _ = closing.Get(context.Background())
		if ctx.IsClosed() && ctx.GetPendingCount() > 0 {
			t.Fatalf("iteration %d: context closed with %d events stranded; calls=%d closing=%v", i, ctx.GetPendingCount(), publisher.calls(), ctx.IsClosing())
		}
	}
}

// Every CloseAsync call that overlaps a running close must get the future of
// that close and must not see it settled before the close finishes.
func TestConcurrentCloseAsyncShareClosingFuture(t *testing.T) {
	for i := 0; i < 200; i++ {
		var publisher = &heldPublisher{}
		var ctx = createHeldPublisherContext(t, publisher, nil)
		assertAny(nil, ctx.Track("goal1", nil), t)

		var results = make([]*future.Future, 8)
		var wg sync.WaitGroup
		for j := range results {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				results[j], _ = ctx.CloseAsync()
			}(j)
		}
		wg.Wait()
		for _, f := range results {
			if f.Ready() {
				t.Fatalf("iteration %d: CloseAsync returned a settled future before the close finished", i)
			}
		}
		publisher.settle(t, 0, nil)
		for _, f := range results {
			assertAny(nil, getWithTimeout(t, f), t)
		}
		assertAny(true, ctx.IsClosed(), t)
	}
}
