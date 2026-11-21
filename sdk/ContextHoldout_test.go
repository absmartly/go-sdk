package sdk

// Holdouts, ported from the Java SDK's ContextHoldoutTest (java-sdk main,
// core-api/src/test/java/com/absmartly/sdk/ContextHoldoutTest.java) with the
// JS SDK (src/context.ts) as the reference where the two differ. A holdout is
// a top-level payload entry; an experiment is covered by the holdouts its
// holdoutIds name. Arm 0 holds the covered experiment out (control, not
// assigned, no exposure of its own); in a three-arm holdout arm 1 holds out
// experiments that are not full-on. Each holdout publishes one ordinary
// exposure per unit type.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

const (
	hoUnitType      = "session_id"
	hoUID           = "e791e240fcd3df7d238cfc285f475e8152fcc0ec"
	hoUIDNotHeldOut = "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3"

	// split [0.5,0.5], seed 100/200: variant 1 for hoUID, 0 for hoUIDNotHeldOut.
	hoNormalVariant = 1

	// split [0.1,0.9], seed 13/111: arm 0 (held out) for hoUID, arm 1 for hoUIDNotHeldOut.
	hoSeedAHi, hoSeedALo = 13, 111
	// split [0.1,0.9], seed 1/222: arm 1 for both units.
	hoSeedBHi, hoSeedBLo = 1, 222

	// split [0.3,0.3,0.4] for hoUID: seedLo 1 -> arm 0, 3 -> arm 1, 0 -> arm 2.
	hoThreeArm0Lo, hoThreeArm1Lo, hoThreeArm2Lo = 1, 3, 0

	hoMillis int64 = 1_620_000_000_000
)

var hoThreeArmSplit = []float64{0.3, 0.3, 0.4}

func hoExperiment(id int, name string, unitType string, fullOnVariant int, holdoutIds ...int) jsonmodels.Experiment {
	var variants = []jsonmodels.ExperimentVariant{{Name: "A"}, {Name: "B"}}
	if fullOnVariant >= 2 {
		variants = append(variants, jsonmodels.ExperimentVariant{Name: "C"})
	}
	return jsonmodels.Experiment{
		Id: id, Name: name, UnitType: unitType, Iteration: 1, SeedHi: 100, SeedLo: 200,
		Split: []float64{0.5, 0.5}, TrafficSeedHi: 1, TrafficSeedLo: 2, TrafficSplit: []float64{0, 1},
		FullOnVariant: fullOnVariant, Applications: []jsonmodels.ExperimentApplication{{Name: "website"}},
		Variants: variants, HoldoutIds: holdoutIds,
	}
}

func hoHoldout(id int, name string, unitType string, seedHi int, seedLo int) jsonmodels.Holdout {
	return jsonmodels.Holdout{Id: id, Name: name, UnitType: unitType, Iteration: 1, SeedHi: seedHi, SeedLo: seedLo,
		Split: []float64{0.1, 0.9}, HoldoutType: "full"}
}

func hoThreeArm(id int, name string, seedLo int) jsonmodels.Holdout {
	var holdout = hoHoldout(id, name, hoUnitType, 0, seedLo)
	holdout.Split = hoThreeArmSplit
	holdout.HoldoutType = "all_full_on"
	return holdout
}

func hoData(holdouts []jsonmodels.Holdout, experiments ...jsonmodels.Experiment) jsonmodels.ContextData {
	return jsonmodels.ContextData{Experiments: experiments, Holdouts: holdouts}
}

func hoExposure(unitType string, id int, name string, variant int) jsonmodels.Exposure {
	return jsonmodels.Exposure{Id: id, Name: name, Unit: unitType, Variant: variant, ExposedAt: hoMillis,
		Assigned: true, Eligible: true}
}

func hoOwn(unitType string, id int, name string, variant int) jsonmodels.Exposure {
	return hoExposure(unitType, id, name, variant)
}

type hoPublisher struct {
	mu     sync.Mutex
	events []jsonmodels.PublishEvent
}

func (p *hoPublisher) Publish(_ Context, event jsonmodels.PublishEvent) *future.Future {
	p.mu.Lock()
	p.events = append(p.events, event)
	p.mu.Unlock()
	var ft, done = future.New()
	done(nil, nil)
	return ft
}

type hoProvider struct {
	mu   sync.Mutex
	next jsonmodels.ContextData
}

func (p *hoProvider) GetContextData() *future.Future {
	p.mu.Lock()
	var data = p.next
	p.mu.Unlock()
	var ft, done = future.New()
	done(data, nil)
	return ft
}

type hoLogger struct {
	mu      sync.Mutex
	onEvent func(EventType, interface{})
}

func (l *hoLogger) HandleEvent(_ Context, event EventType, data interface{}) {
	l.mu.Lock()
	var on = l.onEvent
	l.mu.Unlock()
	if on != nil {
		on(event, data)
	}
}

type hoContext struct {
	t         *testing.T
	ctx       *Context
	publisher *hoPublisher
	provider  *hoProvider
	logger    *hoLogger
}

type hoOption func(*ContextConfig)

func hoOverride(name string, variant int) hoOption {
	return func(c *ContextConfig) { c.Overrides_[name] = variant }
}

func hoCustom(name string, variant int) hoOption {
	return func(c *ContextConfig) { c.Cassigmnents_[name] = variant }
}

func hoPublishDelay(delay int64) hoOption {
	return func(c *ContextConfig) { c.PublishDelay_ = delay }
}

// hoCreate creates a ready context with the given units (nil for none).
func hoCreate(t *testing.T, units map[string]string, data jsonmodels.ContextData, options ...hoOption) *hoContext {
	t.Helper()
	var config = CreateDefaultContextConfig()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	for k, v := range units {
		config.Units_[k] = v
	}
	for _, option := range options {
		option(&config)
	}
	var h = &hoContext{t: t, publisher: &hoPublisher{}, provider: &hoProvider{}, logger: &hoLogger{}}
	var ft, done = future.New()
	done(data, nil)
	h.ctx = CreateContext(internal.FixedClock{Millis_: hoMillis}, config, ft, h.provider, h.publisher, h.logger,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	h.ctx.WaitUntilReady()
	if !h.ctx.IsReady() || h.ctx.IsFailed() {
		t.Fatal("context not ready")
	}
	t.Cleanup(func() {
		h.ctx.ClearTimeout()
		h.ctx.ClearRefreshTimer()
	})
	return h
}

func hoCreateFor(t *testing.T, uid string, data jsonmodels.ContextData, options ...hoOption) *hoContext {
	t.Helper()
	return hoCreate(t, map[string]string{hoUnitType: uid}, data, options...)
}

func (h *hoContext) treatment(name string) int {
	h.t.Helper()
	var variant, err = h.ctx.GetTreatment(name)
	if err != nil {
		h.t.Fatalf("GetTreatment(%s): %v", name, err)
	}
	return variant
}

func (h *hoContext) peek(name string) int {
	h.t.Helper()
	var variant, err = h.ctx.PeekTreatment(name)
	if err != nil {
		h.t.Fatalf("PeekTreatment(%s): %v", name, err)
	}
	return variant
}

func (h *hoContext) variable(key string) interface{} {
	h.t.Helper()
	var value, err = h.ctx.GetVariableValue(key, "default")
	if err != nil {
		h.t.Fatal(err)
	}
	return value
}

func (h *hoContext) peekVariable(key string) interface{} {
	h.t.Helper()
	var value, err = h.ctx.PeekVariableValue(key, "default")
	if err != nil {
		h.t.Fatal(err)
	}
	return value
}

func (h *hoContext) refresh(data jsonmodels.ContextData) {
	h.t.Helper()
	h.provider.mu.Lock()
	h.provider.next = data
	h.provider.mu.Unlock()
	if _, err := h.ctx.RefreshAsync().Get(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

func (h *hoContext) setUnit(unitType string, uid string) {
	h.t.Helper()
	if err := h.ctx.SetUnit(unitType, uid); err != nil {
		h.t.Fatal(err)
	}
}

func (h *hoContext) setAttribute(name string, value interface{}) {
	h.t.Helper()
	if err := h.ctx.SetAttribute(name, value); err != nil {
		h.t.Fatal(err)
	}
}

func (h *hoContext) expectPending(want int) {
	h.t.Helper()
	if got := int(h.ctx.GetPendingCount()); got != want {
		h.t.Fatalf("pending %d, want %d: %+v", got, want, queuedExposures(h.ctx))
	}
}

// expectPublished publishes and requires exactly these exposures, in order.
func (h *hoContext) expectPublished(want ...jsonmodels.Exposure) jsonmodels.PublishEvent {
	h.t.Helper()
	if err := h.ctx.Publish(); err != nil {
		h.t.Fatal(err)
	}
	h.publisher.mu.Lock()
	defer h.publisher.mu.Unlock()
	if len(h.publisher.events) != 1 {
		h.t.Fatalf("published %d events, want 1", len(h.publisher.events))
	}
	var event = h.publisher.events[0]
	if want == nil {
		want = []jsonmodels.Exposure{}
	}
	var got = event.Exposures
	if got == nil {
		got = []jsonmodels.Exposure{}
	}
	if !reflect.DeepEqual(want, got) {
		h.t.Fatalf("published exposures\n got %+v\nwant %+v", got, want)
	}
	return event
}

func expectInt(t *testing.T, want int, got int) {
	t.Helper()
	if want != got {
		t.Fatalf("got %d, want %d", got, want)
	}
}

func expectValue(t *testing.T, want interface{}, got interface{}) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func hoHoldoutA() jsonmodels.Holdout {
	return hoHoldout(11, "holdout_a", hoUnitType, hoSeedAHi, hoSeedALo)
}

func TestHoldout_HeldOutUnitGetsControlAndOnlyTheHoldoutExposure(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_holdout_in", hoUnitType, 0, 11)))
	expectInt(t, 0, h.peek("exp_holdout_in"))
	h.treatment("exp_holdout_in")
	var event = h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
	expectValue(t, []jsonmodels.Unit{{Type: hoUnitType, Uid: string(hashUnitBytes(hoUID))}}, event.Units)
}

func hashUnitBytes(uid string) []byte {
	var hash = HashUnit(uid)
	var out = make([]byte, len(hash))
	for i, b := range hash {
		out[i] = byte(b)
	}
	return out
}

func TestHoldout_NotHeldOutUnitPublishesOwnExposureThenHoldoutArmOne(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_holdout_out", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_holdout_out"))
	h.expectPublished(hoOwn(hoUnitType, 1, "exp_holdout_out", 0), hoExposure(hoUnitType, 11, "holdout_a", 1))
}

func TestHoldout_ExperimentWithoutHoldoutIdsIsNotCovered(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_holdout_in", hoUnitType, 0, 11), hoExperiment(2, "exp_not_covered", hoUnitType, 0)))
	expectInt(t, 0, h.treatment("exp_holdout_in"))
	expectInt(t, hoNormalVariant, h.treatment("exp_not_covered"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0),
		hoOwn(hoUnitType, 2, "exp_not_covered", hoNormalVariant))
}

func TestHoldout_UnionSuppressesAndEveryHoldoutPublishes(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA(),
		hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_multi_holdout", hoUnitType, 0, 11, 12)))
	expectInt(t, 0, h.treatment("exp_multi_holdout"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0), hoExposure(hoUnitType, 12, "holdout_b", 1))
}

// The deciding holdout is the higher id one, so an implementation that only
// looks at the first holdout, or stops at the first one, fails here.
func TestHoldout_UnionAppliesWhenOnlyTheHigherIdHoldoutHoldsOut(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{
		hoHoldout(11, "holdout_low_id", hoUnitType, hoSeedBHi, hoSeedBLo),
		hoHoldout(12, "holdout_high_id", hoUnitType, hoSeedAHi, hoSeedALo)},
		hoExperiment(1, "exp_union_high_id_decides", hoUnitType, 0, 11, 12)))
	expectInt(t, 0, h.treatment("exp_union_high_id_decides"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_low_id", 1), hoExposure(hoUnitType, 12, "holdout_high_id", 0))
}

func TestHoldout_UnitNotHeldOutByEitherHoldoutIsNotSuppressed(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA(),
		hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_multi_holdout_miss", hoUnitType, 0, 11, 12)))
	expectInt(t, 0, h.treatment("exp_multi_holdout_miss"))
	h.expectPublished(hoOwn(hoUnitType, 1, "exp_multi_holdout_miss", 0),
		hoExposure(hoUnitType, 11, "holdout_a", 1), hoExposure(hoUnitType, 12, "holdout_b", 1))
}

func TestHoldout_SuppressesFullOnExperiment(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_fullon_holdout", hoUnitType, 2, 11)))
	expectInt(t, 0, h.treatment("exp_fullon_holdout"))
}

func TestHoldout_ThreeArmZeroHoldsOutFullOnAndNonFullOn(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoThreeArm(21, "holdout_three_arm", hoThreeArm0Lo)},
		hoExperiment(1, "exp_non_fullon", hoUnitType, 0, 21), hoExperiment(2, "exp_fullon", hoUnitType, 2, 21)))
	expectInt(t, 0, h.treatment("exp_non_fullon"))
	expectInt(t, 0, h.treatment("exp_fullon"))
	h.expectPublished(hoExposure(hoUnitType, 21, "holdout_three_arm", 0))
}

func TestHoldout_ThreeArmOneHoldsOutNonFullOnOnly(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoThreeArm(21, "holdout_three_arm", hoThreeArm1Lo)},
		hoExperiment(1, "exp_non_fullon", hoUnitType, 0, 21), hoExperiment(2, "exp_fullon", hoUnitType, 2, 21)))
	expectInt(t, 0, h.treatment("exp_non_fullon"))
	expectInt(t, 2, h.treatment("exp_fullon"))
	var fullOn = hoOwn(hoUnitType, 2, "exp_fullon", 2)
	fullOn.FullOn = true
	h.expectPublished(hoExposure(hoUnitType, 21, "holdout_three_arm", 1), fullOn)
}

func TestHoldout_ThreeArmTwoAssignsBothNormally(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoThreeArm(21, "holdout_three_arm", hoThreeArm2Lo)},
		hoExperiment(1, "exp_non_fullon", hoUnitType, 0, 21), hoExperiment(2, "exp_fullon", hoUnitType, 2, 21)))
	expectInt(t, hoNormalVariant, h.treatment("exp_non_fullon"))
	expectInt(t, 2, h.treatment("exp_fullon"))
	var fullOn = hoOwn(hoUnitType, 2, "exp_fullon", 2)
	fullOn.FullOn = true
	h.expectPublished(hoOwn(hoUnitType, 1, "exp_non_fullon", hoNormalVariant),
		hoExposure(hoUnitType, 21, "holdout_three_arm", 2), fullOn)
}

func TestHoldout_TwoArmWinsUnionOverNonHoldingThreeArm(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_two_arm", hoUnitType, hoSeedAHi, hoSeedALo),
		hoThreeArm(22, "holdout_three_arm", hoThreeArm2Lo)},
		hoExperiment(1, "exp_union", hoUnitType, 0, 11, 22)))
	expectInt(t, 0, h.treatment("exp_union"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_two_arm", 0), hoExposure(hoUnitType, 22, "holdout_three_arm", 2))
}

func TestHoldout_ThreeArmWinsUnionOverNonHoldingTwoArm(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_two_arm", hoUnitType, hoSeedBHi, hoSeedBLo),
		hoThreeArm(22, "holdout_three_arm", hoThreeArm1Lo)},
		hoExperiment(1, "exp_union", hoUnitType, 0, 11, 22)))
	expectInt(t, 0, h.treatment("exp_union"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_two_arm", 1), hoExposure(hoUnitType, 22, "holdout_three_arm", 1))
}

const hoAgeAudience = `{"filter":[{"gte":[{"var":"age"},{"value":20}]}]}`

// Arm 1 of a three-arm holdout defers a full-on experiment to its normal
// path, so a strict audience mismatch still gives control, exactly as arm 2.
func TestHoldout_ThreeArmOneDefersFullOnExperimentToStrictAudience(t *testing.T) {
	for _, arm := range []struct {
		seedLo  int
		variant int
	}{{hoThreeArm1Lo, 1}, {hoThreeArm2Lo, 2}} {
		var experiment = hoExperiment(1, "exp_audience_fullon", hoUnitType, 2, 21)
		experiment.AudienceStrict = true
		experiment.Audience = hoAgeAudience
		var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoThreeArm(21, "holdout_three_arm", arm.seedLo)}, experiment))
		h.setAttribute("age", 5)
		expectInt(t, 0, h.treatment("exp_audience_fullon"))
		var own = hoOwn(hoUnitType, 1, "exp_audience_fullon", 0)
		own.Assigned = false
		own.AudienceMismatch = true
		h.expectPublished(own, hoExposure(hoUnitType, 21, "holdout_three_arm", arm.variant))
	}
}

// With the experiment's unit not set, no arm can be drawn, so nothing is
// held out and no holdout exposure fires. The experiment publishes its own
// unassigned exposure, as without holdouts. This follows the JS SDK; the
// Java SDK publishes nothing here.
func TestHoldout_UnsetUnitTypeGetsControlAndNoHoldoutExposure(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_user", "user_id", hoSeedAHi, hoSeedALo)},
		hoExperiment(1, "exp_user_holdout", "user_id", 0, 11)))
	expectInt(t, 0, h.peek("exp_user_holdout"))
	h.treatment("exp_user_holdout")
	var own = hoOwn("user_id", 1, "exp_user_holdout", 0)
	own.Assigned = false
	h.expectPublished(own)
}

func TestHoldout_AbsentHoldoutsAssignNormally(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp_no_holdouts_key", hoUnitType, 0)))
	expectInt(t, hoNormalVariant, h.peek("exp_no_holdouts_key"))
}

func TestHoldout_DanglingHoldoutIdIsIgnored(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp_dangling", hoUnitType, 0, 999)))
	expectInt(t, hoNormalVariant, h.treatment("exp_dangling"))
	h.expectPublished(hoOwn(hoUnitType, 1, "exp_dangling", hoNormalVariant))
}

func TestHoldout_CustomAssignmentCannotOverrideHeldOutVariant(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_holdout_custom", hoUnitType, 0, 11)), hoCustom("exp_holdout_custom", 3))
	expectInt(t, 0, h.peek("exp_holdout_custom"))
}

func TestHoldout_CachedHeldOutAssignmentWithCustomAssignmentIsReused(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_holdout_custom_cache", hoUnitType, 0, 11)), hoCustom("exp_holdout_custom_cache", 3))
	var first = h.ctx.GetAssignment("exp_holdout_custom_cache")
	expectInt(t, 0, h.treatment("exp_holdout_custom_cache"))
	expectInt(t, 0, h.treatment("exp_holdout_custom_cache"))
	if h.ctx.GetAssignment("exp_holdout_custom_cache") != first {
		t.Fatal("the held-out assignment was rebuilt")
	}
	h.expectPending(1)
}

func TestHoldout_OverrideTakesPrecedence(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_holdout_override", hoUnitType, 0, 11)), hoOverride("exp_holdout_override", 3))
	expectInt(t, 3, h.peek("exp_holdout_override"))
}

// Scenario 210: an override still fires the holdout's exposure, for both arms.
func TestHoldout_OverriddenExperimentStillPublishesHoldoutExposure(t *testing.T) {
	for _, c := range []struct {
		uid string
		arm int
	}{{hoUID, 0}, {hoUIDNotHeldOut, 1}} {
		var h = hoCreateFor(t, c.uid, hoData([]jsonmodels.Holdout{hoHoldoutA()},
			hoExperiment(1, "exp_override", hoUnitType, 0, 11)), hoOverride("exp_override", 3))
		expectInt(t, 3, h.treatment("exp_override"))
		var own = hoOwn(hoUnitType, 1, "exp_override", 3)
		own.Assigned = false
		own.Overridden = true
		h.expectPublished(own, hoExposure(hoUnitType, 11, "holdout_a", c.arm))
	}
}

func TestHoldout_TakesPrecedenceOverAudienceMismatch(t *testing.T) {
	var experiment = hoExperiment(1, "exp_holdout_audience", hoUnitType, 0, 11)
	experiment.AudienceStrict = true
	experiment.Audience = hoAgeAudience
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	h.setAttribute("age", 5)
	expectInt(t, 0, h.treatment("exp_holdout_audience"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

func TestHoldout_ExposureOncePerContextAcrossSuppressedExperiments(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_shared_a", hoUnitType, 0, 11), hoExperiment(2, "exp_shared_b", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_shared_a"))
	expectInt(t, 0, h.treatment("exp_shared_b"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

func TestHoldout_ExposureOnceAcrossAssignedExperiments(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_shared_a", hoUnitType, 0, 11), hoExperiment(2, "exp_shared_b", hoUnitType, 0, 11)))
	h.treatment("exp_shared_a")
	h.treatment("exp_shared_b")
	h.expectPublished(hoOwn(hoUnitType, 1, "exp_shared_a", 0), hoExposure(hoUnitType, 11, "holdout_a", 1),
		hoOwn(hoUnitType, 2, "exp_shared_b", 0))
}

func TestHoldout_RepeatedCallsReuseSuppressedAssignment(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_holdout_cache", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_holdout_cache"))
	expectInt(t, 0, h.treatment("exp_holdout_cache"))
	h.expectPending(1)
}

// A same-iteration seed edit must neither move the unit nor re-expose it.
func TestHoldout_SameIterationSeedChangeKeepsTheArm(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_seed_thrash", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_seed_thrash"))
	h.expectPending(1)
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_a", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_seed_thrash", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_seed_thrash"))
	h.expectPending(1)
}

func TestHoldout_CosmeticEditDoesNotReExposeHeldOutUnit(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_cosmetic", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_cosmetic"))
	var renamed = hoHoldout(11, "holdout_a_renamed", "user_id", hoSeedAHi, hoSeedALo)
	renamed.HoldoutType = "all_full_on"
	h.refresh(hoData([]jsonmodels.Holdout{renamed}, hoExperiment(1, "exp_cosmetic", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_cosmetic"))
	h.expectPending(1)
}

func TestHoldout_CosmeticEditDoesNotDuplicateCoveredExperimentExposure(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_cosmetic_covered", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_cosmetic_covered"))
	h.expectPending(2)
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_a_renamed", hoUnitType, hoSeedAHi, hoSeedALo)},
		hoExperiment(1, "exp_cosmetic_covered", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_cosmetic_covered"))
	h.expectPending(2)
}

// Scenario 224: an iteration bump is a new epoch: one new arm, exposed once.
func TestHoldout_IterationBumpReassignsToExactlyOneNewArm(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_a", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_iteration", hoUnitType, 0, 11)))
	expectInt(t, hoNormalVariant, h.treatment("exp_iteration"))
	h.expectPending(2)
	var next = hoHoldoutA()
	next.Iteration = 2
	h.refresh(hoData([]jsonmodels.Holdout{next}, hoExperiment(1, "exp_iteration", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_iteration"))
	h.expectPending(3)
	expectInt(t, 0, h.treatment("exp_iteration"))
	h.expectPending(3)
}

// An experiment first evaluated after a same-iteration seed edit uses the
// pinned arm, not one drawn from the new seed.
func TestHoldout_NewlyEvaluatedExperimentUsesPinnedArmAfterSeedEdit(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_desync_a", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_desync_a"))
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_a", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_desync_a", hoUnitType, 0, 11), hoExperiment(2, "exp_desync_b", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_desync_b"))
	expectInt(t, 0, h.treatment("exp_desync_a"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// Arity comes from the split the arm was drawn with: arm 1 of a two-arm
// holdout keeps meaning "not held out" after a same-iteration change to
// three arms.
func TestHoldout_TwoToThreeArmsSameIterationKeepsArmMeaning(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(31, "holdout_arity", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_a", hoUnitType, 0, 31)))
	expectInt(t, hoNormalVariant, h.treatment("exp_a"))
	h.refresh(hoData([]jsonmodels.Holdout{hoThreeArm(31, "holdout_arity", hoThreeArm1Lo)},
		hoExperiment(1, "exp_a", hoUnitType, 0, 31), hoExperiment(2, "exp_b", hoUnitType, 0, 31)))
	expectInt(t, hoNormalVariant, h.treatment("exp_b"))
	h.expectPublished(hoOwn(hoUnitType, 1, "exp_a", hoNormalVariant), hoExposure(hoUnitType, 31, "holdout_arity", 1),
		hoOwn(hoUnitType, 2, "exp_b", hoNormalVariant))
}

func TestHoldout_ThreeToTwoArmsSameIterationKeepsArmMeaning(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoThreeArm(41, "holdout_arity_rev", hoThreeArm1Lo)},
		hoExperiment(1, "exp_a", hoUnitType, 0, 41)))
	expectInt(t, 0, h.treatment("exp_a"))
	h.expectPending(1)
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldout(41, "holdout_arity_rev", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_a", hoUnitType, 0, 41), hoExperiment(2, "exp_b", hoUnitType, 0, 41)))
	expectInt(t, 0, h.treatment("exp_b"))
	h.expectPublished(hoExposure(hoUnitType, 41, "holdout_arity_rev", 1))
}

func TestHoldout_ExperimentsBeforeAndAfterArityChangeAgree(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(71, "holdout_c", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_pre", hoUnitType, 0, 71)))
	expectInt(t, hoNormalVariant, h.treatment("exp_pre"))
	h.refresh(hoData([]jsonmodels.Holdout{hoThreeArm(71, "holdout_c", hoThreeArm1Lo)},
		hoExperiment(1, "exp_pre", hoUnitType, 0, 71), hoExperiment(2, "exp_post", hoUnitType, 0, 71)))
	expectInt(t, hoNormalVariant, h.treatment("exp_post"))
	expectInt(t, hoNormalVariant, h.treatment("exp_pre"))
}

// A refresh landing between the decision and its exposure must not publish a
// holdout arm from the new epoch beside the experiment exposure of the old.
func TestHoldout_ExposureFiresTheDecisionEpochDespiteARacingRefresh(t *testing.T) {
	var iteration2 = hoHoldout(11, "holdout_h", hoUnitType, hoSeedAHi, hoSeedALo)
	iteration2.Iteration = 2
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_h", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_epoch_race", hoUnitType, 0, 11)))
	h.provider.next = hoData([]jsonmodels.Holdout{iteration2}, hoExperiment(1, "exp_epoch_race", hoUnitType, 0, 11))
	var once sync.Once
	h.logger.onEvent = func(event EventType, data interface{}) {
		if exposure, ok := data.(jsonmodels.Exposure); ok && event == Exposure && exposure.Id == 1 {
			once.Do(func() { h.ctx.RefreshAsync().Join(context.Background()) })
		}
	}
	expectInt(t, hoNormalVariant, h.treatment("exp_epoch_race"))
	h.expectPublished(hoOwn(hoUnitType, 1, "exp_epoch_race", hoNormalVariant), hoExposure(hoUnitType, 11, "holdout_h", 1))
}

// Scenario 221: an assignment resolved before its unit was set is resolved
// again once it is, so the holdout exposure is not lost.
func TestHoldout_SetUnitEvictsAssignmentWithAMissingArm(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_user_id", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_null_snapshot_fullon", "user_id", 2, 11)))
	expectInt(t, 2, h.peek("exp_null_snapshot_fullon"))
	h.setUnit("user_id", hoUID)
	expectInt(t, 2, h.treatment("exp_null_snapshot_fullon"))
	h.expectPending(2)
	var own = hoOwn("user_id", 1, "exp_null_snapshot_fullon", 2)
	own.FullOn = true
	var event = h.expectPublished(own, hoExposure("user_id", 11, "holdout_user_id", 1))
	expectValue(t, []jsonmodels.Unit{{Type: "user_id", Uid: string(hashUnitBytes(hoUID))}}, event.Units)
}

func TestHoldout_SetUnitKeepsAnExposedAssignment(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_user_id", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_exposed_before_unit", "user_id", 2, 11)))
	expectInt(t, 2, h.treatment("exp_exposed_before_unit"))
	h.expectPending(1)
	var exposed = h.ctx.GetAssignment("exp_exposed_before_unit")
	h.setUnit("user_id", hoUID)
	expectInt(t, 2, h.treatment("exp_exposed_before_unit"))
	h.expectPending(1)
	if h.ctx.GetAssignment("exp_exposed_before_unit") != exposed {
		t.Fatal("an exposed assignment was evicted")
	}
}

func TestHoldout_SetUnitOfAnotherTypeEvictsNothing(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_a", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_unrelated_unit", hoUnitType, 0, 11)))
	expectInt(t, hoNormalVariant, h.treatment("exp_unrelated_unit"))
	h.expectPending(2)
	h.setUnit("other_unit_type", "some-other-uid")
	expectInt(t, hoNormalVariant, h.treatment("exp_unrelated_unit"))
	h.expectPending(2)
}

func TestHoldout_SetUnitEvictsOnlyTheAffectedAssignment(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{
		hoHoldout(11, "holdout_late", "user_id", hoSeedBHi, hoSeedBLo),
		hoHoldout(12, "holdout_present", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_late_unit", "user_id", 2, 11), hoExperiment(2, "exp_present_unit", hoUnitType, 0, 12)))
	expectInt(t, 2, h.peek("exp_late_unit"))
	var present = h.ctx.GetAssignment("exp_present_unit")
	expectInt(t, hoNormalVariant, h.treatment("exp_present_unit"))
	h.expectPending(2)
	h.setUnit("user_id", hoUID)
	expectInt(t, 2, h.treatment("exp_late_unit"))
	h.expectPending(4)
	expectInt(t, hoNormalVariant, h.treatment("exp_present_unit"))
	h.expectPending(4)
	if h.ctx.GetAssignment("exp_present_unit") != present {
		t.Fatal("an unaffected assignment was evicted")
	}
}

// The arm is drawn with the covered experiment's unit type, never the
// holdout's own (scenario 227), so that is the unit type that evicts.
func TestHoldout_SetUnitEvictsByTheCoveredExperimentsUnitType(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_declares_b", "B", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_mismatched_unit_type", "A", 2, 11)))
	expectInt(t, 2, h.peek("exp_mismatched_unit_type"))
	h.setUnit("A", hoUID)
	expectInt(t, 2, h.treatment("exp_mismatched_unit_type"))
	h.expectPending(2)
	expectValue(t, "A", queuedExposures(h.ctx)[1].Unit)
}

func TestHoldout_SetUnitRecomputesSuppressionWhenTheLateArmHoldsOut(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_suppress", "user_id", hoSeedAHi, hoSeedALo)},
		hoExperiment(1, "exp_null_snapshot_suppresses", "user_id", 2, 11)))
	expectInt(t, 2, h.peek("exp_null_snapshot_suppresses"))
	h.setUnit("user_id", hoUID)
	expectInt(t, 0, h.treatment("exp_null_snapshot_suppresses"))
	h.expectPublished(hoExposure("user_id", 11, "holdout_suppress", 0))
}

// A caller still holding an assignment that setUnit evicted cannot publish it
// beside the replacement. Java: detachedTreatmentAssignmentCannotPublish...
func TestHoldout_EvictedAssignmentCannotPublishBesideItsReplacement(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_detached", "user_id", hoSeedAHi, hoSeedALo)},
		hoExperiment(1, "exp_detached_suppressed", "user_id", 2, 11)))
	var retained = h.ctx.GetAssignment("exp_detached_suppressed")
	h.setUnit("user_id", hoUID)
	expectInt(t, 0, h.treatment("exp_detached_suppressed"))
	h.expectPending(1)
	h.ctx.QueueExposure(retained)
	h.expectPublished(hoExposure("user_id", 11, "holdout_detached", 0))
}

func TestHoldout_EvictedAssignmentLeavesTheReplacementToPublish(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_detached_retry", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_detached_retry", "user_id", 2, 11)))
	var retained = h.ctx.GetAssignment("exp_detached_retry")
	h.setUnit("user_id", hoUID)
	h.ctx.QueueExposure(retained)
	h.expectPending(0)
	expectInt(t, 2, h.treatment("exp_detached_retry"))
	expectInt(t, 2, h.treatment("exp_detached_retry"))
	h.expectPending(2)
	var own = hoOwn("user_id", 1, "exp_detached_retry", 2)
	own.FullOn = true
	h.expectPublished(own, hoExposure("user_id", 11, "holdout_detached_retry", 1))
}

func TestHoldout_TreatmentAfterEvictionReturnsTheReplacementVariant(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_detached_variant", "user_id", hoSeedAHi, hoSeedALo)},
		hoExperiment(1, "exp_detached_variant", "user_id", 2, 11)))
	expectInt(t, 2, h.ctx.GetAssignment("exp_detached_variant").Variant)
	h.setUnit("user_id", hoUID)
	expectInt(t, 0, h.treatment("exp_detached_variant"))
	h.expectPending(1)
}

// Java settles an exposure through at most two retired assignments; Go never
// retries an evicted one, so queueing it twice is a bounded no-op.
func TestHoldout_EvictedAssignmentQueuesNothingHoweverOftenItIsQueued(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_bounded", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_bounded_retry", "user_id", 1, 11)))
	var retained = h.ctx.GetAssignment("exp_bounded_retry")
	h.setUnit("user_id", hoUID)
	for i := 0; i < 3; i++ {
		h.ctx.QueueExposure(retained)
	}
	h.expectPending(0)
}

func TestHoldout_ExposureSettlesOnTheCurrentAssignmentAfterTwoEvictions(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{
		hoHoldout(11, "holdout_first", "first", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_settled", "first", 1, 11)))
	var stale = h.ctx.GetAssignment("exp_settled")
	h.setUnit("first", hoUID)
	h.ctx.QueueExposure(stale)
	expectInt(t, 1, h.treatment("exp_settled"))
	h.expectPending(2)
}

// After setUnit and a refresh adding a lower-id experiment for the key, the
// variable resolves to the new winner, and the evicted assignment publishes
// nothing.
func TestHoldout_VariableAfterEvictionResolvesTheNewWinner(t *testing.T) {
	var original = hoExperiment(2, "exp_detached_variable_old", "user_id", 1, 11)
	original.Variants[1].Config = `{"detached_var":"old"}`
	var holdout = hoHoldout(11, "holdout_detached_variable", "user_id", hoSeedBHi, hoSeedBLo)
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{holdout}, original))
	var retained = h.ctx.GetAssignment(original.Name)
	h.setUnit("user_id", hoUID)
	var winner = hoExperiment(1, "exp_detached_variable_winner", "user_id", 1, 11)
	winner.Variants[1].Config = `{"detached_var":"new winner"}`
	h.refresh(hoData([]jsonmodels.Holdout{holdout}, winner, original))
	h.ctx.QueueExposure(retained)
	h.expectPending(0)
	expectValue(t, "new winner", h.variable("detached_var"))
	h.expectPending(2)
	expectValue(t, "new winner", h.variable("detached_var"))
	var own = hoOwn("user_id", 1, winner.Name, 1)
	own.FullOn = true
	h.expectPublished(own, hoExposure("user_id", 11, "holdout_detached_variable", 1))
}

// An exposed assignment that saw no unit keeps its missing arm: a later read
// never draws the arm live and publishes a contradictory holdout exposure.
func TestHoldout_PinnedMissingArmIsNeverDrawnForAnExposedAssignment(t *testing.T) {
	var experiment = hoExperiment(1, "exp_pinned_null", "user_id", 1, 11)
	experiment.Variants[1].Config = `{"my_var":"value_from_variant"}`
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_pinned_null", "user_id", hoSeedAHi, hoSeedALo)}, experiment))
	expectInt(t, 1, h.treatment("exp_pinned_null"))
	h.expectPending(1)
	h.setUnit("user_id", hoUID)
	expectValue(t, "value_from_variant", h.variable("my_var"))
	h.expectPending(1)
}

func TestHoldout_SetUnitEvictsWhateverTheHoldoutsDeclareAsUnitType(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{
		hoHoldout(11, "holdout_11", "type_x", hoSeedBHi, hoSeedBLo),
		hoHoldout(12, "holdout_12", "type_y", hoSeedBHi, hoSeedBLo),
		hoHoldout(13, "holdout_13", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_multi_unit_types", "user_id", 2, 11, 12, 13)))
	expectInt(t, 2, h.peek("exp_multi_unit_types"))
	h.setUnit("user_id", hoUID)
	expectInt(t, 2, h.treatment("exp_multi_unit_types"))
	h.expectPending(4)
}

// Scenario 227 and H6: one holdout covering experiments of two unit types
// keeps an arm, and an exposure, per unit type.
func TestHoldout_OneHoldoutKeepsAnArmPerUnitType(t *testing.T) {
	var h = hoCreate(t, map[string]string{"user_id": hoUID, hoUnitType: hoUID}, hoData(
		[]jsonmodels.Holdout{hoHoldout(11, "holdout_shared", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_unit_a1", "user_id", 0, 11), hoExperiment(2, "exp_unit_b", hoUnitType, 0, 11),
		hoExperiment(3, "exp_unit_a2", "user_id", 0, 11)))
	expectInt(t, hoNormalVariant, h.treatment("exp_unit_a1"))
	expectInt(t, hoNormalVariant, h.treatment("exp_unit_b"))
	expectInt(t, hoNormalVariant, h.treatment("exp_unit_a2"))
	h.expectPublished(hoOwn("user_id", 1, "exp_unit_a1", hoNormalVariant), hoExposure("user_id", 11, "holdout_shared", 1),
		hoOwn(hoUnitType, 2, "exp_unit_b", hoNormalVariant), hoExposure(hoUnitType, 11, "holdout_shared", 1),
		hoOwn("user_id", 3, "exp_unit_a2", hoNormalVariant))
}

func TestHoldout_SetUnitBeforeAnyAssignmentIsHarmless(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_a", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_no_prior_assignment", hoUnitType, 0, 11)))
	h.setUnit(hoUnitType, hoUID)
	expectInt(t, hoNormalVariant, h.treatment("exp_no_prior_assignment"))
}

func TestHoldout_SetUnitTwiceWithTheSameUidIsANoOp(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_a", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_set_unit_twice", hoUnitType, 0, 11)))
	expectInt(t, hoNormalVariant, h.treatment("exp_set_unit_twice"))
	h.expectPending(2)
	h.setUnit(hoUnitType, hoUID)
	expectInt(t, hoNormalVariant, h.treatment("exp_set_unit_twice"))
	h.expectPending(2)
}

// Only assignments with a missing arm are evicted. A fully resolved one
// survives a redundant setUnit: a same-iteration split edit, which would give
// a different variant if it were re-resolved, shows it was kept.
func TestHoldout_RedundantSetUnitKeepsAFullyResolvedAssignment(t *testing.T) {
	var holdout = hoHoldout(11, "holdout_a", hoUnitType, hoSeedBHi, hoSeedBLo)
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{holdout}, hoExperiment(1, "exp_redundant", hoUnitType, 0, 11)))
	expectInt(t, hoNormalVariant, h.peek("exp_redundant"))
	var refreshed = hoExperiment(1, "exp_redundant", hoUnitType, 0, 11)
	refreshed.Split = []float64{1, 0}
	h.refresh(hoData([]jsonmodels.Holdout{holdout}, refreshed))
	h.setUnit(hoUnitType, hoUID)
	expectInt(t, hoNormalVariant, h.treatment("exp_redundant"))
	h.expectPublished(hoOwn(hoUnitType, 1, "exp_redundant", hoNormalVariant), hoExposure(hoUnitType, 11, "holdout_a", 1))
}

func TestHoldout_PeekVariableThenSetUnitRecomputesUnderALateHoldingArm(t *testing.T) {
	var experiment = hoExperiment(1, "exp_var_late", "user_id", 1, 11)
	experiment.Variants[1].Config = `{"my_var":"value_from_variant"}`
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_var_late", "user_id", hoSeedAHi, hoSeedALo)}, experiment))
	expectValue(t, "value_from_variant", h.peekVariable("my_var"))
	h.expectPending(0)
	h.setUnit("user_id", hoUID)
	expectValue(t, "default", h.variable("my_var"))
	h.expectPublished(hoExposure("user_id", 11, "holdout_var_late", 0))
}

// An override resolved without the unit pins a missing arm too (the JS SDK
// resolves holdouts for overrides as well), so setUnit re-resolves it while
// it is unexposed and the holdout exposure fires with the unit.
func TestHoldout_SetUnitResolvesTheHoldoutOfAnUnexposedOverride(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_override_late", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_override_late_unit", "user_id", 0, 11)), hoOverride("exp_override_late_unit", 3))
	expectInt(t, 3, h.peek("exp_override_late_unit"))
	h.setUnit("user_id", hoUID)
	expectInt(t, 3, h.treatment("exp_override_late_unit"))
	h.expectPending(2)
	var own = hoOwn("user_id", 1, "exp_override_late_unit", 3)
	own.Assigned = false
	own.Overridden = true
	h.expectPublished(own, hoExposure("user_id", 11, "holdout_override_late", 1))
}

func TestHoldout_SetUnitLetsACustomAssignmentApply(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_custom_late", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_custom_late_unit", "user_id", 0, 11)), hoCustom("exp_custom_late_unit", 3))
	expectInt(t, 0, h.peek("exp_custom_late_unit"))
	h.setUnit("user_id", hoUID)
	expectInt(t, 3, h.treatment("exp_custom_late_unit"))
	var own = hoOwn("user_id", 1, "exp_custom_late_unit", 3)
	own.Custom = true
	h.expectPublished(own, hoExposure("user_id", 11, "holdout_custom_late", 1))
}

// Cross-SDK parity vectors from the Java SDK, which pins them against the
// collector's verdicts: unicode units, seeds with both sign bits set, the 1%
// and 10% boundaries, and a unit held out by two holdouts.

const hoVerdictUnitType = "verdict_unit_type"

func hoVerdictContext(t *testing.T, uid string, holdouts ...jsonmodels.Holdout) *hoContext {
	var ids []int
	for _, holdout := range holdouts {
		ids = append(ids, holdout.Id)
	}
	return hoCreate(t, map[string]string{hoVerdictUnitType: uid},
		hoData(holdouts, hoExperiment(9, "verdict_vectors_experiment", hoVerdictUnitType, 0, ids...)))
}

func hoVerdictHoldout(id int, seedHi int, seedLo int, split ...float64) jsonmodels.Holdout {
	var holdout = hoHoldout(id, "holdout_"+string(rune('0'+id/100))+string(rune('0'+id/10%10))+string(rune('0'+id%10)),
		hoVerdictUnitType, seedHi, seedLo)
	holdout.Split = split
	return holdout
}

func hoExpectHeldOut(t *testing.T, h *hoContext, holdout jsonmodels.Holdout) {
	t.Helper()
	expectInt(t, 0, h.treatment("verdict_vectors_experiment"))
	h.expectPublished(hoExposure(hoVerdictUnitType, holdout.Id, holdout.Name, 0))
}

func hoExpectNotHeldOut(t *testing.T, h *hoContext, holdout jsonmodels.Holdout) {
	t.Helper()
	h.treatment("verdict_vectors_experiment")
	var exposures = queuedExposures(h.ctx)
	if len(exposures) != 2 || exposures[1] != hoExposure(hoVerdictUnitType, holdout.Id, holdout.Name, 1) {
		t.Fatalf("exposures %+v", exposures)
	}
}

func TestHoldout_VerdictUnicodeUnits(t *testing.T) {
	var holdout = hoVerdictHoldout(107, 0, 42, 0.1, 0.9)
	hoExpectHeldOut(t, hoVerdictContext(t, "façade", holdout), holdout)
	hoExpectNotHeldOut(t, hoVerdictContext(t, "unicode_emoji_\U0001F680_0", holdout), holdout)
}

func TestHoldout_VerdictSignedSeedHalves(t *testing.T) {
	var seed int64 = -9223372034707292160
	var holdout = hoVerdictHoldout(106, int(int32(seed>>32)), int(int32(seed)), 0.1, 0.9)
	hoExpectHeldOut(t, hoVerdictContext(t, "signed_unit_4", holdout), holdout)
	hoExpectNotHeldOut(t, hoVerdictContext(t, "signed_unit_b_1", holdout), holdout)
}

func TestHoldout_VerdictOnePercentBoundary(t *testing.T) {
	var holdout = hoVerdictHoldout(105, 0, 999999, 0.01, 0.99)
	hoExpectHeldOut(t, hoVerdictContext(t, "pct1_unit_188", holdout), holdout)
	hoExpectNotHeldOut(t, hoVerdictContext(t, "pct1_unit_0", holdout), holdout)
}

func TestHoldout_VerdictTenPercentBoundary(t *testing.T) {
	var holdout = hoVerdictHoldout(107, 0, 42, 0.1, 0.9)
	hoExpectHeldOut(t, hoVerdictContext(t, "boundary_unit_175653", holdout), holdout)
	hoExpectNotHeldOut(t, hoVerdictContext(t, "boundary_unit_75792", holdout), holdout)
}

func TestHoldout_VerdictHeldOutByTwoHoldouts(t *testing.T) {
	var a, b = hoVerdictHoldout(107, 0, 42, 0.1, 0.9), hoVerdictHoldout(108, 0, 55, 0.1, 0.9)
	var h = hoVerdictContext(t, "dual_holdout_unit_5", a, b)
	expectInt(t, 0, h.treatment("verdict_vectors_experiment"))
	h.expectPublished(hoExposure(hoVerdictUnitType, 107, "holdout_107", 0), hoExposure(hoVerdictUnitType, 108, "holdout_108", 0))
}

func TestHoldout_VariableReadFiresTheHoldoutForBothArms(t *testing.T) {
	for _, c := range []struct {
		uid     string
		pending int
	}{{hoUID, 1}, {hoUIDNotHeldOut, 2}} {
		var experiment = hoExperiment(1, "exp_var", hoUnitType, 0, 11)
		experiment.Variants[1].Config = `{"var_a":"value_a"}`
		var h = hoCreateFor(t, c.uid, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
		expectValue(t, "default", h.variable("var_a"))
		h.expectPending(c.pending)
	}
}

func TestHoldout_SharedKeySkipsTheSuppressedExperimentForAnAssignedOne(t *testing.T) {
	var suppressed = hoExperiment(1, "exp_var_key_suppressed", hoUnitType, 0, 11)
	suppressed.Variants[1].Config = `{"shared":"from_suppressed"}`
	var assigned = hoExperiment(2, "exp_var_key_assigned", hoUnitType, 0)
	assigned.Variants[1].Config = `{"shared":"from_assigned"}`
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, suppressed, assigned))
	expectValue(t, "from_assigned", h.variable("shared"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0), hoOwn(hoUnitType, 2, "exp_var_key_assigned", hoNormalVariant))
}

func TestHoldout_PeekVariableFiresNothing(t *testing.T) {
	var suppressed = hoExperiment(1, "exp_suppressed", hoUnitType, 0, 11)
	suppressed.Variants[1].Config = `{"shared_peek":"from_suppressed"}`
	var assigned = hoExperiment(2, "exp_assigned", hoUnitType, 0)
	assigned.Variants[1].Config = `{"shared_peek":"from_assigned"}`
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, suppressed, assigned))
	expectValue(t, "from_assigned", h.peekVariable("shared_peek"))
	h.expectPending(0)
}

// A panicking logger is contained, and the sibling holdout still publishes.
func TestHoldout_PanickingLoggerDoesNotLoseASiblingHoldoutExposure(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA(),
		hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_throwing_logger", hoUnitType, 0, 11, 12)))
	h.logger.onEvent = func(event EventType, data interface{}) {
		if exposure, ok := data.(jsonmodels.Exposure); ok && exposure.Id == 11 {
			panic("boom")
		}
	}
	expectInt(t, 0, h.treatment("exp_throwing_logger"))
	h.expectPending(2)
}

func TestHoldout_PanickingLoggerDoesNotMakeExposuresRetryable(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_terminal", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_terminal", hoUnitType, 0, 11)))
	h.logger.onEvent = func(event EventType, data interface{}) {
		if exposure, ok := data.(jsonmodels.Exposure); ok && exposure.Id == 1 {
			panic("boom")
		}
	}
	h.treatment("exp_terminal")
	h.logger.onEvent = nil
	expectInt(t, hoNormalVariant, h.treatment("exp_terminal"))
	h.expectPublished(hoOwn(hoUnitType, 1, "exp_terminal", hoNormalVariant), hoExposure(hoUnitType, 11, "holdout_terminal", 1))
}

func (h *hoContext) flushScheduled() bool {
	h.ctx.TimeoutLock_.Lock()
	defer h.ctx.TimeoutLock_.Unlock()
	return h.ctx.Timeout_ != nil
}

// The suppressed experiment queues nothing of its own, so the holdout
// exposure alone must schedule the flush, even when the logger panics.
func TestHoldout_HoldoutOnlyExposureSchedulesTheFlush(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_stranded_flush", hoUnitType, 0, 11)), hoPublishDelay(3_600_000))
	h.logger.onEvent = func(event EventType, data interface{}) {
		if event == Exposure {
			panic("boom")
		}
	}
	expectInt(t, 0, h.treatment("exp_stranded_flush"))
	h.expectPending(1)
	if !h.flushScheduled() {
		t.Fatal("no flush scheduled")
	}
}

func TestHoldout_MalformedHoldoutEntriesAreIgnored(t *testing.T) {
	for _, holdouts := range []string{`[null]`, `[{"id":11,"name":"holdout_null_split","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":null}]`,
		`[{"id":11,"name":"holdout_empty_split","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":[]}]`,
		`[{"id":11,"name":"holdout_no_split","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111}]`,
		`[5, "x", [1], {"id":"11","split":[0.1,0.9]}]`, `{"id":11}`, `"x"`, `null`} {
		var payload = `{"experiments":[{"id":1,"name":"exp_malformed","unitType":"session_id","iteration":1,"seedHi":100,"seedLo":200,
			"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"fullOnVariant":0,
			"variants":[{"name":"A"},{"name":"B"}],"holdoutIds":[11]}],"holdouts":` + holdouts + `}`
		var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
		if err != nil {
			t.Fatalf("%s: %v", holdouts, err)
		}
		var h = hoCreateFor(t, hoUID, data)
		expectInt(t, hoNormalVariant, h.treatment("exp_malformed"))
		h.expectPublished(hoOwn(hoUnitType, 1, "exp_malformed", hoNormalVariant))
	}
}

func TestHoldout_EmptyOrMissingSplitIsIgnored(t *testing.T) {
	for _, split := range [][]float64{nil, {}} {
		var holdout = hoHoldoutA()
		holdout.Split = split
		var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{holdout}, hoExperiment(1, "exp_split", hoUnitType, 0, 11)))
		expectInt(t, hoNormalVariant, h.treatment("exp_split"))
		h.expectPublished(hoOwn(hoUnitType, 1, "exp_split", hoNormalVariant))
	}
}

// A holdout definition from before a refresh never replaces the arm drawn
// from the installed, newer one.
func TestHoldout_StaleHoldoutNeverReplacesANewerArm(t *testing.T) {
	var it1 = hoHoldout(11, "holdout_h", hoUnitType, hoSeedAHi, hoSeedALo)
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{it1}, hoExperiment(1, "exp_f", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_f"))
	h.expectPending(1)
	var it2 = hoHoldout(11, "holdout_h", hoUnitType, hoSeedBHi, hoSeedBLo)
	it2.Iteration = 2
	h.refresh(hoData([]jsonmodels.Holdout{it2}, hoExperiment(1, "exp_f", hoUnitType, 0, 11), hoExperiment(2, "exp_g", hoUnitType, 0, 11)))
	expectInt(t, hoNormalVariant, h.treatment("exp_g"))
	h.expectPending(3)

	h.ctx.ContextLock_.Lock()
	var arm = h.ctx.holdoutArm(&it1, hoUnitType)
	h.ctx.ContextLock_.Unlock()
	if arm.assignment.Iteration != 2 || arm.assignment.Variant != 1 || !arm.assignment.Exposed.Load().(bool) {
		t.Fatalf("stale holdout gave arm %+v", arm.assignment)
	}
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_h", 0), hoOwn(hoUnitType, 2, "exp_g", hoNormalVariant),
		hoExposure(hoUnitType, 11, "holdout_h", 1))
}

// Scenario 225: coverage added by a refresh re-resolves an exposed
// override, so the new holdout fires (and, as for any re-resolved
// assignment, the override's exposure too).
func TestHoldout_CoverageAddedByRefreshFiresForAnOverriddenExperiment(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp_override_late_holdout", hoUnitType, 0)),
		hoOverride("exp_override_late_holdout", 3))
	expectInt(t, 3, h.treatment("exp_override_late_holdout"))
	h.expectPending(1)
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_late", hoUnitType, hoSeedAHi, hoSeedALo)},
		hoExperiment(1, "exp_override_late_holdout", hoUnitType, 0, 11)))
	expectInt(t, 3, h.treatment("exp_override_late_holdout"))
	h.expectPending(3)
	var own = hoOwn(hoUnitType, 1, "exp_override_late_holdout", 3)
	own.Assigned = false
	own.Overridden = true
	h.expectPublished(own, own, hoExposure(hoUnitType, 11, "holdout_late", 0))
}

func TestHoldout_CoverageRemovedByRefreshReResolves(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp_uncovered", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp_uncovered"))
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp_uncovered", hoUnitType, 0)))
	expectInt(t, hoNormalVariant, h.treatment("exp_uncovered"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0), hoOwn(hoUnitType, 1, "exp_uncovered", hoNormalVariant))
}

func TestHoldout_VariablePathHoldoutExposureSchedulesTheFlush(t *testing.T) {
	var suppressed = hoExperiment(1, "exp_var_flush_suppressed", hoUnitType, 0, 11)
	suppressed.Variants[1].Config = `{"flush_key":"from_suppressed"}`
	var assigned = hoExperiment(2, "exp_var_flush_assigned", hoUnitType, 0)
	assigned.Variants[1].Config = `{"flush_key":"from_assigned"}`
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, suppressed, assigned), hoPublishDelay(3_600_000))
	h.treatment("exp_var_flush_assigned")
	h.ctx.ClearTimeout()
	expectValue(t, "from_assigned", h.variable("flush_key"))
	h.expectPending(2)
	if !h.flushScheduled() {
		t.Fatal("no flush scheduled")
	}
}

func TestHoldout_SetDataDoesNotModifyTheCallersData(t *testing.T) {
	var ids = []int{12, 11}
	var holdouts = []jsonmodels.Holdout{hoHoldoutA()}
	var covered = hoExperiment(1, "exp_no_mutate_in", hoUnitType, 0)
	covered.HoldoutIds = ids
	var h = hoCreateFor(t, hoUID, hoData(holdouts, covered, hoExperiment(2, "exp_no_mutate_not_covered", hoUnitType, 0)))
	expectInt(t, 0, h.treatment("exp_no_mutate_in"))
	expectInt(t, hoNormalVariant, h.treatment("exp_no_mutate_not_covered"))
	expectValue(t, []int{12, 11}, ids)
	holdouts[0].Iteration = 5
	holdouts[0].Id = 99
	expectInt(t, 0, h.treatment("exp_no_mutate_in"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0), hoOwn(hoUnitType, 2, "exp_no_mutate_not_covered", hoNormalVariant))
}

const hoUSRule = `{"rules":[{"name":"US Internal Users","type":"assign","conditions":{"and":[{"eq":[{"var":"country"},{"value":"US"}]}]},"environments":[],"variant":1}]}`

// Scenario 305: a matching assignment rule wins over holdout suppression, and
// the holdout still publishes.
func TestHoldout_MatchingAssignRuleWinsOverSuppression(t *testing.T) {
	var experiment = hoExperiment(1, "exp_holdout_rules", hoUnitType, 0, 11)
	experiment.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_rules", hoUnitType, hoSeedAHi, hoSeedALo)}, experiment))
	h.setAttribute("country", "US")
	expectInt(t, 1, h.treatment("exp_holdout_rules"))
	var own = hoOwn(hoUnitType, 1, "exp_holdout_rules", 1)
	own.Assigned = false
	own.RuleOverride = true
	var event = h.expectPublished(own, hoExposure(hoUnitType, 11, "holdout_rules", 0))
	expectValue(t, []jsonmodels.Attribute{{Name: "country", Value: "US", SetAt: hoMillis}}, event.Attributes)
}

func TestHoldout_MatchingSplitRuleWinsOverSuppression(t *testing.T) {
	var experiment = hoExperiment(1, "exp_holdout_split_rule", hoUnitType, 0, 11)
	experiment.AssignmentRules = `{"rules":[{"name":"Everyone","type":"split","conditions":null,"environments":[],"percentages":"0/100"}]}`
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	expectInt(t, 1, h.peek("exp_holdout_split_rule"))
	expectInt(t, 1, h.treatment("exp_holdout_split_rule"))
	h.expectPending(2)
}

func TestHoldout_HeldOutUnitIsSuppressedUntilTheRuleMatches(t *testing.T) {
	var experiment = hoExperiment(1, "exp_holdout_rules", hoUnitType, 0, 11)
	experiment.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	h.setAttribute("country", "GB")
	expectInt(t, 0, h.treatment("exp_holdout_rules"))
	h.expectPending(1)
	h.setAttribute("country", "US")
	expectInt(t, 1, h.treatment("exp_holdout_rules"))
	h.expectPending(2)
}

func TestHoldout_HeldOutUnitIsSuppressedAgainWhenTheRuleStopsMatching(t *testing.T) {
	var experiment = hoExperiment(1, "exp_holdout_rules", hoUnitType, 0, 11)
	experiment.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	h.setAttribute("country", "US")
	expectInt(t, 1, h.treatment("exp_holdout_rules"))
	h.expectPending(2)
	h.setAttribute("country", "GB")
	expectInt(t, 0, h.treatment("exp_holdout_rules"))
	expectInt(t, 0, h.peek("exp_holdout_rules"))
	h.expectPending(2)
}

func hoWithVariables(experiment jsonmodels.Experiment, key string) jsonmodels.Experiment {
	experiment.Variants = []jsonmodels.ExperimentVariant{
		{Name: "A", Config: `{"` + key + `":"control"}`}, {Name: "B", Config: `{"` + key + `":"treatment"}`}}
	return experiment
}

func TestHoldout_VariableOfARuleWonHeldOutExperiment(t *testing.T) {
	var experiment = hoWithVariables(hoExperiment(1, "exp_holdout_rules", hoUnitType, 0, 11), "color")
	experiment.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_rules", hoUnitType, hoSeedAHi, hoSeedALo)}, experiment))
	h.setAttribute("country", "US")
	expectValue(t, "treatment", h.variable("color"))
	var own = hoOwn(hoUnitType, 1, "exp_holdout_rules", 1)
	own.Assigned = false
	own.RuleOverride = true
	h.expectPublished(own, hoExposure(hoUnitType, 11, "holdout_rules", 0))
}

func TestHoldout_SharedKeyPrefersARuleWinOverASuppressedExperiment(t *testing.T) {
	var heldOut = hoWithVariables(hoExperiment(1, "exp_held_out", hoUnitType, 0, 11), "color")
	var ruleWon = hoWithVariables(hoExperiment(2, "exp_rule_won", hoUnitType, 0, 11), "color")
	ruleWon.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, heldOut, ruleWon))
	h.setAttribute("country", "GB")
	expectValue(t, "control", h.peekVariable("color"))
	h.setAttribute("country", "US")
	expectValue(t, "treatment", h.peekVariable("color"))
}

func TestHoldout_SharedKeyPrefersAnEarlierAssignedOverALaterRuleWin(t *testing.T) {
	var assigned = hoWithVariables(hoExperiment(1, "exp_assigned", hoUnitType, 0), "color")
	assigned.Split = []float64{1, 0}
	var ruleWon = hoWithVariables(hoExperiment(2, "exp_rule_won", hoUnitType, 0), "color")
	ruleWon.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{}, assigned, ruleWon))
	h.setAttribute("country", "US")
	expectValue(t, "control", h.peekVariable("color"))
	expectInt(t, 1, h.peek("exp_rule_won"))
}

// Scenario 226: with no other winner, a suppressed experiment gives its
// control value, not the caller's default.
func TestHoldout_SuppressedVariableReturnsTheControlValue(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoWithVariables(hoExperiment(1, "exp_suppressed", hoUnitType, 0, 11), "color")))
	expectValue(t, "control", h.peekVariable("color"))
	expectValue(t, "control", h.variable("color"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// The first suppressed candidate is the fallback even if it lacks the key, so
// a later suppressed one cannot supply it (JS _resolveVariableValue).
func TestHoldout_SuppressedFallbackIsTheFirstSuppressedCandidate(t *testing.T) {
	var first = hoExperiment(1, "exp_first", hoUnitType, 0, 11)
	first.Variants[0].Config = `{"other":"x"}`
	first.Variants[1].Config = `{"color":"first"}`
	var second = hoWithVariables(hoExperiment(2, "exp_second", hoUnitType, 0, 11), "color")
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, first, second))
	expectValue(t, "default", h.peekVariable("color"))
}

// Scenario 223: publishing keeps the exposed state.
func TestHoldout_ExposureStateSurvivesPublish(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.treatment("exp")
	h.expectPublished(hoOwn(hoUnitType, 1, "exp", 0), hoExposure(hoUnitType, 11, "holdout_a", 1))
	h.treatment("exp")
	h.expectPending(0)
}

// H1: a holdout id listed twice is one holdout with one exposure.
func TestHoldout_DuplicateHoldoutIdsPublishOnce(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA(),
		hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_dup", hoUnitType, 0, 12, 11, 12, 11)))
	h.treatment("exp_dup")
	h.expectPublished(hoOwn(hoUnitType, 1, "exp_dup", 0), hoExposure(hoUnitType, 11, "holdout_a", 1),
		hoExposure(hoUnitType, 12, "holdout_b", 1))
}

// H4: peek publishes no holdout exposure.
func TestHoldout_PeekQueuesNoHoldoutExposure(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, 0, h.peek("exp"))
	h.expectPending(0)
}

// H7: a strict audience mismatch on a covered experiment that is not held out
// still fires the holdout.
func TestHoldout_StrictAudienceMismatchStillFiresTheHoldout(t *testing.T) {
	var experiment = hoExperiment(1, "exp_strict", hoUnitType, 0, 11)
	experiment.AudienceStrict = true
	experiment.Audience = hoAgeAudience
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	h.setAttribute("age", 5)
	expectInt(t, 0, h.treatment("exp_strict"))
	var own = hoOwn(hoUnitType, 1, "exp_strict", 0)
	own.Assigned = false
	own.AudienceMismatch = true
	h.expectPublished(own, hoExposure(hoUnitType, 11, "holdout_a", 1))
}

// H8: the first candidate is suppressed and the second assigned: the
// assigned one wins and both holdouts fire.
func TestHoldout_SharedKeyFiresTheHoldoutsOfEveryCandidate(t *testing.T) {
	var suppressed = hoWithVariables(hoExperiment(1, "exp_suppressed", hoUnitType, 0, 11), "color")
	var assigned = hoWithVariables(hoExperiment(2, "exp_assigned", hoUnitType, 0, 12), "color")
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA(),
		hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)}, suppressed, assigned))
	expectValue(t, "treatment", h.variable("color"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0), hoOwn(hoUnitType, 2, "exp_assigned", hoNormalVariant),
		hoExposure(hoUnitType, 12, "holdout_b", 1))
}

func TestHoldout_IsHeldOutBy(t *testing.T) {
	for _, c := range []struct {
		variant, arms, fullOn int
		want                  bool
	}{
		{0, 2, 0, true}, {0, 2, 3, true}, {0, 3, 0, true}, {0, 3, 2, true},
		{1, 2, 0, false}, {1, 2, 2, false}, {1, 3, 0, true}, {1, 3, 2, false},
		{2, 3, 0, false}, {2, 3, 2, false}, {1, 4, 0, false}, {1, 1, 0, false}, {3, 4, 0, false},
	} {
		if got := isHeldOutBy(c.variant, c.arms, c.fullOn); got != c.want {
			t.Errorf("isHeldOutBy(%d, %d, %d) = %v, want %v", c.variant, c.arms, c.fullOn, got, c.want)
		}
	}
}

// The arity comes from the split, never from holdoutType (scenario 222): a
// three-value split with holdoutType "full", or none, is three-arm.
func TestHoldout_ArityComesFromTheSplitNotTheHoldoutType(t *testing.T) {
	for _, holdoutType := range []string{"", "full", "all_full_on", "future"} {
		var holdout = hoThreeArm(21, "holdout_three_arm", hoThreeArm1Lo)
		holdout.HoldoutType = holdoutType
		var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{holdout},
			hoExperiment(1, "exp_non_fullon", hoUnitType, 0, 21), hoExperiment(2, "exp_fullon", hoUnitType, 2, 21)))
		expectInt(t, 0, h.treatment("exp_non_fullon"))
		expectInt(t, 2, h.treatment("exp_fullon"))
	}
	// A two-value split labelled all_full_on is still two-arm: arm 1 assigns.
	var two = hoHoldout(21, "holdout_two_arm", hoUnitType, hoSeedBHi, hoSeedBLo)
	two.HoldoutType = "all_full_on"
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{two}, hoExperiment(1, "exp_non_fullon", hoUnitType, 0, 21)))
	expectInt(t, hoNormalVariant, h.treatment("exp_non_fullon"))
}

// H5 / contracts §5: unknown keys inside holdout entries, and unknown
// holdout-related fields elsewhere, are ignored by the default decoder, with
// no error event.
func TestHoldout_DefaultDecoderIgnoresUnknownHoldoutFields(t *testing.T) {
	var payload = `{
		"experiments":[
			{"id":1,"name":"covered","unitType":"session_id","iteration":1,"seedHi":100,"seedLo":200,"split":[0.5,0.5],
			 "trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"fullOnVariant":0,
			 "variants":[{"name":"A"},{"name":"B"}],"holdoutIds":[11,"12",null,{"id":13},1.5],"holdoutFuture":{"x":[1]}},
			{"id":2,"name":"other","unitType":"session_id","iteration":1,"seedHi":100,"seedLo":200,"split":[0.5,0.5],
			 "trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"fullOnVariant":0,
			 "variants":[{"name":"A"},{"name":"B"}],"holdoutIds":"11"}
		],
		"holdouts":[
			{"id":11,"name":"holdout_a","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":[0.1,0.9],
			 "holdoutType":"full","futureScalar":1,"futureObject":{"a":[1,{"b":2}]},"futureArray":[{"x":1}],
			 "trafficSplit":[0,1],"variants":[{"name":"A","futureField":1}],"switchbackClusters":{"a":"b"}},
			{"id":12,"name":"holdout_unknown_type","iteration":1,"seedHi":1,"seedLo":222,"split":[0.1,0.9],"holdoutType":"future_kind"}
		],
		"futureHoldoutRoot":{"nested":[1]}
	}`
	var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	expectValue(t, []int{11}, data.Experiments[0].HoldoutIds)
	expectValue(t, []int(nil), data.Experiments[1].HoldoutIds)
	expectInt(t, 2, len(data.Holdouts))
	var errors []interface{}
	var h = hoCreateFor(t, hoUID, data)
	h.logger.onEvent = func(event EventType, data interface{}) {
		if event == Error {
			errors = append(errors, data)
		}
	}
	expectInt(t, 0, h.treatment("covered"))
	expectInt(t, hoNormalVariant, h.treatment("other"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0), hoOwn(hoUnitType, 2, "other", hoNormalVariant))
	expectInt(t, 0, len(errors))
}

// Holdout models round-trip with encoding/json, and a ContextData without
// holdouts marshals exactly as before (no new keys).
func TestHoldout_ModelJSON(t *testing.T) {
	var raw, _ = json.Marshal(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{}})
	expectValue(t, `{"experiments":[]}`, string(raw))
	raw, _ = json.Marshal(jsonmodels.Experiment{Id: 1})
	var fields map[string]interface{}
	_ = json.Unmarshal(raw, &fields)
	if _, found := fields["holdoutIds"]; found {
		t.Fatalf("holdoutIds marshalled when empty: %s", raw)
	}
	var data jsonmodels.ContextData
	if err := json.Unmarshal([]byte(`{"holdouts":[{"id":3,"name":"h","unitType":"u","iteration":2,"seedHi":1,"seedLo":2,"split":[0.5,0.5],"holdoutType":"full","x":1}],"experiments":[{"id":1,"holdoutIds":[3]}]}`), &data); err != nil {
		t.Fatal(err)
	}
	expectValue(t, []jsonmodels.Holdout{{Id: 3, Name: "h", UnitType: "u", Iteration: 2, SeedHi: 1, SeedLo: 2,
		Split: []float64{0.5, 0.5}, HoldoutType: "full"}}, data.Holdouts)
	expectValue(t, []int{3}, data.Experiments[0].HoldoutIds)
}

// hoPublishProbe treats every experiment of a payload with no holdouts for a
// few units and returns each publish body as JSON, units sorted.
func hoPublishProbe(t *testing.T, data jsonmodels.ContextData) string {
	t.Helper()
	var out []byte
	for _, uid := range []string{"bleh@absmartly.com", "façade", "🚀rocket", hoUID, hoUIDNotHeldOut} {
		var h = hoCreate(t, map[string]string{"session_id": uid, "user_id": uid + "u", "email": uid + "e"}, data)
		h.setAttribute("age", 25)
		for _, experiment := range data.Experiments {
			h.treatment(experiment.Name)
		}
		var keys, _ = h.ctx.GetVariableKeys()
		for key := range keys {
			h.variable(key)
		}
		if err := h.ctx.Publish(); err != nil {
			t.Fatal(err)
		}
		for _, event := range h.publisher.events {
			sort.Slice(event.Units, func(i, j int) bool { return event.Units[i].Type < event.Units[j].Type })
			var raw, _ = json.Marshal(event)
			out = append(append(out, raw...), '\n')
		}
	}
	return string(out)
}

// Without holdouts the publish body is byte-identical to the one before
// holdouts were added: testAssets/publish-no-holdouts.golden was written by
// this probe at main 54b4e8a.
func TestHoldout_PublishWithoutHoldoutsIsUnchanged(t *testing.T) {
	var raw, _ = os.ReadFile("testAssets/context.json")
	var data, err = DefaultContextDataDeserializer{}.Deserialize(raw)
	if err != nil {
		t.Fatal(err)
	}
	var got = hoPublishProbe(t, data)
	var want, _ = os.ReadFile("testAssets/publish-no-holdouts.golden")
	if got != string(want) {
		t.Fatalf("publish changed without holdouts:\n got %s\nwant %s", got, want)
	}
}

func hoBenchContext(b *testing.B, data jsonmodels.ContextData) *Context {
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{hoUnitType: hoUID}
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ft, done = future.New()
	done(data, nil)
	var ctx = CreateContext(internal.FixedClock{Millis_: hoMillis}, config, ft, &hoProvider{}, &hoPublisher{}, nil,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	ctx.WaitUntilReady()
	b.Cleanup(func() { ctx.ClearRefreshTimer() })
	return ctx
}

func benchmarkCachedTreatment(b *testing.B, data jsonmodels.ContextData) {
	var ctx = hoBenchContext(b, data)
	ctx.GetTreatment("exp")
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx.GetTreatment("exp")
		}
	})
}

// The cached read path for an experiment no holdout covers, which is every
// experiment of a payload without holdouts.
func BenchmarkHoldout_CachedTreatmentWithoutHoldouts(b *testing.B) {
	benchmarkCachedTreatment(b, hoData(nil, hoWithVariables(hoExperiment(1, "exp", hoUnitType, 0), "color")))
}

func BenchmarkHoldout_CachedTreatmentCovered(b *testing.B) {
	benchmarkCachedTreatment(b, hoData([]jsonmodels.Holdout{hoHoldoutA(), hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoWithVariables(hoExperiment(1, "exp", hoUnitType, 0, 11, 12), "color")))
}

func BenchmarkHoldout_CachedVariableWithoutHoldouts(b *testing.B) {
	var ctx = hoBenchContext(b, hoData(nil, hoWithVariables(hoExperiment(1, "exp", hoUnitType, 0), "color")))
	ctx.GetVariableValue("color", "d")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.GetVariableValue("color", "d")
	}
}

// Duplicate holdout ids resolve to one covering entry (JS insertUniqueSorted).
func TestHoldout_CoveringHoldoutsAreUniqueAndSorted(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA(), hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp_dup", hoUnitType, 0, 12, 11, 999, 12, 11)))
	var ids []int
	for _, holdout := range h.ctx.indexedExperiment("exp_dup").holdouts {
		ids = append(ids, holdout.Id)
	}
	expectValue(t, []int{11, 12}, ids)
}

// A held-out experiment skips its audience, as in the JS SDK and the
// collector, so it is never flagged as an audience mismatch.
func TestHoldout_SuppressedAssignmentSkipsTheAudience(t *testing.T) {
	var experiment = hoExperiment(1, "exp_holdout_audience", hoUnitType, 0, 11)
	experiment.Audience = hoAgeAudience
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	h.setAttribute("age", 5)
	expectInt(t, 0, h.treatment("exp_holdout_audience"))
	var assignment = h.ctx.GetAssignment("exp_holdout_audience")
	if assignment.AudienceMismatch || assignment.Assigned {
		t.Fatalf("suppressed assignment %+v", assignment)
	}
}

// A malformed holdout entry drops only itself: a valid one beside it applies.
func TestHoldout_MalformedHoldoutEntryDoesNotDropTheOthers(t *testing.T) {
	var payload = `{"experiments":[{"id":1,"name":"exp","unitType":"session_id","iteration":1,"seedHi":100,"seedLo":200,
		"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"fullOnVariant":0,
		"variants":[{"name":"A"},{"name":"B"}],"holdoutIds":[11]}],
		"holdouts":[null,5,"x",{"id":"bad"},{"id":11,"name":"holdout_a","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":[0.1,0.9]}]}`
	var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	var h = hoCreateFor(t, hoUID, data)
	expectInt(t, 0, h.treatment("exp"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// A holdout iteration bump re-resolves an exposed rule-won assignment, and
// the new epoch's holdout exposure fires with the rule win exposed again, as
// in the JS SDK.
func TestHoldout_IterationBumpReExposesARuleWonAssignment(t *testing.T) {
	var experiment = hoExperiment(1, "exp_rule", hoUnitType, 0, 11)
	experiment.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_h", hoUnitType, hoSeedBHi, hoSeedBLo)}, experiment))
	h.setAttribute("country", "US")
	expectInt(t, 1, h.treatment("exp_rule"))
	var next = hoHoldout(11, "holdout_h", hoUnitType, hoSeedAHi, hoSeedALo)
	next.Iteration = 2
	h.refresh(hoData([]jsonmodels.Holdout{next}, experiment))
	expectInt(t, 1, h.treatment("exp_rule"))
	var own = hoOwn(hoUnitType, 1, "exp_rule", 1)
	own.Assigned = false
	own.RuleOverride = true
	var arm1 = hoExposure(hoUnitType, 11, "holdout_h", 1)
	var arm0 = hoExposure(hoUnitType, 11, "holdout_h", 0)
	h.expectPublished(own, arm1, own, arm0)
}

// A rule win rebuilt with an unchanged holdout set keeps its exposed state:
// nothing is published again. The rebuild is real: the assign rule gives
// variant 2 without the unit, then a split rule that starts matching gives
// the same full-on variant 2 but as a missing-unit split, so the cached
// assignment is replaced.
func TestHoldout_UnchangedCoverageKeepsAnExposedRuleWin(t *testing.T) {
	var experiment = hoExperiment(1, "exp_rule", hoUnitType, 2, 11)
	experiment.AssignmentRules = `{"rules":[{"type":"split","percentages":"100/0/0","environments":[],` +
		`"conditions":{"eq":[{"var":"split"},{"value":true}]}},{"type":"assign","variant":2,"environments":[]}]}`
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	expectInt(t, 2, h.treatment("exp_rule"))
	h.expectPending(1)
	var before = h.ctx.GetAssignment("exp_rule")
	h.setAttribute("split", true)
	expectInt(t, 2, h.treatment("exp_rule"))
	if h.ctx.GetAssignment("exp_rule") == before {
		t.Fatal("the assignment was not rebuilt")
	}
	h.expectPending(1)
}

// A suppressed assignment superseded by a refresh cannot publish through the
// public QueueExposure: it never publishes an exposure of its own.
func TestHoldout_SupersededSuppressedAssignmentPublishesNothing(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	var stale = h.ctx.GetAssignment("exp")
	var next = hoHoldoutA()
	next.Iteration = 2
	h.refresh(hoData([]jsonmodels.Holdout{next}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.GetAssignment("exp")
	h.ctx.QueueExposure(stale)
	h.expectPending(0)
	expectInt(t, 0, h.treatment("exp"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// A same-iteration seed edit must not re-expose a unit that is not held out
// either: the holdout set identity is only (id, iteration).
func TestHoldout_SeedEditDoesNotReExposeANotHeldOutUnit(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp"))
	h.expectPending(2)
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_a", hoUnitType, hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp"))
	h.expectPending(2)
}

// Coverage removed by a refresh is a coverage change for a rule win too: it
// is exposed again, as in the JS SDK.
func TestHoldout_CoverageRemovedReExposesARuleWin(t *testing.T) {
	var experiment = hoExperiment(1, "exp_rule", hoUnitType, 0, 11)
	experiment.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	h.setAttribute("country", "US")
	expectInt(t, 1, h.treatment("exp_rule"))
	h.expectPending(2)
	var uncovered = experiment
	uncovered.HoldoutIds = nil
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldoutA()}, uncovered))
	expectInt(t, 1, h.treatment("exp_rule"))
	h.expectPending(3)
}

type hoCountingDeserializer struct {
	mu    sync.Mutex
	count int
}

func (d *hoCountingDeserializer) Deserialize(bytes []byte, offset int, length int) (map[string]interface{}, error) {
	d.mu.Lock()
	d.count++
	d.mu.Unlock()
	return DefaultAudienceDeserializer{}.Deserialize(bytes, offset, length)
}

// A suppressed assignment never evaluates its audience, also when a
// SetAttribute makes it revalidate.
func TestHoldout_SuppressedAssignmentNeverEvaluatesTheAudience(t *testing.T) {
	var experiment = hoExperiment(1, "exp_audience", hoUnitType, 0, 11)
	experiment.Audience = hoAgeAudience
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	var counter = &hoCountingDeserializer{}
	h.ctx.AudienceMatcher_ = AudienceMatcher{counter}
	expectInt(t, 0, h.treatment("exp_audience"))
	h.setAttribute("age", 10)
	expectInt(t, 0, h.treatment("exp_audience"))
	expectInt(t, 0, h.treatment("exp_audience"))
	expectInt(t, 0, counter.count)
}

// The public QueueExposure on a suppressed assignment publishes only the
// holdout's exposure, never the experiment's own.
func TestHoldout_QueueExposureOfASuppressedAssignment(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.QueueExposure(h.ctx.GetAssignment("exp"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// A held-out experiment that a rule won is not suppressed, so it is never the
// control-value fallback: the next suppressed experiment is.
func TestHoldout_RuleWonHeldOutExperimentIsNotTheFallback(t *testing.T) {
	var ruleWon = hoExperiment(1, "exp_rule_won", hoUnitType, 0, 11)
	// It defines the key, so it is visited, but not in the variant the rule
	// gives (1), so it does not win the key.
	ruleWon.Variants[0].Config = `{"color":"rule_won_control"}`
	ruleWon.Variants[1].Config = `{"other":"y"}`
	ruleWon.AssignmentRules = hoUSRule
	var suppressed = hoWithVariables(hoExperiment(2, "exp_suppressed", hoUnitType, 0, 11), "color")
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, ruleWon, suppressed))
	h.setAttribute("country", "US")
	expectValue(t, "control", h.peekVariable("color"))
}

// Holdout ids are integral numbers however written (10, 10.0, 1e1), as in
// JavaScript; strings and fractions are not ids.
func TestHoldout_IntegralHoldoutIdsInAnyNotation(t *testing.T) {
	var payload = `{"experiments":[{"id":1,"name":"exp","unitType":"session_id","iteration":1,"seedHi":100,"seedLo":200,
		"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"fullOnVariant":0,
		"variants":[{"name":"A"},{"name":"B"}],"holdoutIds":[1.1e1, 12.0, "13", 14.5, true, 1e300]}],
		"holdouts":[{"id":11.0,"name":"holdout_a","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":[0.1,0.9]},
		{"id":1.2e1,"name":"holdout_b","iteration":1,"seedHi":1,"seedLo":222,"split":[0.1,0.9]},
		{"id":"13","name":"holdout_string","iteration":1,"seedHi":13,"seedLo":111,"split":[0.1,0.9]}]}`
	var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	expectValue(t, []int{11, 12}, data.Experiments[0].HoldoutIds)
	expectInt(t, 2, len(data.Holdouts))
	var h = hoCreateFor(t, hoUID, data)
	expectInt(t, 0, h.treatment("exp"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0), hoExposure(hoUnitType, 12, "holdout_b", 1))
}

// Resolving an experiment no holdout covers allocates nothing for holdouts,
// in a context whose payload has holdouts for other experiments: no holdout
// state is attached, and its first resolution costs what a payload without
// holdouts costs.
func TestHoldout_UncoveredFirstResolutionAllocatesNothingForHoldouts(t *testing.T) {
	var plain = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)))
	var mixed = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0),
		hoExperiment(2, "other", hoUnitType, 0, 11)))
	mixed.treatment("other")
	for _, h := range []*hoContext{plain, mixed} {
		h.treatment("exp")
		if h.ctx.getAssignment("exp").holdouts != nil {
			t.Fatal("holdout state attached to an uncovered experiment")
		}
	}

}

// A held-out unit's holdout exposure rejected while the context is closing
// is queued by the next exposing call, as for any exposure.
func TestHoldout_SuppressedExposureRejectedDuringCloseIsRetried(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.Closing_.Store(true)
	h.ctx.QueueExposure(h.ctx.GetAssignment("exp"))
	h.expectPending(0)
	h.ctx.Closing_.Store(false)
	expectInt(t, 0, h.treatment("exp"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// The installed holdouts are a copy: changing the caller's slice afterwards
// changes neither the arm nor the exposure.
func TestHoldout_InstalledHoldoutsAreCopied(t *testing.T) {
	var data = hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11))
	var h = hoCreateFor(t, hoUID, data)
	data.Holdouts[0].Iteration = 99
	data.Holdouts[0].Name = "mutated"
	data.Holdouts[0].SeedHi, data.Holdouts[0].SeedLo = hoSeedBHi, hoSeedBLo
	expectInt(t, 0, h.treatment("exp"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// A wrongly typed optional field inside a holdout entry is ignored like an
// unknown one; the holdout still applies (JS never reads holdoutType).
func TestHoldout_WronglyTypedHoldoutTypeIsIgnored(t *testing.T) {
	for _, holdoutType := range []string{`5`, `{"a":1}`, `["full"]`, `true`, `null`} {
		var payload = `{"experiments":[{"id":1,"name":"exp","unitType":"session_id","iteration":1,"seedHi":100,"seedLo":200,
			"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"variants":[{"name":"A"},{"name":"B"}],"holdoutIds":[11]}],
			"holdouts":[{"id":11,"name":"holdout_a","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":[0.1,0.9],"holdoutType":` + holdoutType + `}]}`
		var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		var h = hoCreateFor(t, hoUID, data)
		expectInt(t, 0, h.treatment("exp"))
		h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
	}
}

// The holdout decoders allocate nothing for absent or empty fields, and a
// context without holdouts never creates the holdout arm map.
func TestHoldout_AbsentHoldoutFieldsAllocateNothing(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`"x"`), json.RawMessage(`{}`)} {
		if n := hoLeastAllocs(func() { holdouts(raw); holdoutIds(raw) }); n != 0 {
			t.Errorf("decoding %q: %.0f allocations", raw, n)
		}
	}
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)))
	h.treatment("exp")
	h.ctx.ContextLock_.RLock()
	defer h.ctx.ContextLock_.RUnlock()
	if h.ctx.holdoutArms_ != nil {
		t.Fatal("holdout arm map created without holdouts")
	}
}

func TestHoldout_ReadySnapshotSharesArms(t *testing.T) {
	h := hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "a", hoUnitType, 0, 11), hoExperiment(2, "b", hoUnitType, 0, 11)))
	snapshot := h.ctx.WaitUntilReady()
	if _, err := snapshot.GetTreatment("a"); err != nil {
		t.Fatal(err)
	}
	h.treatment("b")
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

func TestHoldout_QueueExposureCopies(t *testing.T) {
	for _, uid := range []string{hoUID, hoUIDNotHeldOut} {
		t.Run(uid, func(t *testing.T) {
			h := hoCreateFor(t, uid, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
			h.ctx.GetAssignment("exp")
			copy := h.ctx.AssignmentCache["exp"]
			h.ctx.QueueExposure(&copy)
			if uid == hoUID {
				h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
			} else {
				h.expectPublished(hoOwn(hoUnitType, 1, "exp", 0), hoExposure(hoUnitType, 11, "holdout_a", 1))
			}
		})
	}
}

func TestHoldout_SupersededCopyDoesNotExpose(t *testing.T) {
	h := hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.GetAssignment("exp")
	stale := h.ctx.AssignmentCache["exp"]
	next := hoHoldoutA()
	next.Iteration++
	h.refresh(hoData([]jsonmodels.Holdout{next}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.GetAssignment("exp")
	h.ctx.QueueExposure(&stale)
	h.expectPending(0)
}

func TestHoldout_OverrideUnitTypeRefresh(t *testing.T) {
	exp := hoExperiment(1, "exp", hoUnitType, 0, 11)
	h := hoCreate(t, map[string]string{hoUnitType: hoUID, "user_id": hoUIDNotHeldOut}, hoData([]jsonmodels.Holdout{hoHoldoutA()}, exp), hoOverride("exp", 1))
	h.treatment("exp")
	exp.UnitType = "user_id"
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldoutA()}, exp))
	h.treatment("exp")
	exposures := queuedExposures(h.ctx)
	if len(exposures) != 4 || exposures[3].Id != 11 || exposures[3].Unit != "user_id" {
		t.Fatalf("exposures: %+v", exposures)
	}
}

func TestHoldout_InstalledSplitIsCopied(t *testing.T) {
	data := hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11))
	h := hoCreateFor(t, hoUID, data)
	data.Holdouts[0].Split[0], data.Holdouts[0].Split[1] = 0, 1
	h.treatment("exp")
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// hoLeastAllocs is the fewest allocations of f over a few AllocsPerRun
// samples. AllocsPerRun counts the whole process, so goroutines other tests
// left running (timers, publishers) can only add to a sample.
func hoLeastAllocs(f func()) float64 {
	var least = -1.0
	for i := 0; i < 7; i++ {
		if n := testing.AllocsPerRun(20, f); least < 0 || n < least {
			least = n
		}
	}
	return least
}

// unitType and iteration of the wrong JSON type drop only that field, as an
// unknown key would: the arm uses the covered experiment's unit type, and a
// missing iteration is iteration 0.
func TestHoldout_WronglyTypedUnitTypeOrIterationIsIgnored(t *testing.T) {
	for _, fields := range []string{`"unitType":5,"iteration":1`, `"unitType":{"a":1},"iteration":1`, `"unitType":"session_id","iteration":"2"`,
		`"unitType":null,"iteration":null`} {
		var payload = `{"experiments":[{"id":1,"name":"exp","unitType":"session_id","iteration":1,"seedHi":100,"seedLo":200,
			"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"variants":[{"name":"A"},{"name":"B"}],"holdoutIds":[11]}],
			"holdouts":[{"id":11,"name":"holdout_a",` + fields + `,"seedHi":13,"seedLo":111,"split":[0.1,0.9]}]}`
		var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		var h = hoCreateFor(t, hoUID, data)
		expectInt(t, 0, h.treatment("exp"))
		h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
	}
}

// A refresh publishes coverage and holdout definitions together with the
// assignments resolved from them: an assignment never pins an arm from one
// holdout iteration beside the coverage of another.
func TestHoldout_RefreshAndResolutionUseOneGeneration(t *testing.T) {
	var experiment = hoExperiment(1, "exp", hoUnitType, 0, 11)
	var payload = func(iteration int) jsonmodels.ContextData {
		var holdout = hoHoldoutA()
		holdout.Iteration = iteration
		return hoData([]jsonmodels.Holdout{holdout}, experiment)
	}
	// The race this pins needs the refresh and the reads on different
	// threads.
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(2))
	var h = hoCreateFor(t, hoUID, payload(1))
	var done = make(chan struct{})
	go func() {
		defer close(done)
		for i := 2; i < 3000; i++ {
			h.ctx.SetData(payload(i))
		}
	}()
	var mixed = 0
	for running := true; running; {
		select {
		case <-done:
			running = false
		default:
		}
		var entry = h.ctx.getAssignment("exp")
		if entry.holdouts != nil {
			for i, arm := range entry.holdouts.arms {
				if arm != nil && arm.assignment.Iteration != entry.holdouts.holdouts[i].Iteration {
					mixed++
				}
			}
		}
	}
	expectInt(t, 0, mixed)
}

// SetData and SetDataFailed act on the live context, also when called on the
// copy WaitUntilReady returns or a logger receives, as Close, Publish and
// Refresh already do.
func TestHoldout_SetDataThroughACopyActsOnTheLiveContext(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)))
	var snapshot = h.ctx.WaitUntilReady()
	snapshot.SetData(hoData(nil, hoExperiment(1, "exp", hoUnitType, 0), hoExperiment(2, "exp2", hoUnitType, 0)))
	var names, _ = h.ctx.GetExperiments()
	sort.Strings(names)
	expectValue(t, []string{"exp", "exp2"}, names)

	snapshot.SetDataFailed(errors.New("load failed"))
	if !h.ctx.IsFailed() {
		t.Fatal("the live context is not failed")
	}
	var data, err = h.ctx.GetData()
	if err != nil {
		t.Fatal(err)
	}
	expectInt(t, 0, len(data.Experiments))
	h.ctx.DataLock.RLock()
	expectInt(t, 0, len(h.ctx.Index_))
	h.ctx.DataLock.RUnlock()
}

// A held-out experiment that a refresh removes keeps its cached decision, as
// in the JS SDK (_assign keeps a not-assigned assignment when the experiment
// is gone): a later read fires the pinned holdout exposure and nothing else.
func TestHoldout_RemovedExperimentKeepsItsPinnedDecision(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, 0, h.peek("exp"))
	h.refresh(hoData(nil))
	expectInt(t, 0, h.treatment("exp"))
	expectInt(t, 0, h.treatment("exp"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// An assignment that a refresh replaces with a held-out one cannot publish
// beside its replacement, also when it had no holdouts itself.
func TestHoldout_AssignmentSupersededByNewCoverageIsRetired(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0)))
	var retained = h.ctx.GetAssignment("exp")
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, 0, h.peek("exp"))
	h.ctx.QueueExposure(retained)
	h.expectPending(0)
	expectInt(t, 0, h.treatment("exp"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// Without holdouts, a replaced assignment whose exposure a close rejected
// stays retryable through QueueExposure, as before holdouts.
func TestHoldout_RejectedExposureWithoutHoldoutsStaysRetryableAfterReplacement(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)))
	var assignment = h.ctx.GetAssignment("exp")
	var refreshed = hoExperiment(1, "exp", hoUnitType, 0)
	refreshed.Iteration = 2
	h.refresh(hoData(nil, refreshed))
	h.ctx.GetAssignment("exp")
	h.ctx.Closing_.Store(true)
	h.ctx.QueueExposure(assignment)
	h.ctx.Closing_.Store(false)
	h.ctx.QueueExposure(assignment)
	h.expectPending(1)
}

// An exposed held-out experiment that a refresh removes keeps its
// assignment, as in the JS SDK: nothing more is published for it.
func TestHoldout_RemovedExposedExperimentPublishesNothingMore(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp"))
	h.refresh(hoData(nil))
	expectInt(t, 0, h.treatment("exp"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// An uncovered assignment whose exposure is in flight when a held-out
// assignment replaces it stays retired when a close rejects the admission.
func TestHoldout_ReplacedUncoveredAssignmentStaysRetiredAfterRejection(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0)))
	var entry = h.ctx.getAssignment("exp")
	var stale = entry.assignment
	if !hoClaim(h, entry) {
		t.Fatal("the exposure was not claimed")
	}
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, 0, h.peek("exp"))
	h.ctx.Closing_.Store(true)
	h.ctx.admitExposure(stale, entry)
	h.ctx.Closing_.Store(false)
	h.ctx.QueueExposure(stale)
	h.expectPending(0)
}

// An exposed override whose experiment a refresh removes keeps its cached
// assignment, as in the JS SDK: nothing is published again.
func TestHoldout_RemovedOverriddenExperimentIsNotReExposed(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)),
		hoOverride("exp", 1))
	expectInt(t, 1, h.treatment("exp"))
	h.refresh(hoData(nil))
	expectInt(t, 1, h.treatment("exp"))
	var own = hoOwn(hoUnitType, 1, "exp", 1)
	own.Assigned = false
	own.Overridden = true
	h.expectPublished(own, hoExposure(hoUnitType, 11, "holdout_a", 0))
}

// A covered assignment whose decision is in flight when an uncovered
// assignment replaces it stays retired when a close rejects the admission.
func TestHoldout_ReplacedCoveredAssignmentWhoseOwnExposureIsRejectedStaysRetired(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	var entry = h.ctx.getAssignment("exp")
	if !hoClaim(h, entry) {
		t.Fatal("the exposure was not claimed")
	}
	h.refresh(hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)))
	h.ctx.GetAssignment("exp")
	h.ctx.Closing_.Store(true)
	h.ctx.admitExposure(entry.assignment, entry)
	h.ctx.Closing_.Store(false)
	if !entry.assignment.Exposed.Load().(bool) {
		t.Fatal("a replaced covered assignment was reopened")
	}
	h.ctx.QueueExposure(entry.assignment)
	h.expectPending(0)
}

// An assignment that is not in the cache (built by hand, or evicted) whose
// exposure a close rejected is queued by a later QueueExposure, as before
// holdouts.
func TestHoldout_RejectedExposureOfAnUncachedAssignmentIsRetried(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)))
	var exposed = &atomic.Value{}
	exposed.Store(false)
	var assignment = &Assignment{Name: "other", Exposed: exposed}
	h.ctx.Closing_.Store(true)
	h.ctx.QueueExposure(assignment)
	h.ctx.Closing_.Store(false)
	if assignment.Exposed.Load().(bool) {
		t.Fatal("a rejected exposure stayed marked exposed")
	}
	h.ctx.QueueExposure(assignment)
	h.expectPending(1)
}

// QueueExposure takes the pinned holdouts only from the cached assignment it
// was given (or a copy of it): an assignment that merely shares the name
// publishes its own exposure, not the cached one's holdouts.
func TestHoldout_QueueExposureOfAnUnrelatedAssignmentWithTheSameName(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.GetAssignment("exp")
	var exposed = &atomic.Value{}
	exposed.Store(false)
	h.ctx.QueueExposure(&Assignment{Id: 1, Name: "exp", UnitType: hoUnitType, Assigned: true, Eligible: true, Exposed: exposed})
	h.expectPublished(hoOwn(hoUnitType, 1, "exp", 0))
}

// Swapping one covering holdout for another of the same iteration is a
// coverage change: the experiment is resolved again and the new holdout
// publishes (JS holdoutSetMatches compares ids).
func TestHoldout_HoldoutIdSwapIsACoverageChange(t *testing.T) {
	var b = hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA(), b}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp"))
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldoutA(), b}, hoExperiment(1, "exp", hoUnitType, 0, 12)))
	expectInt(t, 0, h.treatment("exp"))
	var own = hoOwn(hoUnitType, 1, "exp", 0)
	h.expectPublished(own, hoExposure(hoUnitType, 11, "holdout_a", 1), own, hoExposure(hoUnitType, 12, "holdout_b", 1))
}

// A covered decision's own exposure and its holdout exposures are admitted
// together before any logger runs, so a logger that closes the context on
// the first exposure still publishes the whole decision.
func TestHoldout_LoggerCloseFlushesTheWholeDecision(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	var closed = false
	h.logger.onEvent = func(event EventType, _ interface{}) {
		if event == Exposure && !closed {
			closed = true
			h.ctx.Close()
		}
	}
	expectInt(t, 0, h.treatment("exp"))
	if !h.ctx.IsClosed() {
		t.Fatal("the logger did not close the context")
	}
	h.publisher.mu.Lock()
	defer h.publisher.mu.Unlock()
	if len(h.publisher.events) != 1 {
		t.Fatalf("published %d events, want 1", len(h.publisher.events))
	}
	expectValue(t, []jsonmodels.Exposure{hoOwn(hoUnitType, 1, "exp", 0), hoExposure(hoUnitType, 11, "holdout_a", 1)},
		h.publisher.events[0].Exposures)
}

// A decision rejected by a close is admitted whole on the next read, once:
// no exposure of it is lost and none is duplicated.
func TestHoldout_RejectedDecisionIsRetriedWhole(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.Closing_.Store(true)
	h.ctx.QueueExposure(h.ctx.GetAssignment("exp"))
	h.expectPending(0)
	h.ctx.Closing_.Store(false)
	h.treatment("exp")
	h.treatment("exp")
	h.expectPublished(hoOwn(hoUnitType, 1, "exp", 0), hoExposure(hoUnitType, 11, "holdout_a", 1))
}

// Two suppressed experiments sharing a holdout: when a close rejects the
// first one's decision, its arm is not left marked, so reading only the
// second experiment afterwards still publishes the arm.
func TestHoldout_RejectedSharedArmIsNotTakenAsAccepted(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp_a", hoUnitType, 0, 11), hoExperiment(2, "exp_b", hoUnitType, 0, 11)))
	h.ctx.Closing_.Store(true)
	h.ctx.QueueExposure(h.ctx.GetAssignment("exp_a"))
	h.ctx.Closing_.Store(false)
	expectInt(t, 0, h.treatment("exp_b"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

func TestHoldout_RetirementSurvivesASecondReplacement(t *testing.T) {
	for _, replacements := range []int{2, 12} {
		var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0)))
		var entry = h.ctx.getAssignment("exp")
		var stale = entry.assignment
		if !hoClaim(h, entry) {
			t.Fatal("the exposure was not claimed")
		}
		for i := 0; i < replacements; i++ {
			var experiment = hoExperiment(1, "exp", hoUnitType, 0)
			if i%2 == 0 {
				experiment.HoldoutIds = []int{11}
			}
			experiment.Iteration = i + 2
			h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
			h.ctx.GetAssignment("exp")
		}
		h.ctx.Closing_.Store(true)
		h.ctx.admitExposure(stale, entry)
		h.ctx.Closing_.Store(false)
		h.ctx.QueueExposure(stale)
		h.expectPending(0)
	}
}

// A claim that no replacement retired is rolled back when a close rejects
// it, so the next read retries it.
func TestHoldout_UnretiredRejectedClaimIsReopened(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	var entry = h.ctx.getAssignment("exp")
	if !hoClaim(h, entry) {
		t.Fatal("the exposure was not claimed")
	}
	h.ctx.Closing_.Store(true)
	h.ctx.admitExposure(entry.assignment, entry)
	h.ctx.Closing_.Store(false)
	if entry.assignment.Exposed.Load().(bool) {
		t.Fatal("an unretired rejected claim stayed marked exposed")
	}
	h.treatment("exp")
	h.expectPublished(hoOwn(hoUnitType, 1, "exp", 0), hoExposure(hoUnitType, 11, "holdout_a", 1))
}

// A hand-built assignment that only shares a covered experiment's name is
// retried after a rejected close and publishes its own exposure only.
func TestHoldout_UnrelatedSameNameAssignmentIsRetried(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.GetAssignment("exp")
	var exposed = &atomic.Value{}
	exposed.Store(false)
	var assignment = &Assignment{Id: 1, Name: "exp", UnitType: hoUnitType, Assigned: true, Eligible: true, Exposed: exposed}
	h.ctx.Closing_.Store(true)
	h.ctx.QueueExposure(assignment)
	h.ctx.Closing_.Store(false)
	h.ctx.QueueExposure(assignment)
	h.expectPublished(hoOwn(hoUnitType, 1, "exp", 0))
}

// A decision resolved without its unit and rejected by a close is resolved
// again once the unit is set, so the holdout is drawn for the unit.
func TestHoldout_RejectedMissingUnitDecisionIsResolvedAgain(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_user", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp", "user_id", 2, 11)))
	var entry = h.ctx.getAssignment("exp")
	var assignment = entry.assignment
	if !hoClaim(h, entry) {
		t.Fatal("the exposure was not claimed")
	}
	h.setUnit("user_id", hoUID)
	h.ctx.Closing_.Store(true)
	h.ctx.admitExposure(assignment, entry)
	h.ctx.Closing_.Store(false)
	h.ctx.QueueExposure(assignment)
	h.expectPending(0)
	expectInt(t, 2, h.treatment("exp"))
	var own = hoOwn("user_id", 1, "exp", 2)
	own.FullOn = true
	h.expectPublished(own, hoExposure("user_id", 11, "holdout_user", 1))
}

// Populated holdout models survive encoding/json in both directions.
func TestHoldout_PopulatedModelRoundTrip(t *testing.T) {
	var data = hoData([]jsonmodels.Holdout{hoHoldoutA(), hoThreeArm(21, "three", hoThreeArm1Lo)}, hoExperiment(1, "exp", hoUnitType, 0, 11, 21))
	var raw, err = json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var decoded jsonmodels.ContextData
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	expectValue(t, data.Holdouts, decoded.Holdouts)
	expectValue(t, data.Experiments[0].HoldoutIds, decoded.Experiments[0].HoldoutIds)
	var viaSDK, sdkErr = DefaultContextDataDeserializer{}.Deserialize(raw)
	if sdkErr != nil {
		t.Fatal(sdkErr)
	}
	expectValue(t, data.Holdouts, viaSDK.Holdouts)
}

// A decision rejected by a close while its unit is still missing is
// reopened, not rebuilt.
func TestHoldout_RejectedDecisionStillMissingItsUnitIsReopened(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_user", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp", "user_id", 2, 11)))
	var assignment = h.ctx.GetAssignment("exp")
	h.ctx.Closing_.Store(true)
	h.ctx.QueueExposure(assignment)
	h.ctx.Closing_.Store(false)
	if h.ctx.GetAssignment("exp") != assignment {
		t.Fatal("a rejected decision without its unit was rebuilt")
	}
	if assignment.Exposed.Load().(bool) {
		t.Fatal("a rejected decision stayed marked exposed")
	}
}

// A rejected hand-built assignment sharing a covered experiment's name never
// evicts the cached, already-exposed decision: nothing is published twice.
func TestHoldout_RejectedSameNameAssignmentDoesNotEvictTheExposedEntry(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_user", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp", "user_id", 2, 11)))
	expectInt(t, 2, h.treatment("exp"))
	h.setUnit("user_id", hoUID)
	var exposed = &atomic.Value{}
	exposed.Store(false)
	var handBuilt = &Assignment{Id: 1, Name: "exp", UnitType: "user_id", Exposed: exposed}
	h.ctx.Closing_.Store(true)
	h.ctx.QueueExposure(handBuilt)
	h.ctx.Closing_.Store(false)
	expectInt(t, 2, h.treatment("exp"))
	var own = hoOwn("user_id", 1, "exp", 2)
	own.FullOn = true
	h.expectPublished(own)
}

// A held-out decision taken before a refresh removed every holdout still
// publishes only its holdout exposure through QueueExposure.
func TestHoldout_HeldAssignmentQueuedAfterHoldoutsRemoved(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	var stale = h.ctx.GetAssignment("exp")
	h.ctx.SetData(hoData(nil, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.QueueExposure(stale)
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
}

func TestHoldout_QueueExposureWithoutHoldoutsLeavesNoRegistration(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)))
	h.ctx.QueueExposure(h.ctx.GetAssignment("exp"))
	h.expectPending(1)
}

// QueueExposure on a Context built as a literal returns, as in v1.1.1, for
// an assignment already exposed.
func TestHoldout_QueueExposureOnALiteralContext(t *testing.T) {
	var exposed = &atomic.Value{}
	exposed.Store(true)
	var ctx = &Context{}
	ctx.QueueExposure(&Assignment{Name: "exp", Exposed: exposed})
}

// The uncovered path builds its exposure inline (sharing exposureFor costs
// about 5% on the first exposure without holdouts); both mappings must
// produce the same exposure for every field, with every flag set and with
// every flag clear, so neither dropping nor hardcoding a field passes.
func TestHoldout_CoveredAndUncoveredExposureMappingsAgree(t *testing.T) {
	for _, flag := range []bool{false, true} {
		var assignment = &Assignment{Id: 7, Name: "exp", UnitType: "u", Variant: 2, Assigned: flag, Eligible: flag,
			Overridden: flag, FullOn: flag, Custom: flag, AudienceMismatch: flag, RuleOverride: flag}
		var exposed = &atomic.Value{}
		exposed.Store(true)
		assignment.Exposed = exposed
		var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "other", hoUnitType, 0)))
		if !h.ctx.enqueueExposure(assignment) {
			t.Fatal("not queued")
		}
		var queued = queuedExposures(h.ctx)
		expectValue(t, []jsonmodels.Exposure{h.ctx.exposureFor(assignment, hoMillis)}, queued)
		expectValue(t, jsonmodels.Exposure{Id: 7, Name: "exp", Unit: "u", Variant: 2, ExposedAt: hoMillis, Assigned: flag,
			Eligible: flag, Overridden: flag, FullOn: flag, Custom: flag, AudienceMismatch: flag, RuleOverride: flag}, queued[0])
	}
}

// Holdout ids, iterations and seeds accept integers in any JSON notation up
// to the native int range, and reject anything beyond it.
func TestHoldout_HoldoutIdRange(t *testing.T) {
	var ids = holdoutIds([]byte(`[1e18, -9.223372036854775808e18, 9.223372036854775807e18, -1e19, 1e19]`))
	expectValue(t, []int{1000000000000000000, -9223372036854775808}, ids)
}

// hoLockProbingClock fails the test if the context's EventLock_ is held while
// the clock is read, so a custom clock can never block on its own context.
type hoLockProbingClock struct {
	t   *testing.T
	ctx **Context
}

func (c hoLockProbingClock) Millis() int64 {
	if ctx := *c.ctx; ctx != nil {
		if !ctx.EventLock_.TryLock() {
			c.t.Error("the clock was read with EventLock_ held")
		} else {
			ctx.EventLock_.Unlock()
		}
	}
	return hoMillis
}

// A covered decision reads the clock before taking EventLock_, as the
// uncovered path does.
func TestHoldout_CoveredAdmissionReadsTheClockOutsideEventLock(t *testing.T) {
	var ctx *Context
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{hoUnitType: hoUIDNotHeldOut}
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ft, done = future.New()
	done(hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)), nil)
	ctx = CreateContext(hoLockProbingClock{t: t, ctx: &ctx}, config, ft, &hoProvider{}, &hoPublisher{}, nil,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	ctx.WaitUntilReady()
	t.Cleanup(func() { ctx.ClearRefreshTimer() })
	if _, err := ctx.GetTreatment("exp"); err != nil {
		t.Fatal(err)
	}
	expectInt(t, 2, int(ctx.GetPendingCount()))
}

// A refresh that re-encodes a holdout's integers in another JSON notation
// (1 as 1.0, 13 as 1.3e1) keeps the same epoch and arm: nothing is exposed
// twice. A real iteration bump still re-exposes.
func TestHoldout_EquivalentJSONNumbersKeepTheEpoch(t *testing.T) {
	var payload = func(iteration string, seedHi string) jsonmodels.ContextData {
		var raw = `{"experiments":[{"id":1,"name":"exp","unitType":"session_id","iteration":1,"seedHi":100,"seedLo":200,
			"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"variants":[{"name":"A"},{"name":"B"}],"holdoutIds":[11]}],
			"holdouts":[{"id":11,"name":"holdout_a","unitType":"session_id","iteration":` + iteration + `,"seedHi":` + seedHi +
			`,"seedLo":1.11e2,"split":[0.1,0.9]}]}`
		var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	expectInt(t, 1, payload("1.0", "1.3e1").Holdouts[0].Iteration)
	expectInt(t, 13, payload("1.0", "1.3e1").Holdouts[0].SeedHi)
	expectInt(t, 111, payload("1.0", "1.3e1").Holdouts[0].SeedLo)
	var h = hoCreateFor(t, hoUID, payload("1", "13"))
	expectInt(t, 0, h.treatment("exp"))
	h.refresh(payload("1.0", "13.0"))
	expectInt(t, 0, h.treatment("exp"))
	h.refresh(payload("1e0", "1.3e1"))
	expectInt(t, 0, h.treatment("exp"))
	h.expectPending(1)
	h.refresh(payload("2.0", "13"))
	expectInt(t, 0, h.treatment("exp"))
	h.expectPending(2)
}

// A claim made before the context first had holdouts remains identifiable; if
// a refresh then installs holdouts and replaces the assignment while it is
// in flight, a close rejecting it leaves it retired.
func TestHoldout_ClaimMadeBeforeHoldoutsArrivedStaysRetired(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)))
	var entry = h.ctx.getAssignment("exp")
	var stale = entry.assignment
	if !hoClaim(h, entry) {
		t.Fatal("the exposure was not claimed")
	}
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.GetAssignment("exp")
	h.ctx.Closing_.Store(true)
	h.ctx.admitExposure(stale, entry)
	h.ctx.Closing_.Store(false)
	h.ctx.QueueExposure(stale)
	h.expectPending(0)
}

// A logger that panics while a decision is admitted (contained by the SDK)
// still leaves the complete decision queued.
func TestHoldout_PanickingLoggerLeavesDecisionIntact(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.logger.onEvent = func(event EventType, _ interface{}) {
		if event == Exposure {
			panic("boom")
		}
	}
	h.treatment("exp")
	h.expectPending(2)
}

func TestHoldout_ClaimAndReplacementNeverInterleave(t *testing.T) {
	for round := 0; round < 200; round++ {
		var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0)))
		var entry = h.ctx.getAssignment("exp")
		var stale = entry.assignment
		var claimed = make(chan bool)
		go func() { claimed <- hoClaim(h, entry) }()
		h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
		h.ctx.GetAssignment("exp")
		if !<-claimed {
			continue
		}
		h.ctx.Closing_.Store(true)
		h.ctx.admitExposure(stale, entry)
		h.ctx.Closing_.Store(false)
		h.ctx.QueueExposure(stale)
		h.expectPending(0)
	}
}

// hoClaim claims an entry while retaining its private identity for admission.
func hoClaim(h *hoContext, entry *cachedAssignment) bool {
	return entry.assignment.Exposed.CompareAndSwap(false, true)
}

// Numeric holdout seeds decode as the JS SDK's assigner uses them (JSON.parse,
// then DataView.setUint32). Numeric expected values were computed with node v22.
// Nonnumeric values instead default to zero without JavaScript coercion.
func TestHoldout_SeedsWrapLikeJavaScript(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want uint32
	}{
		{"1", 1}, {"1.0", 1}, {"1e0", 1}, {"-1", 4294967295}, {"-0", 0}, {"2147483648", 2147483648},
		{"4294967295", 4294967295}, {"4294967297", 1}, {"9007199254740993", 0}, {"1e19", 2313682944},
		{"1e21", 3735027712}, {"1.5", 1}, {"-1.5", 4294967295}, {"1e400", 0}, {`"5"`, 0}, {"true", 0}, {"null", 0},
	} {
		if got := uint32(holdoutSeed(json.RawMessage(c.raw))); got != c.want {
			t.Errorf("seed %s: got %d, want %d", c.raw, got, c.want)
		}
	}
	// A wrapped seed draws the same arm as JS does from its 32-bit value.
	var wrapped = hoHoldoutA()
	wrapped.SeedHi = holdoutSeed(json.RawMessage("-4294967283")) // 13 after wrapping
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{wrapped}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp"))
}

// A claim made before the context had holdouts, whose assignment a refresh
// replaces with a covered one and then with an uncovered one, stays retired
// when a close rejects it.
func TestHoldout_UnregisteredClaimStaysRetiredAfterCoverageComesAndGoes(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)))
	var entry = h.ctx.getAssignment("exp")
	var stale = entry.assignment
	if !hoClaim(h, entry) {
		t.Fatal("the exposure was not claimed")
	}
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.ctx.GetAssignment("exp")
	var uncovered = hoExperiment(1, "exp", hoUnitType, 0)
	uncovered.Iteration = 2
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldoutA()}, uncovered))
	h.ctx.GetAssignment("exp")
	h.ctx.Closing_.Store(true)
	h.ctx.admitExposure(stale, entry)
	h.ctx.Closing_.Store(false)
	h.ctx.QueueExposure(stale)
	h.expectPending(0)
	h.treatment("exp")
	h.expectPending(1)
}

// A rejected exposure on a context without holdouts reopens under
// ContextLock_: racing cache writers under -race stay clean.
func TestHoldout_RejectedExposureWithoutHoldoutsReopensUnderTheLock(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp", hoUnitType, 0), hoExperiment(2, "other", hoUnitType, 0)))
	h.ctx.Closing_.Store(true)
	var done = make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = h.ctx.SetOverride("other", i%2)
			h.ctx.GetAssignment("other")
		}
	}()
	for i := 0; i < 200; i++ {
		h.ctx.QueueExposure(h.ctx.GetAssignment("exp"))
	}
	<-done
	h.ctx.Closing_.Store(false)
	h.expectPending(0)
}

// A claim made before any holdout was installed remains retryable when an
// unrelated experiment later adds coverage and the named experiment is replaced
// while still uncovered.
func TestHoldout_UncoveredClaimRetriesAfterUnrelatedHoldout(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)))
	var entry = h.ctx.getAssignment("exp")
	var stale = entry.assignment
	if !hoClaim(h, entry) {
		t.Fatal("claim failed")
	}
	h.refresh(hoData([]jsonmodels.Holdout{hoHoldoutA()},
		hoExperiment(1, "exp", hoUnitType, 0), hoExperiment(2, "other", hoUnitType, 0, 11)))
	if err := h.ctx.SetOverride("exp", 1); err != nil {
		t.Fatal(err)
	}
	h.ctx.GetAssignment("exp")
	h.ctx.Closing_.Store(true)
	h.ctx.admitExposure(stale, entry)
	h.ctx.Closing_.Store(false)
	h.ctx.QueueExposure(stale)
	h.expectPending(1)
}

// Both seed fields must be decoded through the actual payload path, retaining
// the unsigned wrapped value in the public int rather than an int32 value.
func TestHoldout_SeedPayloadDecoding(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
	}{
		{"1.5", 1}, {"1e19", 2313682944}, {"9007199254740993", 0}, {"-1", 4294967295},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			var payload = `{"holdouts":[{"id":11,"seedHi":` + tc.raw + `,"seedLo":` + tc.raw + `}]}`
			var data, err = (DefaultContextDataDeserializer{}).Deserialize([]byte(payload))
			if err != nil || len(data.Holdouts) != 1 {
				t.Fatalf("decode: %+v, %v", data, err)
			}
			if got := data.Holdouts[0]; got.SeedHi != tc.want || got.SeedLo != tc.want {
				t.Fatalf("seed %s: hi=%d lo=%d, want %d", tc.raw, got.SeedHi, got.SeedLo, tc.want)
			}
		})
	}
}

// A rule rebuild that reuses Exposed must retain the original in-flight token
// so a subsequent covered replacement retires both identities.
func TestHoldout_RuleRebuildCarriesRetirementIdentity(t *testing.T) {
	var experiment = hoExperiment(1, "exp_rule", hoUnitType, 2, 11)
	experiment.AssignmentRules = `{"rules":[{"type":"split","percentages":"100/0/0","environments":[],` +
		`"conditions":{"eq":[{"var":"split"},{"value":true}]}},{"type":"assign","variant":2,"environments":[]}]}`
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	var before = h.ctx.getAssignment("exp_rule")
	if !hoClaim(h, before) {
		t.Fatal("claim failed")
	}
	h.setAttribute("split", true)
	var rebuilt = h.ctx.getAssignment("exp_rule")
	if rebuilt == before || rebuilt.assignment.Exposed != before.assignment.Exposed {
		t.Fatal("rule rebuild did not preserve identity")
	}
	var next = hoHoldoutA()
	next.Iteration++
	h.refresh(hoData([]jsonmodels.Holdout{next}, experiment))
	h.ctx.GetAssignment("exp_rule")
	h.ctx.Closing_.Store(true)
	h.ctx.admitExposure(before.assignment, before)
	h.ctx.Closing_.Store(false)
	h.ctx.QueueExposure(before.assignment)
	h.expectPending(0)
}

// An uncovered exposure also reads the clock before taking EventLock_.
func TestHoldout_UncoveredExposureReadsTheClockOutsideEventLock(t *testing.T) {
	var ctx *Context
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{hoUnitType: hoUID}
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ft, done = future.New()
	done(hoData(nil, hoExperiment(1, "exp", hoUnitType, 0)), nil)
	ctx = CreateContext(hoLockProbingClock{t: t, ctx: &ctx}, config, ft, &hoProvider{}, &hoPublisher{}, nil,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	ctx.WaitUntilReady()
	t.Cleanup(func() { ctx.ClearRefreshTimer() })
	if _, err := ctx.GetTreatment("exp"); err != nil {
		t.Fatal(err)
	}
	expectInt(t, 1, int(ctx.GetPendingCount()))
}

// Holdout ids decode through float64 as JavaScript's JSON.parse does, so any
// two spellings of one number give the same id, also above 2^53. Expected
// values computed with node v22.
func TestHoldout_HoldoutIdSpellingsDecodeAlike(t *testing.T) {
	expectValue(t, []int{9007199254740992, 9007199254740992, 9007199254740992},
		holdoutIds([]byte(`[9007199254740993, 9.007199254740993e15, 9007199254740992]`)))
	expectValue(t, []int{1000000000000000000, 1000000000000000000},
		holdoutIds([]byte(`[1000000000000000001, 1.000000000000000001e18]`)))
}

// Duplicate holdout ids: identical definitions count as one, and conflicting
// definitions cover nothing, so neither depends on the entries' order.
func TestHoldout_DuplicateHoldoutDefinitionsAreOrderIndependent(t *testing.T) {
	var a = hoHoldoutA()
	var b = hoHoldout(11, "holdout_a", hoUnitType, hoSeedBHi, hoSeedBLo)
	for _, holdouts := range [][]jsonmodels.Holdout{{a, b}, {b, a}, {a, b, a}, {a, b, b}} {
		var h = hoCreateFor(t, hoUID, hoData(holdouts, hoExperiment(1, "exp", hoUnitType, 0, 11)))
		expectInt(t, hoNormalVariant, h.treatment("exp"))
		h.expectPublished(hoOwn(hoUnitType, 1, "exp", hoNormalVariant))
	}
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{a, a}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, 0, h.treatment("exp"))
	h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
	var r = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{a, b}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, hoNormalVariant, r.treatment("exp"))
	r.refresh(hoData([]jsonmodels.Holdout{b, a}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	expectInt(t, hoNormalVariant, r.treatment("exp"))
	r.expectPublished(hoOwn(hoUnitType, 1, "exp", hoNormalVariant))
}

// Duplicates conflict when any field that changes the arm differs (split,
// seeds, iteration), and cover nothing in either order; duplicates that
// differ only in informational fields (name, holdoutType, unitType) count as
// one; a duplicate with no split is ignored, so the valid entry applies.
func TestHoldout_DuplicateHoldoutConflictFields(t *testing.T) {
	var a = hoHoldoutA()
	var conflicting = []func(*jsonmodels.Holdout){
		func(h *jsonmodels.Holdout) { h.Split = []float64{0.9, 0.1} },
		func(h *jsonmodels.Holdout) { h.SeedHi = 14 },
		func(h *jsonmodels.Holdout) { h.SeedLo = 112 },
		func(h *jsonmodels.Holdout) { h.Iteration = 2 },
	}
	for _, change := range conflicting {
		var b = hoHoldoutA()
		change(&b)
		for _, holdouts := range [][]jsonmodels.Holdout{{a, b}, {b, a}} {
			var h = hoCreateFor(t, hoUID, hoData(holdouts, hoExperiment(1, "exp", hoUnitType, 0, 11)))
			expectInt(t, hoNormalVariant, h.treatment("exp"))
			h.expectPublished(hoOwn(hoUnitType, 1, "exp", hoNormalVariant))
		}
	}
	var informational = []func(*jsonmodels.Holdout){
		func(h *jsonmodels.Holdout) { h.Name = "renamed" },
		func(h *jsonmodels.Holdout) { h.HoldoutType = "all_full_on" },
		func(h *jsonmodels.Holdout) { h.UnitType = "user_id" },
		func(h *jsonmodels.Holdout) { h.Split = nil },
	}
	for _, change := range informational {
		var b = hoHoldoutA()
		change(&b)
		for _, holdouts := range [][]jsonmodels.Holdout{{a, b}, {b, a}} {
			var h = hoCreateFor(t, hoUID, hoData(holdouts, hoExperiment(1, "exp", hoUnitType, 0, 11)))
			expectInt(t, 0, h.treatment("exp"))
			var exposures = queuedExposures(h.ctx)
			if len(exposures) != 1 || exposures[0].Id != 11 || exposures[0].Variant != 0 {
				t.Fatalf("exposures %+v", exposures)
			}
		}
	}
}

// A duplicate entry with a malformed split (a non-number, non-null element) is
// skipped, so it never cancels a valid definition of the same id.
func TestHoldout_MalformedDuplicateDoesNotCancelAValidHoldout(t *testing.T) {
	for _, split := range []string{`[0.1,"x"]`, `[0.1,true]`, `[[0.1],0.9]`, `"0.1/0.9"`} {
		for _, order := range []bool{false, true} {
			var valid = `{"id":11,"name":"holdout_a","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":[0.1,0.9]}`
			var malformed = `{"id":11,"name":"holdout_a","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":` + split + `}`
			var holdouts = valid + "," + malformed
			if order {
				holdouts = malformed + "," + valid
			}
			var payload = `{"experiments":[{"id":1,"name":"exp","unitType":"session_id","iteration":1,"seedHi":100,"seedLo":200,
				"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"variants":[{"name":"A"},{"name":"B"}],"holdoutIds":[11]}],
				"holdouts":[` + holdouts + `]}`
			var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			expectInt(t, 1, len(data.Holdouts))
			var h = hoCreateFor(t, hoUID, data)
			expectInt(t, 0, h.treatment("exp"))
			h.expectPublished(hoExposure(hoUnitType, 11, "holdout_a", 0))
		}
	}
}

// hoReplacingClock replaces the assignment the first time the clock is read
// with armed set, which happens after an exposure is claimed and before its
// admission takes EventLock_.
type hoReplacingClock struct {
	armed   *bool
	replace func()
}

func (c hoReplacingClock) Millis() int64 {
	if *c.armed {
		*c.armed = false
		c.replace()
	}
	return hoMillis
}

// A claimed exposure whose assignment a refresh replaces before the admission
// commits still publishes the claimed decision, as in the Java SDK (only an
// unclaimed assignment is retired): a read may already serve it because the
// claim marked it exposed. The replacement then publishes its own decision.
func TestHoldout_ClaimReplacedBeforeCommitStillPublishes(t *testing.T) {
	for _, covered := range []bool{true, false} {
		var ctx *Context
		var armed = false
		var stale *Assignment
		var experiment = hoExperiment(1, "exp", hoUnitType, 0, 11)
		experiment.Split = []float64{1, 0}
		if !covered {
			experiment.HoldoutIds = nil
		}
		var replacement = hoExperiment(1, "exp", hoUnitType, 0, 11)
		replacement.Iteration = 2
		replacement.Split = []float64{0, 1}
		var clock = hoReplacingClock{armed: &armed, replace: func() {
			ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, replacement))
			ctx.GetAssignment("exp")
		}}
		var config = CreateDefaultContextConfig()
		config.Units_ = map[string]string{hoUnitType: hoUIDNotHeldOut}
		config.PublishDelay_ = -1
		config.RefreshInterval_ = -1
		var ft, done = future.New()
		done(hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment), nil)
		ctx = CreateContext(clock, config, ft, &hoProvider{}, &hoPublisher{}, nil,
			DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
		ctx.WaitUntilReady()
		stale = ctx.GetAssignment("exp")
		armed = true
		ctx.QueueExposure(stale)
		if armed {
			t.Fatal("the clock was not read during the admission")
		}
		var exposures = queuedExposures(ctx)
		var claimed = 1
		if covered {
			claimed = 2
		}
		if len(exposures) != claimed || exposures[0].Name != "exp" || exposures[0].Variant != 0 {
			t.Fatalf("covered=%v: claimed decision %+v", covered, exposures)
		}
		if variant, err := ctx.GetTreatment("exp"); err != nil || variant != 1 {
			t.Fatalf("replacement: %d, %v", variant, err)
		}
		exposures = queuedExposures(ctx)
		if len(exposures) < claimed+1 || exposures[claimed].Name != "exp" || exposures[claimed].Variant != 1 {
			t.Fatalf("covered=%v: replacement decision %+v", covered, exposures)
		}
		ctx.ClearRefreshTimer()
	}
}

// A holdout whose split is null, or has null elements, is kept by the decoder,
// as in the JS SDK (which keeps any non-empty split and adds a null as 0). A
// null split covers nothing; a null element counts as 0, so [1,null] holds the
// unit out and [null,1] does not.
func TestHoldout_NullSplitAndNullElementsAreKept(t *testing.T) {
	var decode = func(split string) []jsonmodels.Holdout {
		var payload = `{"holdouts":[{"id":11,"name":"holdout_a","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":` + split + `}]}`
		var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		return data.Holdouts
	}
	expectInt(t, 1, len(decode(`null`)))
	if split := decode(`null`)[0].Split; len(split) != 0 {
		t.Fatalf("split %v", split)
	}
	expectValue(t, []float64{1, 0}, decode(`[1,null]`)[0].Split)
	expectValue(t, []float64{0, 1}, decode(`[null,1]`)[0].Split)
	expectInt(t, 0, len(decode(`[1,"x"]`)))

	for _, c := range []struct {
		split   string
		variant int
	}{{`[1,null]`, 0}, {`[null,1]`, hoNormalVariant}} {
		var payload = `{"experiments":[{"id":1,"name":"exp","unitType":"session_id","iteration":1,"seedHi":100,"seedLo":200,
			"split":[0.5,0.5],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],"variants":[{"name":"A"},{"name":"B"}],"holdoutIds":[11]}],
			"holdouts":[{"id":11,"name":"holdout_a","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":` + c.split + `}]}`
		var data, err = DefaultContextDataDeserializer{}.Deserialize([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		expectInt(t, c.variant, hoCreateFor(t, hoUID, data).treatment("exp"))
	}
}

// Two readers and one replacement: the first read claims the exposure, the
// second finds it exposed and serves the same variant, then a refresh replaces
// the assignment before the first read commits. The claim still commits, so
// the variant both reads served is published.
func TestHoldout_ClaimedDecisionServedByASecondReadIsPublished(t *testing.T) {
	for _, variable := range []bool{false, true} {
		var experiment = hoExperiment(1, "exp", hoUnitType, 0, 11)
		experiment.Split = []float64{1, 0}
		experiment.Variants[0].Config = `{"v":"old"}`
		experiment.Variants[1].Config = `{"v":"new"}`
		var replacement = experiment
		replacement.Iteration = 2
		replacement.Split = []float64{0, 1}
		var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
		var entry = h.ctx.getAssignment("exp")
		if !hoClaim(h, entry) {
			t.Fatal("the first read did not claim")
		}
		if variable {
			expectValue(t, "old", h.variable("v"))
		} else {
			expectInt(t, 0, h.treatment("exp"))
		}
		h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, replacement))
		h.ctx.PeekTreatment("exp")
		if got := h.ctx.admitExposure(entry.assignment, entry); got != exposureQueued {
			t.Fatalf("the first read's admission reported %v, want queued", got)
		}
		var exposures = queuedExposures(h.ctx)
		if len(exposures) != 2 || exposures[0].Name != "exp" || exposures[0].Variant != 0 || exposures[1].Id != 11 {
			t.Fatalf("exposures %+v", exposures)
		}
	}
}

// A read whose claim a replacement follows (a refresh lands between its claim
// and its admission) serves and exposes the decision it claimed, as the Java
// SDK does; the replacement is exposed by the next read.
func TestHoldout_ReadReplacedAfterItsClaimServesItsClaim(t *testing.T) {
	for _, covered := range []bool{true, false} {
		for _, variable := range []bool{false, true} {
			var ctx *Context
			var armed = false
			var experiment = hoExperiment(1, "exp", hoUnitType, 0)
			experiment.Split = []float64{1, 0}
			experiment.Variants[0].Config = `{"v":"old"}`
			experiment.Variants[1].Config = `{"v":"new"}`
			if covered {
				experiment.HoldoutIds = []int{11}
			}
			var replacement = experiment
			replacement.Iteration = 2
			replacement.Split = []float64{0, 1}
			replacement.HoldoutIds = []int{11}
			var clock = hoReplacingClock{armed: &armed, replace: func() {
				ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, replacement))
				ctx.GetAssignment("exp")
			}}
			var config = CreateDefaultContextConfig()
			config.Units_ = map[string]string{hoUnitType: hoUIDNotHeldOut}
			config.PublishDelay_ = -1
			config.RefreshInterval_ = -1
			var ft, done = future.New()
			done(hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment), nil)
			ctx = CreateContext(clock, config, ft, &hoProvider{}, &hoPublisher{}, nil,
				DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
			ctx.WaitUntilReady()
			ctx.GetAssignment("exp")
			armed = true
			if variable {
				var value, err = ctx.GetVariableValue("v", "default")
				if err != nil {
					t.Fatal(err)
				}
				expectValue(t, "old", value)
			} else {
				var variant, err = ctx.GetTreatment("exp")
				if err != nil {
					t.Fatal(err)
				}
				expectInt(t, 0, variant)
			}
			if armed {
				t.Fatal("the clock was not read during the admission")
			}
			var exposures = queuedExposures(ctx)
			var claimed = 1
			if covered {
				claimed = 2
			}
			if len(exposures) != claimed || exposures[0].Name != "exp" || exposures[0].Variant != 0 ||
				(covered && exposures[1].Id != 11) {
				t.Fatalf("covered=%v variable=%v: exposures %+v", covered, variable, exposures)
			}
			ctx.ClearRefreshTimer()
		}
	}
}

// When every attempt loses to a replacement landing before its claim, a read
// stops after maxExposureAttempts and serves the last assignment it tried.
func TestHoldout_RetiredReadRetriesAreBounded(t *testing.T) {
	var experiment = hoExperiment(1, "exp", hoUnitType, 0, 11)
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	h.ctx.GetAssignment("exp")
	var claims = 0
	beforeSettleClaim = func() {
		claims++
		var next = experiment
		next.Iteration = 1 + claims
		h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, next))
		h.ctx.GetAssignment("exp")
	}
	defer func() { beforeSettleClaim = nil }()
	h.treatment("exp")
	expectInt(t, maxExposureAttempts, claims)
	h.expectPending(0)
}

type hoClockFunc func() int64

func (f hoClockFunc) Millis() int64 { return f() }

// A replacement that lands after a read resolved its assignment and before
// it claimed the exposure retires the entry the read holds: the read
// serves and exposes the replacement instead.
func TestHoldout_ReplacementBeforeTheClaimServesTheReplacement(t *testing.T) {
	for _, covered := range []bool{true, false} {
		for _, variable := range []bool{false, true} {
			var experiment = hoExperiment(1, "exp", hoUnitType, 0)
			experiment.Split = []float64{1, 0}
			experiment.Variants[0].Config = `{"v":"old"}`
			experiment.Variants[1].Config = `{"v":"new"}`
			if covered {
				experiment.HoldoutIds = []int{11}
			}
			var replacement = experiment
			replacement.Iteration = 2
			replacement.Split = []float64{0, 1}
			replacement.HoldoutIds = []int{11}
			var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
			var armed = true
			beforeSettleClaim = func() {
				if armed {
					armed = false
					h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, replacement))
					h.ctx.GetAssignment("exp")
				}
			}
			if variable {
				expectValue(t, "new", h.variable("v"))
			} else {
				expectInt(t, 1, h.treatment("exp"))
			}
			beforeSettleClaim = nil
			if armed {
				t.Fatal("the hook did not run")
			}
			var exposures = queuedExposures(h.ctx)
			if len(exposures) != 2 || exposures[0].Name != "exp" || exposures[0].Variant != 1 || exposures[1].Id != 11 {
				t.Fatalf("exposures %+v", exposures)
			}
		}
	}
}

// A close that rejects a read's exposure is not retried: the read claims
// its assignment once, and the rolled-back exposure stays retryable.
func TestHoldout_RejectedReadIsNotRetried(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	var claims = 0
	beforeSettleClaim = func() {
		claims++
		h.ctx.Closing_.Store(true)
	}
	h.treatment("exp")
	beforeSettleClaim = nil
	h.ctx.Closing_.Store(false)
	expectInt(t, 1, claims)
	h.expectPending(0)
	h.treatment("exp")
	h.expectPending(2)
}

// A SetUnit that evicts an assignment resolved without its unit, landing
// after a read resolved it and before the claim, makes the read serve and
// expose the assignment with the unit.
func TestHoldout_EvictionBeforeTheClaimServesTheResolvedAssignment(t *testing.T) {
	for _, variable := range []bool{false, true} {
		var experiment = hoExperiment(1, "exp", "user_id", 0, 11)
		experiment.Split = []float64{0, 1}
		experiment.Variants[0].Config = `{"v":"control"}`
		experiment.Variants[1].Config = `{"v":"treatment"}`
		var h = hoCreate(t, map[string]string{}, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_u", "user_id", hoSeedBHi, hoSeedBLo)}, experiment))
		var armed = true
		beforeSettleClaim = func() {
			if armed {
				armed = false
				h.setUnit("user_id", hoUID)
			}
		}
		if variable {
			expectValue(t, "treatment", h.variable("v"))
		} else {
			expectInt(t, 1, h.treatment("exp"))
		}
		beforeSettleClaim = nil
		var exposures = queuedExposures(h.ctx)
		if len(exposures) != 2 || exposures[0].Name != "exp" || exposures[0].Variant != 1 || exposures[1].Id != 11 {
			t.Fatalf("exposures %+v", exposures)
		}
	}
}

// A warmed, exposed read does no claim and no further resolution, so it
// costs what a cached read did before holdouts.
func TestHoldout_WarmedReadDoesNoClaim(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil, hoWithVariables(hoExperiment(1, "exp", hoUnitType, 0), "color")))
	h.treatment("exp")
	h.variable("color")
	var claims = 0
	beforeSettleClaim = func() { claims++ }
	defer func() { beforeSettleClaim = nil }()
	for i := 0; i < 5; i++ {
		h.treatment("exp")
		h.variable("color")
	}
	expectInt(t, 0, claims)
	if n := hoLeastAllocs(func() { _, _ = h.ctx.GetTreatment("exp") }); n > 2 {
		t.Errorf("warmed GetTreatment: %.0f allocations", n)
	}
}

// Another read's exposure does not make this read retry.
func TestHoldout_FailedClaimReportsRetiredOnlyWhenRetired(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	h.treatment("exp")
	var entry = h.ctx.getAssignment("exp")
	if got := h.ctx.queueExposure(entry.assignment, entry); got != exposureQueued {
		t.Fatalf("exposed entry: got %v, want queued", got)
	}
	entry.retirement.Store(true)
	if got := h.ctx.queueExposure(entry.assignment, entry); got != exposureRetired {
		t.Fatalf("retired entry: got %v, want retired", got)
	}
}

// An eviction marks the entry retired before it marks it exposed, so a
// read whose claim loses to the eviction always finds the retirement and
// resolves again: it never serves the evicted variant without an exposure.
// The read's claim is replayed at the exact point the eviction has flipped
// the exposed flag.
func TestHoldout_ClaimLosingToAnEvictionSeesTheRetirement(t *testing.T) {
	var experiment = hoExperiment(1, "exp", "user_id", 0, 11)
	experiment.Split = []float64{0, 1}
	var h = hoCreate(t, map[string]string{}, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_u", "user_id", hoSeedBHi, hoSeedBLo)}, experiment))
	var entry = h.ctx.getAssignment("exp")
	var status exposureStatus
	afterEvictionClaim = func(evicted *cachedAssignment) {
		if evicted == entry {
			status = h.ctx.queueExposure(entry.assignment, entry)
		}
	}
	defer func() { afterEvictionClaim = nil }()
	h.setUnit("user_id", hoUID)
	if status != exposureRetired {
		t.Fatalf("a claim losing to the eviction reported %v, want retired", status)
	}
}

// A read that claims an entry after an eviction stored the retirement but
// before the eviction's own claim keeps its claim: the eviction restores the
// retirement and keeps the entry, and the read's admission queues the
// exposure for the variant it serves.
func TestHoldout_ReadClaimingBeforeTheEvictionKeepsItsClaim(t *testing.T) {
	var experiment = hoExperiment(1, "exp", "user_id", 0, 11)
	experiment.Split = []float64{0, 1}
	var h = hoCreate(t, map[string]string{}, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_u", "user_id", hoSeedBHi, hoSeedBLo)}, experiment))
	var entry = h.ctx.getAssignment("exp")
	var claimed bool
	beforeEvictionClaim = func(evicting *cachedAssignment) {
		if evicting == entry {
			claimed = entry.assignment.Exposed.CompareAndSwap(false, true)
		}
	}
	h.setUnit("user_id", hoUID)
	beforeEvictionClaim = nil
	if !claimed {
		t.Fatal("the read did not claim")
	}
	if entry.retirement.Load() {
		t.Fatal("the eviction left a claimed entry retired")
	}
	if got := h.ctx.admitExposure(entry.assignment, entry); got != exposureQueued {
		t.Fatalf("admission reported %v, want queued", got)
	}
	h.expectPending(1)
	h.ctx.ContextLock_.RLock()
	var cached = h.ctx.assignmentCache_["exp"]
	h.ctx.ContextLock_.RUnlock()
	if cached != entry {
		t.Fatal("the claimed entry was evicted")
	}
}

// A replacement retires the replaced entry before it marks it exposed, so a
// claim that loses to the replacement always sees the retirement.
func TestHoldout_ClaimLosingToAReplacementSeesTheRetirement(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	var entry = h.ctx.getAssignment("exp")
	var status = exposureQueued
	var ran = false
	afterReplacementClaim = func(replaced *cachedAssignment) {
		if replaced == entry {
			ran = true
			status = h.ctx.queueExposure(entry.assignment, entry)
		}
	}
	var replacement = hoExperiment(1, "exp", hoUnitType, 0, 11)
	replacement.Iteration = 2
	h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, replacement))
	h.ctx.GetAssignment("exp")
	afterReplacementClaim = nil
	if !ran {
		t.Fatal("the hook did not run")
	}
	if status != exposureRetired {
		t.Fatalf("a claim losing to the replacement reported %v, want retired", status)
	}
}

// A replacement landing after a read resolved its assignment and before the
// read checks whether it is exposed marks the old entry exposed without
// recording it: the read still settles, serving and exposing the
// replacement.
func TestHoldout_ReplacementBeforeTheExposedCheckServesTheReplacement(t *testing.T) {
	for _, variable := range []bool{false, true} {
		var experiment = hoExperiment(1, "exp", hoUnitType, 0, 11)
		experiment.Split = []float64{1, 0}
		experiment.Variants[0].Config = `{"v":"old"}`
		experiment.Variants[1].Config = `{"v":"new"}`
		var replacement = experiment
		replacement.Iteration = 2
		replacement.Split = []float64{0, 1}
		var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
		h.ctx.GetAssignment("exp")
		var armed = true
		afterReadResolve = func() {
			if armed {
				armed = false
				h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, replacement))
				h.ctx.PeekTreatment("exp")
			}
		}
		if variable {
			expectValue(t, "new", h.variable("v"))
		} else {
			expectInt(t, 1, h.treatment("exp"))
		}
		afterReadResolve = nil
		var exposures = queuedExposures(h.ctx)
		if len(exposures) != 2 || exposures[0].Name != "exp" || exposures[0].Variant != 1 || exposures[1].Id != 11 {
			t.Fatalf("exposures %+v", exposures)
		}
	}
}

// A variable read whose claim is retired because the experiment was removed
// returns the default and exposes nothing for the removed experiment.
func TestHoldout_VariableRetryDoesNotExposeARemovedExperiment(t *testing.T) {
	var experiment = hoExperiment(1, "a", hoUnitType, 0, 11)
	experiment.Variants[0].Config = `{"v":"x"}`
	experiment.Variants[1].Config = `{"v":"x"}`
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	h.ctx.GetAssignment("a")
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}))
			h.ctx.GetAssignment("a")
		}
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "default", h.variable("v"))
	for _, exposure := range queuedExposures(h.ctx) {
		if exposure.Name == "a" {
			t.Fatalf("the removed experiment was exposed: %+v", queuedExposures(h.ctx))
		}
	}
}

func TestHoldout_NeedsSettling(t *testing.T) {
	var exposed = &atomic.Value{}
	exposed.Store(true)
	var entry = &cachedAssignment{assignment: &Assignment{Exposed: exposed}, retirement: &atomic.Bool{}}
	if entry.needsSettling() {
		t.Fatal("a warmed entry settles")
	}
	entry.retirement.Store(true)
	if !entry.needsSettling() {
		t.Fatal("a retired entry does not settle")
	}
	exposed.Store(false)
	entry.retirement.Store(false)
	if !entry.needsSettling() {
		t.Fatal("an unexposed entry does not settle")
	}
}

// A replacement and an eviction store the retirement before they mark the
// entry exposed, so a read whose claim fails to either always finds it.
func TestHoldout_RetirementIsStoredBeforeTheClaim(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11)))
	var entry = h.ctx.getAssignment("exp")
	var ran = false
	beforeReplacementClaim = func(replaced *cachedAssignment) {
		if replaced == entry {
			ran = true
			if !replaced.retirement.Load() || replaced.assignment.Exposed.Load().(bool) {
				t.Error("the replacement claims before it retires")
			}
		}
	}
	var replacement = hoExperiment(1, "exp", hoUnitType, 0, 11)
	replacement.Iteration = 2
	h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, replacement))
	h.ctx.GetAssignment("exp")
	beforeReplacementClaim = nil
	if !ran {
		t.Fatal("the replacement hook did not run")
	}

	var experiment = hoExperiment(1, "exp", "user_id", 0, 11)
	var u = hoCreate(t, map[string]string{}, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_u", "user_id", hoSeedBHi, hoSeedBLo)}, experiment))
	var evicted = u.ctx.getAssignment("exp")
	ran = false
	beforeEvictionClaim = func(evicting *cachedAssignment) {
		if evicting == evicted {
			ran = true
			if !evicting.retirement.Load() || evicting.assignment.Exposed.Load().(bool) {
				t.Error("the eviction claims before it retires")
			}
		}
	}
	u.setUnit("user_id", hoUID)
	beforeEvictionClaim = nil
	if !ran {
		t.Fatal("the eviction hook did not run")
	}
}

// A variable read whose assignment is replaced, before its claim, by a
// version of the experiment that no longer defines the key does not expose
// the replacement; it falls through to the default.
func TestHoldout_VariableRetryDoesNotExposeAnExperimentWithoutTheKey(t *testing.T) {
	var experiment = hoExperiment(1, "a", hoUnitType, 0, 11)
	experiment.Variants[0].Config = `{"v":"x"}`
	experiment.Variants[1].Config = `{"v":"x"}`
	var replacement = hoExperiment(1, "a", hoUnitType, 0, 11)
	replacement.Iteration = 2
	replacement.Variants[0].Config = `{"w":"y"}`
	replacement.Variants[1].Config = `{"w":"y"}`
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	h.ctx.GetAssignment("a")
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, replacement))
			h.ctx.GetAssignment("a")
		}
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "default", h.variable("v"))
	if armed {
		t.Fatal("the hook did not run")
	}
	if exposures := queuedExposures(h.ctx); len(exposures) != 0 {
		t.Fatalf("the replacement without the key was exposed: %+v", exposures)
	}
}

// The variable read's claim check judges an experiment by its definition as
// installed now: a refresh that drops the key makes it ineligible, one that
// adds the key makes it eligible, and an experiment that is not installed is
// never eligible.
func TestHoldout_VariableEligibilityIsJudgedByTheInstalledDefinition(t *testing.T) {
	var version = func(iteration int, config string) jsonmodels.Experiment {
		var experiment = hoExperiment(1, "a", hoUnitType, 0, 11)
		experiment.Iteration = iteration
		experiment.Variants[0].Config = config
		experiment.Variants[1].Config = config
		return experiment
	}
	for _, c := range []struct {
		resolved, later string
		eligible        bool
	}{
		{`{"v":"x"}`, `{"w":"y"}`, false},
		{`{"w":"y"}`, `{"v":"x"}`, true},
	} {
		var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, version(2, c.later)))
		if got := hoInstalledDefines(h.ctx, "a", "v"); got != c.eligible {
			t.Errorf("resolved %s, later %s: eligible %v, want %v", c.resolved, c.later, got, c.eligible)
		}
	}
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}))
	if hoInstalledDefines(h.ctx, "a", "v") {
		t.Error("an experiment that is not installed is eligible")
	}
}

// hoKeyed reports whether the installed index builds key sets, and
// hoInstalledDefines whether experiment name as installed now defines key.
func hoKeyed(ctx *Context) bool {
	var index = ctx.index_.Load()
	return index != nil && index.keyed
}

func hoInstalledDefines(ctx *Context, name string, key string) bool {
	var entry = ctx.getAssignment(name)
	ctx.ContextLock_.Lock()
	defer ctx.ContextLock_.Unlock()
	if entry.assignment.Variables == nil {
		// The eligibility check rejects an entry without variables before
		// looking at the key; give it an empty map to probe the key alone.
		var probe = *entry
		var assignment = *entry.assignment
		assignment.Variables = map[string]interface{}{}
		probe.assignment = &assignment
		ctx.assignmentCache_[name] = &probe
		defer func() { ctx.assignmentCache_[name] = entry }()
		return ctx.variableEligible(name, key)(&probe) == exposureClaimed
	}
	return ctx.variableEligible(name, key)(entry) == exposureClaimed
}

// A variable read whose assignment is replaced, before its claim, by a
// version that no longer defines the key does not expose the replacement,
// even though another experiment still defines the key; that experiment
// serves the value instead.
func TestHoldout_VariableRetryChecksTheKeyOnTheSameExperiment(t *testing.T) {
	var a = hoExperiment(1, "a", hoUnitType, 0, 11)
	a.Variants[0].Config = `{"v":"a"}`
	a.Variants[1].Config = `{"v":"a"}`
	var b = hoExperiment(2, "b", hoUnitType, 0)
	b.Variants[0].Config = `{"v":"b"}`
	b.Variants[1].Config = `{"v":"b"}`
	var replacement = a
	replacement.Iteration = 2
	replacement.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"w":"y"}`}, {Name: "B", Config: `{"w":"y"}`}}
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, a, b))
	h.ctx.GetAssignment("a")
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, replacement, b))
			h.ctx.GetAssignment("a")
		}
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "b", h.variable("v"))
	if armed {
		t.Fatal("the hook did not run")
	}
	for _, exposure := range queuedExposures(h.ctx) {
		if exposure.Name == "a" {
			t.Fatalf("the replacement without the key was exposed: %+v", queuedExposures(h.ctx))
		}
	}
}

// The installed key set is per experiment: a key one experiment defines in
// any variant does not make another experiment that lacks it eligible.
func TestHoldout_VariableKeySetIsPerExperiment(t *testing.T) {
	var a = hoExperiment(1, "a", hoUnitType, 0, 11)
	a.Variants[0].Config = `{"v":"selected"}`
	a.Variants[1].Config = `{"other":"unselected"}`
	var b = hoExperiment(2, "b", hoUnitType, 0)
	b.Variants[0].Config = `{"w":"b"}`
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, a, b))
	for _, c := range []struct {
		name, key string
		want      bool
	}{
		{"a", "v", true}, {"a", "other", true}, {"a", "w", false},
		{"b", "w", true}, {"b", "v", false}, {"a", "missing", false},
	} {
		if got := hoInstalledDefines(h.ctx, c.name, c.key); got != c.want {
			t.Errorf("%s defines %s: %v, want %v", c.name, c.key, got, c.want)
		}
	}
}

// aliasingParser returns key strings that point into the variant's config,
// as a custom parser may, so a snapshot that does not copy its keys would
// keep the whole config alive.
type aliasingParser struct{}

func (aliasingParser) Parse(_ Context, _ string, _ string, config string) map[string]interface{} {
	if strings.HasPrefix(config, "big") {
		return map[string]interface{}{config[:3]: "x"}
	}
	return DefaultVariableParser{}.Parse(Context{}, "", "", config)
}

// A cached entry releases an unselected variant's config after the experiment
// is removed, even when a custom parser's keys alias that config.
func TestHoldout_CachedEntryReleasesTheOldDefinition(t *testing.T) {
	const size = 8 << 20
	for _, c := range []struct {
		name   string
		parser VariableParser
		config func() string
	}{
		{"default parser", DefaultVariableParser{}, func() string { return `{"other":"` + strings.Repeat("x", size) + `"}` }},
		{"aliasing parser", aliasingParser{}, func() string { return "big" + strings.Repeat("x", size) }},
	} {
		var heap = func() uint64 {
			var stats runtime.MemStats
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&stats)
			return stats.HeapAlloc
		}
		var before = heap()
		var experiment = hoExperiment(1, "a", hoUnitType, 0, 11)
		experiment.Split = []float64{1, 0}
		experiment.Variants[0].Config = `{"v":"selected"}`
		experiment.Variants[1].Config = c.config()
		var config = CreateDefaultContextConfig()
		config.Units_ = map[string]string{hoUnitType: hoUIDNotHeldOut}
		config.PublishDelay_ = -1
		config.RefreshInterval_ = -1
		var ft, done = future.New()
		done(hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment), nil)
		experiment = jsonmodels.Experiment{}
		var ctx = CreateContext(internal.FixedClock{Millis_: hoMillis}, config, ft, &hoProvider{}, &hoPublisher{}, nil,
			c.parser, AudienceMatcher{DefaultAudienceDeserializer{}})
		ctx.WaitUntilReady()
		var entry = ctx.getAssignment("a")
		ctx.SetData(hoData(nil))
		var retained = int64(heap()) - int64(before)
		runtime.KeepAlive(entry)
		runtime.KeepAlive(ctx)
		if retained > 2<<20 {
			t.Errorf("%s: %d bytes retained after the refresh", c.name, retained)
		}
		ctx.ClearRefreshTimer()
	}
}

// Once a context has seen holdouts, every later install keeps its variable
// key set, so a retry two refreshes after the holdouts left still checks
// the key; a context that never had holdouts builds none.
func TestHoldout_VariableKeySetOnceHoldoutsWereSeen(t *testing.T) {
	var experiment = hoWithVariables(hoExperiment(1, "a", hoUnitType, 0), "v")
	var plain = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, experiment))
	if hoKeyed(plain.ctx) {
		t.Fatal("a context without holdouts built a key set")
	}
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	for i := 2; i <= 3; i++ {
		var next = experiment
		next.Iteration = i
		h.ctx.SetData(hoData(nil, next))
	}
	if !hoKeyed(h.ctx) || !hoInstalledDefines(h.ctx, "a", "v") || hoInstalledDefines(h.ctx, "a", "w") {
		t.Fatal("key set lost after holdouts left")
	}
}

// Without any holdout, a SetUnit eviction of a split rule resolved before the
// unit retires the entry too: a variable read whose retry lands on a version
// that no longer defines the key does not expose it, as before holdouts.
func TestHoldout_SplitRuleEvictionRetryWithoutHoldoutsChecksTheKey(t *testing.T) {
	var experiment = hoWithVariables(hoExperiment(1, "exp", "user_id", 2), "color")
	experiment.Variants = append(experiment.Variants, jsonmodels.ExperimentVariant{Name: "C", Config: `{"color":"full-on"}`})
	experiment.AssignmentRules = `{"rules":[{"name":"Everyone","type":"split","conditions":null,"environments":[],"percentages":"0/100/0"}]}`
	var replacement = experiment
	replacement.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"w":"a"}`}, {Name: "B", Config: `{"w":"b"}`},
		{Name: "C", Config: `{"w":"c"}`}}
	var h = hoCreate(t, map[string]string{}, hoData(nil, experiment))
	h.ctx.GetAssignment("exp")
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData(nil, replacement))
			h.setUnit("user_id", hoUID)
		}
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "default", h.variable("color"))
	if armed {
		t.Fatal("the hook did not run")
	}
	if exposures := queuedExposures(h.ctx); len(exposures) != 0 {
		t.Fatalf("the replacement without the key was exposed: %+v", exposures)
	}
}

// A context that had holdouts keeps building key sets after a public
// SetDataFailed and a later install without holdouts.
func TestHoldout_SetDataFailedKeepsTheKeySetGate(t *testing.T) {
	var experiment = hoWithVariables(hoExperiment(1, "a", hoUnitType, 0), "v")
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, experiment))
	h.ctx.SetDataFailed(errors.New("boom"))
	h.ctx.SetData(hoData(nil, experiment))
	if !hoKeyed(h.ctx) {
		t.Fatal("SetDataFailed reset the key set gate")
	}
	var plain = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, experiment))
	plain.ctx.SetDataFailed(errors.New("boom"))
	plain.ctx.SetData(hoData(nil, experiment))
	if hoKeyed(plain.ctx) {
		t.Fatal("SetDataFailed turned the key set gate on")
	}
}

// Rules that first appear in a later install also turn the gate on.
func TestHoldout_RulesInALaterInstallTurnTheKeySetGateOn(t *testing.T) {
	var plain = hoWithVariables(hoExperiment(1, "exp", "user_id", 2), "color")
	plain.Variants = append(plain.Variants, jsonmodels.ExperimentVariant{Name: "C", Config: `{"color":"full-on"}`})
	var ruled = plain
	ruled.AssignmentRules = `{"rules":[{"name":"Everyone","type":"split","conditions":null,"environments":[],"percentages":"0/100/0"}]}`
	var replacement = plain
	replacement.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"w":"a"}`}, {Name: "B", Config: `{"w":"b"}`},
		{Name: "C", Config: `{"w":"c"}`}}
	var h = hoCreate(t, map[string]string{}, hoData(nil, plain))
	h.ctx.SetData(hoData(nil, ruled))
	h.ctx.GetAssignment("exp")
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData(nil, replacement))
			h.setUnit("user_id", hoUID)
		}
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "default", h.variable("color"))
	if armed {
		t.Fatal("the hook did not run")
	}
	if exposures := queuedExposures(h.ctx); len(exposures) != 0 {
		t.Fatalf("the replacement without the key was exposed: %+v", exposures)
	}
}

// blockingParser holds a "block" config's parse until released, so a test can
// finish another install while this one is still parsing.
type blockingParser struct {
	parsing chan struct{}
	release chan struct{}
}

func (p blockingParser) Parse(context Context, experimentName string, variantName string, config string) map[string]interface{} {
	if config == `{"block":1}` {
		p.parsing <- struct{}{}
		<-p.release
	}
	return DefaultVariableParser{}.Parse(context, experimentName, variantName, config)
}

// An install with holdouts that finishes while a plain install is still
// parsing keeps the key set gate on: the plain install reads the gate under
// the lock, not from before its parse.
func TestHoldout_ConcurrentInstallKeepsTheKeySetGate(t *testing.T) {
	var parser = blockingParser{parsing: make(chan struct{}), release: make(chan struct{})}
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{hoUnitType: hoUIDNotHeldOut}
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	var ft, done = future.New()
	done(hoData(nil), nil)
	var ctx = CreateContext(internal.FixedClock{Millis_: hoMillis}, config, ft, &hoProvider{}, &hoPublisher{}, nil,
		parser, AudienceMatcher{DefaultAudienceDeserializer{}})
	ctx.WaitUntilReady()
	t.Cleanup(func() { ctx.ClearRefreshTimer() })
	var plain = hoExperiment(1, "a", hoUnitType, 0)
	plain.Variants[0].Config = `{"block":1}`
	var finished = make(chan struct{})
	go func() {
		defer close(finished)
		ctx.SetData(hoData(nil, plain))
	}()
	select {
	case <-parser.parsing:
	case <-time.After(10 * time.Second):
		t.Fatal("the plain install never parsed its variants")
	}
	ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoWithVariables(hoExperiment(1, "a", hoUnitType, 0, 11), "v")))
	close(parser.release)
	<-finished
	if index := ctx.index_.Load(); index == nil || !index.keyed {
		t.Fatal("the plain install dropped the key set gate")
	}
}

// A refresh between a shared-key read listing its experiments and resolving
// one of them can install a version that no longer defines the key: that
// experiment, and its holdout, are not exposed by the read.
func TestHoldout_SharedKeyReadSkipsACandidateRefreshedWithoutTheKey(t *testing.T) {
	var a = hoExperiment(1, "a", hoUnitType, 0)
	a.Split = []float64{1, 0}
	a.Variants[0].Config = `{"w":"a"}`
	a.Variants[1].Config = `{"v":"a"}`
	var b = hoWithVariables(hoExperiment(2, "b", hoUnitType, 0, 12), "v")
	var refreshed = b
	refreshed.Iteration = 2
	refreshed.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"w":"b"}`}, {Name: "B", Config: `{"w":"b"}`}}
	var c = hoWithVariables(hoExperiment(3, "c", hoUnitType, 0), "v")
	c.Split = []float64{1, 0}
	var holdout = hoHoldout(12, "holdout_b", hoUnitType, hoSeedBHi, hoSeedBLo)
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{holdout}, a, b, c))
	var armed = true
	afterReadResolve = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData([]jsonmodels.Holdout{holdout}, a, refreshed, c))
		}
	}
	defer func() { afterReadResolve = nil }()
	expectValue(t, "control", h.variable("v"))
	var served = false
	for _, exposure := range queuedExposures(h.ctx) {
		served = served || exposure.Id == 3
	}
	if !served {
		t.Fatalf("the experiment serving the value was not exposed: %+v", queuedExposures(h.ctx))
	}
	if armed {
		t.Fatal("the hook did not run")
	}
	for _, exposure := range queuedExposures(h.ctx) {
		if exposure.Id == 2 || exposure.Id == 12 {
			t.Fatalf("the refreshed candidate without the key was exposed: %+v", queuedExposures(h.ctx))
		}
	}
}

// A read already holding a missing-unit decision whose claim by another read
// a close rejected, after the unit arrived, resolves it again with the unit
// and exposes that decision: the rollback's eviction retires the entry.
func TestHoldout_ReadHoldingARejectedMissingUnitDecisionResolvesAgain(t *testing.T) {
	var h = hoCreate(t, nil, hoData([]jsonmodels.Holdout{hoHoldout(11, "holdout_user", "user_id", hoSeedBHi, hoSeedBLo)},
		hoExperiment(1, "exp", "user_id", 2, 11)))
	h.ctx.GetAssignment("exp")
	var armed = true
	afterReadResolve = func() {
		if armed {
			armed = false
			var entry = h.ctx.getAssignment("exp")
			if !hoClaim(h, entry) {
				t.Error("the other read did not claim")
			}
			h.setUnit("user_id", hoUID)
			h.ctx.Closing_.Store(true)
			h.ctx.admitExposure(entry.assignment, entry)
			h.ctx.Closing_.Store(false)
		}
	}
	defer func() { afterReadResolve = nil }()
	expectInt(t, 2, h.treatment("exp"))
	if armed {
		t.Fatal("the hook did not run")
	}
	var own = hoOwn("user_id", 1, "exp", 2)
	own.FullOn = true
	h.expectPublished(own, hoExposure("user_id", 11, "holdout_user", 1))
}

// A cached, unexposed experiment that a refresh gave the key, without
// changing its assignment, is still exposed by a read of that key.
func TestHoldout_SharedKeyReadExposesACachedExperimentRefreshedWithTheKey(t *testing.T) {
	var before = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "x")
	var after = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, before))
	h.peek("e")
	h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, after))
	h.variable("v")
	var exposed = false
	for _, exposure := range queuedExposures(h.ctx) {
		exposed = exposed || exposure.Id == 1
	}
	if !exposed {
		t.Fatalf("the experiment listed for the key was not exposed: %+v", queuedExposures(h.ctx))
	}
}

// A suppressed experiment whose retry lands on a replacement without the key
// is skipped, so it does not take the suppressed fallback from a later
// suppressed experiment that defines the key.
func TestHoldout_SkippedRetryDoesNotTakeTheSuppressedFallback(t *testing.T) {
	var a = hoExperiment(1, "a", hoUnitType, 0, 11)
	a.Variants[0].Config = `{"v":"a0"}`
	a.Variants[1].Config = `{"v":"a1"}`
	var replacement = a
	replacement.Iteration = 2
	replacement.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"w":"a0"}`}, {Name: "B", Config: `{"w":"a1"}`}}
	var b = hoExperiment(2, "b", hoUnitType, 0, 11)
	b.Variants[0].Config = `{"v":"b0"}`
	b.Variants[1].Config = `{"v":"b1"}`
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{hoHoldoutA()}, a, b))
	h.ctx.GetAssignment("a")
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, replacement, b))
			h.ctx.GetAssignment("a")
		}
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "b0", h.variable("v"))
	if armed {
		t.Fatal("the hook did not run")
	}
}

// A refresh that removes an experiment after a read listed the key's
// experiments leaves only the experiments still installed to be exposed.
func TestHoldout_SharedKeyReadSkipsAnExperimentRefreshedAway(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
	var f = hoWithVariables(hoExperiment(2, "f", hoUnitType, 0), "v")
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, e, f))
	h.peek("e")
	var armed = true
	afterReadResolve = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, f))
		}
	}
	defer func() { afterReadResolve = nil }()
	h.variable("v")
	if armed {
		t.Fatal("the hook did not run")
	}
	var exposures = queuedExposures(h.ctx)
	if len(exposures) != 1 || exposures[0].Id != 2 {
		t.Fatalf("exposures %+v, want only experiment 2", exposures)
	}
}

// A refresh that removes the key right before the read's claim, on the first
// attempt or on a retry, makes the read skip that experiment and its holdout:
// eligibility is checked with the claim, against the data installed then.
func TestHoldout_RefreshRightBeforeTheClaimIsChecked(t *testing.T) {
	for _, retry := range []bool{false, true} {
		var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
		var kept = e
		kept.Iteration = 2
		var dropped = e
		dropped.Iteration = 3
		dropped.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"w":"a"}`}, {Name: "B", Config: `{"w":"b"}`}}
		var f = hoWithVariables(hoExperiment(2, "f", hoUnitType, 0), "v")
		f.Split = []float64{1, 0}
		var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, e, f))
		h.ctx.GetAssignment("e")
		var claims = 0
		beforeSettleClaim = func() {
			claims++
			if retry && claims == 1 {
				h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, kept, f))
				h.ctx.GetAssignment("e")
			} else if claims == 1 || (retry && claims == 2) {
				h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, dropped, f))
			}
		}
		var got = h.variable("v")
		beforeSettleClaim = nil
		expectValue(t, "control", got)
		var exposures = queuedExposures(h.ctx)
		if len(exposures) != 1 || exposures[0].Id != 2 {
			t.Fatalf("retry=%v: exposures %+v, want only experiment 2", retry, exposures)
		}
	}
}

// The read's eligibility check runs under ContextLock_, so no refresh can
// install new data between the check and the claim.
func TestHoldout_EligibilityIsCheckedUnderTheContextLock(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")))
	var ran = false
	beforeEligibleClaim = func() {
		ran = true
		if h.ctx.ContextLock_.TryLock() {
			h.ctx.ContextLock_.Unlock()
			t.Error("a refresh could take the lock during the check")
		}
	}
	defer func() { beforeEligibleClaim = nil }()
	h.variable("v")
	if !ran {
		t.Fatal("the check did not run")
	}
}

// An entry cached from a plain payload, before the context had holdouts, is
// checked like any other once holdouts are installed: a refresh that removes
// its experiment after the read resolved it exposes neither it nor anything
// it no longer names.
func TestHoldout_EntryCachedBeforeHoldoutsIsChecked(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	var f = hoWithVariables(hoExperiment(2, "f", hoUnitType, 0), "v")
	f.Split = []float64{1, 0}
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, f))
	h.peek("e")
	var armed = true
	afterReadResolve = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, f))
		}
	}
	defer func() { afterReadResolve = nil }()
	expectValue(t, "control", h.variable("v"))
	if armed {
		t.Fatal("the hook did not run")
	}
	var exposures = queuedExposures(h.ctx)
	if len(exposures) != 1 || exposures[0].Id != 2 {
		t.Fatalf("exposures %+v, want only experiment 2", exposures)
	}
}

// A claim another read already took is honoured even if a refresh then drops
// the key: a read reaching its claim finds the entry exposed and serves the
// decision that claim publishes, instead of skipping it as ineligible.
func TestHoldout_TakenClaimIsHonouredAfterTheKeyIsDropped(t *testing.T) {
	for _, epochChange := range []bool{false, true} {
		var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
		var dropped = hoExperiment(1, "e", hoUnitType, 0, 11)
		dropped.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"w":"a"}`}, {Name: "B", Config: `{"w":"b"}`}}
		var freshHoldout = hoHoldoutA()
		if epochChange {
			freshHoldout.Iteration++
			freshHoldout.Name = "new_holdout"
		}
		var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
		var entry = h.ctx.getAssignment("e")
		var armed = true
		beforeSettleClaim = func() {
			if armed {
				armed = false
				if !hoClaim(h, entry) {
					t.Error("the other read did not claim")
				}
				h.ctx.SetData(hoData([]jsonmodels.Holdout{freshHoldout}, dropped))
			}
		}
		defer func() { beforeSettleClaim = nil }()
		expectValue(t, "control", h.variable("v"))
		if armed {
			t.Fatal("the hook did not run")
		}
		if got := h.ctx.admitExposure(entry.assignment, entry); got != exposureQueued {
			t.Fatalf("the taken claim reported %v", got)
		}
		var exposures = queuedExposures(h.ctx)
		if len(exposures) != 2 || exposures[0].Id != 1 || exposures[1].Name != "holdout_a" {
			t.Fatalf("exposures %+v, want the taken claim's decision", exposures)
		}
	}
}

// The claim's compare-and-swap runs under ContextLock_ too: right after it
// no refresh can take the lock, so none can land between check and claim.
func TestHoldout_EligibilityClaimCASStaysUnderTheContextLock(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")))
	var ran = false
	afterEligibleCAS = func() {
		ran = true
		if h.ctx.ContextLock_.TryLock() {
			h.ctx.ContextLock_.Unlock()
			t.Error("the claim is outside the context lock")
		}
	}
	defer func() { afterEligibleCAS = nil }()
	h.variable("v")
	if !ran {
		t.Fatal("the CAS hook did not run")
	}
}

func TestHoldout_ReadinessCopyVariableExposureUsesTheLiveQueue(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")))
	var cp = h.ctx.WaitUntilReady()
	if _, err := cp.GetVariableValue("v", "default"); err != nil {
		t.Fatal(err)
	}
	var exposures = queuedExposures(h.ctx)
	if len(exposures) != 2 || exposures[0].Id != 1 || exposures[1].Id != 11 {
		t.Fatalf("live exposures %+v", exposures)
	}
}

func TestHoldout_VariableRetryRejectsAnOverrideWithoutVariables(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")))
	h.ctx.GetAssignment("e")
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			if err := h.ctx.SetOverride("e", 5); err != nil {
				t.Error(err)
			}
			h.ctx.GetAssignment("e")
		}
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "default", h.variable("v"))
	if armed {
		t.Fatal("the hook did not run")
	}
	if exposures := queuedExposures(h.ctx); len(exposures) != 0 {
		t.Fatalf("nil-variable replacement was exposed: %+v", exposures)
	}
}

// The eligibility check and the claim are one critical section: a refresh
// that queues on the lock at the check cannot install before the claim, even
// if the lock were released and taken again between the two.
func TestHoldout_CheckAndClaimAreOneCriticalSection(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
	var without = hoExperiment(1, "e", hoUnitType, 0, 11)
	var seen *contextIndex
	var done = make(chan struct{})
	var armed = true
	beforeEligibleClaim = func() {
		if !armed {
			return
		}
		armed = false
		seen = h.ctx.index_.Load()
		go func() {
			defer close(done)
			h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, without))
		}()
		var deadline = time.Now().Add(5 * time.Second)
		// A queued writer makes TryRLock fail.
		for h.ctx.ContextLock_.TryRLock() {
			h.ctx.ContextLock_.RUnlock()
			if time.Now().After(deadline) {
				t.Fatal("the refresh never queued on the lock")
			}
			time.Sleep(time.Millisecond)
		}
	}
	afterEligibleCAS = func() {
		select {
		case <-done:
		case <-time.After(20 * time.Millisecond):
		}
		if h.ctx.index_.Load() != seen {
			t.Error("a refresh installed between the eligibility check and the claim")
		}
	}
	defer func() { beforeEligibleClaim, afterEligibleCAS = nil, nil }()
	h.variable("v")
	if armed {
		t.Fatal("the read never reached the claim")
	}
	<-done
}

// A restored key cannot authorize a new claim on an intermediate experiment
// or holdout epoch, even without a replacement lookup after the final install.
func TestHoldout_RestoredKeyDoesNotAuthorizeAnIntermediateIteration(t *testing.T) {
	// Each case changes exactly one part of the decision's identity.
	for _, epoch := range []string{"experiment", "holdout", "unitType"} {
		t.Run(epoch, func(t *testing.T) {
			for _, override := range []bool{false, true} {
				var first = hoWithVariables(hoExperiment(2, "b", hoUnitType, 0, 11), "v")
				var middle = hoWithVariables(hoExperiment(2, "b", hoUnitType, 0, 11), "w")
				var last = hoWithVariables(hoExperiment(2, "b", hoUnitType, 0, 11), "v")
				var intermediate = hoHoldoutA()
				intermediate.Name = "intermediate_holdout"
				var restored = hoHoldoutA()
				restored.Name = "restored_holdout"
				switch epoch {
				case "experiment":
					middle.Iteration = 2
					last.Iteration = 3
				case "holdout":
					intermediate.Iteration = 2
					restored.Iteration = 3
				case "unitType":
					middle.UnitType = "user_id"
				}
				var h = hoCreate(t, map[string]string{hoUnitType: hoUIDNotHeldOut, "user_id": hoUID},
					hoData([]jsonmodels.Holdout{hoHoldoutA()}, first))
				if override {
					if err := h.ctx.SetOverride("b", 0); err != nil {
						t.Fatal(err)
					}
				}
				t.Cleanup(func() { afterReadResolve, beforeSettleClaim = nil, nil })
				var armed = true
				afterReadResolve = func() {
					if armed {
						armed = false
						h.ctx.SetData(hoData([]jsonmodels.Holdout{intermediate}, middle))
						h.ctx.GetAssignment("b")
					}
				}
				var claims = 0
				beforeSettleClaim = func() {
					claims++
					if claims == 2 {
						h.ctx.SetData(hoData([]jsonmodels.Holdout{restored}, last))
					}
				}
				// The read resolves again onto the final definition and serves
				// and exposes that decision, never the intermediate one.
				expectValue(t, "control", h.variable("v"))
				afterReadResolve, beforeSettleClaim = nil, nil
				if armed || claims < 3 {
					t.Fatal("the intermediate decision was not retried")
				}
				var exposures = queuedExposures(h.ctx)
				if len(exposures) != 2 || exposures[0].Id != 2 || exposures[0].Unit != hoUnitType ||
					exposures[0].Overridden != override || exposures[1].Id != 11 || exposures[1].Name == "intermediate_holdout" {
					t.Fatalf("override=%v: exposures %+v, want only the final decision", override, exposures)
				}
			}
		})
	}
}

func TestHoldout_UnclaimedOverrideRefreshResolvesCurrentIdentity(t *testing.T) {
	for _, changeID := range []bool{false, true} {
		var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
		var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
		if err := h.ctx.SetOverride("e", 0); err != nil {
			t.Fatal(err)
		}
		h.ctx.PeekVariableValue("v", "default")
		var fresh = e
		if changeID {
			fresh.Id++
		} else {
			fresh.Iteration++
		}
		fresh.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"v":"refreshed"}`}, {Name: "B", Config: `{"v":"other"}`}}
		h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, fresh))
		for i := 0; i < 3; i++ {
			expectValue(t, "refreshed", h.variable("v"))
		}
		var exposures = queuedExposures(h.ctx)
		if len(exposures) != 2 || exposures[0].Id != fresh.Id || !exposures[0].Overridden || exposures[1].Id != 11 {
			t.Fatalf("idChange=%v: exposures %+v", changeID, exposures)
		}
	}
}

func TestHoldout_IdentityChangeAtVariableClaimIsRejected(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
	h.peek("e")
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			e.Id++
			h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
		}
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "control", h.variable("v"))
	if armed {
		t.Fatal("the claim hook did not run")
	}
	var exposures = queuedExposures(h.ctx)
	if len(exposures) != 2 || exposures[0].Id != e.Id || exposures[1].Id != 11 {
		t.Fatalf("exposures %+v, want only the new identity's decision", exposures)
	}
}

func TestHoldout_FreshOverrideVariableClaimHasResolvedIdentity(t *testing.T) {
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")))
	if err := h.ctx.SetOverride("e", 0); err != nil {
		t.Fatal(err)
	}
	expectValue(t, "control", h.variable("v"))
	if got := h.ctx.GetAssignment("e").Iteration; got != 0 {
		t.Fatalf("public override iteration %d, want 0", got)
	}
	var exposures = queuedExposures(h.ctx)
	if len(exposures) != 2 || exposures[0].Id != 1 || !exposures[0].Overridden || exposures[0].Variant != 0 || exposures[1].Id != 11 {
		t.Fatalf("override exposures %+v", exposures)
	}
}

// Without holdouts or rules an unclaimed override keeps its cached values
// across an experiment refresh, as in v1.1.1 and the JS SDK.
func TestHoldout_PlainUnclaimedOverrideKeepsItsValuesAcrossARefresh(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e))
	if err := h.ctx.SetOverride("e", 0); err != nil {
		t.Fatal(err)
	}
	if value, err := h.ctx.PeekVariableValue("v", "default"); err != nil || value != "control" {
		t.Fatalf("peek: %v, %v", value, err)
	}
	var fresh = e
	fresh.Iteration++
	fresh.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"v":"new"}`}, {Name: "B", Config: `{"v":"other"}`}}
	h.ctx.SetData(hoData(nil, fresh))
	expectValue(t, "control", h.variable("v"))
}

func TestHoldout_ExposedOverrideIsKeptAcrossAnIdentityRefresh(t *testing.T) {
	for _, changeID := range []bool{false, true} {
		var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0, 11), "v")
		var h = hoCreateFor(t, hoUIDNotHeldOut, hoData([]jsonmodels.Holdout{hoHoldoutA()}, e))
		if err := h.ctx.SetOverride("e", 0); err != nil {
			t.Fatal(err)
		}
		expectValue(t, "control", h.variable("v"))
		var before = h.ctx.getAssignment("e")
		var fresh = e
		if changeID {
			fresh.Id++
		} else {
			fresh.Iteration++
		}
		fresh.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"v":"new"}`}, {Name: "B", Config: `{"v":"other"}`}}
		h.ctx.SetData(hoData([]jsonmodels.Holdout{hoHoldoutA()}, fresh))
		for i := 0; i < 3; i++ {
			expectValue(t, "control", h.variable("v"))
		}
		if h.ctx.getAssignment("e") != before {
			t.Fatalf("idChange=%v: the exposed override was rebuilt", changeID)
		}
		if exposures := queuedExposures(h.ctx); len(exposures) != 2 {
			t.Fatalf("idChange=%v: exposures %+v", changeID, exposures)
		}
	}
}

// A context with assignment rules but no holdouts also rebuilds an unclaimed
// override after its experiment's id, iteration or unit type changes, so it
// serves and exposes the current definition.
func TestHoldout_RulesOnlyUnclaimedOverrideResolvesCurrentIdentity(t *testing.T) {
	for _, identity := range []string{"iteration", "id", "unitType"} {
		t.Run(identity, func(t *testing.T) {
			var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
			var r = hoExperiment(2, "r", hoUnitType, 0)
			r.AssignmentRules = hoUSRule
			var h = hoCreate(t, map[string]string{hoUnitType: hoUIDNotHeldOut, "user_id": hoUID}, hoData(nil, e, r))
			if !hoKeyed(h.ctx) {
				t.Fatal("a context with rules is not keyed")
			}
			if err := h.ctx.SetOverride("e", 0); err != nil {
				t.Fatal(err)
			}
			if _, err := h.ctx.PeekVariableValue("v", "default"); err != nil {
				t.Fatal(err)
			}
			var old = h.ctx.GetAssignment("e")
			var retainedCopy = *old
			var fresh = e
			switch identity {
			case "id":
				fresh.Id = 3
			case "iteration":
				fresh.Iteration++
			case "unitType":
				fresh.UnitType = "user_id"
			}
			fresh.Variants = []jsonmodels.ExperimentVariant{{Name: "A", Config: `{"v":"new"}`}, {Name: "B", Config: `{"v":"other"}`}}
			h.ctx.SetData(hoData(nil, fresh, r))
			expectValue(t, "new", h.variable("v"))
			h.ctx.QueueExposure(old)
			h.ctx.QueueExposure(&retainedCopy)
			var want = hoOwn(fresh.UnitType, fresh.Id, "e", 0)
			want.Assigned, want.Overridden = false, true
			h.expectPublished(want)
		})
	}
}

// A restored key cannot authorize a claim on a different full-on decision.
// With the unit in arm 1 of a three-arm holdout, the key goes v -> w -> v
// while FullOnVariant goes 0 -> 1 -> 0, and nothing else in the experiment's
// or holdout's identity changes: the read retries onto the middle (full-on,
// not suppressed) decision, and the last refresh lands before it claims.
func TestHoldout_RestoredKeyDoesNotAuthorizeADifferentFullOnDecision(t *testing.T) {
	var three = hoThreeArm(11, "three_arm", hoThreeArm1Lo)
	var first = hoWithVariables(hoExperiment(2, "b", hoUnitType, 0, 11), "v")
	var middle = hoWithVariables(hoExperiment(2, "b", hoUnitType, 1, 11), "w")
	var last = hoWithVariables(hoExperiment(2, "b", hoUnitType, 0, 11), "v")
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{three}, first))
	t.Cleanup(func() { afterReadResolve, beforeSettleClaim = nil, nil })
	var armed = true
	afterReadResolve = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData([]jsonmodels.Holdout{three}, middle))
			h.ctx.GetAssignment("b")
		}
	}
	var claims = 0
	beforeSettleClaim = func() {
		claims++
		if claims == 2 {
			h.ctx.SetData(hoData([]jsonmodels.Holdout{three}, last))
		}
	}
	expectValue(t, "control", h.variable("v"))
	afterReadResolve, beforeSettleClaim = nil, nil
	if armed || claims < 3 {
		t.Fatal("the middle decision was not retried")
	}
	h.expectPublished(hoExposure(hoUnitType, 11, "three_arm", 1))
}

// A refresh that only changes the traffic split, landing between a variable
// read's resolve and its claim, makes the cached decision stale but leaves the
// experiment a candidate: the read resolves again and serves and exposes the
// current decision once, instead of skipping it (no lost exposure), and a
// later GetTreatment adds nothing.
func TestHoldout_StaleUnreplacedCandidateIsResolvedAgain(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	e.Split = []float64{0, 1}
	var r = hoExperiment(2, "r", hoUnitType, 0)
	r.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
	h.peek("e")
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			var traffic = e
			traffic.TrafficSplit = []float64{0, 1, 0}
			h.ctx.SetData(hoData(nil, traffic, r))
		}
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "treatment", h.variable("v"))
	if armed {
		t.Fatal("the hook did not run")
	}
	expectInt(t, 1, h.treatment("e"))
	if exposures := queuedExposures(h.ctx); len(exposures) != 1 || exposures[0].Id != 1 || exposures[0].Variant != 1 {
		t.Fatalf("exposures %+v, want exactly one for the current decision", exposures)
	}
}

// An entry that revalidates to the same outcome (attributes changed but the
// rule still gives the same variant) stays the current decision, so the
// variable read claims it.
func TestHoldout_RevalidatedEntryStaysEligible(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	e.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e))
	h.setAttribute("country", "US")
	h.peek("e")
	var claims = 0
	beforeSettleClaim = func() {
		claims++
		if claims == 1 {
			h.setAttribute("unrelated", 1)
		}
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "treatment", h.variable("v"))
	expectInt(t, 1, claims)
	if exposures := queuedExposures(h.ctx); len(exposures) != 1 || exposures[0].Id != 1 || !exposures[0].RuleOverride {
		t.Fatalf("exposures %+v, want exactly the rule-won decision", exposures)
	}
}

// In a keyed context an experiment whose custom assignment the build cannot
// apply (full-on here) is exposed by every variable read, once per read, as in
// v1.1.1: the claim check does not treat the unapplied custom assignment as a
// newer decision, so the read neither retries nor skips it.
func TestHoldout_UnappliedCustomAssignmentIsStillExposed(t *testing.T) {
	for _, keyed := range []bool{false, true} {
		var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 1), "v")
		var r = hoExperiment(2, "r", hoUnitType, 0)
		if keyed {
			r.AssignmentRules = hoUSRule
		}
		var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
		if hoKeyed(h.ctx) != keyed {
			t.Fatalf("keyed = %v, want %v", hoKeyed(h.ctx), keyed)
		}
		if err := h.ctx.SetCustomAssignment("e", 0); err != nil {
			t.Fatal(err)
		}
		var claims = 0
		beforeSettleClaim = func() { claims++ }
		expectValue(t, "treatment", h.variable("v"))
		expectValue(t, "treatment", h.variable("v"))
		beforeSettleClaim = nil
		expectInt(t, 2, claims)
		var exposures = queuedExposures(h.ctx)
		if len(exposures) != 2 || !exposures[0].FullOn || !exposures[1].FullOn {
			t.Fatalf("keyed=%v: exposures %+v, want one full-on exposure per read", keyed, exposures)
		}
	}
}

// A refresh that replaces the cached entry (a rebuild after a traffic change)
// between a variable read's resolve and its claim, without retiring it, makes
// the read resolve again onto the replacement: exactly one exposure in total,
// even with a following GetTreatment.
func TestHoldout_ReplacedUnretiredEntryIsResolvedAgain(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	e.Split = []float64{0, 1}
	var r = hoExperiment(2, "r", hoUnitType, 0)
	r.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
	h.peek("e")
	var armed = true
	afterReadResolve = func() {
		if armed {
			armed = false
			var traffic = e
			traffic.TrafficSplit = []float64{0, 1, 0}
			h.ctx.SetData(hoData(nil, traffic, r))
			h.ctx.GetAssignment("e")
		}
	}
	defer func() { afterReadResolve = nil }()
	expectValue(t, "treatment", h.variable("v"))
	if armed {
		t.Fatal("the hook did not run")
	}
	expectInt(t, 1, h.treatment("e"))
	if exposures := queuedExposures(h.ctx); len(exposures) != 1 {
		t.Fatalf("exposures %+v, want exactly one", exposures)
	}
}

// When every attempt of a variable read finds its decision stale, the read
// stops after maxExposureAttempts and serves the last decision it tried,
// without an exposure, as in the Java SDK; the next read exposes it. The first
// decision serves treatment and every later one control, so serving the first
// decision instead of the last fails.
func TestHoldout_VariableReadServesTheLastDecisionWhenRetriesRunOut(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	e.Split = []float64{0, 1}
	var r = hoExperiment(2, "r", hoUnitType, 0)
	r.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
	h.peek("e")
	var claims = 0
	beforeSettleClaim = func() {
		claims++
		var next = e
		next.Split = []float64{1, 0}
		next.TrafficSplit = []float64{0, 1, float64(claims)}
		h.ctx.SetData(hoData(nil, next, r))
	}
	expectValue(t, "control", h.variable("v"))
	beforeSettleClaim = nil
	expectInt(t, maxExposureAttempts, claims)
	h.expectPending(0)
	expectValue(t, "control", h.variable("v"))
	h.expectPending(1)
}

// Changing the custom assignment after a decision was built from it is still
// a new decision: an applicable new custom variant rebuilds the entry.
func TestHoldout_ChangedCustomAssignmentStillRebuilds(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	var r = hoExperiment(2, "r", hoUnitType, 0)
	r.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
	if err := h.ctx.SetCustomAssignment("e", 0); err != nil {
		t.Fatal(err)
	}
	expectInt(t, 0, h.peek("e"))
	if err := h.ctx.SetCustomAssignment("e", 1); err != nil {
		t.Fatal(err)
	}
	expectInt(t, 1, h.peek("e"))
	expectValue(t, "treatment", h.variable("v"))
}

// In a keyed context a custom assignment that becomes applicable between a
// variable read's resolve and its claim (here a refresh makes the unit
// traffic-eligible, or an attribute makes a strict audience match) leaves the
// read's decision stale, so the read resolves again: it serves the custom
// variant and exposes it once, with the custom flag.
func TestHoldout_CustomBecomingApplicableDuringAReadIsResolvedAgain(t *testing.T) {
	for _, c := range []struct {
		name   string
		setup  func(*jsonmodels.Experiment)
		mutate func(*hoContext, jsonmodels.Experiment, jsonmodels.Experiment)
	}{
		{"traffic refresh", func(e *jsonmodels.Experiment) { e.TrafficSplit = []float64{1, 0} },
			func(h *hoContext, e jsonmodels.Experiment, r jsonmodels.Experiment) {
				e.TrafficSplit = []float64{0, 1}
				h.ctx.SetData(hoData(nil, e, r))
			}},
		{"strict audience attribute", func(e *jsonmodels.Experiment) {
			e.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
			e.AudienceStrict = true
		}, func(h *hoContext, _ jsonmodels.Experiment, _ jsonmodels.Experiment) { h.setAttribute("country", "GB") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
			c.setup(&e)
			var r = hoExperiment(2, "r", hoUnitType, 0)
			r.AssignmentRules = hoUSRule
			var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
			if err := h.ctx.SetCustomAssignment("e", 1); err != nil {
				t.Fatal(err)
			}
			var armed = true
			afterReadResolve = func() {
				if armed {
					armed = false
					c.mutate(h, e, r)
				}
			}
			defer func() { afterReadResolve = nil }()
			expectValue(t, "treatment", h.variable("v"))
			if armed {
				t.Fatal("the hook did not run")
			}
			var exposures = queuedExposures(h.ctx)
			if len(exposures) != 1 || exposures[0].Variant != 1 || !exposures[0].Custom {
				t.Fatalf("exposures %+v, want one custom variant 1", exposures)
			}
		})
	}
}

// A restored key cannot authorize a decision made before the audience became
// strict. The read resolves v; a refresh installs w (traffic changed, so the
// read retries onto a new full-on decision resolved while the audience was not
// strict); before that retry claims, the last install restores v and makes the
// same non-matching audience strict. Rules and attributes are unchanged, so
// only the audience definition shows the decision is stale.
func TestHoldout_RestoredKeyDoesNotAuthorizeADecisionBeforeAStrictAudience(t *testing.T) {
	var three = hoThreeArm(11, "three_arm", hoThreeArm1Lo)
	var audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	var first = hoWithVariables(hoExperiment(2, "b", hoUnitType, 1, 11), "v")
	first.Audience = audience
	var middle = hoWithVariables(hoExperiment(2, "b", hoUnitType, 1, 11), "w")
	middle.Audience = audience
	middle.TrafficSplit = []float64{0, 1, 0}
	var last = middle
	last.Variants = first.Variants
	last.AudienceStrict = true
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{three}, first))
	t.Cleanup(func() { afterReadResolve, beforeSettleClaim = nil, nil })
	var armed = true
	afterReadResolve = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData([]jsonmodels.Holdout{three}, middle))
			h.ctx.GetAssignment("b")
		}
	}
	var claims = 0
	beforeSettleClaim = func() {
		claims++
		if claims == 2 {
			h.ctx.SetData(hoData([]jsonmodels.Holdout{three}, last))
		}
	}
	h.variable("v")
	afterReadResolve, beforeSettleClaim = nil, nil
	if armed || claims < 2 {
		t.Fatal("the middle decision was not retried")
	}
	for _, exposure := range queuedExposures(h.ctx) {
		if exposure.Id == 2 && (exposure.FullOn || exposure.Assigned) {
			t.Fatalf("the pre-strict full-on decision was exposed: %+v", queuedExposures(h.ctx))
		}
	}
}

// A concurrent read that rebuilds an equivalent decision (an unapplied custom
// assignment makes every lookup rebuild) does not make this read's decision
// stale: the read claims it on the first attempt and exposes it once.
func TestHoldout_EquivalentRebuildDoesNotCostTheExposure(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 1), "v")
	var r = hoExperiment(2, "r", hoUnitType, 0)
	r.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
	if err := h.ctx.SetCustomAssignment("e", 0); err != nil {
		t.Fatal(err)
	}
	var claims = 0
	beforeSettleClaim = func() {
		claims++
		h.peek("e")
	}
	defer func() { beforeSettleClaim = nil }()
	expectValue(t, "treatment", h.variable("v"))
	expectInt(t, 1, claims)
	if exposures := queuedExposures(h.ctx); len(exposures) != 1 || !exposures[0].FullOn {
		t.Fatalf("exposures %+v, want one full-on exposure", exposures)
	}
}

// A panic while the claim revalidates (an audience evaluator panicking on a
// malformed attribute) still releases the context lock, so the context is not
// left stuck once the caller recovers.
func TestHoldout_ClaimReleasesTheLockOnPanic(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	var r = hoExperiment(2, "r", hoUnitType, 0)
	r.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
	h.peek("e")
	beforeEligibleClaim = func() { panic("evaluator panic") }
	func() {
		defer func() { _ = recover() }()
		h.ctx.GetVariableValue("v", "default")
	}()
	beforeEligibleClaim = nil
	if !h.ctx.ContextLock_.TryLock() {
		t.Fatal("the claim left the context lock held after a panic")
	}
	h.ctx.ContextLock_.Unlock()
}

// In a keyed context a refresh that edits an unexposed decision's audience
// (the expression or its strictness) rebuilds the decision, so the next
// variable read claims and exposes it once instead of finding it stale on
// every attempt.
func TestHoldout_AudienceEditRebuildsAnUnexposedKeyedDecision(t *testing.T) {
	var gb = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	var us = `{"filter":[{"eq":[{"var":"country"},{"value":"US"}]}]}`
	for _, c := range []struct {
		name   string
		edit   func(*jsonmodels.Experiment)
		strict bool
	}{
		{"expression", func(e *jsonmodels.Experiment) { e.Audience = us }, false},
		{"strictness", func(e *jsonmodels.Experiment) { e.AudienceStrict = true }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
			e.Audience = gb
			var r = hoExperiment(2, "r", hoUnitType, 0)
			r.AssignmentRules = hoUSRule
			var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
			h.setAttribute("country", "GB")
			h.peek("e")
			var edited = e
			c.edit(&edited)
			h.ctx.SetData(hoData(nil, edited, r))
			for i := 0; i < 3; i++ {
				h.variable("v")
			}
			h.expectPending(1)
		})
	}
}

// A restored key cannot authorize a decision whose audience expression
// changed (strictness unchanged and strict throughout) since it was resolved.
func TestHoldout_RestoredKeyDoesNotAuthorizeADecisionBeforeAnAudienceEdit(t *testing.T) {
	var three = hoThreeArm(11, "three_arm", hoThreeArm1Lo)
	var first = hoWithVariables(hoExperiment(2, "b", hoUnitType, 1, 11), "v")
	first.AudienceStrict = true
	var middle = hoWithVariables(hoExperiment(2, "b", hoUnitType, 1, 11), "w")
	middle.AudienceStrict = true
	middle.TrafficSplit = []float64{0, 1, 0}
	var last = middle
	last.Variants = first.Variants
	last.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	var h = hoCreateFor(t, hoUID, hoData([]jsonmodels.Holdout{three}, first))
	t.Cleanup(func() { afterReadResolve, beforeSettleClaim = nil, nil })
	var armed = true
	afterReadResolve = func() {
		if armed {
			armed = false
			h.ctx.SetData(hoData([]jsonmodels.Holdout{three}, middle))
			h.ctx.GetAssignment("b")
		}
	}
	var claims = 0
	beforeSettleClaim = func() {
		claims++
		if claims == 2 {
			h.ctx.SetData(hoData([]jsonmodels.Holdout{three}, last))
		}
	}
	h.variable("v")
	afterReadResolve, beforeSettleClaim = nil, nil
	if armed || claims < 2 {
		t.Fatal("the middle decision was not retried")
	}
	for _, exposure := range queuedExposures(h.ctx) {
		if exposure.Id == 2 && (exposure.FullOn || exposure.Assigned) {
			t.Fatalf("the pre-edit full-on decision was exposed: %+v", queuedExposures(h.ctx))
		}
	}
}

// An exposed decision keeps the released behaviour across an audience edit
// that leaves rules and attributes alone: it is not rebuilt, so it is not
// exposed a second time.
func TestHoldout_AudienceEditKeepsAnExposedKeyedDecision(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	e.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	var r = hoExperiment(2, "r", hoUnitType, 0)
	r.AssignmentRules = hoUSRule
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e, r))
	h.setAttribute("country", "GB")
	h.variable("v")
	var before = h.ctx.getAssignment("e")
	var edited = e
	edited.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"US"}]}]}`
	h.ctx.SetData(hoData(nil, edited, r))
	h.variable("v")
	h.treatment("e")
	if h.ctx.getAssignment("e") != before {
		t.Fatal("the exposed decision was rebuilt")
	}
	h.expectPending(1)
}

// A context with neither holdouts nor rules keeps the released behaviour across
// an audience edit: the unexposed decision is not rebuilt, and one variable
// read exposes it once.
func TestHoldout_AudienceEditKeepsAPlainUnexposedDecision(t *testing.T) {
	var e = hoWithVariables(hoExperiment(1, "e", hoUnitType, 0), "v")
	e.Audience = `{"filter":[{"eq":[{"var":"country"},{"value":"GB"}]}]}`
	var h = hoCreateFor(t, hoUIDNotHeldOut, hoData(nil, e))
	if hoKeyed(h.ctx) {
		t.Fatal("a plain context is keyed")
	}
	h.peek("e")
	var before = h.ctx.getAssignment("e")
	var strict = e
	strict.AudienceStrict = true
	h.ctx.SetData(hoData(nil, strict))
	h.variable("v")
	if h.ctx.getAssignment("e") != before {
		t.Fatal("the plain unexposed decision was rebuilt")
	}
	h.expectPending(1)
}
