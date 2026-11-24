package sdk_test

import (
	"testing"

	"github.com/absmartly/go-sdk/sdk"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type recordingPublisher struct {
	events []jsonmodels.PublishEvent
}

func (p *recordingPublisher) Publish(context sdk.Context, event jsonmodels.PublishEvent) *future.Future {
	p.events = append(p.events, event)
	return future.Call(func() (future.Value, error) { return nil, nil })
}

func sharedClockContext(t *testing.T, uid string, publisher *recordingPublisher) *sdk.Context {
	t.Helper()
	var abs = sdk.Create(sdk.ABSmartlyConfig{
		ContextDataProvider_: v111Provider{jsonmodels.ContextData{}},
		ContextEventHandler_: publisher,
	})
	var ctx = abs.CreateContext(sdk.ContextConfig{Units_: map[string]string{"session_id": uid}, PublishDelay_: -1})
	ctx.WaitUntilReady()
	return ctx
}

// In v1.1.1, assigning one context's Clock_ to another only shared the clock.
// It must not route the second context's events and lifecycle to the first.
func TestSharingClockBetweenContextsKeepsThemSeparate(t *testing.T) {
	var publisherA, publisherB = &recordingPublisher{}, &recordingPublisher{}
	var a = sharedClockContext(t, "alice", publisherA)
	var b = sharedClockContext(t, "bob", publisherB)
	b.Clock_ = a.Clock_

	if err := b.Track("goal", nil); err != nil {
		t.Fatal(err)
	}
	b.Close()

	if len(publisherA.events) != 0 {
		t.Fatalf("b's goal was published through a: %+v", publisherA.events)
	}
	if len(publisherB.events) != 1 || len(publisherB.events[0].Goals) != 1 {
		t.Fatalf("b published %+v, want one event with b's goal", publisherB.events)
	}
	if !b.IsClosed() || a.IsClosed() {
		t.Errorf("closed: a=%v b=%v, want only b", a.IsClosed(), b.IsClosed())
	}
	// a's own events carry a's unit hash, which b's must not.
	if err := a.Track("goal", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.Publish(); err != nil {
		t.Fatal(err)
	}
	if len(publisherA.events) != 1 {
		t.Fatalf("a published %d events, want 1", len(publisherA.events))
	}
	if publisherA.events[0].Units[0].Uid == publisherB.events[0].Units[0].Uid {
		t.Errorf("b's goal was published under a's unit hash %q", publisherB.events[0].Units[0].Uid)
	}
}

// An event logger receives a snapshot of the context that logged it.
type firstContextLogger struct {
	saved *sdk.Context
}

func (l *firstContextLogger) HandleEvent(context sdk.Context, event sdk.EventType, data interface{}) {
	if l.saved == nil {
		l.saved = &context
	}
}

// The Context a logger receives is a snapshot; copying its Clock_ into
// another context must also only share the clock.
func TestSharingSnapshotClockBetweenContextsKeepsThemSeparate(t *testing.T) {
	var publisherA, publisherB = &recordingPublisher{}, &recordingPublisher{}
	var logger = &firstContextLogger{}
	var abs = sdk.Create(sdk.ABSmartlyConfig{
		ContextDataProvider_: v111Provider{jsonmodels.ContextData{}},
		ContextEventHandler_: publisherA,
		ContextEventLogger_:  logger,
	})
	var a = abs.CreateContext(sdk.ContextConfig{Units_: map[string]string{"session_id": "alice"}, PublishDelay_: -1})
	a.WaitUntilReady()
	if logger.saved == nil {
		t.Fatal("the logger received no context")
	}
	var b = sharedClockContext(t, "bob", publisherB)
	b.Clock_ = logger.saved.Clock_

	if err := b.Track("goal", nil); err != nil {
		t.Fatal(err)
	}
	b.Close()

	if len(publisherA.events) != 0 {
		t.Fatalf("b's goal was published through a: %+v", publisherA.events)
	}
	if len(publisherB.events) != 1 || len(publisherB.events[0].Goals) != 1 {
		t.Fatalf("b published %+v, want one event with b's goal", publisherB.events)
	}
	if !b.IsClosed() || a.IsClosed() {
		t.Errorf("closed: a=%v b=%v, want only b", a.IsClosed(), b.IsClosed())
	}
	if err := a.Track("goal", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.Publish(); err != nil {
		t.Fatal(err)
	}
	if len(publisherA.events) != 1 {
		t.Fatalf("a published %d events, want 1", len(publisherA.events))
	}
	if publisherA.events[0].Units[0].Uid == publisherB.events[0].Units[0].Uid {
		t.Errorf("b's goal was published under a's unit hash %q", publisherB.events[0].Units[0].Uid)
	}
}
