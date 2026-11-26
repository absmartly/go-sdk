package sdk

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// withinDeadline fails the test if f does not return within five seconds, so
// a lock-order regression is a failure rather than a hung suite.
func withinDeadline(t *testing.T, what string, f func()) {
	t.Helper()
	var done = make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return", what)
	}
}

// A first exposing variable read on a context whose Clock_ was replaced
// returns, with or without holdouts: the claim check resolves the context
// state before it takes ContextLock_.
func TestRound21_ReplacedClockFirstVariableReadReturns(t *testing.T) {
	for _, holdouts := range [][]jsonmodels.Holdout{nil, {hoHoldoutA()}} {
		var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
		e.Split = []float64{0, 1}
		var r = hoExperiment(2, "r", hoUnitType, 0)
		r.AssignmentRules = hoUSRule
		var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(holdouts, e, r))
		h.ctx.Clock_ = internal.FixedClock{Millis_: hoMillis}
		withinDeadline(t, "GetVariableValue after Clock_ replacement", func() {
			var value, err = h.ctx.GetVariableValue("v", "default")
			if err != nil || value != "treatment" {
				t.Errorf("got %v, %v; want treatment", value, err)
			}
		})
		h.expectPending(1 + len(holdouts))
	}
}

// QueueExposure that a close rejects on a context whose Clock_ was replaced
// rolls the claim back without hanging, so a later read can queue it.
func TestRound21_ReplacedClockRejectedQueueExposureReturns(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	var r = hoExperiment(2, "r", hoUnitType, 0)
	r.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
	h.ctx.Clock_ = internal.FixedClock{Millis_: hoMillis}
	var assignment = h.ctx.GetAssignment("e")
	h.ctx.Closing_.Store(true)
	withinDeadline(t, "rejected QueueExposure after Clock_ replacement", func() {
		h.ctx.QueueExposure(assignment)
	})
	h.ctx.Closing_.Store(false)
	if assignment.Exposed.Load().(bool) {
		t.Fatal("the rejected claim stayed marked")
	}
}

// literalWithCachedAssignment is a v1.1.1-style literal whose AssignmentCache
// already holds the experiment's assignment, as a caller may import it.
func literalWithCachedAssignment(exposed bool) *Context {
	var e = hoWithVariables(hoExperiment(1, "exp", hoUnitType, 0), "v")
	var literal = audienceLiteral(e)
	literal.Units_ = map[string]string{hoUnitType: hoUID}
	var flag = &atomic.Value{}
	flag.Store(exposed)
	literal.AssignmentCache["exp"] = Assignment{Id: 1, Iteration: 1, Name: "exp", UnitType: hoUnitType,
		TrafficSplit: e.TrafficSplit, Variant: 1, Assigned: true, Eligible: true, Exposed: flag,
		Variables: map[string]interface{}{"v": "treatment"}}
	return literal
}

// Imported literal assignments carry no retirement token: an exposed one is
// served as cached, and an unclaimed one rejected by a close is rolled back
// without panicking or leaving ContextLock_ held.
func TestRound21_ImportedLiteralAssignmentsDoNotPanic(t *testing.T) {
	t.Run("exposed read", func(t *testing.T) {
		var literal = literalWithCachedAssignment(true)
		var variant, err = literal.GetTreatment("exp")
		if err != nil || variant != 1 {
			t.Fatalf("got %d, %v; want the cached variant 1", variant, err)
		}
	})
	t.Run("rejected unclaimed", func(t *testing.T) {
		var literal = literalWithCachedAssignment(false)
		var assignment = literal.GetAssignment("exp")
		literal.Closing_.Store(true)
		literal.QueueExposure(assignment)
		literal.Closing_.Store(false)
		if assignment.Exposed.Load().(bool) {
			t.Fatal("the rejected claim stayed marked")
		}
		if !tryLock(t, literal.ContextLock_) {
			t.Fatal("the rejected claim left ContextLock_ held")
		}
		literal.ContextLock_.Unlock()
	})
}

// A queue-only literal without ContextLock_ clears its claim when a close
// rejects the exposure, without needing a context mutex.
func TestRound21_QueueOnlyLiteralRejectedClaimIsCleared(t *testing.T) {
	var boolValue = func(value bool) *atomic.Value {
		var result = &atomic.Value{}
		result.Store(value)
		return result
	}
	var count = &atomic.Value{}
	count.Store(int32(0))
	var literal = Context{PublishDelay_: -1, EventLock_: &sync.Mutex{}, PendingCount_: count,
		Ready_: boolValue(true), Closing_: boolValue(true), Closed_: boolValue(false),
		TimeoutLock_: &sync.Mutex{}, Clock_: internal.FixedClock{Millis_: 7}}
	var assignment = &Assignment{Id: 1, Name: "exp", Variant: 1, Exposed: boolValue(false)}
	literal.QueueExposure(assignment)
	if assignment.Exposed.Load().(bool) {
		t.Fatal("the rejected claim stayed marked")
	}
	assertAny(int32(0), literal.GetPendingCount(), t)
}

// unitSettingClock calls SetUnit from Millis, which needs ContextLock_ for
// writing, so a read that holds the lock across the clock call hangs.
type unitSettingClock struct{ ctx **Context }

func (c unitSettingClock) Millis() int64 {
	if ctx := *c.ctx; ctx != nil {
		_ = ctx.SetUnit("clock_unit", "clock-uid")
	}
	return hoMillis
}

// An exposing variable read that visits a covered candidate only to fire its
// holdout arm (a strict-audience mismatch, not held out) samples the clock
// outside ContextLock_: a clock that calls SetUnit does not hang it.
func TestRound21_ArmOnlyReadCallsTheClockOutsideTheContextLock(t *testing.T) {
	var strict = hoWithVariables(hoExperiment(1, "exp_strict", hoUnitType, 0, 12), "color")
	strict.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	strict.AudienceStrict = true
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{hoUnitType: hoUIDNotHeldOut}
	config.PublishDelay_, config.RefreshInterval_ = -1, -1
	var ft, done = future.New()
	done(hoData([]jsonmodels.Holdout{hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)}, strict), nil)
	var ctx *Context
	ctx = CreateContext(unitSettingClock{ctx: &ctx}, config, ft, &hoProvider{}, &hoPublisher{}, nil,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	ctx.WaitUntilReady()
	t.Cleanup(func() { ctx.ClearRefreshTimer() })
	withinDeadline(t, "arm-only GetVariableValue with a SetUnit clock", func() {
		var value, err = ctx.GetVariableValue("color", "default")
		if err != nil || value != "default" {
			t.Errorf("got %v, %v; want default", value, err)
		}
	})
	var exposures = queuedExposures(ctx)
	if len(exposures) != 1 || exposures[0].Id != 12 {
		t.Fatalf("exposures %+v, want only holdout 12's arm", exposures)
	}
}

// Clearing only a literal experiment's HoldoutIds removes its coverage at the
// next read: a full-on experiment held out by arm 0 serves its full-on
// variant and queues its own exposure, not the holdout's.
func TestRound21_LiteralHoldoutIdsOnlyEditRecomputesCoverage(t *testing.T) {
	var e = hoExperiment(1, "exp", hoUnitType, 1, 11)
	var literal = audienceLiteral(e)
	literal.Units_ = map[string]string{hoUnitType: hoUID}
	literal.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
	expectInt(t, 0, treatmentPeek(t, literal, "exp"))
	var edited = literal.Index_["exp"]
	edited.Data.HoldoutIds = nil
	literal.Index_["exp"] = edited
	var variant, err = literal.GetTreatment("exp")
	if err != nil {
		t.Fatal(err)
	}
	expectInt(t, 1, variant)
	if exposures := literal.Exposures_; len(exposures) != 1 || exposures[0].Id != 1 || !exposures[0].FullOn {
		t.Fatalf("exposures %+v, want only the experiment's full-on exposure", exposures)
	}
}
