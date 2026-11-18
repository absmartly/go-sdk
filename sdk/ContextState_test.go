package sdk

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	var copied = manual
	assertAny(&manual, copied.live(), t)
	assertAny(manual.state(), copied.state(), t)
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
