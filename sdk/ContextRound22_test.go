package sdk

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// refreshingClock installs data from Millis, on its first call only: an
// exposing read that reads the clock before admission lets that refresh land
// between resolving the decision and admitting it.
type refreshingClock struct {
	ctx  **Context
	data jsonmodels.ContextData
	done bool
}

func (c *refreshingClock) Millis() int64 {
	if ctx := *c.ctx; ctx != nil && !c.done {
		c.done = true
		ctx.SetData(c.data)
	}
	return hoMillis
}

// A holdout whose iteration a refresh bumps while the arm-only read reads the
// clock is not admitted at its old epoch: the read checks the decision against
// the installed definitions under ContextLock_, after the clock returns.
func TestRound22_ArmOnlyReadDoesNotAdmitASupersededHoldoutEpoch(t *testing.T) {
	var strict = hoWithVariables(hoExperiment(1, "exp_strict", hoUnitType, 0, 12), "color")
	strict.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	strict.AudienceStrict = true
	var holdout = hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)
	var bumped = holdout
	bumped.Iteration = 2
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{hoUnitType: hoUIDNotHeldOut}
	config.PublishDelay_, config.RefreshInterval_ = -1, -1
	var ft, done = future.New()
	done(hoData([]jsonmodels.Holdout{holdout}, strict), nil)
	var ctx *Context
	var clock = &refreshingClock{ctx: &ctx, data: hoData([]jsonmodels.Holdout{bumped}, strict)}
	ctx = CreateContext(clock, config, ft, &hoProvider{}, &hoPublisher{}, nil,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	ctx.WaitUntilReady()
	t.Cleanup(func() { ctx.ClearRefreshTimer() })
	clock.done = false
	expectValue(t, "default", func() interface{} { v, _ := ctx.GetVariableValue("color", "default"); return v }())
	for _, exposure := range queuedExposures(ctx) {
		if exposure.Id == 12 {
			var arm = ctx.state().holdoutArms_[holdoutKey{id: 12, unitType: hoUnitType}]
			if arm == nil || arm.assignment.Iteration != 2 {
				t.Fatalf("an arm of the superseded holdout iteration was admitted: %+v", queuedExposures(ctx))
			}
		}
	}
}

// A variable read through a readiness copy revalidates its claim against the
// live context: a rule win for country=US that the live context made obsolete
// (country=GB) before the claim is not published as a rule override.
func TestRound22_ReadinessCopyClaimUsesLiveAttributes(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
	e.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
	h.setAttribute("country", "US")
	var copied = h.ctx.WaitUntilReady()
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			if err := h.ctx.SetAttribute("country", "GB"); err != nil {
				t.Error(err)
			}
		}
	}
	defer func() { beforeSettleClaim = nil }()
	if _, err := copied.GetVariableValue("v", "default"); err != nil {
		t.Fatal(err)
	}
	if armed {
		t.Fatal("the hook did not run")
	}
	for _, exposure := range queuedExposures(h.ctx) {
		if exposure.Id == 1 && exposure.RuleOverride {
			t.Fatalf("an obsolete rule win was published: %+v", queuedExposures(h.ctx))
		}
	}
}

// An assignment imported into a literal's AssignmentCache, read and then
// rebuilt because SetData adds holdout coverage, does not panic, whether it
// was exposed or not.
func TestRound22_ImportedLiteralAssignmentRebuildsWhenCoverageIsAdded(t *testing.T) {
	for _, exposed := range []bool{false, true} {
		var literal = literalWithCachedAssignment(exposed)
		literal.GetAssignment("exp")
		var covered = hoWithVariables(hoExperiment(1, "exp", hoUnitType, 0, 11), "v")
		literal.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, covered))
		expectInt(t, 0, treatmentPeek(t, literal, "exp"))
	}
}

// An in-place edit to a literal experiment's HoldoutIds element (to a holdout
// that is not installed) removes its coverage at the next read, as replacing
// the slice does.
func TestRound22_LiteralInPlaceHoldoutIdsEditRecomputesCoverage(t *testing.T) {
	var e = hoExperiment(1, "exp", hoUnitType, 1, 11)
	var literal = audienceLiteral(e)
	literal.Units_ = map[string]string{hoUnitType: hoUID}
	literal.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
	expectInt(t, 0, treatmentPeek(t, literal, "exp"))
	literal.Index_["exp"].Data.HoldoutIds[0] = 999
	expectInt(t, 1, treatmentPeek(t, literal, "exp"))
}

// A quoted iteration is not an integral number, so it decodes as iteration 0:
// the holdout's epoch, which keys its arm and its re-exposure, is unaffected
// by the string's contents.
func TestRound22_QuotedHoldoutIterationDecodesAsZero(t *testing.T) {
	var payload = `{"experiments":[],"holdouts":[{"id":11,"name":"holdout_a","unitType":"session_id",
		"iteration":"2","seedHi":13,"seedLo":111,"split":[0.1,0.9]}]}`
	var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Holdouts) != 1 || data.Holdouts[0].Iteration != 0 {
		t.Fatalf("decoded %+v, want one holdout at iteration 0", data.Holdouts)
	}
}

// An arm-only variable read and assignment writes for another experiment run
// concurrently without a data race: the read takes ContextLock_ before it reads
// the assignment cache. Run under -race.
func TestRound22_ArmOnlyReadDoesNotRaceWithAssignmentWrites(t *testing.T) {
	var strict = hoWithVariables(hoExperiment(1, "exp_strict", hoUnitType, 0, 12), "color")
	strict.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	strict.AudienceStrict = true
	var others []jsonmodels.Experiment
	others = append(others, strict)
	for i := 2; i < 40; i++ {
		others = append(others, hoExperiment(i, fmt.Sprintf("other_%d", i), hoUnitType, 0))
	}
	var holdout = hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{holdout}, others...))
	var stop = make(chan struct{})
	var writer = make(chan struct{})
	go func() {
		defer close(writer)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			// A new attribute sequence makes every cached decision rebuild,
			// so each write replaces an entry in the assignment cache.
			_ = h.ctx.SetAttribute("tick", i)
			h.ctx.GetAssignment(fmt.Sprintf("other_%d", 2+i%38))
		}
	}()
	for i := 0; i < 2000; i++ {
		if _, err := h.ctx.GetVariableValue("color", "default"); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	<-writer
}

// A warmed, already exposed variable read allocates nothing, with or without
// holdouts or rules: the claim's eligibility check is built only when a read
// needs it.
func TestRound22_WarmedVariableReadAllocatesNothing(t *testing.T) {
	var r = hoExperiment(2, "r", hoUnitType, 0)
	r.AssignmentRules = hoUSRule
	for _, data := range []jsonmodels.ContextData{
		hoData(nil, hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")),
		hoData(nil, hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v"), r),
		hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v"), r),
	} {
		var h = hoCreateFor(t, hoUIDNotHeldOut, data)
		if _, err := h.ctx.GetVariableValue("v", "default"); err != nil {
			t.Fatal(err)
		}
		if n := hoLeastAllocs(func() { _, _ = h.ctx.GetVariableValue("v", "default") }); n != 0 {
			t.Fatalf("a warmed variable read made %.0f allocations, want 0", n)
		}
	}
}

// nonParticipantContext is a context that has seen holdouts, where the key's
// only candidate is an experiment the unit is not in (strict audience
// mismatch), covered or not by the holdout, with the given clock.
func nonParticipantContext(t *testing.T, clock interface{ Millis() int64 }, covered bool) *Context {
	t.Helper()
	var ids []int
	if covered {
		ids = []int{12}
	}
	var strict = hoWithVariables(hoExperiment(1, "exp_strict", hoUnitType, 0, ids...), "color")
	strict.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	strict.AudienceStrict = true
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{hoUnitType: hoUIDNotHeldOut}
	config.PublishDelay_, config.RefreshInterval_ = -1, -1
	var ft, done = future.New()
	done(hoData([]jsonmodels.Holdout{hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)}, strict), nil)
	var ctx = CreateContext(clock, config, ft, &hoProvider{}, &hoPublisher{}, nil,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	ctx.WaitUntilReady()
	t.Cleanup(func() { ctx.ClearRefreshTimer() })
	return ctx
}

// A warmed variable read past a non-participant candidate, in a context that
// has seen holdouts, reads no clock and allocates nothing: there is no
// unexposed arm left to timestamp.
func TestRound22_WarmedNonParticipantReadReadsNoClockAndAllocatesNothing(t *testing.T) {
	for _, covered := range []bool{false, true} {
		var clock = &countingClock{}
		var ctx = nonParticipantContext(t, clock, covered)
		if _, err := ctx.GetVariableValue("color", "default"); err != nil {
			t.Fatal(err)
		}
		var before = atomic.LoadInt64(&clock.n)
		for i := 0; i < 10; i++ {
			if _, err := ctx.GetVariableValue("color", "default"); err != nil {
				t.Fatal(err)
			}
		}
		if calls := atomic.LoadInt64(&clock.n) - before; calls != 0 {
			t.Fatalf("covered=%v: 10 warmed reads made %d clock calls, want 0", covered, calls)
		}
		if n := hoLeastAllocs(func() { _, _ = ctx.GetVariableValue("color", "default") }); n != 0 {
			t.Fatalf("covered=%v: a warmed non-participant read made %.0f allocations, want 0", covered, n)
		}
	}
}

// A held-out experiment with an audience never evaluated that audience, so an
// attribute change does not recompute its decision: the cached entry survives
// SetAttribute.
func TestRound22_HeldOutDecisionSkipsAudienceRevalidation(t *testing.T) {
	var e = hoExperiment(1, "exp", hoUnitType, 0, 11)
	e.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
	expectInt(t, 0, h.peek("exp"))
	var before = h.ctx.assignmentEntry("exp")
	if !before.holdouts.isSuppressed() {
		t.Fatal("the experiment is not held out")
	}
	h.setAttribute("country", "GB")
	expectInt(t, 0, h.peek("exp"))
	if h.ctx.assignmentEntry("exp") != before {
		t.Fatal("the held-out decision was recomputed after an attribute change")
	}
}

// A read whose claim's CAS loses does not admit a second exposure. The flag is
// set between the exposed check and the CAS, so this read's CAS fails while
// its own checks all passed, as when a concurrent read claims first.
func TestRound23_LostClaimCASAdmitsNoSecondExposure(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
	var entry = h.ctx.assignmentEntry("e")
	var eligible = h.ctx.variableEligible("e", "v")
	var raced = false
	var racing = func(c *cachedAssignment) exposureStatus {
		var status = eligible(c)
		if !raced {
			raced = true
			c.assignment.Exposed.Store(true)
		}
		return status
	}
	if status := h.ctx.queueEligibleExposure(entry, racing); status != exposureQueued {
		t.Fatalf("a lost claim reported %v, want exposureQueued", status)
	}
	if !raced {
		t.Fatal("the race did not run")
	}
	h.expectPending(0)
}
