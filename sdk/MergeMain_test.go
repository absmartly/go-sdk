package sdk

import (
	"testing"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// Holdouts and switchbacks on top of the v1.2.0 context: private state behind
// the clock carrier, lock-free audience parses and the rule flag kept off the
// public Assignment.

// A switchback's audience on an eligible cluster is parsed by the context's
// AudienceDeserializer with no context lock held, as a unit experiment's is,
// and only once for the period.
func TestSwitchback_AudienceIsParsedOutsideTheContextLock(t *testing.T) {
	var sb = sbDefault(2, sbHour)
	sb.Audience = sbStrictGoldAudience
	var h = sbCreate(t, sbData(sb))
	var d = &lockProbeDeserializer{}
	h.ctx.AudienceMatcher_ = AudienceMatcher{d}
	d.mu.Lock()
	d.ctx = h.ctx
	d.mu.Unlock()
	h.at(sbAnchor + 5).treatment("exp_sb")
	h.treatment("exp_sb")
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.calls != 1 || d.free != 1 {
		t.Fatalf("deserializer calls %d, with the context lock free %d; want 1, 1", d.calls, d.free)
	}
}

// A switchback whose cluster is ineligible never asks the deserializer: the
// Collector evaluates the audience only for an eligible cluster.
func TestSwitchback_IneligibleClusterDoesNotParseTheAudience(t *testing.T) {
	var sb = sbDefault(2, sbHour)
	sb.Audience = sbStrictGoldAudience
	sb.SwitchbackClusterValues = []string{"another-store"}
	var h = sbCreate(t, sbData(sb))
	var counter = &countingDeserializer{}
	h.ctx.AudienceMatcher_ = AudienceMatcher{counter}
	expectInt(t, 0, h.at(sbAnchor+5).treatment("exp_sb"))
	if texts := counter.texts(); len(texts) != 0 {
		t.Fatalf("parsed %v for an ineligible cluster", texts)
	}
}

// A strict-mismatched switchback on an eligible cluster is recorded, and its
// audience is evaluated with the parse made outside the lock: an attribute
// that makes it match revalidates the slot within the period.
func TestSwitchback_StrictAudienceUsesTheUnlockedParse(t *testing.T) {
	var sb = sbDefault(2, sbHour)
	sb.Audience, sb.AudienceStrict = sbStrictGoldAudience, true
	var h = sbCreate(t, sbData(sb))
	var counter = &countingDeserializer{}
	h.ctx.AudienceMatcher_ = AudienceMatcher{counter}
	expectInt(t, 0, h.at(sbAnchor+5).treatment("exp_sb"))
	h.setAttribute("tier", "gold")
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	if texts := counter.texts(); len(texts) != 2 {
		t.Fatalf("parsed %d times, want 2 (the build and the revalidation): %v", len(texts), texts)
	}
	h.expectPublished(sbStrictMismatchExposure(sbAnchor+5), sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+5, true, true))
}

// A switchback rule win keeps the rule flag off the public Assignment: the
// exposure of a copy GetAssignment returned is still published as a rule
// win, read from the cache entry the copy shares its exposed flag with.
func TestSwitchback_QueueExposureOfARuleWonCopyKeepsTheRuleFlag(t *testing.T) {
	var sb = sbDefault(2, sbHour)
	sb.AssignmentRules = sbAssignVariant1
	var h = sbCreate(t, sbData(sb))
	h.at(sbAnchor + 5)
	h.ctx.QueueExposure(h.ctx.GetAssignment("exp_sb"))
	h.expectPublished(sbRuleExposure("exp_sb", 1, sbAnchor+5))
}

// Replacing Clock_ keeps the switchback state: the context still finds its
// period slots, generations and retired slots in its private state, so a
// cached slot is reused rather than drawn and exposed again.
func TestSwitchback_ReplacingTheClockKeepsTheSlots(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+5).treatment("exp_sb"))
	var before = h.ctx.getAssignment("exp_sb")
	var clock = &sbClock{now: sbAnchor + 6}
	h.ctx.Clock_ = clock
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	if h.ctx.getAssignment("exp_sb") != before {
		t.Fatal("the period slot was resolved again after the clock was replaced")
	}
	clock.now = sbAnchor + sbHour + 5
	expectInt(t, sbExpected2Arms[1], h.treatment("exp_sb"))
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+5, true, true),
		sbExposure(1, "exp_sb", sbExpected2Arms[1], sbAnchor+sbHour+5, true, true))
}

// Replacing Clock_ keeps the holdout arms in the private state: a second
// covered experiment reuses the arm, and the holdout is exposed once.
func TestHoldout_ReplacingTheClockKeepsTheArms(t *testing.T) {
	var a = hoExperiment(1, "a", hoUnitType, 0, 11)
	var b = hoExperiment(2, "b", hoUnitType, 0, 11)
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, a, b))
	h.treatment("a")
	h.ctx.Clock_ = h.ctx.Clock_.(*contextClock).Clock
	h.treatment("b")
	var holdouts = 0
	for _, exposure := range queuedExposures(h.ctx) {
		if exposure.Id == 11 {
			holdouts++
		}
	}
	expectInt(t, 1, holdouts)
}

// With nothing winning a shared key, the first suppressed candidate supplies
// its control value; GetVariableValue fires the holdouts of every candidate
// visited, and neither suppressed experiment publishes its own exposure.
func TestHoldout_SharedKeyWithOnlySuppressedCandidatesTakesTheFirstControl(t *testing.T) {
	var first = hoExperiment(1, "first", hoUnitType, 0, 11)
	first.Variants[0].Config = `{"color":"first_control"}`
	var second = hoExperiment(2, "second", hoUnitType, 0, 12)
	second.Variants[0].Config = `{"color":"second_control"}`
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA(), hoHoldout(12, "holdout_c", hoUnitType, hoSeedAHi, hoSeedALo)},
		first, second))
	expectValue(t, "first_control", h.peekVariable("color"))
	h.expectPending(0)
	expectValue(t, "first_control", h.variable("color"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0), hoExposure(hoUnitType, 12, "holdout_c", 0))
}

// A candidate that is neither assigned nor suppressed (outside a strict
// audience) is not the fallback and exposes nothing; the suppressed one
// after it is.
func TestHoldout_SharedKeyStrictMismatchIsNotTheFallback(t *testing.T) {
	var mismatched = hoWithVariables(hoExperiment(1, "mismatched", hoUnitType, 0), "color")
	mismatched.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	mismatched.AudienceStrict = true
	var suppressed = hoWithVariables(hoExperiment(2, "suppressed", hoUnitType, 0, 11), "color")
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, mismatched, suppressed))
	expectValue(t, "control", h.variable("color"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}
