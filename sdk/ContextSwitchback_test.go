package sdk

// Switchback serving, ported from the Java SDK's ContextSwitchbackTest
// (java-sdk main 07807b5, core-api/src/test/java/com/absmartly/sdk/
// ContextSwitchbackTest.java; spec SWITCHBACK.md). The expected schedules
// are the Java test's constants, which an independent Python reference
// computed from the platform formulas.

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

const (
	sbUnitType = "store_id"
	sbUID      = "e791e240fcd3df7d238cfc285f475e8152fcc0ec"
	// HashUnit(sbUID).
	sbUIDHash = "pAE3a1i5Drs5mKRNq56adA"

	sbHour int64 = 3_600_000
	sbDay        = 24 * sbHour
	// 2026-01-01T00:00:00Z, hour aligned.
	sbAnchor int64 = 1_767_225_600_000

	sbSeedHi, sbSeedLo = -5, -7

	// The store unit's cluster value: attribute sbClusterPath, set on every
	// default context at creation.
	sbClusterPath = "store_key"
	sbClusterKey  = "store-42"

	sbStrictGoldAudience = `{"filter":[{"eq":[{"var":"tier"},{"value":"gold"}]}]}`
	sbAssignVariant1     = `{"rules":[{"type":"assign","environments":[],"variant":1}]}`
	sbAssignVariant2     = `{"rules":[{"type":"assign","environments":[],"variant":2}]}`
)

// Periods 0..11 of the raw cluster key "store-42" under seed (-5, -7).
var (
	sbExpected2Arms  = []int{0, 1, 0, 1, 1, 0, 1, 0, 1, 0, 0, 1}
	sbExpected3Arms  = []int{0, 2, 1, 2, 1, 0, 1, 2, 0, 1, 0, 2}
	sbExpected10Arms = []int{8, 3, 7, 4, 2, 0, 5, 1, 9, 6, 3, 7}
	// What keying by HashUnit(sbUID) or by sbUID instead of the cluster
	// value would serve.
	sbUIDHash3Arms = []int{1, 2, 0, 2, 1, 0, 2, 0, 1, 1, 2, 0}
	sbUID3Arms     = []int{2, 0, 1, 0, 1, 2, 1, 2, 0, 0, 1, 2}
)

// sbClock is a settable clock. next, when set, is returned by the next read
// only, as the Java test's MutableClock.nextRead.
type sbClock struct {
	mu   sync.Mutex
	now  int64
	next *int64
}

func (c *sbClock) Millis() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.next != nil {
		var value = *c.next
		c.next = nil
		return value
	}
	return c.now
}

func (c *sbClock) set(now int64) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

func (c *sbClock) setNextRead(value int64) {
	c.mu.Lock()
	c.next = &value
	c.mu.Unlock()
}

func sbInt64(v int64) *int64 {
	return &v
}

func sbClusters(entries ...string) jsonmodels.SwitchbackClusters {
	var clusters = jsonmodels.SwitchbackClusters{}
	for i := 0; i < len(entries); i += 2 {
		clusters = append(clusters, jsonmodels.SwitchbackCluster{UnitType: entries[i], Path: entries[i+1]})
	}
	return clusters
}

func sbExperiment(id int, name string, armCount int, periodMs int64) jsonmodels.Experiment {
	var split = make([]float64, armCount)
	var variants = make([]jsonmodels.ExperimentVariant, armCount)
	for i := 0; i < armCount; i++ {
		split[i] = 1.0 / float64(armCount)
		variants[i] = jsonmodels.ExperimentVariant{Name: string(rune('A' + i)), Config: fmt.Sprintf(`{"sb.arm":%d}`, i)}
	}
	return jsonmodels.Experiment{
		Id: id, Name: name, UnitType: sbUnitType, Iteration: 1, SeedHi: sbSeedHi, SeedLo: sbSeedLo, Split: split,
		TrafficSeedHi: 1, TrafficSeedLo: 2, TrafficSplit: []float64{0, 1},
		Applications: []jsonmodels.ExperimentApplication{{Name: "website"}}, Variants: variants,
		RandomizationType: "switchback", SwitchbackClusters: sbClusters(sbUnitType, sbClusterPath),
		SwitchbackStartAt: sbInt64(sbAnchor), SwitchbackPeriodMs: sbInt64(periodMs),
		SwitchbackCarryoverDurationMs: sbInt64(0), SwitchbackBurnInDurationMs: sbInt64(0), SwitchbackBurnOutDurationMs: sbInt64(0),
		SwitchbackDesignType: "balanced_block", SwitchbackEstimand: "global_average_treatment_effect",
	}
}

func sbDefault(armCount int, periodMs int64) jsonmodels.Experiment {
	return sbExperiment(1, "exp_sb", armCount, periodMs)
}

// sbUnit turns a switchback fixture into a unit experiment.
func sbUnit(experiment jsonmodels.Experiment) jsonmodels.Experiment {
	experiment.RandomizationType = ""
	experiment.SwitchbackClusters = nil
	experiment.SwitchbackStartAt = nil
	experiment.SwitchbackPeriodMs = nil
	return experiment
}

func sbData(experiments ...jsonmodels.Experiment) jsonmodels.ContextData {
	return jsonmodels.ContextData{Experiments: experiments}
}

func sbExposure(id int, name string, variant int, exposedAt int64, assigned bool, eligible bool) jsonmodels.Exposure {
	return jsonmodels.Exposure{Id: id, Name: name, Unit: sbUnitType, Variant: variant, ExposedAt: exposedAt,
		Assigned: assigned, Eligible: eligible}
}

func sbRuleExposure(name string, variant int, exposedAt int64) jsonmodels.Exposure {
	return jsonmodels.Exposure{Id: 1, Name: name, Unit: sbUnitType, Variant: variant, ExposedAt: exposedAt,
		Eligible: true, RuleOverride: true}
}

func sbStrictMismatchExposure(exposedAt int64) jsonmodels.Exposure {
	return jsonmodels.Exposure{Id: 1, Name: "exp_sb", Unit: sbUnitType, ExposedAt: exposedAt, Eligible: true,
		AudienceMismatch: true}
}

type sbContext struct {
	t         *testing.T
	ctx       *Context
	clock     *sbClock
	publisher *hoPublisher
	provider  *hoProvider
}

type sbOption func(*ContextConfig)

func sbOverride(name string, variant int) sbOption {
	return func(c *ContextConfig) { c.Overrides_[name] = variant }
}

func sbCustom(name string, variant int) sbOption {
	return func(c *ContextConfig) { c.Cassigmnents_[name] = variant }
}

// sbNoClusterAttribute leaves out the default cluster attribute.
func sbNoClusterAttribute() sbOption {
	return func(c *ContextConfig) { delete(c.Attributes_, sbClusterPath) }
}

// sbCreateAt creates a ready context at the clock reading now, with the units
// (nil for none) and the cluster attribute set at creation.
func sbCreateAt(t *testing.T, now int64, units map[string]string, data jsonmodels.ContextData, options ...sbOption) *sbContext {
	t.Helper()
	var config = CreateDefaultContextConfig()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	for k, v := range units {
		config.Units_[k] = v
	}
	config.Attributes_ = map[string]interface{}{sbClusterPath: sbClusterKey}
	for _, option := range options {
		option(&config)
	}
	var h = &sbContext{t: t, clock: &sbClock{now: now}, publisher: &hoPublisher{}, provider: &hoProvider{}}
	var ft, done = future.New()
	done(data, nil)
	h.ctx = CreateContext(h.clock, config, ft, h.provider, h.publisher, nil, DefaultVariableParser{},
		AudienceMatcher{DefaultAudienceDeserializer{}})
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

func sbCreate(t *testing.T, data jsonmodels.ContextData, options ...sbOption) *sbContext {
	t.Helper()
	return sbCreateAt(t, sbAnchor, map[string]string{sbUnitType: sbUID}, data, options...)
}

func (h *sbContext) at(now int64) *sbContext {
	h.clock.set(now)
	return h
}

func (h *sbContext) treatment(name string) int {
	h.t.Helper()
	var variant, err = h.ctx.GetTreatment(name)
	if err != nil {
		h.t.Fatal(err)
	}
	return variant
}

func (h *sbContext) peek(name string) int {
	h.t.Helper()
	var variant, err = h.ctx.PeekTreatment(name)
	if err != nil {
		h.t.Fatal(err)
	}
	return variant
}

// variable returns a variable value, a JSON number as an int, or -1 for
// the default.
func (h *sbContext) variable(key string) interface{} {
	h.t.Helper()
	var value, err = h.ctx.GetVariableValue(key, -1)
	if err != nil {
		h.t.Fatal(err)
	}
	return sbNumber(value)
}

func (h *sbContext) peekVariable(key string) interface{} {
	h.t.Helper()
	var value, err = h.ctx.PeekVariableValue(key, -1)
	if err != nil {
		h.t.Fatal(err)
	}
	return sbNumber(value)
}

func sbNumber(value interface{}) interface{} {
	if number, ok := value.(float64); ok && number == float64(int(number)) {
		return int(number)
	}
	return value
}

func (h *sbContext) refresh(experiments ...jsonmodels.Experiment) {
	h.t.Helper()
	h.refreshData(sbData(experiments...))
}

func (h *sbContext) refreshData(data jsonmodels.ContextData) {
	h.t.Helper()
	h.provider.mu.Lock()
	h.provider.next = data
	h.provider.mu.Unlock()
	if _, err := h.ctx.RefreshAsync().Get(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

func (h *sbContext) setAttribute(name string, value interface{}) {
	h.t.Helper()
	if err := h.ctx.SetAttribute(name, value); err != nil {
		h.t.Fatal(err)
	}
}

func (h *sbContext) pending() int {
	return int(h.ctx.GetPendingCount())
}

func (h *sbContext) expectPending(want int) {
	h.t.Helper()
	if got := h.pending(); got != want {
		h.t.Fatalf("pending %d, want %d: %+v", got, want, queuedExposures(h.ctx))
	}
}

// publishEvents publishes and returns every event published so far.
func (h *sbContext) publishEvents() []jsonmodels.PublishEvent {
	h.t.Helper()
	if err := h.ctx.Publish(); err != nil {
		h.t.Fatal(err)
	}
	h.publisher.mu.Lock()
	defer h.publisher.mu.Unlock()
	return append([]jsonmodels.PublishEvent(nil), h.publisher.events...)
}

// publishExposures publishes and returns the exposures of the single
// publish (none when nothing was pending).
func (h *sbContext) publishExposures() []jsonmodels.Exposure {
	h.t.Helper()
	var events = h.publishEvents()
	if len(events) == 0 {
		return []jsonmodels.Exposure{}
	}
	if len(events) != 1 {
		h.t.Fatalf("published %d events, want 1", len(events))
	}
	return append([]jsonmodels.Exposure{}, events[0].Exposures...)
}

func (h *sbContext) expectPublished(want ...jsonmodels.Exposure) {
	h.t.Helper()
	if want == nil {
		want = []jsonmodels.Exposure{}
	}
	if got := h.publishExposures(); !reflect.DeepEqual(want, got) {
		h.t.Fatalf("published exposures\n got %+v\nwant %+v", got, want)
	}
}

func (h *sbContext) peekSchedule(name string, periodMs int64, periods int) []int {
	var result = make([]int, periods)
	for i := range result {
		h.at(sbAnchor + int64(i)*periodMs)
		result[i] = h.peek(name)
	}
	return result
}

// sbDraw is the schedule draw of key in a period under the fixtures' seed.
func sbDraw(key string, period int64, armCount int) int {
	var variant, _ = SwitchbackAssign(key, period, int64(switchbackSeed(sbSeedHi, sbSeedLo)), armCount)
	return variant
}

// The cluster identity is the raw attribute value, neither hashed nor the
// unit uid.
func TestSwitchback_ClusterIdentityIsTheRawClusterValue(t *testing.T) {
	expectValue(t, sbUIDHash, string(hashUnitBytes(sbUID)))
	var h = sbCreate(t, sbData(sbDefault(3, sbHour)))
	var served = h.peekSchedule("exp_sb", sbHour, 12)

	expectValue(t, sbExpected3Arms, served)
	if reflect.DeepEqual(sbUIDHash3Arms, served) || reflect.DeepEqual(sbUID3Arms, served) {
		t.Fatal("served the unit's schedule")
	}
	var hashedKey = string(hashUnitBytes(sbClusterKey))
	var hashedDiffers = false
	for i := 0; i < 12; i++ {
		expectInt(t, sbDraw(sbClusterKey, int64(i), 3), served[i])
		hashedDiffers = hashedDiffers || sbDraw(hashedKey, int64(i), 3) != served[i]
	}
	if !hashedDiffers {
		t.Fatal("a hashed key serves the same schedule")
	}
}

func TestSwitchback_ServesTwoThreeAndTenArmPermutations(t *testing.T) {
	var h = sbCreate(t, sbData(sbExperiment(1, "exp_2", 2, sbHour), sbExperiment(2, "exp_3", 3, sbHour),
		sbExperiment(3, "exp_10", 10, sbHour)))
	expectValue(t, sbExpected2Arms, h.peekSchedule("exp_2", sbHour, 12))
	expectValue(t, sbExpected3Arms, h.peekSchedule("exp_3", sbHour, 12))
	expectValue(t, sbExpected10Arms, h.peekSchedule("exp_10", sbHour, 12))
}

func TestSwitchback_OneHourBoundaryAtExactEdgeMilliseconds(t *testing.T) {
	var h = sbCreateAt(t, sbAnchor-1, map[string]string{sbUnitType: sbUID}, sbData(sbDefault(2, sbHour)))
	expectInt(t, 0, h.at(sbAnchor-1).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+sbHour-1).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[2], h.at(sbAnchor+3*sbHour-1).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[3], h.at(sbAnchor+3*sbHour).peek("exp_sb"))
}

func TestSwitchback_TwentyFourHourBoundaryAtExactEdgeMilliseconds(t *testing.T) {
	var h = sbCreateAt(t, sbAnchor-1, map[string]string{sbUnitType: sbUID}, sbData(sbDefault(2, sbDay)))
	expectInt(t, 0, h.at(sbAnchor-1).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+sbHour).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+sbDay-1).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbDay).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[3], h.at(sbAnchor+4*sbDay-1).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[4], h.at(sbAnchor+4*sbDay).peek("exp_sb"))
}

// The anchor is served as-is, never rounded; before it the experiment is an
// ineligible control that records nothing. An anchor that is not hour
// aligned is unservable: an ineligible control in every period.
func TestSwitchback_UnalignedStartIsUnservableAndPreAnchorIsIneligible(t *testing.T) {
	var aligned = sbExperiment(1, "exp_aligned", 2, sbHour)
	aligned.SwitchbackStartAt = sbInt64(sbAnchor + sbHour)
	var unaligned = sbExperiment(2, "exp_unaligned", 2, sbHour)
	var start = sbAnchor + 1_234_567
	unaligned.SwitchbackStartAt = sbInt64(start)
	var h = sbCreate(t, sbData(aligned, unaligned))

	for _, now := range []int64{sbAnchor, start - 1, start, sbAnchor + sbHour - 1} {
		h.at(now)
		expectInt(t, 0, h.treatment("exp_aligned"))
		expectInt(t, 0, h.treatment("exp_unaligned"))
	}
	h.expectPending(0)
	for i := int64(1); i < 4; i++ {
		h.at(sbAnchor + i*sbHour)
		expectInt(t, sbExpected2Arms[i-1], h.treatment("exp_aligned"))
		expectInt(t, 0, h.treatment("exp_unaligned"))
		expectInt(t, 0, h.at(start+i*sbHour).treatment("exp_unaligned"))
	}
	h.expectPublished(
		sbExposure(1, "exp_aligned", sbExpected2Arms[0], sbAnchor+sbHour, true, true),
		sbExposure(1, "exp_aligned", sbExpected2Arms[1], sbAnchor+2*sbHour, true, true),
		sbExposure(1, "exp_aligned", sbExpected2Arms[2], sbAnchor+3*sbHour, true, true))
}

// The Collector Registry's anchor bounds: an anchor that is not hour
// aligned, negative, or at or after 10000-01-01T00:00:00Z is unservable on
// resolution and publishes nothing.
func TestSwitchback_AnchorOutsideTheRegistryBoundsIsUnservable(t *testing.T) {
	const maxAnchor int64 = 253_402_300_800_000
	const maxInt64 = int64(^uint64(0) >> 1)
	for _, startAt := range []int64{sbAnchor + 1, sbAnchor - 1, sbAnchor + 30*60_000, sbAnchor + sbHour - 1, -sbHour, -1,
		maxAnchor, maxAnchor + sbHour, maxInt64 - maxInt64%sbHour} {
		var experiment = sbDefault(2, sbHour)
		experiment.SwitchbackStartAt = sbInt64(startAt)
		var h = sbCreateAt(t, 0, map[string]string{sbUnitType: sbUID}, sbData(experiment))
		for _, now := range []int64{0, sbAnchor, sbAnchor + 2*sbHour + 1, startAt, startAt + sbHour, maxAnchor + sbDay} {
			h.at(now)
			if got := h.treatment("exp_sb"); got != 0 {
				t.Fatalf("startAt %d now %d: %d", startAt, now, got)
			}
			expectValue(t, 0, h.variable("sb.arm"))
		}
		h.expectPending(0)
		expectInt(t, 0, len(h.publishEvents()))
	}
	// In-range hour-aligned anchors, 0 and the last hour before the bound
	// included, serve.
	for _, startAt := range []int64{0, sbAnchor, maxAnchor - sbHour} {
		var experiment = sbDefault(2, sbHour)
		experiment.SwitchbackStartAt = sbInt64(startAt)
		var h = sbCreateAt(t, startAt+sbHour, map[string]string{sbUnitType: sbUID}, sbData(experiment))
		expectInt(t, sbExpected2Arms[1], h.treatment("exp_sb"))
		h.expectPending(1)
	}
}

// A schedule the Registry cannot serve, period 0 or negative and fewer than
// two arms included, never falls back to unit assignment.
func TestSwitchback_UnservableScheduleIsIneligibleAndNeverFallsBackToUnitAssignment(t *testing.T) {
	var noStart = sbDefault(2, sbHour)
	noStart.SwitchbackStartAt = nil
	var noPeriod = sbExperiment(1, "exp_sb", 2, sbHour)
	noPeriod.SwitchbackPeriodMs = nil
	var zeroPeriod = sbDefault(2, 0)
	var negativePeriod = sbDefault(2, -sbHour)
	var oneArm = sbDefault(2, sbHour)
	oneArm.Split = []float64{1}
	for _, experiment := range []jsonmodels.Experiment{noStart, noPeriod, zeroPeriod, negativePeriod, oneArm} {
		var h = sbCreate(t, sbData(experiment))
		for i := int64(0); i < 4; i++ {
			h.at(sbAnchor + i*sbHour)
			expectInt(t, 0, h.treatment("exp_sb"))
			// Variant 0's own config, like any ineligible assignment.
			expectValue(t, 0, h.variable("sb.arm"))
		}
		h.expectPending(0)
	}
}

func TestSwitchback_SamePeriodIsCachedAndNextPeriodRollsOverWithOneExposureEach(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+10).treatment("exp_sb"))
	h.expectPending(1)
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+sbHour-1).treatment("exp_sb"))
	expectInt(t, sbExpected2Arms[0], h.peek("exp_sb"))
	h.expectPending(1)

	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour).peek("exp_sb"))
	h.expectPending(1)
	expectInt(t, sbExpected2Arms[1], h.treatment("exp_sb"))
	h.expectPending(2)
	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour+30*60_000).treatment("exp_sb"))
	h.expectPending(2)

	h.expectPublished(
		sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+10, true, true),
		sbExposure(1, "exp_sb", sbExpected2Arms[1], sbAnchor+sbHour, true, true))
}

func TestSwitchback_VariableLookupsFollowThePeriodAndExposeOncePerPeriod(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(3, sbHour)))
	var keys, _ = h.ctx.GetVariableKeysAll()
	expectValue(t, map[string][]string{"sb.arm": {"exp_sb"}}, keys)
	var names, _ = h.ctx.GetExperiments()
	expectValue(t, []string{"exp_sb"}, names)

	for i := 0; i < 3; i++ {
		h.at(sbAnchor + int64(i)*sbHour)
		expectValue(t, sbExpected3Arms[i], h.peekVariable("sb.arm"))
		expectValue(t, sbExpected3Arms[i], h.variable("sb.arm"))
		expectValue(t, sbExpected3Arms[i], h.variable("sb.arm"))
		expectInt(t, sbExpected3Arms[i], h.treatment("exp_sb"))
	}
	h.expectPending(3)
}

// No unit of a linked type: the experiment does not apply and records
// nothing; unitType is never a fallback. Setting a linked unit later makes
// it apply.
func TestSwitchback_MissingClusterUnitIsUnassignedWithoutFallback(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.UnitType = "user_id"
	var h = sbCreateAt(t, sbAnchor, map[string]string{"user_id": sbUID}, sbData(experiment))
	for i := int64(0); i < 3; i++ {
		h.at(sbAnchor + i*sbHour)
		expectInt(t, 0, h.peek("exp_sb"))
		expectInt(t, 0, h.treatment("exp_sb"))
		expectValue(t, -1, h.variable("sb.arm"))
	}
	h.expectPending(0)

	if err := h.ctx.SetUnit(sbUnitType, sbUID); err != nil {
		t.Fatal(err)
	}
	expectInt(t, sbExpected2Arms[2], h.treatment("exp_sb"))
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[2], sbAnchor+2*sbHour, true, true))
}

// An override on a switchback that does not apply yet is exposed; once a
// linked unit is set, the overridden slot is resolved again under it, and
// its exposure is not repeated.
func TestSwitchback_OverrideWithoutALinkedUnitIsKeptOnceExposed(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	// Recorded under the first linked type, never the declared unitType.
	experiment.UnitType = "user_id"
	var h = sbCreateAt(t, sbAnchor+5, nil, sbData(experiment), sbOverride("exp_sb", 1))
	expectInt(t, 1, h.treatment("exp_sb"))
	h.expectPending(1)
	if err := h.ctx.SetUnit(sbUnitType, sbUID); err != nil {
		t.Fatal(err)
	}
	expectInt(t, 1, h.treatment("exp_sb"))
	h.expectPublished(jsonmodels.Exposure{Id: 1, Name: "exp_sb", Unit: sbUnitType, Variant: 1, ExposedAt: sbAnchor + 5,
		Eligible: true, Overridden: true})
}

func TestSwitchback_FullOnServesTheFullOnVariantInEveryPeriod(t *testing.T) {
	var experiment = sbDefault(3, sbHour)
	experiment.FullOnVariant = 2
	var h = sbCreate(t, sbData(experiment))
	for i := int64(0); i < 4; i++ {
		expectInt(t, 2, h.at(sbAnchor+i*sbHour).treatment("exp_sb"))
	}
	var exposures = h.publishExposures()
	expectInt(t, 4, len(exposures))
	for _, exposure := range exposures {
		if exposure.Variant != 2 || !exposure.FullOn {
			t.Fatalf("%+v", exposure)
		}
	}
}

func TestSwitchback_OverrideAndCustomAssignmentWinInEveryPeriod(t *testing.T) {
	var h = sbCreate(t, sbData(sbExperiment(1, "exp_override", 3, sbHour), sbExperiment(2, "exp_custom", 3, sbHour)),
		sbOverride("exp_override", 7), sbCustom("exp_custom", 2))
	for i := int64(0); i < 3; i++ {
		h.at(sbAnchor + i*sbHour)
		expectInt(t, 7, h.treatment("exp_override"))
		expectInt(t, 2, h.treatment("exp_custom"))
	}
	var exposures = h.publishExposures()
	expectInt(t, 6, len(exposures))
	for _, exposure := range exposures {
		if exposure.Name == "exp_override" && (!exposure.Overridden || exposure.Variant != 7) ||
			exposure.Name == "exp_custom" && (!exposure.Custom || exposure.Variant != 2) {
			t.Fatalf("%+v", exposure)
		}
	}
}

// A custom assignment set or changed within a period resolves the slot
// again, from the cluster it already read.
func TestSwitchback_CustomAssignmentChangeWithinThePeriodResolvesTheSlotAgain(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(3, sbHour)))
	h.at(sbAnchor + sbHour + 1)
	expectInt(t, sbExpected3Arms[1], h.peek("exp_sb"))
	if err := h.ctx.SetCustomAssignment("exp_sb", 1); err != nil {
		t.Fatal(err)
	}
	expectInt(t, 1, h.peek("exp_sb"))
	if err := h.ctx.SetCustomAssignment("exp_sb", 2); err != nil {
		t.Fatal(err)
	}
	expectInt(t, 2, h.treatment("exp_sb"))
	expectInt(t, 2, h.treatment("exp_sb"))
	h.expectPublished(jsonmodels.Exposure{Id: 1, Name: "exp_sb", Unit: sbUnitType, Variant: 2, ExposedAt: sbAnchor + sbHour + 1,
		Assigned: true, Eligible: true, Custom: true})
}

// Traffic eligibility uses the per-unit traffic hash, so it matches a
// unit-randomized twin with the same traffic seed and stays fixed across
// periods.
func TestSwitchback_TrafficEligibilityIsClusterSticky(t *testing.T) {
	var eligible, ineligible = 0, 0
	for u := 0; u < 24; u++ {
		var sb = sbExperiment(1, "exp_sb", 2, sbHour)
		sb.TrafficSplit = []float64{0.5, 0.5}
		var twin = sbExperiment(2, "exp_unit", 2, sbHour)
		twin.TrafficSplit = []float64{0.5, 0.5}
		twin.RandomizationType = ""
		twin.SwitchbackStartAt = nil
		var h = sbCreateAt(t, sbAnchor, map[string]string{sbUnitType: "store_" + strconv.Itoa(u)}, sbData(sb, twin))
		h.treatment("exp_unit")
		for i := int64(0); i < 4; i++ {
			h.at(sbAnchor + i*sbHour).treatment("exp_sb")
		}
		var exposures = h.publishExposures()
		expectInt(t, 5, len(exposures))
		var twinEligible = exposures[0].Eligible
		for _, exposure := range exposures[1:] {
			if exposure.Name != "exp_sb" || exposure.Eligible != twinEligible || (!twinEligible && exposure.Variant != 0) {
				t.Fatalf("%+v, twin eligible %v", exposure, twinEligible)
			}
		}
		if twinEligible {
			eligible++
		} else {
			ineligible++
		}
	}
	if eligible == 0 || ineligible == 0 {
		t.Fatalf("eligible %d, ineligible %d", eligible, ineligible)
	}
}

func TestSwitchback_UnitExperimentsAreUnaffected(t *testing.T) {
	var unit = sbDefault(2, sbHour)
	unit.RandomizationType = "unit"
	var absent = sbExperiment(2, "exp_absent", 2, sbHour)
	absent.RandomizationType = ""
	var h = sbCreate(t, sbData(unit, absent))
	var variant = h.treatment("exp_sb")
	var absentVariant = h.treatment("exp_absent")
	for i := int64(1); i < 6; i++ {
		h.at(sbAnchor + i*sbHour)
		expectInt(t, variant, h.treatment("exp_sb"))
		expectInt(t, absentVariant, h.treatment("exp_absent"))
	}
	h.expectPending(2)
	h.ctx.ContextLock_.RLock()
	defer h.ctx.ContextLock_.RUnlock()
	if h.ctx.switchbackEpochs_ != nil {
		t.Fatal("unit experiments created switchback state")
	}
}

func TestSwitchback_RefreshInvalidatesWhenGridFieldsChangeButNotForAnalysisOnlyFields(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+10).treatment("exp_sb"))
	h.expectPending(1)

	var burnsChanged = sbDefault(2, sbHour)
	burnsChanged.SwitchbackBurnInDurationMs = sbInt64(600_000)
	burnsChanged.SwitchbackBurnOutDurationMs = sbInt64(300_000)
	burnsChanged.SwitchbackCarryoverDurationMs = sbInt64(900_000)
	burnsChanged.SwitchbackEstimand = "other"
	h.refresh(burnsChanged)
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(1)

	// Period index 0 under both grids, so only the identity can invalidate.
	h.refresh(sbDefault(2, 2*sbHour))
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(2)

	var designChanged = sbDefault(2, 2*sbHour)
	designChanged.SwitchbackDesignType = "other_design"
	h.refresh(designChanged)
	h.treatment("exp_sb")
	h.expectPending(3)

	var toUnit = sbDefault(2, 2*sbHour)
	toUnit.RandomizationType = "unit"
	h.refresh(toUnit)
	h.treatment("exp_sb")
	h.expectPending(4)

	h.refresh(sbDefault(2, 2*sbHour))
	h.treatment("exp_sb")
	h.expectPending(5)

	// The cluster map (types, paths and their order) and the allow-list are
	// identity.
	var pathChanged = sbDefault(2, 2*sbHour)
	pathChanged.SwitchbackClusters = sbClusters(sbUnitType, "'store_key'")
	h.refresh(pathChanged)
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(6)

	var linkAdded = sbDefault(2, 2*sbHour)
	linkAdded.SwitchbackClusters = sbClusters(sbUnitType, "'store_key'", "device_id", "city")
	h.refresh(linkAdded)
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(7)

	var reordered = sbDefault(2, 2*sbHour)
	reordered.SwitchbackClusters = sbClusters("device_id", "city", sbUnitType, "'store_key'")
	h.refresh(reordered)
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(8)

	var valuesSet = reordered
	valuesSet.SwitchbackClusterValues = []string{sbClusterKey}
	h.refresh(valuesSet)
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(9)

	// An unrelated unitType edit is not identity: switchbacks never read it.
	var unitTypeChanged = valuesSet
	unitTypeChanged.UnitType = "anything"
	h.refresh(unitTypeChanged)
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(9)

	// Seed and split edits do not invalidate the cached slot either.
	var seedChanged = unitTypeChanged
	seedChanged.SeedHi, seedChanged.SeedLo = 99, 98
	h.refresh(seedChanged)
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(9)
}

func TestSwitchback_RefreshSettingStartAtStartsTheGrid(t *testing.T) {
	var pending = sbDefault(2, sbHour)
	pending.SwitchbackStartAt = nil
	var h = sbCreate(t, sbData(pending))
	expectInt(t, 0, h.at(sbAnchor+10).treatment("exp_sb"))
	h.refresh(sbDefault(2, sbHour))
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(1)
}

// Holdouts never apply to a switchback: a holdout that would hold the unit
// out neither suppresses it nor publishes its own exposure through it.
func TestSwitchback_HoldoutsNeverApply(t *testing.T) {
	var holdout = jsonmodels.Holdout{Id: 11, Name: "holdout_a", UnitType: sbUnitType, Iteration: 1, SeedHi: 13, SeedLo: 111,
		Split: []float64{1, 0}}
	var covered = sbDefault(2, sbHour)
	covered.HoldoutIds = []int{11}
	var data = sbData(covered)
	data.Holdouts = []jsonmodels.Holdout{holdout}
	var h = sbCreate(t, data)
	for i := int64(0); i < 3; i++ {
		h.at(sbAnchor + i*sbHour)
		expectInt(t, sbExpected2Arms[i], h.treatment("exp_sb"))
		expectValue(t, sbExpected2Arms[i], h.variable("sb.arm"))
	}
	h.expectPublished(
		sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor, true, true),
		sbExposure(1, "exp_sb", sbExpected2Arms[1], sbAnchor+sbHour, true, true),
		sbExposure(1, "exp_sb", sbExpected2Arms[2], sbAnchor+2*sbHour, true, true))
	h.ctx.ContextLock_.RLock()
	defer h.ctx.ContextLock_.RUnlock()
	if h.ctx.holdoutArms_ != nil {
		t.Fatal("a holdout arm was drawn for a switchback")
	}
}

// An override wins over a holdout that covers a switchback, and the holdout
// still never fires.
func TestSwitchback_OverrideIsReresolvedWhenTheGridChanges(t *testing.T) {
	var holdout = jsonmodels.Holdout{Id: 11, Name: "holdout_a", UnitType: sbUnitType, Iteration: 1, SeedHi: 13, SeedLo: 111,
		Split: []float64{0, 1}}
	var original = sbDefault(3, sbHour)
	original.HoldoutIds = []int{11}
	var data = sbData(original)
	data.Holdouts = []jsonmodels.Holdout{holdout}
	var h = sbCreate(t, data, sbOverride("exp_sb", 2))
	h.at(sbAnchor + 10)
	expectInt(t, 2, h.treatment("exp_sb"))
	expectValue(t, 2, h.variable("sb.arm"))
	h.expectPending(1)

	// Same index 0 under the new period length; variant 2 carries another
	// value now.
	var regridded = sbDefault(3, 2*sbHour)
	regridded.HoldoutIds = []int{11}
	regridded.Variants[2] = jsonmodels.ExperimentVariant{Name: "C", Config: `{"sb.arm":22}`}
	var refreshed = sbData(regridded)
	refreshed.Holdouts = []jsonmodels.Holdout{holdout}
	h.refreshData(refreshed)
	expectInt(t, 2, h.treatment("exp_sb"))
	expectValue(t, 22, h.variable("sb.arm"))
	h.expectPending(2)

	var override = jsonmodels.Exposure{Id: 1, Name: "exp_sb", Unit: sbUnitType, Variant: 2, ExposedAt: sbAnchor + 10,
		Eligible: true, Overridden: true}
	h.expectPublished(override, override)
}

// The Collector evaluates assignment rules first, so a matching rule assigns
// its variant before the anchor too, recorded with ruleOverride.
func TestSwitchback_AssignmentRuleOverridesTheSchedule(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.AssignmentRules = sbAssignVariant1
	for u := 0; u < 10; u++ {
		var h = sbCreateAt(t, sbAnchor-1, map[string]string{sbUnitType: "user-" + strconv.Itoa(u)}, sbData(experiment))
		expectInt(t, 1, h.treatment("exp_sb"))
		expectValue(t, 1, h.variable("sb.arm"))
		for i := int64(0); i < 4; i++ {
			expectInt(t, 1, h.at(sbAnchor+i*sbHour).treatment("exp_sb"))
		}
		var want = []jsonmodels.Exposure{sbRuleExposure("exp_sb", 1, sbAnchor-1)}
		for i := int64(0); i < 4; i++ {
			want = append(want, sbRuleExposure("exp_sb", 1, sbAnchor+i*sbHour))
		}
		h.expectPublished(want...)
	}
}

// An unservable schedule only replaces the draw: full-on and a matching rule
// still apply and are recorded.
func TestSwitchback_UnservableOnlyReplacesTheVariantDrawNotFullOnOrRules(t *testing.T) {
	var fullOn = sbExperiment(1, "sb_full_on", 2, sbHour)
	fullOn.SwitchbackStartAt = nil
	fullOn.FullOnVariant = 1
	var rules = sbExperiment(2, "sb_rules", 2, sbHour)
	rules.SwitchbackPeriodMs = sbInt64(0)
	rules.AssignmentRules = sbAssignVariant1
	var unalignedRules = sbExperiment(3, "sb_unaligned_rules", 2, sbHour)
	unalignedRules.SwitchbackStartAt = sbInt64(sbAnchor + 1)
	unalignedRules.AssignmentRules = sbAssignVariant1
	var h = sbCreateAt(t, sbAnchor, map[string]string{sbUnitType: "15"}, sbData(fullOn, rules, unalignedRules))
	expectInt(t, 1, h.treatment("sb_full_on"))
	expectInt(t, 1, h.treatment("sb_rules"))
	expectInt(t, 1, h.treatment("sb_unaligned_rules"))
	h.expectPublished(
		jsonmodels.Exposure{Id: 1, Name: "sb_full_on", Unit: sbUnitType, Variant: 1, ExposedAt: sbAnchor, Assigned: true, Eligible: true, FullOn: true},
		jsonmodels.Exposure{Id: 2, Name: "sb_rules", Unit: sbUnitType, Variant: 1, ExposedAt: sbAnchor, Eligible: true, RuleOverride: true},
		jsonmodels.Exposure{Id: 3, Name: "sb_unaligned_rules", Unit: sbUnitType, Variant: 1, ExposedAt: sbAnchor, Eligible: true, RuleOverride: true})
}

// Rules, the audience and full-on are evaluated only for an eligible
// cluster: an ineligible one stays a silent control.
func TestSwitchback_RulesAudienceAndFullOnDoNotApplyToAnIneligibleCluster(t *testing.T) {
	var rule = sbExperiment(1, "exp_rule", 2, sbHour)
	rule.AssignmentRules = sbAssignVariant1
	rule.SwitchbackClusterValues = []string{"other-store"}
	var strict = sbExperiment(2, "exp_strict", 2, sbHour)
	strict.Audience, strict.AudienceStrict = sbStrictGoldAudience, true
	strict.SwitchbackClusters = sbClusters(sbUnitType, "unset_zone")
	var fullOn = sbExperiment(3, "exp_full_on", 3, sbHour)
	fullOn.FullOnVariant = 2
	fullOn.SwitchbackClusterValues = []string{"other-store"}
	var h = sbCreate(t, sbData(rule, strict, fullOn))
	expectInt(t, 0, h.treatment("exp_rule"))
	expectInt(t, 0, h.treatment("exp_strict"))
	expectInt(t, 0, h.treatment("exp_full_on"))
	expectValue(t, 0, h.variable("sb.arm"))
	h.expectPending(0)
}

// A split rule randomizes over the serving unit, not the payload's unitType.
func TestSwitchback_SplitRuleRandomizesOverTheServingUnit(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.UnitType = "user_id"
	experiment.AssignmentRules = `{"rules":[{"type":"split","environments":[],"percentages":"50/50"}]}`
	var ones = 0
	for u := 0; u < 32; u++ {
		var uid = "store-" + strconv.Itoa(u)
		var buf [12]int8
		var want = NewVariantAssigner(HashUnit(uid)).Assign([]float64{0.5, 0.5}, sbSeedHi, sbSeedLo, buf[:])
		var h = sbCreateAt(t, sbAnchor, map[string]string{sbUnitType: uid, "user_id": "fixed-user"}, sbData(experiment))
		expectInt(t, want, h.treatment("exp_sb"))
		h.expectPublished(sbRuleExposure("exp_sb", want, sbAnchor))
		ones += want
	}
	if ones == 0 || ones == 32 {
		t.Fatalf("%d of 32 units in variant 1", ones)
	}
}

func TestSwitchback_AudienceStrictMismatchIsRecordedAsControl(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.Audience, experiment.AudienceStrict = sbStrictGoldAudience, true
	var h = sbCreate(t, sbData(experiment))
	expectInt(t, 0, h.at(sbAnchor+sbHour).treatment("exp_sb"))
	h.expectPublished(sbStrictMismatchExposure(sbAnchor + sbHour))
}

// The audience check comes before the period check, so a strict mismatch is
// recorded before the anchor and with an unservable schedule.
func TestSwitchback_AudienceStrictMismatchBeforeTheAnchorOrUnservableIsRecorded(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.Audience, experiment.AudienceStrict = sbStrictGoldAudience, true
	var h = sbCreateAt(t, sbAnchor-1, map[string]string{sbUnitType: sbUID}, sbData(experiment))
	expectInt(t, 0, h.treatment("exp_sb"))
	h.expectPublished(sbStrictMismatchExposure(sbAnchor - 1))

	experiment.SwitchbackStartAt = nil
	h = sbCreateAt(t, sbAnchor+sbHour+3, map[string]string{sbUnitType: sbUID}, sbData(experiment))
	expectInt(t, 0, h.treatment("exp_sb"))
	h.expectPublished(sbStrictMismatchExposure(sbAnchor + sbHour + 3))
}

// A missing or unreadable cluster map takes the legacy path, which returns
// an ineligible control before the audience is evaluated.
func TestSwitchback_AudienceStrictMismatchWithAMissingOrUnreadableClusterMapRecordsNothing(t *testing.T) {
	for _, clusters := range []jsonmodels.SwitchbackClusters{nil, {}, sbClusters(sbUnitType, "'geo")} {
		var experiment = sbDefault(2, sbHour)
		experiment.Audience, experiment.AudienceStrict = sbStrictGoldAudience, true
		experiment.SwitchbackClusters = clusters
		var h = sbCreateAt(t, sbAnchor+sbHour+3, map[string]string{sbUnitType: sbUID}, sbData(experiment))
		expectInt(t, 0, h.treatment("exp_sb"))
		h.expectPending(0)
	}
}

// A strict-mismatched switchback is not assigned, so on a shared variable
// key it gives way to the next experiment, as a unit experiment does.
// GetVariableValue exposes every experiment it visits (the JS rule this SDK
// follows for unit experiments; the Java SDK exposes only the winner), so
// the switchback's strict mismatch is recorded too, exactly as GetTreatment
// records it.
func TestSwitchback_StrictMismatchedSwitchbackGivesWayOnASharedVariableKey(t *testing.T) {
	var sb = sbDefault(2, sbHour)
	sb.Audience, sb.AudienceStrict = sbStrictGoldAudience, true
	var unit = sbUnit(sbExperiment(2, "exp_unit", 2, sbHour))
	unit.Variants[0].Config = `{"sb.arm":10}`
	unit.Variants[1].Config = `{"sb.arm":11}`
	unit.FullOnVariant = 1
	var h = sbCreateAt(t, sbAnchor+sbHour+3, map[string]string{sbUnitType: sbUID}, sbData(sb, unit))
	var keys, _ = h.ctx.GetVariableKeysAll()
	expectValue(t, map[string][]string{"sb.arm": {"exp_sb", "exp_unit"}}, keys)

	expectValue(t, 11, h.peekVariable("sb.arm"))
	h.expectPending(0)
	expectValue(t, 11, h.variable("sb.arm"))
	h.expectPublished(sbStrictMismatchExposure(sbAnchor+sbHour+3),
		jsonmodels.Exposure{Id: 2, Name: "exp_unit", Unit: sbUnitType, Variant: 1, ExposedAt: sbAnchor + sbHour + 3,
			Assigned: true, Eligible: true, FullOn: true})
}

// Alone on the key, a strict-mismatched switchback gives the default value;
// GetVariableValue records the mismatch it visited, as for a unit
// experiment.
func TestSwitchback_StrictMismatchedSwitchbackAloneOnAVariableKeyReturnsTheDefault(t *testing.T) {
	var sb = sbDefault(2, sbHour)
	sb.Audience, sb.AudienceStrict = sbStrictGoldAudience, true
	var h = sbCreateAt(t, sbAnchor+sbHour+3, map[string]string{sbUnitType: sbUID}, sbData(sb))
	expectValue(t, -1, h.peekVariable("sb.arm"))
	h.expectPending(0)
	expectValue(t, -1, h.variable("sb.arm"))
	h.expectPublished(sbStrictMismatchExposure(sbAnchor + sbHour + 3))
}

// The SWITCHBACK.md "Exposure flags" table: every branch an eligible-cluster
// switchback shares with a unit experiment publishes the same flags; only the
// unit and the variant can differ.
func TestSwitchback_ExposureFlagsMatchAUnitExperimentForEveryBranch(t *testing.T) {
	type branch struct {
		name      string
		configure func(*jsonmodels.Experiment)
		options   []sbOption
		// assigned, eligible, overridden, fullOn, custom, audienceMismatch, ruleOverride
		flags   [7]bool
		variant int
		draws   bool
	}
	var strict = func(e *jsonmodels.Experiment) { e.Audience, e.AudienceStrict = sbStrictGoldAudience, true }
	var rule = func(e *jsonmodels.Experiment) { e.AssignmentRules = sbAssignVariant2 }
	var override = []sbOption{sbOverride("exp_parity", 2)}
	var none = func(*jsonmodels.Experiment) {}
	// Period 2's 3-arm draw is 1; override, rule, full-on and custom force 2.
	expectInt(t, 1, sbExpected3Arms[2])
	for _, b := range []branch{
		{"override", none, override, [7]bool{false, true, true, false, false, false, false}, 2, false},
		{"override over strict mismatch", strict, override, [7]bool{false, true, true, false, false, false, false}, 2, false},
		{"rule", rule, nil, [7]bool{false, true, false, false, false, false, true}, 2, false},
		{"rule over strict mismatch", func(e *jsonmodels.Experiment) { strict(e); rule(e) }, nil, [7]bool{false, true, false, false, false, false, true}, 2, false},
		{"strict mismatch", strict, nil, [7]bool{false, true, false, false, false, true, false}, 0, false},
		{"strict mismatch over full-on", func(e *jsonmodels.Experiment) { strict(e); e.FullOnVariant = 2 }, nil, [7]bool{false, true, false, false, false, true, false}, 0, false},
		{"strict mismatch over traffic miss", func(e *jsonmodels.Experiment) { strict(e); e.TrafficSplit = []float64{1, 0} }, nil, [7]bool{false, true, false, false, false, true, false}, 0, false},
		{"non-strict mismatch", func(e *jsonmodels.Experiment) { e.Audience = sbStrictGoldAudience }, nil, [7]bool{true, true, false, false, false, true, false}, sbExpected3Arms[2], true},
		{"full-on", func(e *jsonmodels.Experiment) { e.FullOnVariant = 2 }, nil, [7]bool{true, true, false, true, false, false, false}, 2, false},
		{"traffic miss", func(e *jsonmodels.Experiment) { e.TrafficSplit = []float64{1, 0} }, nil, [7]bool{true, false, false, false, false, false, false}, 0, false},
		{"custom", none, []sbOption{sbCustom("exp_parity", 2)}, [7]bool{true, true, false, false, true, false, false}, 2, false},
		{"draw", none, nil, [7]bool{true, true, false, false, false, false, false}, sbExpected3Arms[2], true},
	} {
		var expose = func(experiment jsonmodels.Experiment) jsonmodels.Exposure {
			b.configure(&experiment)
			var h = sbCreateAt(t, sbAnchor+2*sbHour+3, map[string]string{sbUnitType: sbUID}, sbData(experiment), b.options...)
			h.treatment("exp_parity")
			var exposures = h.publishExposures()
			if len(exposures) != 1 {
				t.Fatalf("%s: %d exposures", b.name, len(exposures))
			}
			return exposures[0]
		}
		var unit = expose(sbUnit(sbExperiment(1, "exp_parity", 3, sbHour)))
		var switchback = expose(sbExperiment(1, "exp_parity", 3, sbHour))
		var flags = func(e jsonmodels.Exposure) [7]bool {
			return [7]bool{e.Assigned, e.Eligible, e.Overridden, e.FullOn, e.Custom, e.AudienceMismatch, e.RuleOverride}
		}
		if flags(unit) != b.flags || flags(switchback) != b.flags {
			t.Errorf("%s: unit %v, switchback %v, want %v", b.name, flags(unit), flags(switchback), b.flags)
		}
		if switchback.Variant != b.variant || (!b.draws && unit.Variant != b.variant) {
			t.Errorf("%s: unit variant %d, switchback variant %d, want %d", b.name, unit.Variant, switchback.Variant, b.variant)
		}
		if unit.Id != switchback.Id || unit.Name != switchback.Name || unit.ExposedAt != switchback.ExposedAt || unit.Unit != switchback.Unit {
			t.Errorf("%s: unit %+v, switchback %+v", b.name, unit, switchback)
		}
	}
}

// A traffic miss on an eligible cluster is recorded under the serving unit
// with eligible=false; an ineligible cluster records nothing.
func TestSwitchback_TrafficMissOnAnEligibleClusterPublishesAnIneligibleExposure(t *testing.T) {
	var miss = sbExperiment(1, "exp_miss", 2, sbHour)
	miss.TrafficSplit = []float64{1, 0}
	var ineligible = sbExperiment(2, "exp_cluster_ineligible", 2, sbHour)
	ineligible.TrafficSplit = []float64{1, 0}
	ineligible.SwitchbackClusterValues = []string{"other-store"}
	var h = sbCreate(t, sbData(miss, ineligible))
	h.at(sbAnchor + 2*sbHour + 3)
	expectInt(t, 0, h.treatment("exp_miss"))
	expectInt(t, 0, h.treatment("exp_cluster_ineligible"))
	h.expectPublished(sbExposure(1, "exp_miss", 0, sbAnchor+2*sbHour+3, true, false))
}

func TestSwitchback_CustomFieldValuesAndUnitLookupsWork(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	expectValue(t, sbUID, h.ctx.GetUnit(sbUnitType))
	var keys, _ = h.ctx.GetCustomFieldValueKeys()
	expectInt(t, 0, len(keys))
	expectValue(t, nil, h.ctx.GetCustomFieldValue("exp_sb", "missing"))
}

// Each experiment keeps entries for at most its two most recent periods.
func TestSwitchback_PerPeriodCacheIsBounded(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	for i := int64(0); i < 50; i++ {
		h.at(sbAnchor + i*sbHour).treatment("exp_sb")
	}
	h.ctx.ContextLock_.RLock()
	expectInt(t, 2, len(h.ctx.assignmentCache_))
	expectValue(t, []int64{48, 49}, h.ctx.switchbackEpochs_["exp_sb"].periods)
	h.ctx.ContextLock_.RUnlock()
	h.expectPending(50)
}

// unitType on a switchback payload is never used for assignment.
func TestSwitchback_UnitTypeIsNotUsedForSwitchbackAssignment(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.UnitType = "user_id"
	var h = sbCreateAt(t, sbAnchor, map[string]string{sbUnitType: sbUID, "user_id": "u1"}, sbData(experiment))
	for i := int64(0); i < 3; i++ {
		expectInt(t, sbExpected2Arms[i], h.at(sbAnchor+i*sbHour).treatment("exp_sb"))
	}
	var exposures = h.publishExposures()
	expectInt(t, 3, len(exposures))
	for _, exposure := range exposures {
		expectValue(t, sbUnitType, exposure.Unit)
	}
}

func sbSerialized(t *testing.T, event jsonmodels.PublishEvent) string {
	t.Helper()
	var bytes, err = DefaultContextEventSerializer{}.Serialize(event)
	if err != nil {
		t.Fatal(err)
	}
	return string(bytes)
}

func sbExposureJSON(id int, name string, variant int, exposedAt int64, flags string) string {
	return fmt.Sprintf(`{"id":%d,"name":"%s","unit":"store_id","variant":%d,"exposedAt":%d,%s}`, id, name, variant, exposedAt, flags)
}

func sbPublishJSON(publishedAt int64, exposures ...string) string {
	return `{"hashed":true,"units":[{"type":"store_id","uid":"` + sbUIDHash + `"}],"publishedAt":` + strconv.FormatInt(publishedAt, 10) +
		`,"exposures":[` + strings.Join(exposures, ",") + `],"goals":[],"attributes":[{"name":"` + sbClusterPath + `","value":"` + sbClusterKey +
		`","setAt":` + strconv.FormatInt(sbAnchor, 10) + `}]}`
}

const sbAssignedFlags = `"assigned":true,"eligible":true,"overridden":false,"fullOn":false,"custom":false,"audienceMismatch":false,"ruleOverride":false`

// The publish payload carries no switchback fields. The cluster attribute is
// published timestamped, as before: the Collector resolves the cluster again
// at exposedAt from it.
func TestSwitchback_PublishPayloadHasNoNewFields(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 5).treatment("exp_sb")
	h.at(sbAnchor + sbHour + 7).treatment("exp_sb")
	h.at(sbAnchor + sbHour + 9)
	var events = h.publishEvents()
	expectInt(t, 1, len(events))
	expectValue(t, sbPublishJSON(sbAnchor+sbHour+9,
		sbExposureJSON(1, "exp_sb", sbExpected2Arms[0], sbAnchor+5, sbAssignedFlags),
		sbExposureJSON(1, "exp_sb", sbExpected2Arms[1], sbAnchor+sbHour+7, sbAssignedFlags)), sbSerialized(t, events[0]))
}

// Exact wire form of a strict mismatch, a rule win and a traffic miss.
func TestSwitchback_StrictMismatchRuleWonAndTrafficMissExposuresSerializeExactly(t *testing.T) {
	var strict = sbExperiment(1, "exp_strict", 2, sbHour)
	strict.Audience, strict.AudienceStrict = sbStrictGoldAudience, true
	var rule = sbExperiment(2, "exp_rule", 2, sbHour)
	rule.AssignmentRules = sbAssignVariant1
	var miss = sbExperiment(3, "exp_miss", 2, sbHour)
	miss.TrafficSplit = []float64{1, 0}
	var h = sbCreate(t, sbData(strict, rule, miss))
	// Period 1 draws 1, so the 0s below are not a draw of arm 0.
	expectInt(t, 1, sbExpected2Arms[1])
	h.at(sbAnchor + sbHour + 5)
	h.treatment("exp_strict")
	h.treatment("exp_rule")
	h.treatment("exp_miss")
	h.at(sbAnchor + sbHour + 9)
	var events = h.publishEvents()
	expectValue(t, sbPublishJSON(sbAnchor+sbHour+9,
		sbExposureJSON(1, "exp_strict", 0, sbAnchor+sbHour+5,
			`"assigned":false,"eligible":true,"overridden":false,"fullOn":false,"custom":false,"audienceMismatch":true,"ruleOverride":false`),
		sbExposureJSON(2, "exp_rule", 1, sbAnchor+sbHour+5,
			`"assigned":false,"eligible":true,"overridden":false,"fullOn":false,"custom":false,"audienceMismatch":false,"ruleOverride":true`),
		sbExposureJSON(3, "exp_miss", 0, sbAnchor+sbHour+5,
			`"assigned":true,"eligible":false,"overridden":false,"fullOn":false,"custom":false,"audienceMismatch":false,"ruleOverride":false`)),
		sbSerialized(t, events[0]))
}

func TestSwitchback_PeriodsPublishedAcrossFlushesStayOnePerPeriod(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor).treatment("exp_sb")
	if err := h.ctx.Publish(); err != nil {
		t.Fatal(err)
	}
	h.at(sbAnchor + 1).treatment("exp_sb")
	h.expectPending(0)
	h.at(sbAnchor + sbHour).treatment("exp_sb")
	var variants []int
	for _, event := range h.publishEvents() {
		for _, exposure := range event.Exposures {
			variants = append(variants, exposure.Variant)
		}
	}
	expectValue(t, []int{sbExpected2Arms[0], sbExpected2Arms[1]}, variants)
}

// The clock crosses into period 1 between the resolution and any later
// read; the exposure still carries the reading that selected period 0.
func TestSwitchback_TreatmentExposureIsStampedWithTheResolutionInstant(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + sbHour)
	h.clock.setNextRead(sbAnchor + sbHour - 1)
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+sbHour-1, true, true))
}

func TestSwitchback_VariableExposureIsStampedWithTheResolutionInstant(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + sbHour)
	h.clock.setNextRead(sbAnchor + sbHour - 1)
	expectValue(t, sbExpected2Arms[0], h.variable("sb.arm"))
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+sbHour-1, true, true))
}

// A peek caches the period's assignment without exposing it; the later
// GetTreatment exposes at its own reading, not the peek's.
func TestSwitchback_ExposureAfterPeekUsesTheExposingCallsInstant(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+10).peek("exp_sb"))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+20*60_000).treatment("exp_sb"))
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+20*60_000, true, true))
}

// QueueExposure stamps a switchback assignment with the current reading
// while it is in the assignment's period, and with the resolving reading
// once the period is over.
func TestSwitchback_QueueExposureStampsWithinTheAssignmentsPeriod(t *testing.T) {
	var h = sbCreate(t, sbData(sbExperiment(1, "exp_a", 2, sbHour), sbExperiment(2, "exp_b", 2, sbHour)))
	var a = h.ctx.GetAssignment("exp_a")
	h.at(sbAnchor + 10)
	var b = h.ctx.GetAssignment("exp_b")
	h.at(sbAnchor + 20)
	h.ctx.QueueExposure(a)
	h.at(sbAnchor + sbHour + 5)
	h.ctx.QueueExposure(b)
	h.expectPublished(sbExposure(1, "exp_a", sbExpected2Arms[0], sbAnchor+20, true, true),
		sbExposure(2, "exp_b", sbExpected2Arms[0], sbAnchor+10, true, true))
}

// Unit experiments keep stamping at enqueue time.
func TestSwitchback_UnitExposureTimingIsUnchanged(t *testing.T) {
	var unit = sbDefault(2, sbHour)
	unit.RandomizationType = "unit"
	var h = sbCreate(t, sbData(unit))
	h.at(sbAnchor + sbHour)
	h.clock.setNextRead(sbAnchor + sbHour - 1)
	var variant = h.treatment("exp_sb")
	h.expectPublished(sbExposure(1, "exp_sb", variant, sbAnchor+sbHour-1, true, true))
}

// A grid change that lowers the current index below the retained ones must
// not evict the entry it just installed.
func TestSwitchback_GridRefreshToALowerIndexExposesOnce(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 99*sbHour).treatment("exp_sb")
	h.at(sbAnchor + 100*sbHour).treatment("exp_sb")
	h.expectPending(2)
	var restarted = sbDefault(2, sbHour)
	restarted.SwitchbackStartAt = sbInt64(sbAnchor + 100*sbHour)
	h.refresh(restarted)
	for i := 0; i < 3; i++ {
		expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	}
	h.expectPending(3)
	// The new epoch dropped the old one's slots together.
	h.ctx.ContextLock_.RLock()
	defer h.ctx.ContextLock_.RUnlock()
	expectInt(t, 1, len(h.ctx.assignmentCache_))
	expectValue(t, []int64{0}, h.ctx.switchbackEpochs_["exp_sb"].periods)
}

func TestSwitchback_PeriodLengthRefreshToALowerIndexExposesOnce(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 100*sbHour).treatment("exp_sb")
	h.at(sbAnchor + 101*sbHour).treatment("exp_sb")
	h.refresh(sbDefault(2, 200*sbHour))
	h.treatment("exp_sb")
	h.treatment("exp_sb")
	h.expectPending(3)
}

// Rewinding the clock across retained periods exposes each re-entered
// evicted period again at most once, never once per call.
func TestSwitchback_ClockRewindReexposesAnEvictedPeriodAtMostOnce(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	for i := int64(0); i < 3; i++ {
		h.at(sbAnchor + i*sbHour).treatment("exp_sb")
	}
	h.expectPending(3)
	h.at(sbAnchor)
	for i := 0; i < 3; i++ {
		expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	}
	h.expectPending(4)
	// Retained {0, 2}: period 2 is still deduplicated, period 1 was evicted.
	h.at(sbAnchor + 2*sbHour).treatment("exp_sb")
	h.expectPending(4)
	h.at(sbAnchor + sbHour)
	for i := 0; i < 3; i++ {
		expectInt(t, sbExpected2Arms[1], h.treatment("exp_sb"))
	}
	h.expectPending(5)
	var exposures = h.publishExposures()
	expectValue(t, sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor, true, true), exposures[3])
	expectValue(t, sbExposure(1, "exp_sb", sbExpected2Arms[1], sbAnchor+sbHour, true, true), exposures[4])
}

// Each identity transition is a new epoch: returning to an identical grid
// resolves again instead of resurrecting the slot from before.
func TestSwitchback_SwitchbackToUnitToIdenticalSwitchbackReresolves(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	expectInt(t, sbExpected2Arms[0], h.at(sbAnchor+10).treatment("exp_sb"))
	var toUnit = sbDefault(2, sbHour)
	toUnit.RandomizationType = "unit"
	h.refresh(toUnit)
	h.treatment("exp_sb")
	h.expectPending(2)
	h.refresh(sbDefault(2, sbHour))
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(3)
}

func TestSwitchback_RemovedAndRestoredSwitchbackReresolves(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 10).treatment("exp_sb")
	h.refresh()
	expectInt(t, 0, h.treatment("exp_sb"))
	h.expectPending(2)
	h.refresh(sbDefault(2, sbHour))
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(3)
}

// The slot without a period shares the lifecycle of the period slots.
func TestSwitchback_PreAnchorSlotIsDroppedWhenTheGridChanges(t *testing.T) {
	var gridA = sbDefault(2, sbHour)
	gridA.SwitchbackStartAt = sbInt64(sbAnchor + 10*sbHour)
	var h = sbCreate(t, sbData(gridA))
	expectInt(t, 0, h.at(sbAnchor+10).treatment("exp_sb"))
	h.expectPending(0)
	h.refresh(sbDefault(2, sbHour))
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(1)
	h.refresh(gridA)
	expectInt(t, 0, h.treatment("exp_sb"))
	expectValue(t, 0, h.variable("sb.arm"))
	h.expectPending(1)
}

// Epochs are reconciled lazily; an intermediate state that is never looked
// up still ends the epoch.
func TestSwitchback_TransitionsWithoutAnInterveningLookupStillReresolve(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 10).treatment("exp_sb")
	h.expectPending(1)
	var toUnit = sbDefault(2, sbHour)
	toUnit.RandomizationType = "unit"
	h.refresh(toUnit)
	h.refresh(sbDefault(2, sbHour))
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(2)

	h.refresh()
	h.refresh(sbDefault(2, sbHour))
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(3)

	var regridded = sbDefault(2, sbHour)
	regridded.SwitchbackDesignType = "other"
	h.refresh(regridded)
	h.refresh(sbDefault(2, sbHour))
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(4)

	// An unchanged identity keeps the epoch.
	h.refresh(sbDefault(2, sbHour))
	h.treatment("exp_sb")
	h.expectPending(4)
}

// A legacy switchback (missing or unreadable cluster map) serves under its
// declared unitType, so a unitType change is identity for it.
func TestSwitchback_LegacySwitchbackUnitTypeChangeIsIdentity(t *testing.T) {
	var legacy = sbDefault(2, sbHour)
	legacy.SwitchbackClusters = nil
	var h = sbCreateAt(t, sbAnchor+10, map[string]string{sbUnitType: sbUID, "user_id": "u1"}, sbData(legacy))
	expectInt(t, 0, h.treatment("exp_sb"))
	expectValue(t, 0, h.variable("sb.arm"))
	expectValue(t, sbUnitType, h.ctx.GetAssignment("exp_sb").UnitType)

	var other = legacy
	other.UnitType = "other"
	h.refresh(other)
	expectInt(t, 0, h.treatment("exp_sb"))
	expectValue(t, -1, h.variable("sb.arm"))

	var user = legacy
	user.UnitType = "user_id"
	h.refresh(user)
	expectInt(t, 0, h.treatment("exp_sb"))
	expectValue(t, 0, h.variable("sb.arm"))
	var assignment = h.ctx.GetAssignment("exp_sb")
	if assignment.UnitType != "user_id" || assignment.Eligible {
		t.Fatalf("%+v", *assignment)
	}
	h.expectPending(0)
}

// An ineligible or absent switchback slot never consulted its rules, so
// neither a set attribute nor a rules refresh changes its result.
func TestSwitchback_RulesAreNotReevaluatedForAnIneligibleOrAbsentSlot(t *testing.T) {
	var ineligible = sbExperiment(1, "exp_ineligible", 2, sbHour)
	ineligible.AssignmentRules = sbAssignVariant1
	ineligible.SwitchbackClusterValues = []string{"other-store"}
	var absent = sbExperiment(2, "exp_absent", 2, sbHour)
	absent.AssignmentRules = sbAssignVariant1
	absent.SwitchbackClusters = sbClusters("device_id", sbClusterPath)
	var h = sbCreateAt(t, sbAnchor+10, map[string]string{sbUnitType: sbUID}, sbData(ineligible, absent))
	expectInt(t, 0, h.treatment("exp_ineligible"))
	expectInt(t, 0, h.treatment("exp_absent"))
	var before = h.ctx.GetAssignment("exp_ineligible")

	h.setAttribute("x", 1)
	var changed = `{"rules":[{"type":"assign","environments":[],"variant":1,"name":"changed"}]}`
	ineligible.AssignmentRules, absent.AssignmentRules = changed, changed
	h.refresh(ineligible, absent)
	expectInt(t, 0, h.treatment("exp_ineligible"))
	expectInt(t, 0, h.treatment("exp_absent"))
	if h.ctx.GetAssignment("exp_ineligible") != before {
		t.Fatal("the ineligible slot was resolved again")
	}
	h.expectPending(0)
}

// A rule-won switchback resolved again in the same period keeps its variant
// and is exposed once.
func TestSwitchback_InPeriodRuleRefreshKeepsTheVariantAndExposesOnce(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.AssignmentRules = sbAssignVariant1
	var h = sbCreate(t, sbData(experiment))
	expectInt(t, 1, h.at(sbAnchor+5).treatment("exp_sb"))
	h.setAttribute("tier", "x")
	experiment.AssignmentRules = `{"rules":[{"type":"assign","environments":[],"variant":1,"conditions":{"and":[{"eq":[{"var":"tier"},{"value":"x"}]}]}}]}`
	h.refresh(experiment)
	expectInt(t, 1, h.at(sbAnchor+6).treatment("exp_sb"))
	h.expectPublished(sbRuleExposure("exp_sb", 1, sbAnchor+5))
}

// Before the anchor nothing is recorded, a traffic miss included, because
// the period check precedes traffic. From the anchor the miss is recorded.
func TestSwitchback_PreAnchorTrafficMissIsSilent(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.TrafficSplit = []float64{1, 0}
	var h = sbCreateAt(t, sbAnchor-1, map[string]string{sbUnitType: sbUID}, sbData(experiment))
	expectInt(t, 0, h.treatment("exp_sb"))
	h.expectPending(0)
	expectInt(t, 0, h.at(sbAnchor).treatment("exp_sb"))
	h.expectPublished(sbExposure(1, "exp_sb", 0, sbAnchor, true, false))
}

// An override on a switchback without a serving unit is exposed under the
// declared unitType when the cluster map is missing or unreadable, and
// under the first linked type when it is readable.
func TestSwitchback_OverrideWithoutAServingUnitUsesTheLegacyOrFirstLinkedUnitType(t *testing.T) {
	var missing = sbExperiment(1, "exp_missing_map", 2, sbHour)
	missing.UnitType = "user_id"
	missing.SwitchbackClusters = nil
	var unreadable = sbExperiment(2, "exp_unreadable_map", 2, sbHour)
	unreadable.UnitType = "user_id"
	unreadable.SwitchbackClusters = sbClusters(sbUnitType, "'unterminated")
	var noValue = sbExperiment(3, "exp_no_value", 2, sbHour)
	noValue.UnitType = "user_id"
	noValue.SwitchbackClusters = sbClusters("device_id", "unset_zone", sbUnitType, "unset_zone")
	var h = sbCreateAt(t, sbAnchor+5, map[string]string{sbUnitType: sbUID}, sbData(missing, unreadable, noValue),
		sbOverride("exp_missing_map", 1), sbOverride("exp_unreadable_map", 1), sbOverride("exp_no_value", 1))
	expectInt(t, 1, h.treatment("exp_missing_map"))
	expectInt(t, 1, h.treatment("exp_unreadable_map"))
	expectInt(t, 1, h.treatment("exp_no_value"))
	var overridden = func(id int, name string, unit string) jsonmodels.Exposure {
		return jsonmodels.Exposure{Id: id, Name: name, Unit: unit, Variant: 1, ExposedAt: sbAnchor + 5, Eligible: true, Overridden: true}
	}
	h.expectPublished(overridden(1, "exp_missing_map", "user_id"), overridden(2, "exp_unreadable_map", "user_id"),
		overridden(3, "exp_no_value", sbUnitType))
}

// Two readers resolving the same period at once, while the cluster
// attribute changes, expose once and keep the key read first.
func TestSwitchback_ConcurrentResolutionOfTheSamePeriodExposesOnce(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(4))
	for round := 0; round < 50; round++ {
		var experiment = sbDefault(2, sbHour)
		experiment.Audience = `{"filter":[{"value":true}]}`
		experiment.SwitchbackClusters = sbClusters(sbUnitType, "city")
		var h = sbCreateAt(t, sbAnchor+sbHour+10, map[string]string{sbUnitType: sbUID}, sbData(experiment))
		h.setAttribute("city", "Lisboa")
		var want = sbDraw("Lisboa", 1, 2)
		expectInt(t, want, h.peek("exp_sb"))
		h.setAttribute("country", "PT")
		h.setAttribute("city", "Faro")
		if sbDraw("Faro", 1, 2) == want {
			t.Fatal("fixture: Faro draws Lisboa's variant")
		}

		var variants [3]int
		var stop atomic.Bool
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				variants[i], _ = h.ctx.GetTreatment("exp_sb")
			}(i)
		}
		var writer sync.WaitGroup
		writer.Add(1)
		go func() {
			defer writer.Done()
			for !stop.Load() {
				_ = h.ctx.SetAttribute("city", "Faro")
			}
		}()
		wg.Wait()
		stop.Store(true)
		writer.Wait()
		expectValue(t, [3]int{want, want, want}, variants)
		h.expectPublished(sbExposure(1, "exp_sb", want, sbAnchor+sbHour+10, true, true))
	}
}

// json.Marshal of the model keeps the cluster order and the switchback
// fields round-trip.
func TestSwitchback_ExperimentJSONRoundTripKeepsTheClusterOrder(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.SwitchbackClusters = sbClusters("z_type", "a", "a_type", "b", "m_type", "c")
	experiment.SwitchbackClusterValues = []string{"x"}
	var encoded, err = json.Marshal(experiment)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"switchbackClusters":{"z_type":"a","a_type":"b","m_type":"c"}`) {
		t.Fatalf("%s", encoded)
	}
	var decoded jsonmodels.Experiment
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	expectValue(t, experiment, decoded)
}

// Carryover and burn windows are analysis-only: with every window set, the
// schedule, the period boundaries and one exposure per period, inside the
// windows too, are those of the same experiment without them.
func TestSwitchback_BurnAndCarryoverWindowsDoNotChangeServing(t *testing.T) {
	var windowed = sbDefault(3, sbHour)
	windowed.SwitchbackCarryoverDurationMs = sbInt64(20 * 60_000)
	windowed.SwitchbackBurnInDurationMs = sbInt64(10 * 60_000)
	windowed.SwitchbackBurnOutDurationMs = sbInt64(15 * 60_000)
	windowed.SwitchbackEstimand = "other"
	var h = sbCreate(t, sbData(windowed))
	var want []jsonmodels.Exposure
	for i := int64(0); i < 12; i++ {
		// The first minute, inside burn-in and carryover, and the last
		// minute, inside burn-out.
		for _, offset := range []int64{0, 60_000, sbHour - 60_000, sbHour - 1} {
			expectInt(t, sbExpected3Arms[i], h.at(sbAnchor+i*sbHour+offset).treatment("exp_sb"))
		}
		want = append(want, sbExposure(1, "exp_sb", sbExpected3Arms[i], sbAnchor+i*sbHour, true, true))
	}
	h.expectPublished(want...)

	// Each period first resolved inside its burn-out window, then inside
	// burn-in and carryover.
	for _, offset := range []int64{sbHour - 60_000, 60_000} {
		var late = sbCreate(t, sbData(windowed))
		want = nil
		for i := int64(0); i < 12; i++ {
			expectInt(t, sbExpected3Arms[i], late.at(sbAnchor+i*sbHour+offset).treatment("exp_sb"))
			want = append(want, sbExposure(1, "exp_sb", sbExpected3Arms[i], sbAnchor+i*sbHour+offset, true, true))
		}
		late.expectPublished(want...)
	}
}

// A split-rule switchback revalidates its rule against the serving unit, not
// the declared unitType: an unrelated SetAttribute keeps the cached slot.
func TestSwitchback_SplitRuleRevalidatesAgainstTheServingUnit(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.UnitType = "user_id"
	experiment.AssignmentRules = `{"rules":[{"type":"split","environments":[],"percentages":"50/50"}]}`
	for u := 0; u < 16; u++ {
		var h = sbCreateAt(t, sbAnchor+5, map[string]string{sbUnitType: "store-" + strconv.Itoa(u), "user_id": "fixed-user"}, sbData(experiment))
		h.peek("exp_sb")
		var first = h.ctx.GetAssignment("exp_sb")
		h.setAttribute("unrelated", u)
		h.peek("exp_sb")
		if h.ctx.GetAssignment("exp_sb") != first {
			t.Fatalf("store-%d: the slot was resolved again", u)
		}
	}
}

// A switchback slot stays current while the custom assignment is the one it
// was resolved with, even when it serves another variant (full-on here).
func TestSwitchback_UnchangedCustomAssignmentKeepsAFullOnSlot(t *testing.T) {
	var experiment = sbDefault(3, sbHour)
	experiment.FullOnVariant = 1
	var h = sbCreate(t, sbData(experiment), sbCustom("exp_sb", 2))
	h.at(sbAnchor + 5)
	expectInt(t, 1, h.treatment("exp_sb"))
	expectInt(t, 1, h.treatment("exp_sb"))
	h.expectPending(1)
}

// A unit experiment refreshed to another unitType is resolved again and
// exposed under the new unit.
func TestSwitchback_UnitExperimentUnitTypeRefreshReresolves(t *testing.T) {
	var unit = sbUnit(sbDefault(2, sbHour))
	var h = sbCreateAt(t, sbAnchor, map[string]string{sbUnitType: sbUID, "user_id": "u1"}, sbData(unit))
	h.treatment("exp_sb")
	unit.UnitType = "user_id"
	h.refresh(unit)
	h.treatment("exp_sb")
	var exposures = h.publishExposures()
	expectInt(t, 2, len(exposures))
	expectValue(t, "user_id", exposures[1].Unit)
}

// A slot resolved without a linked unit reads the cluster again once a unit
// is set: an exposed overridden slot survives SetUnit, and the rebuild after
// a later override change is recorded under the serving unit.
func TestSwitchback_ResolutionAfterSetUnitReadsTheClusterAgain(t *testing.T) {
	var experiment = sbDefault(2, sbHour)
	experiment.SwitchbackClusters = sbClusters("device_id", "unset_zone", sbUnitType, sbClusterPath)
	var h = sbCreateAt(t, sbAnchor+5, nil, sbData(experiment), sbOverride("exp_sb", 1))
	expectInt(t, 1, h.treatment("exp_sb"))
	if err := h.ctx.SetUnit(sbUnitType, sbUID); err != nil {
		t.Fatal(err)
	}
	_ = h.ctx.SetOverride("exp_sb", 0)
	expectInt(t, 0, h.treatment("exp_sb"))
	var exposures = h.publishExposures()
	expectInt(t, 2, len(exposures))
	expectValue(t, "device_id", exposures[0].Unit)
	expectValue(t, sbUnitType, exposures[1].Unit)
}

// sbTickingClock advances one millisecond per read.
type sbTickingClock struct {
	mu  sync.Mutex
	now int64
}

func (c *sbTickingClock) Millis() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now++
	return c.now - 1
}

// GetTreatment and GetVariableValue stamp a switchback exposure with the
// reading that resolved it, not a later one within the period.
func TestSwitchback_ExposureIsStampedWithTheResolvingReadingWithinThePeriod(t *testing.T) {
	for _, read := range []func(*Context){
		func(c *Context) { _, _ = c.GetTreatment("exp_sb") },
		func(c *Context) { _, _ = c.GetVariableValue("sb.arm", -1) },
	} {
		var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
		var clock = &sbTickingClock{now: sbAnchor + 100}
		h.ctx.Clock_ = clock
		read(h.ctx)
		var exposures = h.publishExposures()
		expectInt(t, 1, len(exposures))
		expectValue(t, sbAnchor+100, exposures[0].ExposedAt)
	}
}

// An overridden switchback removed by a refresh is resolved again, as an
// experiment that is no longer there: the override still applies, under no
// unit, and it is exposed again once.
func TestSwitchback_OverriddenSwitchbackRemovedByARefresh(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)), sbOverride("exp_sb", 1))
	h.at(sbAnchor + 5)
	expectInt(t, 1, h.treatment("exp_sb"))
	h.refresh()
	expectInt(t, 1, h.treatment("exp_sb"))
	expectInt(t, 1, h.treatment("exp_sb"))
	var exposures = h.publishExposures()
	expectInt(t, 2, len(exposures))
	expectValue(t, jsonmodels.Exposure{Name: "exp_sb", Variant: 1, ExposedAt: sbAnchor + 5, Eligible: true, Overridden: true}, exposures[1])
}

// A switchback exposure rejected by a close is queued by the next read once
// the context is open again.
func TestSwitchback_ExposureRejectedByACloseIsRetried(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 5)
	h.ctx.Closing_.Store(true)
	_ = h.ctx.GetAssignment("exp_sb")
	h.ctx.QueueExposure(h.ctx.GetAssignment("exp_sb"))
	h.expectPending(0)
	h.ctx.Closing_.Store(false)
	h.treatment("exp_sb")
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+5, true, true))
}

// An overridden unit experiment refreshed into a switchback is resolved
// again under the switchback's serving unit, and exposed again.
func TestSwitchback_OverriddenUnitExperimentRefreshedIntoASwitchback(t *testing.T) {
	var unit = sbUnit(sbDefault(2, sbHour))
	unit.UnitType = "user_id"
	var h = sbCreateAt(t, sbAnchor+5, map[string]string{sbUnitType: sbUID, "user_id": "u1"}, sbData(unit), sbOverride("exp_sb", 1))
	expectInt(t, 1, h.treatment("exp_sb"))
	// In its grid and before its start: the switchback resolves in the
	// period slot and in the slot without a period, which is the unit
	// experiment's.
	for _, startAt := range []int64{sbAnchor, sbAnchor + sbHour} {
		var switchback = sbDefault(2, sbHour)
		switchback.UnitType = "user_id"
		switchback.SwitchbackStartAt = sbInt64(startAt)
		h.refresh(switchback)
		expectInt(t, 1, h.treatment("exp_sb"))
		h.refresh(unit)
		expectInt(t, 1, h.treatment("exp_sb"))
	}
	var exposures = h.publishExposures()
	var units []string
	for _, exposure := range exposures {
		units = append(units, exposure.Unit)
	}
	expectValue(t, []string{"user_id", sbUnitType, "user_id", sbUnitType, "user_id"}, units)
}

// A traffic miss wins over a custom assignment: control, recorded with
// eligible=false and custom=false.
func TestSwitchback_TrafficMissWinsOverACustomAssignment(t *testing.T) {
	var experiment = sbDefault(3, sbHour)
	experiment.TrafficSplit = []float64{1, 0}
	var h = sbCreate(t, sbData(experiment), sbCustom("exp_sb", 2))
	expectInt(t, 0, h.at(sbAnchor+5).treatment("exp_sb"))
	h.expectPublished(sbExposure(1, "exp_sb", 0, sbAnchor+5, true, false))
}

// randomizationType matches "switchback" exactly, as in the Java SDK: any
// other spelling is a unit experiment.
func TestSwitchback_RandomizationTypeMatchesExactly(t *testing.T) {
	for _, randomization := range []string{"Switchback", "SWITCHBACK", " switchback", "switchback "} {
		var experiment = sbDefault(2, sbHour)
		experiment.RandomizationType = randomization
		var h = sbCreate(t, sbData(experiment), sbNoClusterAttribute())
		h.treatment("exp_sb")
		var exposures = h.publishExposures()
		if len(exposures) != 1 || !exposures[0].Assigned || !exposures[0].Eligible {
			t.Fatalf("%q: %+v", randomization, exposures)
		}
	}
}

// QueueExposure finds a switchback slot without holdouts installed, and a
// silent slot (no cluster value) publishes nothing through it.
func TestSwitchback_QueueExposureWithoutHoldoutsUsesTheSlot(t *testing.T) {
	var silent = sbExperiment(2, "exp_silent", 2, sbHour)
	silent.SwitchbackClusters = sbClusters(sbUnitType, "unset_zone")
	var h = sbCreate(t, sbData(sbDefault(2, sbHour), silent))
	h.at(sbAnchor + 5)
	var assignment = h.ctx.GetAssignment("exp_sb")
	h.ctx.QueueExposure(h.ctx.GetAssignment("exp_silent"))
	h.at(sbAnchor + sbHour + 1)
	h.ctx.QueueExposure(assignment)
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+5, true, true))
}

// QueueExposure on a switchback assignment whose slot was evicted or dropped
// still finds the slot: a silent one publishes nothing, and an eligible one
// is published once, stamped inside its own period.
func TestSwitchback_QueueExposureOfAnEvictedOrDroppedSlot(t *testing.T) {
	var silent = sbExperiment(2, "exp_silent", 2, sbHour)
	silent.SwitchbackClusters = sbClusters(sbUnitType, "unset_zone")
	var h = sbCreate(t, sbData(sbDefault(2, sbHour), silent))
	var held = map[string]*Assignment{}
	for _, name := range []string{"exp_sb", "exp_silent"} {
		held[name] = h.at(sbAnchor + 5).ctx.GetAssignment(name)
		// Periods 1 and 2 evict period 0.
		h.at(sbAnchor + sbHour).ctx.GetAssignment(name)
		h.at(sbAnchor + 2*sbHour).ctx.GetAssignment(name)
	}
	h.ctx.QueueExposure(held["exp_sb"])
	h.ctx.QueueExposure(held["exp_silent"])
	h.ctx.QueueExposure(held["exp_sb"])
	h.expectPending(1)

	// A new epoch drops every slot of the old one, the slot without a
	// period included.
	var preStart = silent
	preStart.SwitchbackStartAt = sbInt64(sbAnchor + 10*sbHour)
	h.refresh(sbDefault(2, sbHour), preStart)
	var dropped = h.ctx.GetAssignment("exp_silent")
	var regridded = preStart
	regridded.SwitchbackDesignType = "other"
	h.refresh(sbDefault(2, sbHour), regridded)
	h.ctx.GetAssignment("exp_silent")
	h.ctx.QueueExposure(dropped)
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+5, true, true))
}

// A read that resolved a slot exposes it even when the slot is dropped
// before the exposure is queued.
func TestSwitchback_ExposureOfASlotDroppedAfterItsResolutionIsKept(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 5)
	var at = clockReading{clock: h.ctx.Clock_}
	var entry = h.ctx.getAssignmentAt("exp_sb", &at)
	var regridded = sbDefault(2, sbHour)
	regridded.SwitchbackDesignType = "other"
	h.refresh(regridded)
	h.ctx.GetAssignment("exp_sb")
	h.ctx.queueExposureAt(entry.assignment, entry, &at)
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+5, true, true))
}

// A slot resolved without a linked unit and dropped by SetUnit is silent: it
// never publishes through QueueExposure.
func TestSwitchback_QueueExposureOfASlotDroppedBySetUnitPublishesNothing(t *testing.T) {
	var h = sbCreateAt(t, sbAnchor+5, nil, sbData(sbDefault(2, sbHour)))
	var held = h.ctx.GetAssignment("exp_sb")
	if err := h.ctx.SetUnit(sbUnitType, sbUID); err != nil {
		t.Fatal(err)
	}
	h.ctx.QueueExposure(held)
	h.expectPending(0)
	expectInt(t, sbExpected2Arms[0], h.treatment("exp_sb"))
	h.expectPending(1)
}

// Once metadata leaves the bounded retired-slot window, QueueExposure fails
// closed: it cannot publish a silent assignment or stamp an old draw now.
func TestSwitchback_ExpiredRetiredSlotsFailClosed(t *testing.T) {
	var silent = sbExperiment(2, "exp_silent", 2, sbHour)
	silent.SwitchbackClusters = sbClusters(sbUnitType, "missing")
	var h = sbCreate(t, sbData(sbDefault(2, sbHour), silent))
	var eligible = h.ctx.GetAssignment("exp_sb")
	var ineligible = h.ctx.GetAssignment("exp_silent")
	for i := int64(1); i < 20; i++ {
		h.at(sbAnchor + i*sbHour)
		h.ctx.GetAssignment("exp_sb")
		h.ctx.GetAssignment("exp_silent")
	}
	h.ctx.QueueExposure(eligible)
	h.ctx.QueueExposure(ineligible)
	h.expectPending(0)
}

// Replacing a slot in the same period preserves the old assignment's silent
// status and timestamp for a later explicit QueueExposure call.
func TestSwitchback_SamePeriodReplacementKeepsHeldSlotMetadata(t *testing.T) {
	for _, silent := range []bool{false, true} {
		var experiment = sbDefault(3, sbHour)
		if silent {
			experiment.SwitchbackClusters = sbClusters(sbUnitType, "missing")
		}
		var h = sbCreate(t, sbData(experiment))
		h.at(sbAnchor + 5)
		var held = h.ctx.GetAssignment("exp_sb")
		_ = h.ctx.SetCustomAssignment("exp_sb", 2)
		h.ctx.GetAssignment("exp_sb")
		h.at(sbAnchor + sbHour + 1)
		h.ctx.QueueExposure(held)
		if silent {
			h.expectPending(0)
		} else {
			h.expectPublished(sbExposure(1, "exp_sb", sbExpected3Arms[0], sbAnchor+5, true, true))
		}
	}
}

// A covered unit decision replaced by a switchback, and a switchback replaced
// by a covered unit decision, retire the old decision: a caller holding it
// publishes nothing beside its replacement, as for any replacement that
// involves holdouts.
func TestSwitchback_ReplacementAcrossRandomizationRetiresTheHeldDecision(t *testing.T) {
	var holdout = jsonmodels.Holdout{Id: 11, Name: "holdout_a", UnitType: sbUnitType, Iteration: 1, SeedHi: 13, SeedLo: 111,
		Split: []float64{0, 1}}
	var unit = sbUnit(sbDefault(2, sbHour))
	unit.HoldoutIds = []int{11}
	var covered = sbData(unit)
	covered.Holdouts = []jsonmodels.Holdout{holdout}
	var switchback = sbData(sbDefault(2, sbHour))
	switchback.Holdouts = []jsonmodels.Holdout{holdout}

	// Covered unit, then a switchback.
	var h = sbCreate(t, covered)
	h.at(sbAnchor + 5)
	var held = h.ctx.GetAssignment("exp_sb")
	h.refreshData(switchback)
	h.ctx.GetAssignment("exp_sb")
	h.ctx.QueueExposure(held)
	h.expectPending(0)

	// A switchback, then a covered unit.
	h = sbCreate(t, switchback)
	h.at(sbAnchor + 5)
	held = h.ctx.GetAssignment("exp_sb")
	h.refreshData(covered)
	h.ctx.GetAssignment("exp_sb")
	h.ctx.QueueExposure(held)
	h.expectPending(0)
}

// A slot claimed, aged out of the retired window and then rejected by a
// close stays retired: QueueExposure cannot reopen and publish it.
func TestSwitchback_ExpiredSlotRejectedByACloseStaysRetired(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 5)
	var entry = h.ctx.getAssignment("exp_sb")
	if !entry.assignment.Exposed.CompareAndSwap(false, true) {
		t.Fatal("not claimed")
	}
	for i := int64(1); i < 20; i++ {
		h.at(sbAnchor + i*sbHour).ctx.GetAssignment("exp_sb")
	}
	h.ctx.Closing_.Store(true)
	h.ctx.admitExposure(entry.assignment, entry)
	h.ctx.Closing_.Store(false)
	h.ctx.QueueExposure(entry.assignment)
	h.expectPending(0)
}

// A held slot stays findable across several period rolls inside the
// retired window, so its exposure keeps its own period's stamp.
func TestSwitchback_HeldSlotIsFoundAcrossSeveralPeriodRolls(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 5)
	var held = h.ctx.GetAssignment("exp_sb")
	for i := int64(1); i <= 4; i++ {
		h.at(sbAnchor + i*sbHour).ctx.GetAssignment("exp_sb")
	}
	h.ctx.QueueExposure(held)
	h.expectPublished(sbExposure(1, "exp_sb", sbExpected2Arms[0], sbAnchor+5, true, true))
}

// A covered unit decision whose exposure is in flight when a switchback
// replaces it stays retired after a close rejects that admission.
func TestSwitchback_InFlightDecisionReplacedByASwitchbackStaysRetired(t *testing.T) {
	var holdout = jsonmodels.Holdout{Id: 11, Name: "holdout_a", UnitType: sbUnitType, Iteration: 1, SeedHi: 13, SeedLo: 111,
		Split: []float64{0, 1}}
	var unit = sbUnit(sbDefault(2, sbHour))
	unit.HoldoutIds = []int{11}
	var covered = sbData(unit)
	covered.Holdouts = []jsonmodels.Holdout{holdout}
	var switchback = sbData(sbDefault(2, sbHour))
	switchback.Holdouts = []jsonmodels.Holdout{holdout}
	var h = sbCreate(t, covered)
	h.at(sbAnchor + 5)
	var entry = h.ctx.getAssignment("exp_sb")
	if !entry.assignment.Exposed.CompareAndSwap(false, true) {
		t.Fatal("not claimed")
	}
	h.refreshData(switchback)
	h.ctx.GetAssignment("exp_sb")
	h.ctx.Closing_.Store(true)
	h.ctx.admitExposure(entry.assignment, entry)
	h.ctx.Closing_.Store(false)
	h.ctx.QueueExposure(entry.assignment)
	h.expectPending(0)
}

// Without holdouts on either side, a replacement across the randomization
// boundary leaves the held decision retryable, as a same-key replacement
// without holdouts does: QueueExposure still publishes it.
func TestSwitchback_ReplacementAcrossRandomizationWithoutHoldoutsStillPublishes(t *testing.T) {
	for _, c := range []struct {
		from, to jsonmodels.Experiment
	}{
		{sbUnit(sbDefault(2, sbHour)), sbDefault(2, sbHour)},
		{sbDefault(2, sbHour), sbUnit(sbDefault(2, sbHour))},
	} {
		var h = sbCreate(t, sbData(c.from))
		h.at(sbAnchor + 5)
		var held = h.ctx.GetAssignment("exp_sb")
		h.refresh(c.to)
		h.treatment("exp_sb")
		var before = h.pending()
		h.ctx.QueueExposure(held)
		h.expectPending(before + 1)
	}
}

// A SetUnit landing after a read resolved an overridden switchback that had
// no linked unit, and before the read claimed it, retires the slot first: the
// read loses the claim, resolves again and exposes the replacement, through
// either read API.
func TestSwitchback_SetUnitBeforeTheClaimExposesTheReplacement(t *testing.T) {
	for _, variable := range []bool{false, true} {
		var h = sbCreateAt(t, sbAnchor+5, nil, sbData(sbDefault(2, sbHour)), sbOverride("exp_sb", 1))
		var armed = true
		beforeSettleClaim = func() {
			if armed {
				armed = false
				_ = h.ctx.SetUnit(sbUnitType, sbUID)
			}
		}
		if variable {
			expectValue(t, 1, h.variable("sb.arm"))
		} else {
			expectInt(t, 1, h.treatment("exp_sb"))
		}
		beforeSettleClaim = nil
		if armed {
			t.Fatal("the hook did not run")
		}
		h.expectPublished(jsonmodels.Exposure{Id: 1, Name: "exp_sb", Unit: sbUnitType, Variant: 1, ExposedAt: sbAnchor + 5,
			Eligible: true, Overridden: true})
	}
}

// A covered decision replaced by a switchback is retired before it is
// claimed: a claim replayed right after the replacement's reports retired.
func TestSwitchback_ReplacementAcrossRandomizationRetiresBeforeTheClaim(t *testing.T) {
	var holdout = jsonmodels.Holdout{Id: 11, Name: "holdout_a", UnitType: sbUnitType, Iteration: 1, SeedHi: 13, SeedLo: 111,
		Split: []float64{0, 1}}
	var unit = sbUnit(sbDefault(2, sbHour))
	unit.HoldoutIds = []int{11}
	var covered = sbData(unit)
	covered.Holdouts = []jsonmodels.Holdout{holdout}
	var switchback = sbData(sbDefault(2, sbHour))
	switchback.Holdouts = []jsonmodels.Holdout{holdout}
	var h = sbCreate(t, covered)
	h.at(sbAnchor + 5)
	var entry = h.ctx.getAssignment("exp_sb")
	var status = exposureQueued
	var ran = false
	// The read claims between the retirement and the replacement's claim: it
	// must see the retirement.
	beforeSwitchbackRetireClaim = func(replaced *cachedAssignment) {
		if replaced == entry {
			ran = true
			status = h.ctx.queueExposure(entry.assignment, entry)
		}
	}
	h.refreshData(switchback)
	h.ctx.GetAssignment("exp_sb")
	beforeSwitchbackRetireClaim = nil
	if !ran || status != exposureRetired {
		t.Fatalf("hook ran %v, status %v; want a retired claim", ran, status)
	}
}

// A slot that ages out of the retired window is retired before it is
// claimed: a claim replayed right after reports retired.
func TestSwitchback_ExpiredSlotRetiresBeforeTheClaim(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	h.at(sbAnchor + 5)
	var entry = h.ctx.getAssignment("exp_sb")
	var status = exposureQueued
	var ran = false
	beforeSwitchbackRetireClaim = func(expired *cachedAssignment) {
		if expired == entry {
			ran = true
			status = h.ctx.queueExposure(entry.assignment, entry)
		}
	}
	for i := int64(1); i < 20 && !ran; i++ {
		h.at(sbAnchor + i*sbHour).ctx.GetAssignment("exp_sb")
	}
	beforeSwitchbackRetireClaim = nil
	if !ran || status != exposureRetired {
		t.Fatalf("hook ran %v, status %v; want a retired claim", ran, status)
	}
	h.expectPending(0)
}

// A read whose claim is retired resolves again at a fresh clock reading and
// stamps the replacement's exposure with it.
func TestSwitchback_RetriedReadStampsAFreshReading(t *testing.T) {
	var h = sbCreateAt(t, sbAnchor+5, nil, sbData(sbDefault(2, sbHour)), sbOverride("exp_sb", 1))
	var armed = true
	beforeSettleClaim = func() {
		if armed {
			armed = false
			h.at(sbAnchor + 20*sbHour + 7)
			_ = h.ctx.SetUnit(sbUnitType, sbUID)
		}
	}
	expectInt(t, 1, h.treatment("exp_sb"))
	beforeSettleClaim = nil
	if armed {
		t.Fatal("the hook did not run")
	}
	h.expectPublished(jsonmodels.Exposure{Id: 1, Name: "exp_sb", Unit: sbUnitType, Variant: 1, ExposedAt: sbAnchor + 20*sbHour + 7,
		Eligible: true, Overridden: true})
}
