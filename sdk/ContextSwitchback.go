package sdk

import (
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// Switchback serving in the context, a port of the Java SDK's Context
// (java-sdk#15, SWITCHBACK.md) and of the Collector's switchback branch of
// ContextAPI (R33, cluster-defined switchbacks).

const (
	randomizationSwitchback = "switchback"
	// The Collector's anchor bounds (Registry): an anchor must be hour
	// aligned and in [0, 10000-01-01T00:00:00Z).
	switchbackAnchorAlignmentMs int64 = 3_600_000
	switchbackMaxAnchorMs       int64 = 253_402_300_800_000
	// switchbackRetainedPeriods is how many period slots a switchback
	// experiment keeps cached.
	switchbackRetainedPeriods = 2
	// switchbackRetiredSlots is how many removed slots an experiment keeps
	// findable for QueueExposure; older ones fail closed.
	switchbackRetiredSlots = 8
)

func isSwitchback(experiment jsonmodels.Experiment) bool {
	return experiment.RandomizationType == randomizationSwitchback
}

// clockReading is one reading of the context clock, taken on first use, so
// a call that resolves and exposes a switchback stamps the exposure with the
// time that chose its period. Unit experiments never read it.
type clockReading struct {
	clock internal.Clock
	read  bool
	value int64
}

func (r *clockReading) now() int64 {
	if !r.read {
		r.value, r.read = r.clock.Millis(), true
	}
	return r.value
}

// assignmentSlot is where an assignment is cached: under the experiment name,
// or for a switchback inside its grid under the name and the period index,
// so each period gets its own assignment and exposure.
type assignmentSlot struct {
	key       string
	period    int64
	hasPeriod bool
}

func switchbackSlotKey(experimentName string, period int64) string {
	return experimentName + switchbackKeySeparator + strconv.FormatInt(period, 10)
}

// assignmentSlotFor is the slot an experiment resolves to at the clock
// reading, which is taken only for a switchback.
func assignmentSlotFor(experimentName string, indexed *indexedExperiment, at *clockReading) assignmentSlot {
	if indexed == nil || indexed.switchback == nil {
		return assignmentSlot{key: experimentName}
	}
	if period, ok := currentSwitchbackPeriod(indexed, at.now()); ok {
		return assignmentSlot{key: switchbackSlotKey(experimentName, period), period: period, hasPeriod: true}
	}
	return assignmentSlot{key: experimentName}
}

// switchbackScheduleServable reports whether the Collector's Registry can
// serve the schedule: a start that is set, hour aligned and in [0,
// 10000-01-01) (used as served, never rounded), a period > 0 and at least two
// arms. An unservable schedule only replaces the draw: assignment rules and
// full-on still apply.
func switchbackScheduleServable(data jsonmodels.Experiment) bool {
	if data.SwitchbackStartAt == nil || data.SwitchbackPeriodMs == nil {
		return false
	}
	var startAt = *data.SwitchbackStartAt
	if startAt < 0 || startAt >= switchbackMaxAnchorMs || startAt%switchbackAnchorAlignmentMs != 0 {
		return false
	}
	return *data.SwitchbackPeriodMs > 0 && len(data.Split) >= 2
}

// currentSwitchbackPeriod is floorDiv(now - switchbackStartAt,
// switchbackPeriodMs), as the Collector computes it. It reports false before
// the start, for an unservable schedule and for a cluster map taking the
// legacy path.
func currentSwitchbackPeriod(indexed *indexedExperiment, now int64) (int64, bool) {
	var data = indexed.experiment.Data
	if indexed.switchback.paths == nil || !switchbackScheduleServable(data) {
		return 0, false
	}
	var startAt = *data.SwitchbackStartAt
	if now < startAt {
		return 0, false
	}
	return floorDiv(now-startAt, *data.SwitchbackPeriodMs), true
}

// switchbackSlot is a switchback assignment's serving resolution and the
// period it was resolved for.
type switchbackSlot struct {
	serving switchbackServing
	// generation is the switchback definition's generation the slot was
	// resolved under.
	generation uint64
	period     int64
	hasPeriod  bool
	// startAt and periodMs bound the slot's period, for QueueExposure.
	startAt  int64
	periodMs int64
	// resolvedAt is the clock reading that resolved the slot.
	resolvedAt int64
	// silent is set for a resolution the Collector records nothing for: no
	// linked unit, an ineligible cluster, or no period to draw in.
	silent bool
	// customSet and custom are the custom assignment the slot was resolved
	// with.
	customSet bool
	custom    int
}

func (s *switchbackSlot) isSilent() bool {
	return s != nil && s.silent
}

// stamp is the exposedAt of a slot exposed through QueueExposure at now: now
// while it is still in the slot's period, the reading that resolved the slot
// otherwise.
func (s *switchbackSlot) stamp(now int64) int64 {
	if s.hasPeriod && now >= s.startAt && floorDiv(now-s.startAt, s.periodMs) == s.period {
		return now
	}
	return s.resolvedAt
}

// customMatches reports whether a custom assignment leaves the assignment
// current. A unit assignment keeps the v1.1.1 rule: no custom assignment, or
// one equal to the variant. A switchback slot is current while the custom
// assignment is the one it was resolved with, as in the Java SDK, since a
// traffic miss, a full-on or an ineligible cluster serve another variant.
func (s *switchbackSlot) customMatches(custom interface{}, cfound bool, assignment *Assignment) bool {
	if s == nil {
		return !cfound || custom.(int) == assignment.Variant
	}
	return cfound == s.customSet && (!cfound || custom.(int) == s.custom)
}

// unitType is the unit type a switchback slot is recorded under: the serving
// unit's, or without one the first linked type of a readable cluster map,
// or else the declared unitType the legacy path reads.
func (s *switchbackSlot) unitType(indexed *indexedExperiment) string {
	if len(s.serving.unitType) > 0 {
		return s.serving.unitType
	}
	if indexed.switchback.paths != nil {
		return indexed.switchback.sources[0]
	}
	return indexed.experiment.Data.UnitType
}

// switchbackSlotMatches reports whether a cached entry was resolved for the
// installed definition: a unit entry for a unit experiment, a switchback
// slot for a switchback of the same generation.
func switchbackSlotMatches(indexed *indexedExperiment, entry *cachedAssignment) bool {
	if indexed == nil || indexed.switchback == nil {
		return entry.switchback == nil
	}
	return entry.switchback != nil && entry.switchback.generation == indexed.switchback.generation
}

// entryMatches is ExperimentMatches for a cache entry. A switchback serves
// under the unit its clusters select, so its unit identity is the cluster
// map, compared through the generation.
func (c *Context) entryMatches(indexed *indexedExperiment, entry *cachedAssignment) bool {
	// Through a pointer: the experiment is too large to copy on every read.
	var data, assignment = &indexed.experiment.Data, entry.assignment
	if entry.switchback == nil {
		return indexed.switchback == nil && data.Id == assignment.Id && data.UnitType == assignment.UnitType &&
			data.Iteration == assignment.Iteration && data.FullOnVariant == assignment.FullOnVariant &&
			reflect.DeepEqual(data.TrafficSplit, assignment.TrafficSplit)
	}
	return switchbackSlotMatches(indexed, entry) && data.Id == assignment.Id && data.Iteration == assignment.Iteration &&
		data.FullOnVariant == assignment.FullOnVariant && reflect.DeepEqual(data.TrafficSplit, assignment.TrafficSplit)
}

// ruleUnitType is the unit a split rule hashes: the experiment's unitType,
// or for a switchback the serving unit (the Collector resets its assigner to
// it).
func ruleUnitType(data jsonmodels.Experiment, entry *cachedAssignment) string {
	if entry.switchback != nil {
		return entry.assignment.UnitType
	}
	return data.UnitType
}

// switchbackEpoch records the period slots a switchback experiment has
// cached, all resolved under one generation, in ascending order. Indices of
// different generations are never compared: a changed start or period length
// re-maps them.
type switchbackEpoch struct {
	generation uint64
	periods    []int64
}

// switchbackEpochStale reports whether an experiment's cached switchback
// slots belong to another definition: the installed one is absent, not a
// switchback, or of another generation. The caller holds ContextLock_.
func (c *Context) switchbackEpochStale(experimentName string, indexed *indexedExperiment) bool {
	if c.switchbackEpochs_ == nil {
		// No switchback slot was ever cached.
		return false
	}
	var epoch = c.switchbackEpochs_[experimentName]
	return epoch != nil && (indexed == nil || indexed.switchback == nil || epoch.generation != indexed.switchback.generation)
}

// dropSwitchbackEpoch drops every slot of an experiment's epoch, the one
// without a period included. The caller holds ContextLock_ for writing.
func (c *Context) dropSwitchbackEpoch(experimentName string) {
	var epoch = c.switchbackEpochs_[experimentName]
	if epoch == nil {
		return
	}
	delete(c.switchbackEpochs_, experimentName)
	for _, period := range epoch.periods {
		c.retireSwitchbackSlot(experimentName, switchbackSlotKey(experimentName, period))
	}
	c.retireSwitchbackSlot(experimentName, experimentName)
}

// retireAcrossRandomization retires a covered decision replaced across the
// randomization boundary, which the same-key replacement in getAssignmentAt
// does not see: a unit entry lives under the experiment name and a switchback
// slot under name and period. A covered unit decision replaced by a
// switchback slot, and a switchback slot replaced by a covered unit decision,
// are superseded like any replacement involving holdouts: a caller still
// holding the old one must not publish it beside its replacement. The caller
// holds ContextLock_ for writing.
func (c *Context) retireAcrossRandomization(experimentName string, slot assignmentSlot, entry *cachedAssignment) {
	var retire = func(old *cachedAssignment) {
		old.retirement.Store(true)
		old.assignment.Exposed.CompareAndSwap(false, true)
	}
	if entry.switchback != nil {
		if old, found := c.assignmentCache_[experimentName]; found && slot.key != experimentName &&
			old.switchback == nil && old.holdouts != nil {
			retire(old)
			delete(c.assignmentCache_, experimentName)
		}
		return
	}
	if entry.holdouts == nil {
		return
	}
	// A switchback replaced by a unit experiment changed its generation, so
	// getAssignmentAt has already dropped its live slots into the retired
	// window; only that window can still hold them.
	for _, old := range c.retiredSwitchbackSlots_[experimentName] {
		retire(old)
	}
}

// retireSwitchbackSlot removes a cached switchback slot and remembers it in
// a small window per experiment, so QueueExposure on an assignment a caller
// still holds finds its slot: a silent resolution stays silent, and an
// exposure is stamped within the slot's period. The exposed flag is left
// alone, so a read that resolved the slot just before it was removed still
// exposes it. The caller holds ContextLock_ for writing.
func (c *Context) retireSwitchbackSlot(experimentName string, key string) {
	var entry, found = c.assignmentCache_[key]
	if !found {
		return
	}
	delete(c.assignmentCache_, key)
	if c.retiredSwitchbackSlots_ == nil {
		c.retiredSwitchbackSlots_ = map[string][]*cachedAssignment{}
	}
	var retired = append(c.retiredSwitchbackSlots_[experimentName], entry)
	if len(retired) > switchbackRetiredSlots {
		// Metadata is no longer available outside this bounded window.
		// Fail closed rather than publishing a silent resolution, or an
		// old variant stamped in the current period, as a unit exposure:
		// the exposure is claimed, and its retirement keeps a rejected
		// admission from reopening it.
		for _, expired := range retired[:len(retired)-switchbackRetiredSlots] {
			expired.retirement.Store(true)
			expired.assignment.Exposed.CompareAndSwap(false, true)
		}
		retired = retired[len(retired)-switchbackRetiredSlots:]
	}
	c.retiredSwitchbackSlots_[experimentName] = retired
}

// retainSwitchbackSlot records a switchback slot under the current epoch.
// At most switchbackRetainedPeriods period indices are kept: the lowest
// index other than the one just installed is evicted, by index, not by
// recency. Exposure dedup therefore holds only within the retained window:
// re-entering an evicted period (a clock rewind, or a caller delayed across
// two boundaries) creates one new assignment for it, which is then cached
// like any other. The caller holds ContextLock_ for writing.
func (c *Context) retainSwitchbackSlot(experimentName string, indexed *indexedExperiment, slot assignmentSlot) {
	var epoch = c.switchbackEpochs_[experimentName]
	if epoch == nil {
		if c.switchbackEpochs_ == nil {
			c.switchbackEpochs_ = map[string]*switchbackEpoch{}
		}
		epoch = &switchbackEpoch{generation: indexed.switchback.generation}
		c.switchbackEpochs_[experimentName] = epoch
	}
	if !slot.hasPeriod {
		return
	}
	var i = sort.Search(len(epoch.periods), func(i int) bool { return epoch.periods[i] >= slot.period })
	if i < len(epoch.periods) && epoch.periods[i] == slot.period {
		return
	}
	epoch.periods = append(epoch.periods, 0)
	copy(epoch.periods[i+1:], epoch.periods[i:])
	epoch.periods[i] = slot.period
	for len(epoch.periods) > switchbackRetainedPeriods {
		var evict = 0
		if epoch.periods[0] == slot.period {
			evict = 1
		}
		c.retireSwitchbackSlot(experimentName, switchbackSlotKey(experimentName, epoch.periods[evict]))
		epoch.periods = append(epoch.periods[:evict], epoch.periods[evict+1:]...)
	}
}

// newSwitchbackSlot starts the slot of a switchback resolution. Within one
// slot the serving unit and cluster key are read once: a slot replaced in the
// same epoch, after an attribute or custom assignment change, keeps the
// previous resolution's cluster unless no linked unit was set then. The
// caller holds ContextLock_ for writing.
func (c *Context) newSwitchbackSlot(indexed *indexedExperiment, previous *cachedAssignment, slot assignmentSlot, at *clockReading) *switchbackSlot {
	var now = at.now()
	var resolved = &switchbackSlot{generation: indexed.switchback.generation, period: slot.period,
		hasPeriod: slot.hasPeriod, resolvedAt: now}
	if slot.hasPeriod {
		resolved.startAt, resolved.periodMs = *indexed.experiment.Data.SwitchbackStartAt, *indexed.experiment.Data.SwitchbackPeriodMs
	}
	if previous != nil && previous.switchback != nil && previous.switchback.generation == resolved.generation &&
		previous.switchback.serving.status != switchbackAbsent {
		resolved.serving = previous.switchback.serving
	} else {
		resolved.serving = c.resolveSwitchbackServing(indexed, now)
	}
	return resolved
}

// resolveSwitchbackServing reads the switchback's serving unit and cluster
// key at now (see resolveSwitchbackServing in SwitchbackServing.go). The
// cluster is resolved whether or not the schedule can be drawn now, as the
// Collector does, so assignment rules and full-on still see the serving
// unit. The caller holds ContextLock_.
func (c *Context) resolveSwitchbackServing(indexed *indexedExperiment, now int64) switchbackServing {
	var data = indexed.experiment.Data
	if indexed.switchback.paths == nil {
		return legacySwitchbackServing(data.UnitType, c.Units_)
	}
	return resolveSwitchbackServing(indexed.switchback, data.SwitchbackClusterValues, c.Units_,
		func(path *switchbackPath) string {
			if attribute, found := c.latestAttribute(path.rootKey(), now); found {
				return path.resolveAttribute(attribute.Value)
			}
			return ""
		})
}

// latestAttribute is the attribute the Collector records with an exposure at
// now (ContextAPI.extractAttributes): among the attributes set at or before
// now whose trimmed name matches, the greatest setAt wins, and a tie goes to
// the one set last. Only attributes set on the context are read, never the
// system attributes. The caller holds ContextLock_.
func (c *Context) latestAttribute(name string, now int64) (jsonmodels.Attribute, bool) {
	var selected jsonmodels.Attribute
	var found = false
	for _, value := range c.Attributes_ {
		var attribute, ok = value.(jsonmodels.Attribute)
		if ok && attribute.SetAt <= now && strings.TrimSpace(attribute.Name) == name && (!found || attribute.SetAt >= selected.SetAt) {
			selected, found = attribute, true
		}
	}
	return selected, found
}

// resolveSwitchback resolves a switchback experiment that is not overridden,
// in the Collector's assignExperiment order for a cluster-defined
// switchback: the serving resolution decides whether the experiment applies
// and under which unit; for an eligible cluster, assignment rules (a split
// hashes the serving unit), the audience, full-on, traffic (sticky on the
// serving unit's hash), the custom assignment and the draw over the raw
// cluster key follow. Holdouts never apply. Only the draw needs a period:
// before the start or with an unservable schedule the result is an
// ineligible control that records nothing, while rules, the audience and
// full-on are unaffected. The caller holds ContextLock_ for writing.
func (c *Context) resolveSwitchback(entry *cachedAssignment, indexed *indexedExperiment, slot assignmentSlot) {
	var data, assignment, resolved = indexed.experiment.Data, entry.assignment, entry.switchback
	assignment.Id = data.Id
	assignment.Iteration = data.Iteration
	assignment.TrafficSplit = data.TrafficSplit
	assignment.FullOnVariant = data.FullOnVariant
	assignment.AttrsSeq = entry.attrsSeq
	assignment.Variant = 0

	var serving = resolved.serving
	if serving.status == switchbackAbsent {
		// No linked unit: the experiment does not apply, like an unset unit.
		assignment.UnitType = resolved.unitType(indexed)
		assignment.Eligible = false
		resolved.silent = true
		return
	}
	assignment.UnitType = serving.unitType
	assignment.Assigned = true
	if serving.status == switchbackIneligible {
		assignment.Eligible = false
		resolved.silent = true
		return
	}

	if rule := c.resolveRule(data, indexed.rules, serving.unitType, false, true); rule != nil {
		// Recorded like a unit experiment's rule win: not assigned.
		assignment.Assigned = false
		assignment.RuleOverride = true
		assignment.Variant = rule.variant
		entry.ruleMissingUnit = rule.missingUnit
		return
	}
	if len(data.Audience) > 0 {
		if match, err := c.AudienceMatcher_.Evaluate(data.Audience, c.evaluationAttributes()); err == nil {
			assignment.AudienceMismatch = !match.Get()
		}
	}
	if data.AudienceStrict && assignment.AudienceMismatch {
		// Recorded like a unit experiment's strict mismatch: control, not
		// assigned.
		assignment.Assigned = false
		return
	}
	if data.FullOnVariant != 0 {
		assignment.Variant = data.FullOnVariant
		assignment.FullOn = true
		return
	}
	if !slot.hasPeriod {
		// Before the start or with an unservable schedule there is no draw:
		// control, and no exposure.
		assignment.Eligible = false
		resolved.silent = true
		return
	}

	var hash [22]byte
	var assigner = c.GetVariantAssigner(serving.unitType, c.GetUnitHash(serving.unitType, serving.uid, hash[:], false), false)
	var buf [12]int8
	if assigner.Assign(data.TrafficSplit, data.TrafficSeedHi, data.TrafficSeedLo, buf[:]) != 1 {
		// A traffic miss on an eligible cluster is recorded, as for a unit
		// experiment.
		assignment.Eligible = false
		return
	}
	if resolved.customSet {
		assignment.Variant = resolved.custom
		assignment.Custom = true
		return
	}
	assignment.Variant = switchbackAssign(serving.clusterKey, slot.period, switchbackSeed(data.SeedHi, data.SeedLo), len(data.Split))
}

// assignSwitchbackGenerations gives each installed switchback definition a
// generation: the previous definition's while it was a switchback with the
// same identity, a fresh one otherwise. Because a removal or a unit
// definition breaks the chain, a switchback that returns later with an
// identical definition gets a new generation. The caller holds ContextLock_
// for writing.
func (c *Context) assignSwitchbackGenerations(previous *contextIndex, installed map[string]*indexedExperiment) {
	for name, experiment := range installed {
		if experiment.switchback == nil {
			continue
		}
		var before *indexedExperiment
		if previous != nil {
			before = previous.experiments[name]
		}
		if sameSwitchbackIdentity(before, experiment) {
			experiment.switchback.generation = before.switchback.generation
		} else {
			c.switchbackGeneration_++
			experiment.switchback.generation = c.switchbackGeneration_
		}
	}
}

// sameSwitchbackIdentity compares what maps periods and clusters to
// variants: the id, the iteration, the ordered cluster map, the allow-list,
// the start, the period length and the design, plus the unitType when either
// side takes the legacy path. Seed, split and the analysis-only fields are
// left out, as they do not invalidate unit assignments either.
func sameSwitchbackIdentity(previous *indexedExperiment, installed *indexedExperiment) bool {
	if previous == nil || previous.switchback == nil {
		return false
	}
	var a, b = previous.experiment.Data, installed.experiment.Data
	return a.Id == b.Id && a.Iteration == b.Iteration &&
		reflect.DeepEqual(a.SwitchbackClusters, b.SwitchbackClusters) &&
		reflect.DeepEqual(a.SwitchbackClusterValues, b.SwitchbackClusterValues) &&
		equalInt64Pointers(a.SwitchbackStartAt, b.SwitchbackStartAt) &&
		equalInt64Pointers(a.SwitchbackPeriodMs, b.SwitchbackPeriodMs) &&
		a.SwitchbackDesignType == b.SwitchbackDesignType &&
		((previous.switchback.paths != nil && installed.switchback.paths != nil) || a.UnitType == b.UnitType)
}

func equalInt64Pointers(a *int64, b *int64) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}
