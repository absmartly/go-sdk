package sdk

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

func TestContextZeroAndLoggerOnlyLiterals(t *testing.T) {
	var zero Context
	if _, found := zero.GetExperiment("missing"); found {
		t.Fatal("zero context found experiment")
	}
	zero.LogError(nil)
	var logger = &MockEventLogger{}
	var manual = Context{EventLogger_: logger,
		Index_: map[string]ExperimentVariables{"exp": {Data: jsonmodels.Experiment{Name: "exp"}}}}
	manual.LogEvent(Ready, "manual")
	assertAny(1, len(logger.GetEvents()), t)
	var experiment, found = manual.GetExperiment("exp")
	assertAny(true, found, t)
	assertAny("exp", experiment.Data.Name, t)
}

// A fully initialized old-style literal must not need a constructor to own
// private state, including concurrent first use of that state.
func TestContextManualLiteralState(t *testing.T) {
	var boolValue = func(value bool) *atomic.Value {
		var result = &atomic.Value{}
		result.Store(value)
		return result
	}
	var count = &atomic.Value{}
	count.Store(int32(0))
	var manual = Context{
		PublishDelay_: -1, RefreshInterval_: -1,
		VariableParser_: DefaultVariableParser{},
		Units_:          map[string]string{}, Failed_: boolValue(false), Ready_: boolValue(false),
		DataLock: &sync.RWMutex{}, ContextLock_: &sync.RWMutex{},
		HashedUnits_: map[interface{}]interface{}{}, Assigners_: map[interface{}]interface{}{},
		AssignmentCache: map[string]Assignment{}, EventLock_: &sync.Mutex{},
		Overrides_: map[interface{}]interface{}{}, Cassignments_: map[interface{}]interface{}{},
		PendingCount_: count, Closing_: boolValue(false), Closed_: boolValue(false),
		Refreshing_: boolValue(false), TimeoutLock_: &sync.Mutex{},
		Clock_: internal.FixedClock{Millis_: 123},
	}
	var originalClock = manual.Clock_
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = manual.GetAssignment("missing")
		}()
	}
	wg.Wait()
	assertAny(originalClock, manual.Clock_, t)
	assertAny(nil, manual.SetUnit("session_id", ruleUnit), t)
	manual.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)),
	}})
	assertAny(2, treatment(t, &manual, "exp"), t)
	assertAny(true, expectExposures(t, &manual, 1)[0].RuleOverride, t)
	assertAny(nil, manual.SetAttribute("country", "CA"), t)
	assertAny("CA", manual.GetAttribute("country"), t)
	// As in v1.1.1, a value copy of a literal acts on its own fields; it
	// shares the state found through their shared maps and locks.
	var copied = manual
	if copied.live() != &copied {
		t.Error("a copy of a literal context redirects to another context")
	}
	if copied.state() != manual.state() {
		t.Error("a copy of a literal context does not share its state")
	}
	manual.ClearRefreshTimer()
	manual.ClearTimeout()
}

func TestContextValueCopiesAndSnapshotsShareLifecycle(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp", ""))
	var copied = *ctx
	var snapshot = ctx.snapshot()
	assertAny(ctx.state(), copied.state(), t)
	assertAny(ctx.state(), snapshot.state(), t)
	assertAny(ctx, copied.live(), t)
	assertAny(ctx, snapshot.live(), t)
	assertAny(false, copied.isSnapshot(), t)
	assertAny(true, snapshot.isSnapshot(), t)
	assertAny(ctx.Clock_.Millis(), snapshot.Clock_.Millis(), t)
	assertAny(nil, copied.Track("goal", nil), t)
	assertAny(int32(1), ctx.GetPendingCount(), t)
	copied.Close()
	assertAny(true, ctx.IsClosed(), t)
	assertAny(true, snapshot.IsClosed(), t)
}

func TestRuleAssignmentIdentitiesDoNotRetainAbandonedFlags(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2,"conditions":{"eq":[{"var":"country"},{"value":"US"}]}}`)))
	var identities = ctx.state().ruleIdentities
	var retain = func() *Assignment {
		assertAny(nil, ctx.SetAttribute("country", "US"), t)
		var retained = ctx.GetAssignment("exp")
		assertAny(nil, ctx.SetAttribute("country", "CA"), t)
		_, _ = ctx.PeekTreatment("exp")
		return retained
	}
	var retained = retain()
	for i := 0; i < 100; i++ {
		_ = retain()
	}
	var deadline = time.Now().Add(5 * time.Second)
	for {
		runtime.GC()
		identities.mu.Lock()
		var size = len(identities.ids)
		identities.mu.Unlock()
		if size == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d identity entries retained, want only retained assignment", size)
		}
		time.Sleep(time.Millisecond)
	}
	ctx.QueueExposure(retained)
	assertAny(true, expectExposures(t, ctx, 1)[0].RuleOverride, t)
	runtime.KeepAlive(retained)
}

func TestAbandonedContextAndSnapshotsAreCollectible(t *testing.T) {
	var collected = make(chan struct{}, 1)
	func() {
		var ctx, _ = newRuleContext(t, "production", withUnit(),
			ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2}`)))
		_ = ctx.GetAssignment("exp")
		// The probe has no finalizer cycle itself; it becomes unreachable only
		// after the context, its state and root/snapshot cycles are collected.
		var probe = &struct{ data [64]byte }{}
		runtime.SetFinalizer(probe, func(*struct{ data [64]byte }) { collected <- struct{}{} })
		assertAny(nil, ctx.SetAttribute("probe", probe), t)
		_ = ctx.snapshot()
	}()
	var deadline = time.Now().Add(5 * time.Second)
	for {
		runtime.GC()
		select {
		case <-collected:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("abandoned context retained its attributes")
		}
		time.Sleep(time.Millisecond)
	}
}

// v1.1.1 let callers replace the exported Clock_. A constructed context keeps
// its rules, system attributes and lifecycle after the swap.
func TestReplacingClockKeepsContextState(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		ruleExperiment("exp_rule", rulesJSON(`{"type":"assign","variant":2}`)),
		audienceExperiment("exp_app", appFilter, true))
	assertAny(nil, ctx.SetUnit("session_id", ruleUnit), t)
	ctx.Clock_ = internal.FixedClock{Millis_: 42}

	assertAny(2, treatment(t, ctx, "exp_rule"), t)
	assertAny(1, treatment(t, ctx, "exp_app"), t)
	var exposures = expectExposures(t, ctx, 2)
	assertAny(true, exposures[0].RuleOverride, t)
	assertAny(false, exposures[1].AudienceMismatch, t)
	assertAny(int64(42), exposures[0].ExposedAt, t)
}

// Close still waits for a publish already in flight after Clock_ is replaced.
func TestReplacingClockKeepsCloseWaitingForInflightPublish(t *testing.T) {
	var publisher = &heldPublisher{}
	var ctx = createHeldPublisherContext(t, publisher, nil)
	assertAny(nil, ctx.Track("goal1", nil), t)
	var published, err = ctx.PublishAsync()
	assertAny(nil, err, t)
	ctx.Clock_ = internal.FixedClock{Millis_: 42}

	var closing, closeErr = ctx.CloseAsync()
	assertAny(nil, closeErr, t)
	if closing.Ready() {
		t.Fatal("close settled while a publish was still in flight")
	}
	publisher.settle(t, 0, nil)
	assertAny(nil, getWithTimeout(t, published), t)
	assertAny(nil, getWithTimeout(t, closing), t)
	assertAny(true, ctx.IsClosed(), t)
}

// A literal context's pre-filled v1.1.1 caches are honoured, as in v1.1.1.
func TestContextLiteralHonoursAssignmentCacheAndIndexVariables(t *testing.T) {
	var boolValue = func(value bool) *atomic.Value {
		var result = &atomic.Value{}
		result.Store(value)
		return result
	}
	var count = &atomic.Value{}
	count.Store(int32(0))
	var experiment = ExperimentVariables{
		Data:      jsonmodels.Experiment{Id: 5, Name: "exp", UnitType: "session_id", Iteration: 1, TrafficSplit: []float64{0, 1}},
		Variables: []map[string]interface{}{{"color": "red"}, {"color": "blue"}},
	}
	var exposed = boolValue(false)
	var manual = Context{
		PublishDelay_: -1, RefreshInterval_: -1,
		Units_: map[string]string{}, Failed_: boolValue(false), Ready_: boolValue(true),
		DataLock: &sync.RWMutex{}, ContextLock_: &sync.RWMutex{},
		Index_:          map[string]ExperimentVariables{"exp": experiment},
		IndexVariables_: map[interface{}]interface{}{"color": experiment},
		HashedUnits_:    map[interface{}]interface{}{}, Assigners_: map[interface{}]interface{}{},
		AssignmentCache: map[string]Assignment{"exp": {Id: 5, Name: "exp", UnitType: "session_id", Iteration: 1,
			TrafficSplit: []float64{0, 1}, Variant: 1, Assigned: true, Eligible: true,
			Variables: experiment.Variables[1], Exposed: exposed}},
		EventLock_: &sync.Mutex{}, Overrides_: map[interface{}]interface{}{}, Cassignments_: map[interface{}]interface{}{},
		PendingCount_: count, Closing_: boolValue(false), Closed_: boolValue(false),
		Refreshing_: boolValue(false), TimeoutLock_: &sync.Mutex{},
		Clock_: internal.FixedClock{Millis_: 123},
	}

	var variant, err = manual.PeekTreatment("exp")
	assertAny(nil, err, t)
	assertAny(1, variant, t)
	value, err := manual.PeekVariableValue("color", "none")
	assertAny(nil, err, t)
	assertAny("blue", value, t)
}

// reentrantDeserializer calls back into the context from inside an audience
// parse.
type reentrantDeserializer struct {
	ctx   *Context
	calls int32
}

func (d *reentrantDeserializer) Deserialize(bytes []byte, offset int, length int) (map[string]interface{}, error) {
	if d.ctx != nil {
		atomic.AddInt32(&d.calls, 1)
		d.ctx.GetExperiment("exp_app")
		_, _ = d.ctx.GetVariableExperiment("banner")
		_, _ = d.ctx.GetVariableKeys()
		_, _ = d.ctx.GetVariableKeysAll()
		_ = d.ctx.Track("evaluated", nil)
	}
	return DefaultAudienceDeserializer{}.Deserialize(bytes, offset, length)
}

// A deserializer that reads the context during an evaluation must not
// deadlock after Clock_ is replaced, as it does not with the original clock.
func TestReentrantDeserializerAfterClockReplacementDoesNotDeadlock(t *testing.T) {
	// "build" reaches the deserializer while rebuilding the assignment;
	// "revalidate" while revalidating a cached one after an attribute change.
	for _, path := range []string{"build", "revalidate"} {
		for _, replace := range []bool{false, true} {
			var deserializer = &reentrantDeserializer{}
			var client = &systemAttrClient{application: "website", environment: "production"}
			var abs = Create(ABsmartlyConfig{
				Client_:               client,
				ContextDataProvider_:  DefaultContextDataProvider{client_: client},
				ContextEventHandler_:  DefaultContextEventHandler{client_: client},
				AudienceDeserializer_: deserializer,
			})
			var config = CreateDefaultContextConfig()
			config.Units_ = withUnit()
			config.PublishDelay_ = -1
			config.RefreshInterval_ = -1
			var ctx = abs.CreateContextWith(config, jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
				audienceExperiment("exp_app", appFilter, true)}})
			ctx.WaitUntilReady()
			var cached *atomic.Value
			if path == "revalidate" {
				assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
				cached = ctx.GetAssignment("exp_app").Exposed
			}
			if replace {
				ctx.Clock_ = internal.FixedClock{Millis_: 1}
			}
			deserializer.ctx = ctx
			var done = make(chan int, 1)
			go func() {
				var variant int
				if path == "revalidate" {
					variant, _ = revalidateAudience(ctx)
				} else {
					variant, _ = reparseAudience(ctx)
				}
				done <- variant
			}()
			select {
			case variant := <-done:
				assertAny(1, variant, t)
			case <-time.After(5 * time.Second):
				t.Fatalf("PeekTreatment deadlocked (%s, Clock_ replaced: %v)", path, replace)
			}
			assertAny(int32(1), atomic.LoadInt32(&deserializer.calls), t)
			if cached != nil && ctx.GetAssignment("exp_app").Exposed != cached {
				t.Errorf("revalidation rebuilt the assignment (Clock_ replaced: %v)", replace)
			}
		}
	}
}

// A literal context reads Index_ and IndexVariables_ as v1.1.1 did, including
// entries replaced after the first read, with or without ContextLock_.
func TestContextLiteralReadsCurrentLegacyIndexes(t *testing.T) {
	var five = ExperimentVariables{Data: jsonmodels.Experiment{Id: 5, Name: "exp"}}
	var six = ExperimentVariables{Data: jsonmodels.Experiment{Id: 6, Name: "exp"}}
	for _, lock := range []*sync.RWMutex{nil, {}} {
		var manual = Context{DataLock: &sync.RWMutex{}, ContextLock_: lock,
			Index_:          map[string]ExperimentVariables{"exp": five},
			IndexVariables_: map[interface{}]interface{}{"color": five}}
		var got, found = manual.GetExperiment("exp")
		assertAny(true, found, t)
		assertAny(5, got.Data.Id, t)
		var variable, err = manual.GetVariableExperiment("color")
		assertAny(nil, err, t)
		assertAny(5, variable.Data.Id, t)

		manual.Index_["exp"] = six
		manual.IndexVariables_["color"] = six
		got, _ = manual.GetExperiment("exp")
		assertAny(6, got.Data.Id, t)
		variable, _ = manual.GetVariableExperiment("color")
		assertAny(6, variable.Data.Id, t)
	}
}

// A literal that has an event queue but no ContextLock_ can queue an
// exposure, as in v1.1.1.
func TestContextLiteralWithoutContextLockQueuesExposure(t *testing.T) {
	var boolValue = func(value bool) *atomic.Value {
		var result = &atomic.Value{}
		result.Store(value)
		return result
	}
	var count = &atomic.Value{}
	count.Store(int32(0))
	var manual = Context{PublishDelay_: -1, EventLock_: &sync.Mutex{}, PendingCount_: count,
		Ready_: boolValue(true), Closing_: boolValue(false), Closed_: boolValue(false),
		TimeoutLock_: &sync.Mutex{}, Clock_: internal.FixedClock{Millis_: 7}}
	manual.QueueExposure(&Assignment{Id: 1, Name: "exp", Variant: 1, Exposed: boolValue(false)})
	assertAny(int32(1), manual.GetPendingCount(), t)
	assertAny(1, len(manual.Exposures_), t)
	assertAny(int64(7), manual.Exposures_[0].ExposedAt, t)
}

// A value copy evaluates and installs data on the live context, so the
// cache stamps, attributes, Data_ and indexes stay consistent.
func TestContextCopyActsOnLiveContextForAssignmentsAndData(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2,"conditions":{"eq":[{"var":"country"},{"value":"US"}]}}`)))
	assertAny(0, treatmentPeek(t, ctx, "exp"), t)

	var copied = *ctx
	assertAny(nil, ctx.SetAttribute("country", "US"), t)
	var variant, err = copied.PeekTreatment("exp")
	assertAny(nil, err, t)
	assertAny(2, variant, t)
	assertAny(2, treatment(t, ctx, "exp"), t)
	assertAny(true, expectExposures(t, ctx, 1)[0].RuleOverride, t)

	copied.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{ruleExperiment("other", "")}})
	var names, _ = ctx.GetExperiments()
	assertAny([]string{"other"}, names, t)
	var _, found = ctx.GetExperiment("other")
	assertAny(true, found, t)

	copied.SetDataFailed(errors.New("failed"))
	names, _ = ctx.GetExperiments()
	assertAny(0, len(names), t)
	assertAny(true, ctx.IsFailed(), t)
}

func treatmentPeek(t *testing.T, ctx *Context, name string) int {
	t.Helper()
	var variant, err = ctx.PeekTreatment(name)
	if err != nil {
		t.Fatalf("PeekTreatment(%s): %v", name, err)
	}
	return variant
}

// readyGateLogger hands TimeoutLock_ to the test from inside the Ready event,
// which the ready callback logs after installing the data.
type readyGateLogger struct {
	ctx    *Context
	locked chan struct{}
}

func (l *readyGateLogger) HandleEvent(_ Context, event EventType, _ interface{}) {
	if event == Ready {
		l.ctx.TimeoutLock_.Lock()
		close(l.locked)
	}
}

// The ready callback finishes everything that reads the context (clearing
// ReadyFuture_, arming the publish timeout) before it releases waiters, so a
// caller may replace Clock_ as soon as WaitUntilReady returns. The logger
// holds TimeoutLock_, which SetTimeout needs: the waiters must stay blocked
// until it is released, whatever the scheduling.
func TestReadyReleasesWaitersOnlyAfterItsLastContextRead(t *testing.T) {
	setUp()
	var logger = &readyGateLogger{locked: make(chan struct{})}
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.PublishDelay_ = -1
	config.RefreshInterval_ = 0
	config.EventLogger_ = logger
	var pending, settle = future.New()
	var ctx = createTrackedContext(clock, config, pending, dataProvider, eventHandler, logger, variableParser, audienceMatcher)
	logger.ctx = ctx
	assertAny(nil, ctx.Track("goal", nil), t)
	var ready = ctx.WaitUntilReadyAsync()

	go settle(data, nil)
	<-logger.locked
	// A waiter that arrives after the data is installed, while the callback
	// is still arming the timeout, must wait too.
	var late = ctx.WaitUntilReadyAsync()
	for name, waiter := range map[string]*future.Future{"early": ready, "late": late} {
		select {
		case <-readyChannel(waiter):
			ctx.TimeoutLock_.Unlock()
			t.Fatalf("%s waiter was released before the ready callback armed the publish timeout", name)
		case <-time.After(200 * time.Millisecond):
		}
	}
	ctx.TimeoutLock_.Unlock()
	assertAny(nil, getWithTimeout(t, ready), t)
	assertAny(nil, getWithTimeout(t, late), t)
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var _, err = ctx.PeekTreatment("exp_test_ab")
	assertAny(nil, err, t)
}

func readyChannel(f *future.Future) <-chan struct{} {
	var done = make(chan struct{})
	go func() {
		_, _ = f.Get(context.Background())
		close(done)
	}()
	return done
}

// After Clock_ is replaced, GetExperiment reads Index_ under DataLock, so it
// does not race SetData installing new data (run with -race).
func TestReplacedClockGetExperimentDoesNotRaceSetData(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp", ""))
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var done = make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{ruleExperiment("exp", "")}})
		}
	}()
	for i := 0; i < 200; i++ {
		ctx.GetExperiment("exp")
	}
	<-done
}

// The same holds for a copy of a context whose Clock_ was replaced: it acts on
// the live context.
func TestContextCopyAfterClockReplacementActsOnLiveContext(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2,"conditions":{"eq":[{"var":"country"},{"value":"US"}]}}`)))
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	assertAny(0, treatmentPeek(t, ctx, "exp"), t)

	var copied = *ctx
	assertAny(nil, ctx.SetAttribute("country", "US"), t)
	assertAny(2, treatmentPeek(t, &copied, "exp"), t)
	assertAny(2, treatment(t, ctx, "exp"), t)
	assertAny(true, expectExposures(t, ctx, 1)[0].RuleOverride, t)
}

// GetVariableExperiment after Clock_ is replaced reads IndexVariables_ under
// DataLock too (run with -race).
func TestReplacedClockGetVariableExperimentDoesNotRaceSetData(t *testing.T) {
	var experiment = ruleExperiment("exp", "")
	var ctx, _ = newRuleContext(t, "production", withUnit(), experiment)
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var done = make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{experiment}})
		}
	}()
	for i := 0; i < 200; i++ {
		_, _ = ctx.GetVariableExperiment("color")
	}
	<-done
}

// A publish in flight when Clock_ is replaced is removed from the state that
// registered it, so a later Close does not wait for it for ever.
func TestReplacingClockWithPublishInFlightKeepsCloseFinishing(t *testing.T) {
	var publisher = &heldPublisher{}
	var ctx = createHeldPublisherContext(t, publisher, nil)
	assertAny(nil, ctx.Track("goal1", nil), t)
	var published, err = ctx.PublishAsync()
	assertAny(nil, err, t)
	// Replace Clock_ while the publish settles on another goroutine; with
	// -race this catches the settle path reading Clock_ again.
	var settled = make(chan struct{})
	go func() {
		publisher.settle(t, 0, nil)
		close(settled)
	}()
	ctx.Clock_ = internal.FixedClock{Millis_: 42}
	<-settled
	assertAny(nil, getWithTimeout(t, published), t)
	assertAny(0, ctx.inflightCount(), t)

	var closing, closeErr = ctx.CloseAsync()
	assertAny(nil, closeErr, t)
	assertAny(nil, getWithTimeout(t, closing), t)
	assertAny(true, ctx.IsClosed(), t)
}

// v1.1.1: when a copy of a literal makes the first SDK call, a later SetData
// on the original is still visible through the original.
func TestCopiedLiteralDoesNotCaptureTheOriginal(t *testing.T) {
	var boolValue = func(value bool) *atomic.Value {
		var result = &atomic.Value{}
		result.Store(value)
		return result
	}
	var count = &atomic.Value{}
	count.Store(int32(0))
	var original = Context{
		PublishDelay_: -1, RefreshInterval_: -1, VariableParser_: DefaultVariableParser{},
		Units_: map[string]string{}, Failed_: boolValue(false), Ready_: boolValue(false),
		DataLock: &sync.RWMutex{}, ContextLock_: &sync.RWMutex{},
		Index_: map[string]ExperimentVariables{}, IndexVariables_: map[interface{}]interface{}{},
		HashedUnits_: map[interface{}]interface{}{}, Assigners_: map[interface{}]interface{}{},
		AssignmentCache: map[string]Assignment{}, EventLock_: &sync.Mutex{},
		Overrides_: map[interface{}]interface{}{}, Cassignments_: map[interface{}]interface{}{},
		PendingCount_: count, Closing_: boolValue(false), Closed_: boolValue(false),
		Refreshing_: boolValue(false), TimeoutLock_: &sync.Mutex{},
		Clock_: internal.FixedClock{Millis_: 1},
	}
	var copied = original
	_ = copied.GetAssignment("missing")
	original.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{ruleExperiment("new", "")}})

	var _, found = original.GetExperiment("new")
	assertAny(true, found, t)
	var names, err = original.GetExperiments()
	assertAny(nil, err, t)
	assertAny([]string{"new"}, names, t)
}

// GetVariableAssignment through a copy of a context whose Clock_ was
// replaced resolves the key and the assignment against the same data.
func TestGetVariableAssignmentThroughStaleCopyUsesLiveData(t *testing.T) {
	var a = ruleExperiment("a", "")
	var ctx, _ = newRuleContext(t, "production", withUnit(), a)
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var copied = *ctx
	var b = ruleExperiment("b", "")
	b.Id = 8
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{b}})

	var assignment, err = copied.GetVariableAssignment("color")
	assertAny(nil, err, t)
	assertAny("b", assignment.Name, t)
	assertAny(8, assignment.Id, t)
}

func TestSharedVariableKeysKeepAllExperimentsAfterClockReplacement(t *testing.T) {
	var first, second = ruleExperiment("first", ""), ruleExperiment("second", "")
	first.Id, second.Id = 1, 2
	var ctx, _ = newRuleContext(t, "production", withUnit(), second, first)
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var all, err = ctx.GetVariableKeysAll()
	assertAny(nil, err, t)
	assertAny([]string{"first", "second"}, all["color"], t)
	var one, oneErr = ctx.GetVariableKeys()
	assertAny(nil, oneErr, t)
	assertAny("first", one["color"], t)
}

func TestContextLiteralVariableKeysReadCurrentLegacyIndex(t *testing.T) {
	var ready = &atomic.Value{}
	ready.Store(true)
	var closing = &atomic.Value{}
	closing.Store(false)
	var closed = &atomic.Value{}
	closed.Store(false)
	var five = ExperimentVariables{Data: jsonmodels.Experiment{Id: 5, Name: "five"}}
	var six = ExperimentVariables{Data: jsonmodels.Experiment{Id: 6, Name: "six"}}
	for _, lock := range []*sync.RWMutex{nil, {}} {
		var manual = Context{Ready_: ready, Closing_: closing, Closed_: closed, DataLock: &sync.RWMutex{},
			ContextLock_: lock, IndexVariables_: map[interface{}]interface{}{"color": five}}
		var keys, err = manual.GetVariableKeys()
		assertAny(nil, err, t)
		assertAny(map[string]string{"color": "five"}, keys, t)
		var all, allErr = manual.GetVariableKeysAll()
		assertAny(nil, allErr, t)
		assertAny(map[string][]string{"color": {"five"}}, all, t)

		manual.IndexVariables_["color"] = six
		manual.IndexVariables_["size"] = five
		keys, _ = manual.GetVariableKeys()
		assertAny(map[string]string{"color": "six", "size": "five"}, keys, t)
		all, _ = manual.GetVariableKeysAll()
		assertAny(map[string][]string{"color": {"six"}, "size": {"five"}}, all, t)
	}
}

type keysInEvaluation struct {
	ctx  *Context
	keys map[string]string
	all  map[string][]string
}

func (d *keysInEvaluation) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	if d.ctx != nil && d.keys == nil {
		d.keys, _ = d.ctx.GetVariableKeys()
		d.all, _ = d.ctx.GetVariableKeysAll()
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// v1.1.1: a literal's variable keys read its current IndexVariables_, also
// from inside an audience evaluation.
func TestContextLiteralVariableKeysInsideEvaluationAreCurrent(t *testing.T) {
	var boolValue = func(value bool) *atomic.Value {
		var result = &atomic.Value{}
		result.Store(value)
		return result
	}
	var count = &atomic.Value{}
	count.Store(int32(0))
	var five = ExperimentVariables{Data: jsonmodels.Experiment{Id: 5, Name: "five"}}
	var six = ExperimentVariables{Data: jsonmodels.Experiment{Id: 6, Name: "six"}}
	var d = &keysInEvaluation{}
	var manual = Context{
		PublishDelay_: -1, RefreshInterval_: -1, VariableParser_: DefaultVariableParser{},
		AudienceMatcher_: AudienceMatcher{d},
		Units_:           map[string]string{"session_id": ruleUnit}, Failed_: boolValue(false), Ready_: boolValue(true),
		DataLock: &sync.RWMutex{}, ContextLock_: &sync.RWMutex{},
		IndexVariables_: map[interface{}]interface{}{"color": five},
		HashedUnits_:    map[interface{}]interface{}{}, Assigners_: map[interface{}]interface{}{},
		AssignmentCache: map[string]Assignment{}, EventLock_: &sync.Mutex{},
		Overrides_: map[interface{}]interface{}{}, Cassignments_: map[interface{}]interface{}{},
		PendingCount_: count, Closing_: boolValue(false), Closed_: boolValue(false),
		Refreshing_: boolValue(false), TimeoutLock_: &sync.Mutex{},
		Clock_: internal.FixedClock{Millis_: 1},
	}
	manual.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", appFilter, true)}})
	manual.IndexVariables_ = map[interface{}]interface{}{"color": five}
	_, _ = manual.GetVariableKeys()
	manual.IndexVariables_["size"] = six
	d.ctx = &manual
	_, _ = manual.PeekTreatment("exp_app")
	assertAny(map[string]string{"color": "five", "size": "six"}, d.keys, t)
	assertAny(map[string][]string{"color": {"five"}, "size": {"six"}}, d.all, t)
}

// An SDK-created context reads its own index even after Clock_ is replaced,
// so writes to IndexVariables_ do not change its variable keys (CHANGELOG).
func TestReplacedClockVariableKeysIgnoreLegacyIndexWrites(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp", ""))
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	ctx.DataLock.Lock()
	ctx.IndexVariables_["injected"] = ExperimentVariables{Data: jsonmodels.Experiment{Name: "injected"}}
	ctx.DataLock.Unlock()
	var keys, err = ctx.GetVariableKeys()
	assertAny(nil, err, t)
	if _, found := keys["injected"]; found {
		t.Errorf("GetVariableKeys sees an IndexVariables_ write: %v", keys)
	}
	var all, allErr = ctx.GetVariableKeysAll()
	assertAny(nil, allErr, t)
	if _, found := all["injected"]; found {
		t.Errorf("GetVariableKeysAll sees an IndexVariables_ write: %v", all)
	}
	assertAny("exp", keys["color"], t)
}

func TestAttributeWrittenThroughCopyIsEvaluated(t *testing.T) {
	var ctx, _ = newRuleContext(t, "production", withUnit(),
		ruleExperiment("exp", rulesJSON(`{"type":"assign","variant":2,"conditions":{"eq":[{"var":"country"},{"value":"US"}]}}`)))
	assertAny(0, treatmentPeek(t, ctx, "exp"), t)
	var copied = *ctx
	assertAny(nil, copied.SetAttribute("country", "US"), t)
	assertAny("US", copied.GetAttribute("country"), t)
	assertAny("US", copied.GetAttributes()["country"], t)
	assertAny(2, treatmentPeek(t, &copied, "exp"), t)
	assertAny(2, treatmentPeek(t, ctx, "exp"), t)
	assertAny("US", ctx.GetAttribute("country"), t)
}

// queueingDeserializer queues an exposure it already holds from inside an
// audience parse.
type queueingDeserializer struct {
	ctx      *Context
	retained *Assignment
}

func (d *queueingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	if d.ctx != nil && d.retained != nil {
		var retained = d.retained
		d.retained = nil
		d.ctx.QueueExposure(retained)
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

func TestQueueExposureFromInsideEvaluationDoesNotDeadlock(t *testing.T) {
	for _, replace := range []bool{false, true} {
		var d = &queueingDeserializer{}
		var client = &systemAttrClient{application: "website", environment: "production"}
		var abs = Create(ABsmartlyConfig{Client_: client,
			ContextDataProvider_: DefaultContextDataProvider{client_: client},
			ContextEventHandler_: DefaultContextEventHandler{client_: client}, AudienceDeserializer_: d})
		var config = CreateDefaultContextConfig()
		config.Units_, config.PublishDelay_, config.RefreshInterval_ = withUnit(), -1, -1
		var ctx = abs.CreateContextWith(config, jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
			ruleExperiment("rule", rulesJSON(`{"type":"assign","variant":2}`)),
			audienceExperiment("exp_app", appFilter, true)}})
		ctx.WaitUntilReady()
		if replace {
			ctx.Clock_ = internal.FixedClock{Millis_: 1}
		}
		d.ctx, d.retained = ctx, ctx.GetAssignment("rule")
		var done = make(chan int, 1)
		go func() {
			var variant, _ = reparseAudience(ctx)
			done <- variant
		}()
		select {
		case variant := <-done:
			assertAny(1, variant, t)
		case <-time.After(5 * time.Second):
			t.Fatalf("PeekTreatment deadlocked when the deserializer queued an exposure (Clock_ replaced: %v)", replace)
		}
		var exposures = expectExposures(t, ctx, 1)
		assertAny("rule", exposures[0].Name, t)
		assertAny(true, exposures[0].RuleOverride, t)
	}
}

// Data installed through a value copy is what every read through that copy,
// and through the original, returns.
func TestCopySetDataIsReadBackConsistently(t *testing.T) {
	for _, replace := range []bool{false, true} {
		var before, after = ruleExperiment("before", ""), ruleExperiment("after", "")
		after.Id = 9
		after.CustomFieldValues = []jsonmodels.CustomFieldValue{{Name: "f", Type: "string", Value: "v"}}
		after.Variants[1].Config = `{"after_key":1}`
		var ctx, _ = newRuleContext(t, "production", withUnit(), before)
		if replace {
			// Replacing the clock before copying leaves the copy without a
			// state carrier; it still acts on the original's state.
			ctx.Clock_ = internal.FixedClock{Millis_: 1}
		}
		var copied = *ctx
		copied.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{after}})
		checkCopySetData(t, replace, &copied, ctx)
	}
}

func checkCopySetData(t *testing.T, replace bool, copied *Context, ctx *Context) {
	t.Helper()
	for name, c := range map[string]*Context{"copy": copied, "original": ctx} {
		var label = fmt.Sprintf("%s (Clock_ replaced: %v)", name, replace)
		assertAny(9, c.GetAssignment("after").Id, t)
		if experiment, found := c.GetExperiment("after"); !found || experiment.Data.Id != 9 {
			t.Errorf("%s: GetExperiment does not see the installed data", label)
		}
		if _, found := c.GetExperiment("before"); found {
			t.Errorf("%s: GetExperiment still sees the replaced data", label)
		}
		if experiment, err := c.GetVariableExperiment("after_key"); err != nil || experiment.Data.Id != 9 {
			t.Errorf("%s: GetVariableExperiment = %v, %v", label, experiment.Data.Id, err)
		}
		var names, err = c.GetExperiments()
		assertAny(nil, err, t)
		assertAny([]string{"after"}, names, t)
		var data, dataErr = c.GetData()
		assertAny(nil, dataErr, t)
		assertAny(1, len(data.Experiments), t)
		assertAny("after", data.Experiments[0].Name, t)
		var keys, keysErr = c.GetCustomFieldValueKeys()
		assertAny(nil, keysErr, t)
		assertAny([]string{"f"}, keys, t)
		assertAny("v", c.GetCustomFieldValue("after", "f"), t)
		assertAny("string", c.GetCustomFieldValueType("after", "f"), t)
	}
}

// A waiter that arrives while a failed initial load is being reported gets
// the load error: the callback settles the ready future before clearing
// ReadyFuture_. With the clear first, a waiter in between would find no
// future and Ready_ already set, and would be told the context is ready. A
// listener on the ready future runs as it settles and checks the future is
// still installed then.
func TestFailedLoadSettlesReadyFutureBeforeClearingIt(t *testing.T) {
	for _, failure := range []string{"provider", "type", "panic"} {
		setUp()
		var config = CreateDefaultContextConfig()
		config.Units_ = units
		config.PublishDelay_ = -1
		config.RefreshInterval_ = 0
		var pending, settle = future.New()
		var parser VariableParser = variableParser
		if failure == "panic" {
			parser = panickingParser{}
		}
		var ctx = createTrackedContext(clock, config, pending, dataProvider, eventHandler, nil, parser, audienceMatcher)
		var ready = ctx.getReadyFuture()
		var installedAtSettlement = make(chan bool, 1)
		ready.Listen(func(_ future.Value, _ error) {
			installedAtSettlement <- ctx.getReadyFuture() != nil
		})
		switch failure {
		case "provider":
			settle(nil, errors.New("load failed"))
		case "type":
			settle("not context data", nil)
		case "panic":
			settle(data, nil)
		}
		if !<-installedAtSettlement {
			t.Errorf("%s failure: ReadyFuture_ was cleared before the ready future settled", failure)
		}
		var _, err = ready.Get(context.Background())
		if err == nil {
			t.Errorf("%s failure: ready future settled without the load error", failure)
		}
		assertAny(true, ctx.IsFailed(), t)
		if ctx.getReadyFuture() != nil {
			t.Errorf("%s failure: ReadyFuture_ still installed after settlement", failure)
		}
	}
}

// loggingPanicParser logs a diagnostic through the context it is given, then
// panics.
type loggingPanicParser struct{}

func (loggingPanicParser) Parse(ctx Context, experimentName string, _ string, _ string) map[string]interface{} {
	ctx.LogEvent(Exposure, "diagnostic from "+experimentName)
	panic("parser panic")
}

// A failed initial load delivers the diagnostics its parser logged before
// the Error event, as a failed refresh does.
func TestFailedInitialLoadDeliversDiagnosticsBeforeTheError(t *testing.T) {
	setUp()
	var logger = &MockEventLogger{}
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.PublishDelay_ = -1
	config.RefreshInterval_ = 0
	config.EventLogger_ = logger
	var pending, settle = future.New()
	var ctx = createTrackedContext(clock, config, pending, dataProvider, eventHandler, logger, loggingPanicParser{}, audienceMatcher)
	settle(data, nil)
	ctx.WaitUntilReady()
	assertAny(true, ctx.IsFailed(), t)
	var kinds []EventType
	for _, e := range logger.GetEvents() {
		kinds = append(kinds, e.EventType)
	}
	assertAny([]EventType{Exposure, Error}, kinds, t)
}

// Exposures queued through a copy of a context whose Clock_ was replaced are
// queued on the original, through QueueExposure and through GetTreatment.
func TestCopyAfterClockReplacementQueuesExposuresOnTheOriginal(t *testing.T) {
	for _, path := range []string{"QueueExposure", "GetTreatment"} {
		var ctx, _ = newRuleContext(t, "production", withUnit(), ruleExperiment("exp", ""))
		ctx.Clock_ = internal.FixedClock{Millis_: 1}
		var copied = *ctx
		if path == "QueueExposure" {
			copied.QueueExposure(copied.GetAssignment("exp"))
		} else {
			var _, _ = copied.GetTreatment("exp")
		}
		// The copy shares the pending count; the queue itself is a field, so
		// the exposure must be in the original's.
		ctx.EventLock_.Lock()
		var queued = len(ctx.Exposures_)
		ctx.EventLock_.Unlock()
		assertAny(1, queued, t)
	}
}

// GetExperiment and GetVariableExperiment never take ContextLock_, as in
// v1.1.1: a caller holding it (for reading or writing) still gets an answer,
// for a literal, an SDK-created context and one whose Clock_ was replaced.
func TestExperimentGettersDoNotTakeContextLock(t *testing.T) {
	var experiment = ruleExperiment("exp", "")
	experiment.Variants[1].Config = `{"key":1}`
	var sdkCtx, _ = newRuleContext(t, "production", withUnit(), experiment)
	var replaced, _ = newRuleContext(t, "production", withUnit(), experiment)
	replaced.Clock_ = internal.FixedClock{Millis_: 1}
	var literal = audienceLiteral(experiment)
	literal.IndexVariables_ = map[interface{}]interface{}{"key": literal.Index_["exp"]}
	for name, c := range map[string]*Context{"sdk": sdkCtx, "replaced": replaced, "literal": literal} {
		for _, write := range []bool{false, true} {
			if write {
				c.ContextLock_.Lock()
			} else {
				c.ContextLock_.RLock()
			}
			var done = make(chan bool, 1)
			go func() {
				var _, found = c.GetExperiment("exp")
				var _, err = c.GetVariableExperiment("key")
				done <- found && err == nil
			}()
			select {
			case ok := <-done:
				assertAny(true, ok, t)
			case <-time.After(5 * time.Second):
				t.Fatalf("%s: an experiment getter took ContextLock_ (write: %v)", name, write)
			}
			if write {
				c.ContextLock_.Unlock()
			} else {
				c.ContextLock_.RUnlock()
			}
		}
	}
}

// SetData through a copy of a context whose Clock_ was replaced, racing
// reads through the copy and the original, is race-free (run with -race).
func TestCopySetDataRacingReadsAfterClockReplacement(t *testing.T) {
	var before, after = ruleExperiment("before", ""), ruleExperiment("after", "")
	var ctx, _ = newRuleContext(t, "production", withUnit(), before)
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var copied = *ctx
	var stop int32
	var wg sync.WaitGroup
	for _, c := range []*Context{&copied, ctx} {
		var c = c
		wg.Add(1)
		go func() {
			defer wg.Done()
			for atomic.LoadInt32(&stop) == 0 {
				var _, _ = c.GetExperiment("after")
				var _, _ = c.GetVariableExperiment("key")
				var _, _ = c.PeekTreatment("after")
			}
		}()
	}
	for i := 0; i < 20; i++ {
		var next = after
		next.Iteration = i + 1
		copied.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{next}})
	}
	atomic.StoreInt32(&stop, 1)
	wg.Wait()
	var experiment, found = copied.GetExperiment("after")
	assertAny(true, found, t)
	assertAny(20, experiment.Data.Iteration, t)
	copied.DataLock.RLock()
	defer copied.DataLock.RUnlock()
	assertAny(20, copied.Data_.Experiments[0].Iteration, t)
	var _, hasFields = copied.ContextCustomFields_["after"]
	assertAny(true, hasFields, t)
}

// An assignment built with a failed parse is evaluated again once a different
// deserializer is installed.
func TestFailedAudienceParseIsRetriedWithANewDeserializer(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":false}]}`, true))
	ctx.AudienceMatcher_ = AudienceMatcher{&recoveringDeserializer{}}
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	ctx.AudienceMatcher_ = AudienceMatcher{DefaultAudienceDeserializer{}}
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	assertAny(0, treatmentPeek(t, ctx, "exp_app"), t)
}

// endlessDeserializer installs a new audience text on every parse, then parses
// normally, so a read gives up after maxAudienceAttempts.
type endlessDeserializer struct {
	ctx    *Context
	calls  int32
	stable int32
}

func (d *endlessDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	var n = atomic.AddInt32(&d.calls, 1)
	if atomic.LoadInt32(&d.stable) == 0 {
		d.ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
			audienceExperiment("exp_app", fmt.Sprintf(`{"filter":[{"value":false},{"value":%d}]}`, n), true)}})
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// After giving up on a read whose data changed during every parse, the next
// revalidation parses the installed audience and evaluates it.
func TestGivenUpAudienceIsParsedAgainOnRevalidation(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":false}]}`, true))
	var d = &endlessDeserializer{ctx: ctx}
	ctx.AudienceMatcher_ = AudienceMatcher{d}
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	atomic.StoreInt32(&d.stable, 1)
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	assertAny(0, treatmentPeek(t, ctx, "exp_app"), t)
}

// panicOnceDeserializer panics on its first parse, then parses normally.
type panicOnceDeserializer struct{ calls int32 }

func (d *panicOnceDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	if atomic.AddInt32(&d.calls, 1) == 1 {
		panic("temporary")
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// A panicked parse is local to its experiment: another experiment with the
// same audience text asks the recovered deserializer and evaluates its audience.
func TestTemporaryAudiencePanicIsNotShared(t *testing.T) {
	var exclude = `{"filter":[{"value":false}]}`
	var first, second = audienceExperiment("first", exclude, true), audienceExperiment("second", exclude, true)
	second.Id = 2
	var d = &panicOnceDeserializer{}
	var ctx, _ = newSystemAttrContext("website", "production", first, second)
	ctx.AudienceMatcher_ = AudienceMatcher{d}
	assertAny(1, treatmentPeek(t, ctx, "first"), t)
	assertAny(0, treatmentPeek(t, ctx, "second"), t)
	assertAny(int32(2), atomic.LoadInt32(&d.calls), t)
}

type callbackEventLogger func(Context, EventType, interface{})

func (l callbackEventLogger) HandleEvent(c Context, event EventType, data interface{}) {
	l(c, event, data)
}

// A panic logger may read the failing experiment without recursion, but a
// different experiment must parse the same text with the recovered deserializer.
func TestAudiencePanicLoggerReadsDifferentExperiment(t *testing.T) {
	var exclude = `{"filter":[{"value":false}]}`
	var first, second = audienceExperiment("first", exclude, true), audienceExperiment("second", exclude, true)
	second.Id = 2
	var ctx, _ = newSystemAttrContext("website", "production", first, second)
	var d = &panicOnceDeserializer{}
	ctx.AudienceMatcher_ = AudienceMatcher{d}
	var seen = make(chan []int, 1)
	var errorsLogged int32
	ctx.EventLogger_ = callbackEventLogger(func(c Context, event EventType, _ interface{}) {
		if event != Error {
			return
		}
		if atomic.AddInt32(&errorsLogged, 1) > 2 {
			return // Bound a regression that recursively logs the same panic.
		}
		var own, _ = c.PeekTreatment("first")
		var other, _ = c.PeekTreatment("second")
		seen <- []int{own, other}
	})
	var done = make(chan int, 1)
	go func() { v, _ := ctx.PeekTreatment("first"); done <- v }()
	select {
	case variant := <-done:
		assertAny(1, variant, t)
	case <-time.After(5 * time.Second):
		t.Fatal("panic logger reading treatments did not return")
	}
	select {
	case variants := <-seen:
		assertAny([]int{1, 0}, variants, t)
	case <-time.After(5 * time.Second):
		t.Fatal("panic logger did not read both experiments")
	}
	assertAny(int32(1), atomic.LoadInt32(&errorsLogged), t)
	assertAny(int32(2), atomic.LoadInt32(&d.calls), t)
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	assertAny(1, treatmentPeek(t, ctx, "first"), t)
	assertAny(0, treatmentPeek(t, ctx, "second"), t)
	assertAny(int32(2), atomic.LoadInt32(&d.calls), t)
}

// nilPanicOnceDeserializer panics with nil on its first parse, then parses
// normally. On Go before 1.21, recover returns nil for it.
type nilPanicOnceDeserializer struct{ calls int32 }

func (d *nilPanicOnceDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	if atomic.AddInt32(&d.calls, 1) == 1 {
		panic(nil)
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// A panic(nil) counts as a failed parse, local to the experiment that hit it:
// another experiment with the same text asks the recovered deserializer, and
// the panic is reported.
func TestNilPanicAudienceParseIsNotShared(t *testing.T) {
	var exclude = `{"filter":[{"value":false}]}`
	var first, second = audienceExperiment("first", exclude, true), audienceExperiment("second", exclude, true)
	second.Id = 2
	var ctx, _ = newSystemAttrContext("website", "production", first, second)
	var d = &nilPanicOnceDeserializer{}
	ctx.AudienceMatcher_ = AudienceMatcher{d}
	var errorsLogged int32
	ctx.EventLogger_ = callbackEventLogger(func(_ Context, event EventType, _ interface{}) {
		if event == Error {
			atomic.AddInt32(&errorsLogged, 1)
		}
	})
	assertAny(1, treatmentPeek(t, ctx, "first"), t)
	assertAny(0, treatmentPeek(t, ctx, "second"), t)
	assertAny(int32(2), atomic.LoadInt32(&d.calls), t)
	assertAny(int32(1), atomic.LoadInt32(&errorsLogged), t)
}

// A logger reporting a panic that installs a replacement experiment under the
// same name (new id and iteration, same text) and reads it does not hand the
// replacement the old run's failed parse: the recovered deserializer decides.
func TestReplacementExperimentDoesNotInheritPanicFallback(t *testing.T) {
	var exclude = `{"filter":[{"value":false}]}`
	var original = audienceExperiment("exp_app", exclude, true)
	var replacement = original
	replacement.Id, replacement.Iteration = 2, 2
	var ctx, _ = newSystemAttrContext("website", "production", original)
	var d = &panicOnceDeserializer{}
	ctx.AudienceMatcher_ = AudienceMatcher{d}
	var inside = make(chan int, 1)
	var logged int32
	ctx.EventLogger_ = callbackEventLogger(func(c Context, event EventType, _ interface{}) {
		if event != Error || atomic.AddInt32(&logged, 1) > 1 {
			return
		}
		c.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{replacement}})
		var v, _ = c.PeekTreatment("exp_app")
		inside <- v
	})
	var done = make(chan int, 1)
	go func() { v, _ := ctx.PeekTreatment("exp_app"); done <- v }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the read did not return")
	}
	select {
	case v := <-inside:
		assertAny(0, v, t)
	case <-time.After(5 * time.Second):
		t.Fatal("the logger did not read the replacement")
	}
	assertAny(0, treatmentPeek(t, ctx, "exp_app"), t)
	assertAny(int32(2), atomic.LoadInt32(&d.calls), t)
}

// A recovered parse from inside the logger must not replace the original
// build's failed outcome, even when no nested read has cached that assignment.
func TestAudiencePanicLoggerRecoveryKeepsOriginalFallback(t *testing.T) {
	var exclude = `{"filter":[{"value":false}]}`
	var first, second = audienceExperiment("first", exclude, true), audienceExperiment("second", exclude, true)
	second.Id = 2
	var ctx, _ = newSystemAttrContext("website", "production", first, second)
	var d = &panicOnceDeserializer{}
	ctx.AudienceMatcher_ = AudienceMatcher{d}
	var seen = make(chan int, 1)
	ctx.EventLogger_ = callbackEventLogger(func(c Context, event EventType, _ interface{}) {
		if event == Error {
			var other, _ = c.PeekTreatment("second")
			seen <- other
		}
	})
	var done = make(chan int, 1)
	go func() { v, _ := ctx.PeekTreatment("first"); done <- v }()
	select {
	case variant := <-done:
		assertAny(1, variant, t)
	case <-time.After(5 * time.Second):
		t.Fatal("panic logger recovery did not return")
	}
	select {
	case variant := <-seen:
		assertAny(0, variant, t)
	case <-time.After(5 * time.Second):
		t.Fatal("panic logger did not read the recovered experiment")
	}
	assertAny(int32(2), atomic.LoadInt32(&d.calls), t)
	assertAny(0, len(ctx.state().audiencePanics), t)
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	assertAny(1, treatmentPeek(t, ctx, "first"), t)
	assertAny(int32(2), atomic.LoadInt32(&d.calls), t)
}

// A document with no usable filter is a local failure, not a shared success.
func TestUnusableAudienceDocumentIsNotShared(t *testing.T) {
	for _, document := range []string{`{}`, `null`, `{"filter":null}`, `{"filter":false}`, `{"filter":5}`, `{"filter":"invalid"}`} {
		t.Run(document, func(t *testing.T) {
			var exclude = `{"filter":[{"value":false}]}`
			var first, second = audienceExperiment("first", exclude, true), audienceExperiment("second", exclude, true)
			second.Id = 2
			var ctx, _ = newSystemAttrContext("website", "production", first, second)
			var calls int32
			ctx.AudienceMatcher_ = AudienceMatcher{funcDeserializer(func(b []byte, offset, length int) (map[string]interface{}, error) {
				if atomic.AddInt32(&calls, 1) == 1 {
					return DefaultAudienceDeserializer{}.Deserialize([]byte(document), 0, len(document))
				}
				return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
			})}
			assertAny(1, treatmentPeek(t, ctx, "first"), t)
			assertAny(0, treatmentPeek(t, ctx, "second"), t)
			assertAny(int32(2), atomic.LoadInt32(&calls), t)
			assertAny(nil, ctx.SetAttribute("x", 1), t)
			assertAny(1, treatmentPeek(t, ctx, "first"), t)
			assertAny(0, treatmentPeek(t, ctx, "second"), t)
			assertAny(int32(2), atomic.LoadInt32(&calls), t)
		})
	}
}

type controlledRefreshProvider struct {
	result *future.Future
	calls  int32
}

func (p *controlledRefreshProvider) GetContextData() *future.Future {
	atomic.AddInt32(&p.calls, 1)
	return p.result
}

// A completion logger that synchronously refreshes again leaves a joined
// copy's reads consistent when the copy's refresh reports success.
func TestJoinedCopyReadsAgreeAfterNestedRefresh(t *testing.T) {
	var a, b, before = ruleExperiment("a", ""), ruleExperiment("b", ""), ruleExperiment("before", "")
	var ctx, _ = newRuleContext(t, "production", withUnit(), before)
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var copied = *ctx
	var pendingA, settleA = future.New()
	var provider = &controlledRefreshProvider{result: pendingA}
	ctx.DataProvider_ = provider
	var nested int32
	ctx.EventLogger_ = callbackEventLogger(func(c Context, event EventType, _ interface{}) {
		if event != Refresh || atomic.AddInt32(&nested, 1) > 1 {
			return
		}
		var doneB, settleB = future.New()
		settleB(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{b}}, nil)
		provider.result = doneB
		ctx.Refresh()
	})
	var started = ctx.RefreshAsync()
	var joined = copied.RefreshAsync()
	settleA(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{a}}, nil)
	assertAny(nil, getWithTimeout(t, started), t)
	assertAny(nil, getWithTimeout(t, joined), t)
	var data, _ = copied.GetData()
	assertAny(1, len(data.Experiments), t)
	assertAny("b", data.Experiments[0].Name, t)
	if _, found := copied.GetExperiment("b"); !found {
		t.Error("the copy's GetExperiment misses the data GetData reports")
	}
	if _, found := copied.GetExperiment("a"); found {
		t.Error("the copy's GetExperiment still finds replaced data")
	}
}

// An earlier listener must be able to wait for a joining copy, independently
// of listener registration order. All copies adopt before success is visible.
func TestOriginalRefreshListenerCanWaitForJoiningCopies(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("error=%v", fail), func(t *testing.T) {
			var before, after = ruleExperiment("before", ""), ruleExperiment("after", "")
			var ctx, _ = newRuleContext(t, "production", withUnit(), before)
			ctx.Clock_ = internal.FixedClock{Millis_: 1}
			var first, second = *ctx, *ctx
			var pending, settle = future.New()
			var provider = &controlledRefreshProvider{result: pending}
			ctx.DataProvider_ = provider
			var expected error
			if fail {
				expected = errors.New("refresh failed")
			}
			var started = ctx.RefreshAsync()
			var joined, another *future.Future
			var seen = make(chan error, 1)
			started.Listen(func(_ future.Value, err error) {
				var wait, cancel = context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if _, got := joined.Get(wait); got != expected {
					seen <- fmt.Errorf("joined Get = %v, want %v", got, expected)
					return
				}
				if _, got := another.Get(wait); got != expected {
					seen <- fmt.Errorf("second joined Get = %v, want %v", got, expected)
					return
				}
				for _, copy := range []*Context{&first, &second} {
					var _, found = copy.GetExperiment("after")
					if found == fail {
						seen <- fmt.Errorf("copy adopted=%v, failure=%v", found, fail)
						return
					}
				}
				seen <- err
			})
			joined = first.RefreshAsync()
			another = second.RefreshAsync()
			assertAny(false, joined.Ready(), t)
			var completed = make(chan struct{})
			go func() {
				defer close(completed)
				settle(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{after}}, expected)
			}()
			select {
			case got := <-seen:
				assertAny(expected, got, t)
			case <-time.After(5 * time.Second):
				t.Fatal("original refresh listener did not return")
			}
			select {
			case <-completed:
			case <-time.After(5 * time.Second):
				t.Fatal("refresh completion did not return")
			}
			assertAny(expected, getWithTimeout(t, joined), t)
			assertAny(expected, getWithTimeout(t, another), t)
			assertAny(int32(1), atomic.LoadInt32(&provider.calls), t)
		})
	}
}

// Adoption and operation cleanup precede logger reentry, on success and error.
// Reentry must not wait for the operation whose event it is delivering.
func TestRefreshCompletionAllowsLoggerReentryWithJoiningCopy(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("error=%v", fail), func(t *testing.T) {
			var before, after, latest = ruleExperiment("before", ""), ruleExperiment("after", ""), ruleExperiment("latest", "")
			var ctx, _ = newRuleContext(t, "production", withUnit(), before)
			ctx.Clock_ = internal.FixedClock{Millis_: 1}
			var copied = *ctx
			var pending, settle = future.New()
			ctx.DataProvider_ = &controlledRefreshProvider{result: pending}
			var expected error
			var event = Refresh
			if fail {
				expected, event = errors.New("refresh failed"), Error
			}
			var entered int32
			var seen = make(chan error, 1)
			ctx.EventLogger_ = callbackEventLogger(func(_ Context, kind EventType, _ interface{}) {
				if kind != event || !atomic.CompareAndSwapInt32(&entered, 0, 1) {
					return
				}
				var name = "after"
				if fail {
					name = "before"
				}
				if _, found := copied.GetExperiment(name); !found {
					seen <- fmt.Errorf("logger did not observe copy's %s data", name)
					return
				}
				ctx.DataProvider_ = syncDataProvider{jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{latest}}}
				var nested = ctx.RefreshAsync()
				var wait, cancel = context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				var _, err = nested.Get(wait)
				seen <- err
			})
			var started = ctx.RefreshAsync()
			var joined = copied.RefreshAsync()
			var completed = make(chan struct{})
			go func() {
				defer close(completed)
				settle(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{after}}, expected)
			}()
			select {
			case err := <-seen:
				assertAny(nil, err, t)
			case <-time.After(5 * time.Second):
				t.Fatal("refresh logger reentry did not return")
			}
			select {
			case <-completed:
			case <-time.After(5 * time.Second):
				t.Fatal("outer refresh completion did not return")
			}
			assertAny(expected, getWithTimeout(t, started), t)
			assertAny(expected, getWithTimeout(t, joined), t)
			var _, found = ctx.GetExperiment("latest")
			assertAny(true, found, t)
		})
	}
}

// A registrant that already captured an operation may reach it after completion.
func TestRefreshOperationLateCopyRegistration(t *testing.T) {
	for _, success := range []bool{false, true} {
		var before, after = ruleExperiment("before", ""), ruleExperiment("after", "")
		var ctx, _ = newRuleContext(t, "production", withUnit(), before)
		ctx.Clock_ = internal.FixedClock{Millis_: 1}
		var copied = *ctx
		var op = &refreshOperation{receivers: map[*Context]struct{}{}}
		ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{after}})
		op.complete(ctx, success)
		op.join(&copied, ctx)
		var _, found = copied.GetExperiment("after")
		assertAny(success, found, t)
		assertAny(0, len(op.receivers), t)
	}
}

// Registering copies concurrently with completion cannot lose an adoption.
func TestRefreshOperationConcurrentCopyRegistration(t *testing.T) {
	var before, after = ruleExperiment("before", ""), ruleExperiment("after", "")
	var ctx, _ = newRuleContext(t, "production", withUnit(), before)
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var copies = make([]Context, 32)
	for i := range copies {
		copies[i] = *ctx
	}
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{after}})
	var op = &refreshOperation{receivers: map[*Context]struct{}{}}
	var start = make(chan struct{})
	var wg sync.WaitGroup
	for i := range copies {
		var receiver = &copies[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			op.join(receiver, ctx)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		op.complete(ctx, true)
	}()
	var done = make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	close(start)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent copy registration did not return")
	}
	for i := range copies {
		var _, found = copies[i].GetExperiment("after")
		assertAny(true, found, t)
	}
}

// A copy of a clock-replaced context that joins a refresh started elsewhere
// reads the refreshed experiments once its joined refresh succeeds.
func TestCopyJoiningRefreshAfterClockReplacementIsReadBack(t *testing.T) {
	var before, after = ruleExperiment("before", ""), ruleExperiment("after", "")
	before.Variants[1].Config = `{"before_key":1}`
	after.Id = 9
	after.Variants[1].Config = `{"after_key":1}`
	var p = &pendingProvider{release: make(chan struct{}),
		data: jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{after}}}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var ctx = Create(ABsmartlyConfig{Client_: client, ContextDataProvider_: p,
		ContextEventHandler_: DefaultContextEventHandler{client_: client}}).CreateContextWith(cc,
		jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{before}})
	ctx.WaitUntilReady()
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var copied = *ctx
	var started = ctx.RefreshAsync()
	var joined = copied.RefreshAsync()
	assertAny(false, joined.Ready(), t)
	var seenAtSettle = make(chan bool, 1)
	joined.Listen(func(future.Value, error) {
		var _, found = copied.GetExperiment("after")
		seenAtSettle <- found
	})
	close(p.release)
	assertAny(nil, getWithTimeout(t, started), t)
	assertAny(nil, getWithTimeout(t, joined), t)
	select {
	case seen := <-seenAtSettle:
		assertAny(true, seen, t)
	case <-time.After(5 * time.Second):
		t.Fatal("joined refresh listener did not run")
	}
	if _, found := copied.GetExperiment("before"); found {
		t.Error("the copy still finds an experiment the joined refresh removed")
	}
	if experiment, found := copied.GetExperiment("after"); !found || experiment.Data.Id != 9 {
		t.Error("the copy does not find the experiment the joined refresh installed")
	}
	if experiment, err := copied.GetVariableExperiment("after_key"); err != nil || experiment.Data.Id != 9 {
		t.Errorf("GetVariableExperiment through the copy = %v, %v", experiment.Data.Id, err)
	}
	assertAny(9, copied.GetAssignment("after").Id, t)
}

// SetDataFailed through a copy of a clock-replaced context clears the copy's
// own experiment and variable lookups too.
func TestCopySetDataFailedAfterClockReplacementClearsLookups(t *testing.T) {
	var before = ruleExperiment("before", "")
	before.Variants[1].Config = `{"before_key":1}`
	var ctx, _ = newRuleContext(t, "production", withUnit(), before)
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var copied = *ctx
	copied.SetDataFailed(errors.New("load failed"))
	assertAny(true, copied.IsFailed(), t)
	for name, c := range map[string]*Context{"copy": &copied, "original": ctx} {
		if _, found := c.GetExperiment("before"); found {
			t.Errorf("%s: GetExperiment still finds invalidated data", name)
		}
		if _, err := c.GetVariableExperiment("before_key"); err == nil {
			t.Errorf("%s: GetVariableExperiment still finds invalidated data", name)
		}
	}
}

// recoveringDeserializer fails its first parse, then parses normally.
type recoveringDeserializer struct{ calls int32 }

func (d *recoveringDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	if atomic.AddInt32(&d.calls, 1) == 1 {
		return nil, errors.New("temporary")
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// A temporary parse failure for one experiment is not reused for another
// with the same audience text: the second asks the recovered deserializer
// and evaluates its strict audience, as v1.1.1 did. The first keeps its
// own unparseable outcome through revalidation, without a new parse.
func TestTemporaryAudienceParseFailureIsNotShared(t *testing.T) {
	var exclude = `{"filter":[{"value":false}]}`
	var first, second = audienceExperiment("first", exclude, true), audienceExperiment("second", exclude, true)
	second.Id = 2
	var d = &recoveringDeserializer{}
	var ctx, _ = newSystemAttrContext("website", "production", first, second)
	ctx.AudienceMatcher_ = AudienceMatcher{d}
	assertAny(1, treatmentPeek(t, ctx, "first"), t)
	assertAny(0, treatmentPeek(t, ctx, "second"), t)
	assertAny(int32(2), atomic.LoadInt32(&d.calls), t)
	var exposed = ctx.GetAssignment("first").Exposed
	assertAny(false, ctx.GetAssignment("first").AudienceMismatch, t)
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	assertAny(1, treatmentPeek(t, ctx, "first"), t)
	if ctx.GetAssignment("first").Exposed != exposed {
		t.Error("revalidating an assignment built with a failed parse rebuilt it")
	}
	assertAny(int32(2), atomic.LoadInt32(&d.calls), t)
}

// Refresh through a copy of a context whose Clock_ was replaced updates the
// experiments read through that copy, as SetData does.
func TestCopyRefreshAfterClockReplacementIsReadBack(t *testing.T) {
	var before, after = ruleExperiment("before", ""), ruleExperiment("after", "")
	after.Id = 9
	var p = &pendingProvider{release: make(chan struct{}),
		data: jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{after}}}
	close(p.release)
	var client = &systemAttrClient{application: "website", environment: "production"}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var ctx = Create(ABsmartlyConfig{Client_: client, ContextDataProvider_: p,
		ContextEventHandler_: DefaultContextEventHandler{client_: client}}).CreateContextWith(cc,
		jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{before}})
	ctx.WaitUntilReady()
	ctx.Clock_ = internal.FixedClock{Millis_: 1}
	var copied = *ctx
	copied.Refresh()
	for name, c := range map[string]*Context{"copy": &copied, "original": ctx} {
		assertAny(9, c.GetAssignment("after").Id, t)
		if _, found := c.GetExperiment("after"); !found {
			t.Errorf("%s: GetExperiment does not see the refreshed data", name)
		}
	}
}

// callingDeserializer runs call (once per evaluation, from inside it) while
// the context evaluates an audience.
type callingDeserializer struct {
	mu   sync.Mutex
	ctx  *Context
	call func(*Context)
}

func (d *callingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	d.mu.Lock()
	var ctx, call = d.ctx, d.call
	d.mu.Unlock()
	if ctx != nil && call != nil {
		call(ctx)
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// v1.1.1 handed event loggers the context without locking, so an
// AudienceDeserializer that logs an event (QueueExposure, Track) with a
// logger configured did not hang. Covers the build and revalidation paths,
// with and without a replaced clock, and concurrent evaluations.
func TestDeserializerThatLogsEventsWithLoggerDoesNotDeadlock(t *testing.T) {
	for _, name := range []string{"QueueExposure", "Track"} {
		for _, replace := range []bool{false, true} {
			var d = &callingDeserializer{}
			var logger = &MockEventLogger{}
			var client = &systemAttrClient{application: "website", environment: "production"}
			var abs = Create(ABsmartlyConfig{Client_: client,
				ContextDataProvider_:  DefaultContextDataProvider{client_: client},
				ContextEventHandler_:  DefaultContextEventHandler{client_: client},
				AudienceDeserializer_: d, ContextEventLogger_: logger})
			var config = CreateDefaultContextConfig()
			config.Units_, config.PublishDelay_, config.RefreshInterval_ = withUnit(), -1, -1
			var ctx = abs.CreateContextWith(config, jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
				ruleExperiment("rule", rulesJSON(`{"type":"assign","variant":2}`)),
				audienceExperiment("exp_app", appFilter, true)}})
			ctx.WaitUntilReady()
			if replace {
				ctx.Clock_ = internal.FixedClock{Millis_: 1}
			}
			// Take the assignments before the evaluations: GetAssignment from
			// inside one would wait for the lock it holds (as in v1.1.1).
			var retained = make(chan *Assignment, 64)
			for i := 0; i < 64; i++ {
				retained <- ctx.GetAssignment("rule")
			}
			var call = func(c *Context) { _ = c.Track("evaluated", nil) }
			if name == "QueueExposure" {
				call = func(c *Context) {
					select {
					case a := <-retained:
						c.QueueExposure(a)
					default:
					}
				}
			}
			d.mu.Lock()
			d.ctx, d.call = ctx, call
			d.mu.Unlock()

			var done = make(chan struct{})
			go func() {
				defer close(done)
				var wg sync.WaitGroup
				for g := 0; g < 4; g++ {
					wg.Add(1)
					go func(g int) {
						defer wg.Done()
						for i := 0; i < 8; i++ {
							_, _ = reparseAudience(ctx)
							_ = ctx.SetAttribute("round", g*100+i)
						}
					}(g)
				}
				wg.Wait()
			}()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatalf("%s from the deserializer with a logger deadlocked (Clock_ replaced: %v)", name, replace)
			}
			var wanted = Exposure
			if name == "Track" {
				wanted = Goal
			}
			var logged = 0
			for _, event := range logger.GetEvents() {
				if event.EventType == wanted {
					logged++
				}
			}
			if logged == 0 {
				t.Errorf("%s: no %s event was logged", name, wanted)
			}
		}
	}
}

// snapshotCallingDeserializer calls WaitUntilReady and Refresh, which take a
// snapshot of the context, from inside an audience evaluation.
type snapshotCallingDeserializer struct {
	mu  sync.Mutex
	ctx *Context
}

func (d *snapshotCallingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	d.mu.Lock()
	var ctx = d.ctx
	d.ctx = nil
	d.mu.Unlock()
	if ctx != nil {
		ctx.WaitUntilReady()
		ctx.Refresh()
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// syncDataProvider settles its future before returning it, so a refresh
// installs the data on the calling goroutine.
type syncDataProvider struct {
	data jsonmodels.ContextData
}

func (p syncDataProvider) GetContextData() *future.Future {
	var f, done = future.New()
	done(p.data, nil)
	return f
}

// v1.1.1: a custom deserializer with no logger could call WaitUntilReady and
// Refresh from inside an evaluation.
func TestDeserializerWithoutLoggerCanWaitAndRefresh(t *testing.T) {
	var d = &snapshotCallingDeserializer{}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var data = jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", appFilter, true)}}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var ctx = Create(ABsmartlyConfig{Client_: client,
		ContextDataProvider_:  syncDataProvider{data},
		ContextEventHandler_:  DefaultContextEventHandler{client_: client},
		AudienceDeserializer_: d}).CreateContextWith(cc, data)
	ctx.WaitUntilReady()
	d.mu.Lock()
	d.ctx = ctx
	d.mu.Unlock()
	var done = make(chan int, 1)
	go func() {
		var variant, _ = reparseAudience(ctx)
		done <- variant
	}()
	select {
	case variant := <-done:
		assertAny(1, variant, t)
	case <-time.After(5 * time.Second):
		t.Fatal("WaitUntilReady/Refresh from a deserializer without a logger deadlocked")
	}
}

func TestDeserializerCanRefreshWithAsyncProvider(t *testing.T) {
	for _, withLogger := range []bool{false, true} {
		var d = &snapshotCallingDeserializer{}
		var client = &fetchClient{systemAttrClient: systemAttrClient{application: "website", environment: "production"},
			data: jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", appFilter, true)}}}
		var cfg = ABSmartlyConfig{Client_: client, ContextDataProvider_: DefaultContextDataProvider{client_: client},
			ContextEventHandler_: DefaultContextEventHandler{client_: client}, AudienceDeserializer_: d}
		if withLogger {
			cfg.ContextEventLogger_ = &MockEventLogger{}
		}
		var cc = CreateDefaultContextConfig()
		cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
		var ctx = Create(cfg).CreateContextWith(cc, client.data)
		ctx.WaitUntilReady()
		d.ctx = ctx
		var done = make(chan int, 1)
		go func() { v, _ := ctx.PeekTreatment("exp_app"); done <- v }()
		select {
		case v := <-done:
			assertAny(1, v, t)
		case <-time.After(5 * time.Second):
			t.Fatalf("async Refresh deadlocked, logger=%v", withLogger)
		}
	}
}

func TestDeserializerCanPublishQueuedEvents(t *testing.T) {
	for _, withLogger := range []bool{false, true} {
		for _, async := range []bool{false, true} {
			var d = &callingDeserializer{}
			var publisher = &CapturingPublishClient{}
			var logger = &MockEventLogger{}
			var cfg = ABSmartlyConfig{ContextDataProvider_: syncDataProvider{},
				ContextEventHandler_: DefaultContextEventHandler{client_: publisher}, AudienceDeserializer_: d}
			if withLogger {
				cfg.ContextEventLogger_ = logger
			}
			var cc = CreateDefaultContextConfig()
			cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
			var ctx = Create(cfg).CreateContextWith(cc, jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true)}})
			ctx.WaitUntilReady()
			assertAny(nil, ctx.Track("goal", map[string]interface{}{"value": 1}), t)
			var result = make(chan error, 1)
			d.ctx = ctx
			d.call = func(c *Context) {
				if async {
					f, err := c.PublishAsync()
					if err == nil {
						_, err = f.Get(context.Background())
					}
					result <- err
				} else {
					result <- c.Publish()
				}
			}
			var done = make(chan struct{})
			go func() { _, _ = reparseAudience(ctx); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("Publish deadlocked, logger=%v async=%v", withLogger, async)
			}
			assertAny(nil, deserializerResult(t, result), t)
			assertAny(1, len(publisher.publishEvents), t)
			assertAny("goal", publisher.publishEvents[0].Goals[0].Name, t)
			if withLogger {
				var n int
				for _, e := range logger.GetEvents() {
					if e.EventType == Publish {
						n++
					}
				}
				assertAny(1, n, t)
			}
		}
	}
}

// deserializerResult returns what the deserializer reported. The read that
// called it has returned, so a missing result means it was never called.
func deserializerResult(t *testing.T, result chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	default:
		t.Fatal("the read returned without calling the deserializer")
		return nil
	}
}

// sameSplit must agree with reflect.DeepEqual, which ExperimentMatches used.
func TestSameSplitMatchesDeepEqual(t *testing.T) {
	var nan = math.NaN()
	var shared = []float64{0, 1}
	var nanShared = []float64{nan}
	var cases = [][2][]float64{
		{nil, nil}, {nil, {}}, {{}, nil}, {{}, {}},
		{{0, 1}, {0, 1}}, {{0, 1}, {1, 0}}, {{0, 1}, {0, 1, 0}},
		{shared, shared}, {nanShared, nanShared}, {{nan}, {nan}},
		{{0}, {math.Copysign(0, -1)}}, {{0.5, 0.5}, {0.5, 0.5000001}},
	}
	for _, tc := range cases {
		assertAny(reflect.DeepEqual(tc[0], tc[1]), sameSplit(tc[0], tc[1]), t)
	}
}

// A failing Publish from inside an evaluation, with a logger set, returns the
// error, logs one Error event and keeps the batch queued.
func TestDeserializerFailingPublishLogsErrorAndKeepsEvents(t *testing.T) {
	var d = &callingDeserializer{}
	var failure = errors.New("collector down")
	var publisher = &CapturingPublishClient{publishErr: failure}
	var logger = &MockEventLogger{}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var ctx = Create(ABSmartlyConfig{ContextDataProvider_: syncDataProvider{},
		ContextEventHandler_: DefaultContextEventHandler{client_: publisher}, AudienceDeserializer_: d,
		ContextEventLogger_: logger}).CreateContextWith(cc, jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true)}})
	ctx.WaitUntilReady()
	assertAny(nil, ctx.Track("goal", nil), t)
	var result = make(chan error, 1)
	d.ctx = ctx
	d.call = func(c *Context) { result <- c.Publish() }
	var done = make(chan struct{})
	go func() { _, _ = reparseAudience(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a failing Publish from a deserializer with a logger deadlocked")
	}
	assertAny(failure, deserializerResult(t, result), t)
	var errors_ = 0
	for _, e := range logger.GetEvents() {
		if e.EventType == Error {
			errors_++
		}
	}
	assertAny(1, errors_, t)
	assertAny(int32(1), ctx.GetPendingCount(), t)
}

// publishClosingLogger closes the context from its Publish event.
type publishClosingLogger struct {
	ctx    *Context
	closed chan struct{}
}

func (l *publishClosingLogger) HandleEvent(_ Context, event EventType, _ interface{}) {
	if event == Publish && l.ctx != nil {
		var ctx = l.ctx
		l.ctx = nil
		ctx.Close()
		close(l.closed)
	}
}

// A logger that calls Close from the Publish event of a publish started by a
// deserializer does not wait for its own delivery.
func TestLoggerClosingFromDeserializerStartedPublishDoesNotDeadlock(t *testing.T) {
	var d = &callingDeserializer{}
	var publisher = &CapturingPublishClient{}
	var logger = &publishClosingLogger{closed: make(chan struct{})}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var ctx = Create(ABSmartlyConfig{ContextDataProvider_: syncDataProvider{},
		ContextEventHandler_: DefaultContextEventHandler{client_: publisher}, AudienceDeserializer_: d,
		ContextEventLogger_: logger}).CreateContextWith(cc, jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true)}})
	ctx.WaitUntilReady()
	assertAny(nil, ctx.Track("goal", nil), t)
	logger.ctx = ctx
	d.ctx = ctx
	d.call = func(c *Context) {
		if f, err := c.PublishAsync(); err == nil {
			_, _ = f.Get(context.Background())
		}
	}
	var done = make(chan struct{})
	go func() { _, _ = reparseAudience(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("PeekTreatment deadlocked")
	}
	select {
	case <-logger.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close from the logger's Publish event deadlocked")
	}
	assertAny(true, ctx.IsClosed(), t)
}

// ExperimentMatches compares the traffic split: a changed split invalidates
// the cached assignment.
func TestChangedTrafficSplitInvalidatesCachedAssignment(t *testing.T) {
	var experiment = ruleExperiment("exp", "")
	var ctx, _ = newRuleContext(t, "production", withUnit(), experiment)
	var before = ctx.GetAssignment("exp")
	assertAny(true, before.Eligible, t)
	experiment.TrafficSplit = []float64{1, 0}
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{experiment}})
	var after = ctx.GetAssignment("exp")
	assertAny(false, after.Eligible, t)
	assertAny([]float64{1, 0}, after.TrafficSplit, t)
}

// lockProbeDeserializer counts its calls and those made with ContextLock_
// free.
type lockProbeDeserializer struct {
	mu    sync.Mutex
	ctx   *Context
	calls int
	free  int
}

func (d *lockProbeDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	d.mu.Lock()
	var ctx = d.ctx
	d.mu.Unlock()
	if ctx != nil {
		var free = make(chan struct{})
		go func() {
			ctx.ContextLock_.Lock()
			ctx.ContextLock_.Unlock()
			close(free)
		}()
		select {
		case <-free:
			d.mu.Lock()
			d.free++
			d.mu.Unlock()
		case <-time.After(200 * time.Millisecond):
		}
		d.mu.Lock()
		d.calls++
		d.mu.Unlock()
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// The AudienceDeserializer is customer code, so it runs on the reading
// goroutine before any context lock is taken, never under ContextLock_. That
// lets it call any context method, as in v1.1.1, without the SDK bypassing
// its own locks. As in v1.1.1 it parses each audience once: at the first read
// that evaluates it, not again for cached reads, attribute changes or a
// refresh with unchanged audience text.
func TestAudienceDeserializerRunsOutsideTheContextLock(t *testing.T) {
	var d = &lockProbeDeserializer{}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var data = jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", appFilter, true)}}
	var ctx = Create(ABsmartlyConfig{Client_: client,
		ContextDataProvider_:  syncDataProvider{data},
		ContextEventHandler_:  DefaultContextEventHandler{client_: client},
		AudienceDeserializer_: d}).CreateContextWith(cc, data)
	ctx.WaitUntilReady()
	d.mu.Lock()
	d.ctx = ctx
	d.mu.Unlock()

	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	d.mu.Lock()
	assertAny(1, d.calls, t)
	assertAny(1, d.free, t)
	d.mu.Unlock()

	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	assertAny(nil, ctx.SetAttribute("other", 1), t)
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	ctx.Refresh()
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	d.mu.Lock()
	assertAny(1, d.calls, t)
	d.mu.Unlock()

	// A new iteration with a new audience text is a new assignment, which
	// parses the new text (a text change alone keeps the cached assignment,
	// as in v1.1.1).
	var changed = audienceExperiment("exp_app", `{"filter":[{"eq":[{"var":"application"},{"value":"website"}]},{"value":true}]}`, true)
	changed.Iteration = 2
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{changed}})
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	d.mu.Lock()
	assertAny(2, d.calls, t)
	assertAny(2, d.free, t)
	d.mu.Unlock()
}

// reparseAudience installs the context's data again with a new audience text
// and iteration for exp_app, then reads the treatment, so the
// AudienceDeserializer parses the new text.
var reparseRound int32

// revalidateAudience changes only exp_app's audience text, which keeps its
// cached assignment (as in v1.1.1), then sets an attribute, so the next read
// revalidates the cached assignment against the new text.
func revalidateAudience(ctx *Context) (int, error) {
	var data, _ = ctx.GetData()
	var round = atomic.AddInt32(&reparseRound, 1)
	var experiments = make([]jsonmodels.Experiment, len(data.Experiments))
	copy(experiments, data.Experiments)
	for i := range experiments {
		if experiments[i].Name == "exp_app" {
			experiments[i].Audience = fmt.Sprintf(`{"filter":[{"eq":[{"var":"application"},{"value":"website"}]},{"value":%d}]}`, round)
		}
	}
	ctx.SetData(jsonmodels.ContextData{Experiments: experiments})
	if err := ctx.SetAttribute("revalidate", round); err != nil {
		return 0, err
	}
	return ctx.PeekTreatment("exp_app")
}

func reparseAudience(ctx *Context) (int, error) {
	var data, _ = ctx.GetData()
	var round = atomic.AddInt32(&reparseRound, 1)
	var experiments = make([]jsonmodels.Experiment, len(data.Experiments))
	copy(experiments, data.Experiments)
	for i := range experiments {
		if experiments[i].Name == "exp_app" {
			experiments[i].Audience = fmt.Sprintf(`{"filter":[{"eq":[{"var":"application"},{"value":"website"}]},{"value":%d}]}`, round)
			// A new iteration invalidates the cached assignment, so the read
			// evaluates the new audience and calls the deserializer.
			experiments[i].Iteration += int(round)
		}
	}
	ctx.SetData(jsonmodels.ContextData{Experiments: experiments})
	return ctx.PeekTreatment("exp_app")
}

// pendingProvider answers a refresh only when released.
type pendingProvider struct {
	release chan struct{}
	data    jsonmodels.ContextData
}

func (p *pendingProvider) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		<-p.release
		return p.data, nil
	})
}

type refreshingDeserializer struct {
	mu  sync.Mutex
	ctx *Context
}

func (d *refreshingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	d.mu.Lock()
	var ctx = d.ctx
	d.ctx = nil
	d.mu.Unlock()
	if ctx != nil {
		ctx.Refresh()
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// A deserializer that calls Refresh while another refresh is pending joins
// it, and the pending refresh completes.
func TestDeserializerRefreshJoinsAPendingRefresh(t *testing.T) {
	var p = &pendingProvider{release: make(chan struct{}),
		data: jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", appFilter, true)}}}
	var joined = make(chan *future.Future, 1)
	var d = &callingDeserializer{}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var ctx = Create(ABsmartlyConfig{Client_: client, ContextDataProvider_: p,
		ContextEventHandler_: DefaultContextEventHandler{client_: client}, AudienceDeserializer_: d}).CreateContextWith(cc, p.data)
	ctx.WaitUntilReady()
	var pending = ctx.RefreshAsync()
	d.mu.Lock()
	d.ctx = ctx
	d.call = func(c *Context) {
		d.mu.Lock()
		d.ctx = nil
		d.mu.Unlock()
		// Report the joined future before waiting on it, so the test can
		// check the refresh is still pending when the deserializer joins.
		var f = c.RefreshAsync()
		joined <- f
		c.Refresh()
	}
	d.mu.Unlock()
	var done = make(chan struct{})
	go func() {
		_, _ = ctx.PeekTreatment("exp_app")
		close(done)
	}()
	select {
	case f := <-joined:
		assertAny(false, f.Ready(), t)
		assertAny(true, f == pending, t)
	case <-time.After(5 * time.Second):
		t.Fatal("the deserializer was not called")
	}
	close(p.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Refresh from a deserializer joined a pending refresh and deadlocked")
	}
	assertAny(nil, getWithTimeout(t, pending), t)
}

// trackingLogger calls Track from the Publish event.
type trackingLogger struct {
	mu  sync.Mutex
	ctx *Context
}

func (l *trackingLogger) HandleEvent(_ Context, event EventType, _ interface{}) {
	l.mu.Lock()
	var ctx = l.ctx
	if event == Publish {
		l.ctx = nil
	}
	l.mu.Unlock()
	if event == Publish && ctx != nil {
		_ = ctx.Track("follow-up", nil)
	}
}

// A publish started by a deserializer, whose logger calls Track from the
// Publish event, completes and queues the follow-up goal.
func TestDeserializerPublishWithTrackingLoggerCompletes(t *testing.T) {
	var d = &callingDeserializer{}
	var publisher = &CapturingPublishClient{}
	var logger = &trackingLogger{}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var ctx = Create(ABsmartlyConfig{ContextDataProvider_: syncDataProvider{},
		ContextEventHandler_: DefaultContextEventHandler{client_: publisher}, AudienceDeserializer_: d,
		ContextEventLogger_: logger}).CreateContextWith(cc, jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true)}})
	ctx.WaitUntilReady()
	assertAny(nil, ctx.Track("goal", nil), t)
	logger.mu.Lock()
	logger.ctx = ctx
	logger.mu.Unlock()
	var result = make(chan error, 1)
	d.mu.Lock()
	d.ctx = ctx
	d.call = func(c *Context) { result <- c.Publish() }
	d.mu.Unlock()
	var done = make(chan struct{})
	go func() {
		_, _ = reparseAudience(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a publish whose logger calls Track deadlocked")
	}
	assertAny(nil, deserializerResult(t, result), t)
	assertAny(1, len(publisher.publishEvents), t)
	assertAny(int32(1), ctx.GetPendingCount(), t)
}

// waitingDeserializer calls WaitUntilReady from inside the deserializer.
type waitingDeserializer struct {
	mu  sync.Mutex
	ctx *Context
}

func (d *waitingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	d.mu.Lock()
	var ctx = d.ctx
	d.mu.Unlock()
	if ctx != nil {
		ctx.WaitUntilReady()
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// A deserializer may call WaitUntilReady during the first read after a load
// that is still pending elsewhere, and Refresh after a refresh that changed
// the audience.
func TestDeserializerCallsDuringLoadAndRefreshDoNotHang(t *testing.T) {
	var d = &waitingDeserializer{}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var pending, settle = future.New()
	var ctx = createTrackedContext(clock, cc, pending, dataProvider, eventHandler, nil, variableParser, AudienceMatcher{d})
	d.mu.Lock()
	d.ctx = ctx
	d.mu.Unlock()
	var done = make(chan int, 1)
	go func() {
		ctx.WaitUntilReady()
		var variant, _ = ctx.PeekTreatment("exp_app")
		done <- variant
	}()
	var settled = make(chan struct{})
	go func() {
		settle(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", appFilter, true)}}, nil)
		close(settled)
	}()
	var deadline = time.After(5 * time.Second)
	for _, step := range []chan struct{}{settled, nil} {
		if step != nil {
			select {
			case <-step:
			case <-deadline:
				t.Fatal("settling the first load hung")
			}
			continue
		}
		select {
		case <-done:
		case <-deadline:
			t.Fatal("WaitUntilReady from a deserializer during the first load hung")
		}
	}

	var r = &refreshingDeserializer{}
	var p = &pendingProvider{release: make(chan struct{}),
		data: jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
			audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true)}}}
	var ctx2 = Create(ABsmartlyConfig{Client_: client, ContextDataProvider_: p,
		ContextEventHandler_: DefaultContextEventHandler{client_: client}, AudienceDeserializer_: r}).CreateContextWith(cc,
		jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", appFilter, true)}})
	ctx2.WaitUntilReady()
	r.mu.Lock()
	r.ctx = ctx2
	r.mu.Unlock()
	var refreshing = ctx2.RefreshAsync()
	close(p.release)
	assertAny(nil, getWithTimeout(t, refreshing), t)
	var done2 = make(chan struct{})
	go func() {
		_, _ = ctx2.PeekTreatment("exp_app")
		close(done2)
	}()
	select {
	case <-done2:
	case <-time.After(5 * time.Second):
		t.Fatal("Refresh from a deserializer after a refresh with a changed audience hung")
	}
	assertAny(nil, getWithTimeout(t, ctx2.RefreshAsync()), t)
}

// A v1.1.1-style literal with a hand-filled Index_ evaluates its audiences.
func TestContextLiteralIndexEvaluatesAudiences(t *testing.T) {
	var manual = audienceLiteral(audienceExperiment("exp_app", `{"filter":[{"eq":[{"var":"country"},{"value":"US"}]}]}`, true))
	var variant, err = manual.PeekTreatment("exp_app")
	assertAny(nil, err, t)
	assertAny(0, variant, t) // strict audience, no country: variant 0, as in v1.1.1
}

// audienceLiteral is a v1.1.1-style Context literal with a hand-filled Index_.
func audienceLiteral(experiment jsonmodels.Experiment) *Context {
	var boolValue = func(value bool) *atomic.Value {
		var result = &atomic.Value{}
		result.Store(value)
		return result
	}
	var count = &atomic.Value{}
	count.Store(int32(0))
	return &Context{
		PublishDelay_: -1, RefreshInterval_: -1, VariableParser_: DefaultVariableParser{},
		AudienceMatcher_: AudienceMatcher{DefaultAudienceDeserializer{}},
		Units_:           map[string]string{"session_id": ruleUnit}, Failed_: boolValue(false), Ready_: boolValue(true),
		DataLock: &sync.RWMutex{}, ContextLock_: &sync.RWMutex{},
		Index_:          map[string]ExperimentVariables{experiment.Name: {Data: experiment, Variables: []map[string]interface{}{{}, {}}}},
		IndexVariables_: map[interface{}]interface{}{},
		HashedUnits_:    map[interface{}]interface{}{}, Assigners_: map[interface{}]interface{}{},
		AssignmentCache: map[string]Assignment{}, EventLock_: &sync.Mutex{},
		Overrides_: map[interface{}]interface{}{}, Cassignments_: map[interface{}]interface{}{},
		PendingCount_: count, Closing_: boolValue(false), Closed_: boolValue(false),
		Refreshing_: boolValue(false), TimeoutLock_: &sync.Mutex{},
		Clock_: internal.FixedClock{Millis_: 1},
	}
}

// Editing a literal's Index_ after its first read neither recurses nor
// crashes: the unlocked parse and the locked evaluation read the same index.
func TestEditedLiteralIndexDoesNotRecurse(t *testing.T) {
	var manual = audienceLiteral(audienceExperiment("exp_other", `{"filter":[{"value":true}]}`, true))
	var counter = &countingDeserializer{}
	manual.AudienceMatcher_ = AudienceMatcher{counter}
	manual.Index_["exp_app"] = ExperimentVariables{Data: audienceExperiment("exp_app", `{"filter":[{"value":1}]}`, true), Variables: []map[string]interface{}{{}, {}}}
	assertAny(1, treatmentPeek(t, manual, "exp_other"), t)
	manual.DataLock.Lock()
	manual.Index_["exp_app"] = ExperimentVariables{Data: audienceExperiment("exp_app", `{"filter":[{"value":false}]}`, true), Variables: []map[string]interface{}{{}, {}}}
	manual.DataLock.Unlock()
	var done = make(chan int, 1)
	go func() { done <- treatmentPeek(t, manual, "exp_app") }()
	select {
	case variant := <-done:
		// As in v1.1.1, the read uses the edited Index_ entry: the strict
		// audience does not match.
		assertAny(0, variant, t)
	case <-time.After(5 * time.Second):
		t.Fatal("PeekTreatment after an Index_ edit did not return")
	}
	// Only the audiences the reads evaluated are parsed, each once.
	assertAny([]string{`{"filter":[{"value":true}]}`, `{"filter":[{"value":false}]}`}, counter.texts(), t)

	// Editing an entry already assigned is picked up as in v1.1.1: a changed
	// iteration is a different experiment.
	var edited = audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true)
	edited.Iteration = 2
	manual.DataLock.Lock()
	manual.Index_["exp_app"] = ExperimentVariables{Data: edited, Variables: []map[string]interface{}{{}, {}}}
	manual.DataLock.Unlock()
	assertAny(1, treatmentPeek(t, manual, "exp_app"), t)
}

// countingDeserializer records the audience texts it parses.
type countingDeserializer struct {
	mu     sync.Mutex
	parsed []string
}

func (d *countingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	d.mu.Lock()
	d.parsed = append(d.parsed, string(b[offset:offset+length]))
	d.mu.Unlock()
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

func (d *countingDeserializer) texts() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.parsed...)
}

// wrappingDeserializer is comparable by type but holds an interface whose
// value may not be (a func adapter), so == on it panics.
type wrappingDeserializer struct{ inner AudienceDeserializer }

func (d wrappingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	return d.inner.Deserialize(b, offset, length)
}

// nanDeserializer is comparable but never equal to itself.
type nanDeserializer struct {
	weight float64
	inner  *countingDeserializer
}

func (d nanDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	return d.inner.Deserialize(b, offset, length)
}

// Deserializers whose values panic on == or never equal themselves are used
// as in v1.1.1: no panic, no hang, and a strict audience is evaluated.
func TestDeserializerComparisonNeverPanicsOrSpins(t *testing.T) {
	var counter = &countingDeserializer{}
	var adapter = funcDeserializer(counter.Deserialize)
	for _, d := range []AudienceDeserializer{wrappingDeserializer{adapter}, nanDeserializer{math.NaN(), counter}} {
		var ctx, _ = newSystemAttrContext("website", "production",
			audienceExperiment("exp_app", `{"filter":[{"value":false}]}`, true),
			audienceExperiment("exp_two", `{"filter":[{"value":true}]}`, true))
		ctx.AudienceMatcher_ = AudienceMatcher{d}
		var done = make(chan [2]int, 1)
		go func() {
			var first = treatmentPeek(t, ctx, "exp_app")
			_ = ctx.SetAttribute("x", 1)
			var second = treatmentPeek(t, ctx, "exp_two")
			_ = ctx.SetAttribute("x", 2)
			assertAny(0, treatmentPeek(t, ctx, "exp_app"), t)
			done <- [2]int{first, second}
		}()
		select {
		case got := <-done:
			assertAny([2]int{0, 1}, got, t)
		case <-time.After(5 * time.Second):
			t.Fatalf("%T: reads did not return", d)
		}
		// Each audience is parsed once; the reads after SetAttribute reuse it.
		assertAny(2, len(counter.texts()), t)
		counter.mu.Lock()
		counter.parsed = nil
		counter.mu.Unlock()
	}
}

// Replacing a func-adapter deserializer with one of another type parses
// again with the new one.
func TestReplacingUncomparableDeserializerWithAnotherTypeParsesAgain(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	ctx.AudienceMatcher_ = AudienceMatcher{funcDeserializer(DefaultAudienceDeserializer{}.Deserialize)}
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	ctx.AudienceMatcher_ = AudienceMatcher{rejectingDeserializer{}}
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	assertAny(0, treatmentPeek(t, ctx, "exp_app"), t)
}

// restlessDeserializer installs a new audience text on every parse.
type restlessDeserializer struct {
	ctx   *Context
	calls int32
}

func (d *restlessDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	var n = atomic.AddInt32(&d.calls, 1)
	d.ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		audienceExperiment("exp_app", fmt.Sprintf(`{"filter":[{"value":true},{"value":%d}]}`, n), true)}})
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// Data changing during every parse cannot keep a read retrying: after a few
// attempts the audience counts as unparseable and, as an audience the
// deserializer rejects in v1.1.1, is ignored.
func TestAudienceRetryIsBounded(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	var d = &restlessDeserializer{ctx: ctx}
	ctx.AudienceMatcher_ = AudienceMatcher{d}
	var done = make(chan int, 1)
	go func() { done <- treatmentPeek(t, ctx, "exp_app") }()
	select {
	case variant := <-done:
		assertAny(1, variant, t)
	case <-time.After(5 * time.Second):
		t.Fatal("the read kept retrying")
	}
	assertAny(int32(maxAudienceAttempts), atomic.LoadInt32(&d.calls), t)
}

// A literal's treatments can be read while the caller holds DataLock, as in
// v1.1.1, whose assignment path never took it.
func TestLiteralReadWhileHoldingDataLock(t *testing.T) {
	for _, write := range []bool{true, false} {
		var manual = audienceLiteral(audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
		if write {
			manual.DataLock.Lock()
		} else {
			manual.DataLock.RLock()
		}
		var done = make(chan int, 1)
		go func() {
			var variant, _ = manual.PeekTreatment("exp_app")
			done <- variant
		}()
		select {
		case variant := <-done:
			assertAny(1, variant, t)
		case <-time.After(5 * time.Second):
			t.Fatal("PeekTreatment on a literal hung while the caller held DataLock")
		}
	}
}

// Editing one literal Index_ entry keeps the others, and keeps the rules
// SetData parsed for an entry while its assignmentRules text is unchanged.
func TestLiteralIndexEditKeepsRulesAndOtherEntries(t *testing.T) {
	var manual = audienceLiteral(audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	var ruled = ruleExperiment("ruled", rulesJSON(`{"type":"assign","variant":2}`))
	manual.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true), ruled}})
	assertAny(2, treatmentPeek(t, manual, "ruled"), t)

	// Same rules text, new iteration: the rules still apply.
	var next = ruled
	next.Iteration = 2
	manual.Index_["ruled"] = ExperimentVariables{Data: next, Variables: manual.Index_["ruled"].Variables}
	assertAny(2, treatmentPeek(t, manual, "ruled"), t)
	// The other entry is kept, though Index_ was not read for it.
	var _, _, stillThere = manual.state().experimentAndRules("exp_app")
	assertAny(true, stillThere, t)

	// Changed rules text: an edited literal entry has no rules, as v1.1.1.
	next.Iteration = 3
	next.AssignmentRules = rulesJSON(`{"type":"assign","variant":1}`)
	manual.Index_["ruled"] = ExperimentVariables{Data: next, Variables: manual.Index_["ruled"].Variables}
	var _ = treatmentPeek(t, manual, "ruled")
	var _, rules, _ = manual.state().experimentAndRules("ruled")
	if rules != nil {
		t.Fatal("an edited literal entry with new assignmentRules kept the old rules")
	}
}

// Each field an assignment reads is picked up from an edited literal entry,
// and a deleted entry is unassigned, as v1.1.1 read Index_.
func TestLiteralIndexEditsArePickedUp(t *testing.T) {
	var base = audienceExperiment("exp_app", "", false)
	base.FullOnVariant = 0
	var vars = []map[string]interface{}{{"color": "red"}, {"color": "blue"}}
	var setup = func() *Context {
		var manual = audienceLiteral(base)
		manual.Index_["exp_app"] = ExperimentVariables{Data: base, Variables: vars}
		assertAny(1, treatmentPeek(t, manual, "exp_app"), t)
		return manual
	}
	var edit = func(manual *Context, change func(*jsonmodels.Experiment)) {
		var data = base
		change(&data)
		manual.Index_["exp_app"] = ExperimentVariables{Data: data, Variables: vars}
	}

	var manual = setup()
	edit(manual, func(e *jsonmodels.Experiment) { e.FullOnVariant = 2 })
	assertAny(2, treatmentPeek(t, manual, "exp_app"), t)

	// A Split edit keeps the cached assignment (ExperimentMatches does not
	// compare it, as in v1.1.1), but the private copy follows Index_.
	manual = setup()
	edit(manual, func(e *jsonmodels.Experiment) { e.Split = []float64{1, 0} })
	assertAny(1, treatmentPeek(t, manual, "exp_app"), t)
	var split, _, _ = manual.state().experimentAndRules("exp_app")
	assertAny([]float64{1, 0}, split.Data.Split, t)

	manual = setup()
	delete(manual.Index_, "exp_app")
	assertAny(0, treatmentPeek(t, manual, "exp_app"), t)

	// New variable values (a new Variables slice) are read, as from Index_.
	manual = setup()
	manual.Index_["exp_app"] = ExperimentVariables{Data: base, Variables: []map[string]interface{}{{"color": "red"}, {"color": "green"}}}
	var assignment = manual.GetAssignment("exp_app")
	assertAny(1, assignment.Variant, t)
	var _, _ = manual.PeekVariableValue("color", "")
	var current, _, _ = manual.state().experimentAndRules("exp_app")
	assertAny("green", current.Variables[1]["color"], t)

	// A new assignmentRules text drops SetData's rules.
	var ruled = ruleExperiment("ruled", rulesJSON(`{"type":"assign","variant":2}`))
	manual = audienceLiteral(base)
	manual.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{ruled}})
	assertAny(2, treatmentPeek(t, manual, "ruled"), t)
	var next = ruled
	next.AssignmentRules = rulesJSON(`{"type":"assign","variant":1}`)
	manual.Index_["ruled"] = ExperimentVariables{Data: next, Variables: manual.Index_["ruled"].Variables}
	assertAny(0, treatmentPeek(t, manual, "ruled"), t)
}

// An edited audience in a literal's Index_ entry is evaluated at the next
// rebuild.
func TestLiteralAudienceEditIsEvaluated(t *testing.T) {
	var manual = audienceLiteral(audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	assertAny(1, treatmentPeek(t, manual, "exp_app"), t)
	var entry = manual.Index_["exp_app"]
	entry.Data.Audience = `{"filter":[{"value":false}]}`
	manual.Index_["exp_app"] = entry
	assertAny(nil, manual.SetAttribute("x", 1), t)
	assertAny(0, treatmentPeek(t, manual, "exp_app"), t)
}

// Literal reads racing SetDataFailed are race-free (run with -race).
func TestLiteralReadsRacingSetDataFailed(t *testing.T) {
	var manual = audienceLiteral(audienceExperiment("exp_app", "", false))
	var stop int32
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for atomic.LoadInt32(&stop) == 0 {
				var _, _ = manual.PeekTreatment("exp_app")
			}
		}()
	}
	runtime.Gosched()
	manual.SetDataFailed(errors.New("load failed"))
	atomic.StoreInt32(&stop, 1)
	wg.Wait()
	var _, _, found = manual.state().experimentAndRules("exp_app")
	assertAny(false, found, t)
}

// A literal read racing a SetData that changes the rules never leaves the
// private index with the new data but without its rules.
func TestLiteralSyncDoesNotOverwriteConcurrentSetData(t *testing.T) {
	var oldRules = ruleExperiment("ruled", rulesJSON(`{"type":"assign","variant":1}`))
	var newRules = ruleExperiment("ruled", rulesJSON(`{"type":"assign","variant":2}`))
	newRules.Iteration = 2
	var wrong = 0
	for trial := 0; trial < 500; trial++ {
		var manual = audienceLiteral(oldRules)
		manual.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{oldRules}})
		assertAny(1, treatmentPeek(t, manual, "ruled"), t)
		var stop int32
		var wg sync.WaitGroup
		for r := 0; r < 4; r++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for atomic.LoadInt32(&stop) == 0 {
					var _, _ = manual.PeekTreatment("ruled")
				}
			}()
		}
		runtime.Gosched()
		manual.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{newRules}})
		atomic.StoreInt32(&stop, 1)
		wg.Wait()
		if variant := treatmentPeek(t, manual, "ruled"); variant != 2 {
			wrong++
		}
	}
	assertAny(0, wrong, t)
}

// funcDeserializer is a func-typed adapter, whose values Go cannot hash or
// compare.
type funcDeserializer func(b []byte, offset, length int) (map[string]interface{}, error)

func (f funcDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	return f(b, offset, length)
}

// mapDeserializer is a value struct holding a map: not comparable either.
type mapDeserializer struct{ seen map[string]int }

func (d mapDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	d.seen[string(b[offset:offset+length])]++
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// A deserializer of a non-comparable type works as in v1.1.1, and is still
// called once per audience text.
func TestUncomparableDeserializerDoesNotPanic(t *testing.T) {
	var calls int32
	var seen = map[string]int{}
	for _, d := range []AudienceDeserializer{
		funcDeserializer(func(b []byte, offset, length int) (map[string]interface{}, error) {
			atomic.AddInt32(&calls, 1)
			return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
		}),
		mapDeserializer{seen},
	} {
		var ctx, _ = newSystemAttrContext("website", "production", audienceExperiment("exp_app", appFilter, true))
		ctx.AudienceMatcher_ = AudienceMatcher{d}
		assertAny(nil, ctx.SetAttribute("x", 1), t)
		assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
		assertAny(nil, ctx.SetAttribute("x", 2), t)
		assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	}
	assertAny(int32(1), atomic.LoadInt32(&calls), t)
	assertAny(map[string]int{appFilter: 1}, seen, t)
}

// SetData that changes only the audience text keeps the cached assignment, as
// in v1.1.1, until an attribute change revalidates it: the new text is then
// parsed and evaluated.
func TestAudienceTextChangeIsEvaluatedAfterSetAttribute(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		audienceExperiment("exp_app", `{"filter":[{"value":false}]}`, true)}})
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	assertAny(0, treatmentPeek(t, ctx, "exp_app"), t)
}

// installingDeserializer installs new data, with a new audience text, while
// it parses: the evaluation that follows must parse that text too, with no
// lock held, and use it.
type installingDeserializer struct {
	mu    sync.Mutex
	ctx   *Context
	next  []string
	calls []string
}

func (d *installingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	d.mu.Lock()
	d.calls = append(d.calls, string(b[offset:offset+length]))
	var ctx, next = d.ctx, ""
	if len(d.next) > 0 {
		next, d.next = d.next[0], d.next[1:]
	}
	d.mu.Unlock()
	if ctx != nil && next != "" {
		ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", next, true)}})
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

func TestDataInstalledDuringParseIsParsedBeforeEvaluation(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	var d = &installingDeserializer{ctx: ctx, next: []string{`{"filter":[{"value":false}]}`, `{"filter":[{"value":1}]}`}}
	ctx.AudienceMatcher_ = AudienceMatcher{d}
	var done = make(chan int, 1)
	go func() { done <- treatmentPeek(t, ctx, "exp_app") }()
	select {
	case variant := <-done:
		// The last installed audience ({"value":1}) matches.
		assertAny(1, variant, t)
	case <-time.After(5 * time.Second):
		t.Fatal("evaluation did not finish")
	}
	assertAny([]string{`{"filter":[{"value":true}]}`, `{"filter":[{"value":false}]}`, `{"filter":[{"value":1}]}`}, d.calls, t)
}

// A cached assignment whose audience text is replaced between the unlocked
// parse and the lock is revalidated against the new text, not kept.
func TestAudienceReplacedBeforeRevalidationIsEvaluated(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	var d = &installingDeserializer{ctx: ctx, next: []string{"", `{"filter":[{"value":false}]}`}}
	ctx.AudienceMatcher_ = AudienceMatcher{d}
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		audienceExperiment("exp_app", `{"filter":[{"value":1}]}`, true)}})
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	assertAny(0, treatmentPeek(t, ctx, "exp_app"), t)
}

// An override or a matching assignment rule decides the variant without the
// audience, so, as in v1.1.1, the deserializer is not called for it.
func TestOverrideAndRuleSkipTheAudienceParse(t *testing.T) {
	var counter = &countingDeserializer{}
	var ruled = ruleExperiment("ruled", rulesJSON(`{"type":"assign","variant":2}`))
	ruled.Audience = `{"filter":[{"value":"ruled"}]}`
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":"overridden"}]}`, true), ruled)
	ctx.AudienceMatcher_ = AudienceMatcher{counter}
	assertAny(nil, ctx.SetOverride("exp_app", 1), t)
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	assertAny(2, treatmentPeek(t, ctx, "ruled"), t)
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	assertAny(2, treatmentPeek(t, ctx, "ruled"), t)
	assertAny(0, len(counter.texts()), t)
}

// readingLogger reads a treatment from inside the Error event.
type readingLogger struct {
	mu       sync.Mutex
	ctx      *Context
	errors   int
	variants []int
}

func (l *readingLogger) HandleEvent(context Context, event EventType, data interface{}) {
	if event != Error {
		return
	}
	l.mu.Lock()
	l.errors++
	var ctx = l.ctx
	l.mu.Unlock()
	if ctx != nil {
		var variant, _ = ctx.PeekTreatment("exp_app")
		l.mu.Lock()
		l.variants = append(l.variants, variant)
		l.mu.Unlock()
	}
}

// countingPanicDeserializer panics on every call and counts them.
type countingPanicDeserializer struct{ calls int32 }

func (d *countingPanicDeserializer) Deserialize([]byte, int, int) (map[string]interface{}, error) {
	atomic.AddInt32(&d.calls, 1)
	panic("deserializer bug")
}

// A same-experiment logger read uses its local failure without another parse.
func TestPanicLoggerReadingTheExperimentDoesNotReparse(t *testing.T) {
	var logger = &readingLogger{}
	var d = &countingPanicDeserializer{}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var ctx = Create(ABsmartlyConfig{Client_: client,
		ContextDataProvider_:  DefaultContextDataProvider{client_: client},
		ContextEventHandler_:  DefaultContextEventHandler{client_: client},
		AudienceDeserializer_: d, ContextEventLogger_: logger}).CreateContextWith(cc,
		jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", appFilter, true)}})
	ctx.WaitUntilReady()
	logger.mu.Lock()
	logger.ctx = ctx
	logger.mu.Unlock()
	var done = make(chan int, 1)
	go func() { done <- treatmentPeek(t, ctx, "exp_app") }()
	select {
	case variant := <-done:
		assertAny(1, variant, t)
	case <-time.After(5 * time.Second):
		t.Fatal("the read did not return")
	}
	assertAny(int32(1), atomic.LoadInt32(&d.calls), t)
	logger.mu.Lock()
	defer logger.mu.Unlock()
	assertAny(1, logger.errors, t)
	assertAny([]int{1}, logger.variants, t)
}

// removingDeserializer installs data without the audience it is parsing.
type removingDeserializer struct {
	once sync.Once
	ctx  *Context
}

func (d *removingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	d.once.Do(func() {
		d.ctx.SetData(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
			audienceExperiment("exp_app", `{"filter":[{"value":1}]}`, true)}})
	})
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// A parse that finishes after new data removed its audience is not kept.
func TestParseOfRemovedAudienceIsNotCached(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	ctx.AudienceMatcher_ = AudienceMatcher{&removingDeserializer{ctx: ctx}}
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	var texts []string
	ctx.state().audiences.Range(func(text, _ interface{}) bool {
		texts = append(texts, text.(string))
		return true
	})
	assertAny([]string{`{"filter":[{"value":1}]}`}, texts, t)
}

// The parse cache keeps only the installed audience texts.
func TestAudienceCacheIsPrunedOnInstall(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production", audienceExperiment("exp_app", appFilter, true))
	for i := 0; i < 5; i++ {
		var _, _ = reparseAudience(ctx)
		assertAny(nil, ctx.SetAttribute("round", i), t) // revalidates, so the new text is parsed
		var _ = treatmentPeek(t, ctx, "exp_app")
	}
	var n = 0
	ctx.state().audiences.Range(func(_, _ interface{}) bool { n++; return true })
	assertAny(1, n, t)
}

// Replacing AudienceMatcher_ after creation takes effect, as in v1.1.1.
func TestReplacingAudienceMatcherTakesEffect(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	ctx.AudienceMatcher_ = AudienceMatcher{rejectingDeserializer{}}
	assertAny(nil, ctx.SetAttribute("other", 1), t)
	assertAny(0, treatmentPeek(t, ctx, "exp_app"), t)
}

// rejectingDeserializer gives every audience a filter that never matches.
type rejectingDeserializer struct{}

func (rejectingDeserializer) Deserialize([]byte, int, int) (map[string]interface{}, error) {
	return map[string]interface{}{"filter": []interface{}{map[string]interface{}{"value": false}}}, nil
}

// panicDeserializer panics on every audience.
type panicDeserializer struct{}

func (panicDeserializer) Deserialize([]byte, int, int) (map[string]interface{}, error) {
	panic("deserializer bug")
}

// A deserializer panic is reported to the event logger and the audience is
// treated as unparseable (ignored, as an audience the deserializer rejects),
// instead of crashing the read.
func TestDeserializerPanicIsReportedAndTreatedAsUnparseable(t *testing.T) {
	var logger = &MockEventLogger{}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var ctx = Create(ABsmartlyConfig{Client_: client,
		ContextDataProvider_:  DefaultContextDataProvider{client_: client},
		ContextEventHandler_:  DefaultContextEventHandler{client_: client},
		AudienceDeserializer_: panicDeserializer{}, ContextEventLogger_: logger}).CreateContextWith(cc,
		jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", appFilter, true)}})
	ctx.WaitUntilReady()
	logger.Reset()
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	var errors_ = 0
	for _, e := range logger.GetEvents() {
		if e.EventType == Error {
			errors_++
		}
	}
	assertAny(1, errors_, t)
}
