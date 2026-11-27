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
	switchbackRetainedPeriods         = 2
	// switchbackRetiredSlots is how many removed slots an experiment keeps
	// findable for QueueExposure; older ones fail closed.
	switchbackRetiredSlots = 8
)

func isSwitchback(experiment *jsonmodels.Experiment) bool {
	return experiment != nil && experiment.RandomizationType == randomizationSwitchback
}

// clockReading is one reading of the context clock, taken on first use, so
// a call that resolves and exposes a switchback stamps the exposure with the
// time that chose its period. Unit experiments never read it.
type clockReading struct {
	clock internal.Clock
	read  bool
	value int64
}

// readServing is a serving result for one definition generation. That
// identifies the slot too: generations are unique within a context, and one
// clock reading fixes the period.
type readServing struct {
	generation uint64
	serving    switchbackServing
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

// periodSlotKey is switchbackSlotKey for the definition's experiment, built
// once per period rather than on every read.
func (d *switchbackDefinition) periodSlotKey(experimentName string, period int64) string {
	if cached, _ := d.slotKey.Load().(*switchbackSlotKeyCache); cached != nil && cached.period == period {
		return cached.key
	}
	var key = switchbackSlotKey(experimentName, period)
	d.slotKey.Store(&switchbackSlotKeyCache{period: period, key: key})
	return key
}

// assignmentSlotFor is the slot an experiment resolves to at the clock
// reading, which is taken only for a switchback.
func assignmentSlotFor(experimentName string, indexed *indexedExperiment, at *clockReading) assignmentSlot {
	if indexed == nil || indexed.switchback == nil {
		return assignmentSlot{key: experimentName}
	}
	if period, ok := currentSwitchbackPeriod(indexed, at.now()); ok {
		return assignmentSlot{key: indexed.switchback.periodSlotKey(experimentName, period), period: period, hasPeriod: true}
	}
	return assignmentSlot{key: experimentName}
}

// switchbackScheduleServable reports whether the Collector's Registry can
// serve the schedule: a start that is set, hour aligned and in [0,
// 10000-01-01) (used as served, never rounded), a period > 0 and at least two
// arms. An unservable schedule only replaces the draw: assignment rules and
// full-on still apply.
func switchbackScheduleServable(data *jsonmodels.Experiment) bool {
	if data == nil || data.SwitchbackStartAt == nil || data.SwitchbackPeriodMs == nil {
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
	var data = &indexed.experiment.Data
	if indexed.switchback.sources == nil || !switchbackScheduleServable(data) {
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
	serving    switchbackServing
	generation uint64
	period     int64
	hasPeriod  bool
	startAt    int64
	periodMs   int64
	resolvedAt int64
	// silent is set for a resolution the Collector records nothing for: no
	// linked unit, an ineligible cluster, or no period to draw in.
	silent    bool
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
	if indexed.switchback.sources != nil {
		return indexed.switchback.sources[0].unitType
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
			sameSplit(data.TrafficSplit, assignment.TrafficSplit)
	}
	return switchbackSlotMatches(indexed, entry) && data.Id == assignment.Id && data.Iteration == assignment.Iteration &&
		data.FullOnVariant == assignment.FullOnVariant && sameSplit(data.TrafficSplit, assignment.TrafficSplit) &&
		sameSplit(data.Split, entry.resolvedSplit)
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
func (st *contextState) switchbackEpochStale(experimentName string, indexed *indexedExperiment) bool {
	if st.switchbackEpochs_ == nil {
		return false
	}
	var epoch = st.switchbackEpochs_[experimentName]
	return epoch != nil && (indexed == nil || indexed.switchback == nil || epoch.generation != indexed.switchback.generation)
}

// dropSwitchbackEpoch drops every slot of an experiment's epoch, the one
// without a period included. The caller holds ContextLock_ for writing.
func (c *Context) dropSwitchbackEpoch(st *contextState, experimentName string) {
	var epoch = st.switchbackEpochs_[experimentName]
	if epoch == nil {
		return
	}
	delete(st.switchbackEpochs_, experimentName)
	for _, period := range epoch.periods {
		c.retireSwitchbackSlot(st, experimentName, switchbackSlotKey(experimentName, period))
	}
	if entry := st.assignmentCache_[experimentName]; entry != nil && entry.switchback != nil {
		c.retireSwitchbackSlot(st, experimentName, experimentName)
	}
}

// afterSwitchbackRetireClaim, when set by a test, runs right after a
// switchback retirement marks the retired entry exposed.
var afterSwitchbackRetireClaim func(*cachedAssignment)

// claimRetiredSwitchback retires before claiming so a losing read retries;
// a claim already taken still commits.
func (c *Context) claimRetiredSwitchback(entry *cachedAssignment) {
	c.EventLock_.Lock()
	entry.retirement.Store(true)
	c.EventLock_.Unlock()
	entry.assignment.Exposed.CompareAndSwap(false, true)
	if afterSwitchbackRetireClaim != nil {
		afterSwitchbackRetireClaim(entry)
	}
}

// retireAcrossRandomization retires a covered decision replaced across the
// randomization boundary, which the same-key replacement in getAssignmentAt
// does not see: a unit entry lives under the experiment name and a switchback
// slot under name and period. A covered unit decision replaced by a
// switchback slot, and a switchback slot replaced by a covered unit decision,
// are superseded like any replacement involving holdouts: a caller still
// holding the old one must not publish it beside its replacement. The caller
// holds ContextLock_ for writing.
func (c *Context) retireAcrossRandomization(st *contextState, experimentName string, slot assignmentSlot, entry *cachedAssignment) {
	if entry.switchback != nil {
		if old, found := st.assignmentCache_[experimentName]; found && slot.key != experimentName &&
			old.switchback == nil {
			if old.holdouts != nil {
				c.claimRetiredSwitchback(old)
			}
			delete(st.assignmentCache_, experimentName)
		}
		return
	}
	if entry.holdouts == nil {
		return
	}
	// A switchback replaced by a unit experiment changed its generation, so
	// getAssignmentAt has already dropped its live slots into the retired
	// window; only that window can still hold them.
	for _, old := range st.retiredSwitchbackSlots_[experimentName] {
		c.claimRetiredSwitchback(old)
	}
}

// retireSwitchbackSlot removes a cached switchback slot and remembers it in
// a small window per experiment, so QueueExposure on an assignment a caller
// still holds finds its slot: a silent resolution stays silent, and an
// exposure is stamped within the slot's period. The exposed flag is left
// alone, so a read that resolved the slot just before it was removed still
// exposes it. The caller holds ContextLock_ for writing.
func (c *Context) retireSwitchbackSlot(st *contextState, experimentName string, key string) {
	var entry, found = st.assignmentCache_[key]
	if !found {
		return
	}
	delete(st.assignmentCache_, key)
	if st.retiredSwitchbackSlots_ == nil {
		st.retiredSwitchbackSlots_ = map[string][]*cachedAssignment{}
	}
	var retired = append(st.retiredSwitchbackSlots_[experimentName], entry)
	if len(retired) > switchbackRetiredSlots {
		// Metadata is no longer available outside this bounded window.
		// Fail closed rather than publishing a silent resolution, or an
		// old variant stamped in the current period, as a unit exposure:
		// the exposure is claimed, and its retirement keeps a rejected
		// admission from reopening it.
		var kept = retired[len(retired)-switchbackRetiredSlots:]
		for _, expired := range retired[:len(retired)-switchbackRetiredSlots] {
			// An equivalent rebuild shares the exposed flag and retirement
			// with its predecessor: while a cached or retained slot still
			// holds them, expiring the old entry must not claim them, or the
			// current decision could never be exposed.
			if !st.switchbackClaimHeld(experimentName, expired, kept) {
				c.claimRetiredSwitchback(expired)
			}
		}
		retired = kept
	}
	st.retiredSwitchbackSlots_[experimentName] = retired
}

// switchbackClaimHeld reports whether a cached slot of the experiment, or a
// retained one, shares expired's exposed flag. The caller holds ContextLock_.
func (st *contextState) switchbackClaimHeld(experimentName string, expired *cachedAssignment, retained []*cachedAssignment) bool {
	var flag = expired.assignment.Exposed
	for _, entry := range retained {
		if entry.assignment.Exposed == flag {
			return true
		}
	}
	if entry, found := st.assignmentCache_[experimentName]; found && entry.assignment.Exposed == flag {
		return true
	}
	if epoch := st.switchbackEpochs_[experimentName]; epoch != nil {
		for _, period := range epoch.periods {
			if entry := st.assignmentCache_[switchbackSlotKey(experimentName, period)]; entry != nil && entry.assignment.Exposed == flag {
				return true
			}
		}
	}
	return false
}

// retainSwitchbackSlot records a switchback slot under the current epoch.
// At most switchbackRetainedPeriods period indices are kept: the lowest
// index other than the one just installed is evicted, by index, not by
// recency. Exposure dedup therefore holds only within the retained window:
// re-entering an evicted period (a clock rewind, or a caller delayed across
// two boundaries) creates one new assignment for it, which is then cached
// like any other. The caller holds ContextLock_ for writing.
func (c *Context) retainSwitchbackSlot(st *contextState, experimentName string, indexed *indexedExperiment, slot assignmentSlot) {
	var epoch = st.switchbackEpochs_[experimentName]
	if epoch == nil {
		if st.switchbackEpochs_ == nil {
			st.switchbackEpochs_ = map[string]*switchbackEpoch{}
		}
		epoch = &switchbackEpoch{generation: indexed.switchback.generation}
		st.switchbackEpochs_[experimentName] = epoch
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
		c.retireSwitchbackSlot(st, experimentName, switchbackSlotKey(experimentName, epoch.periods[evict]))
		epoch.periods = append(epoch.periods[:evict], epoch.periods[evict+1:]...)
	}
}

// newSwitchbackSlot starts the slot of a switchback resolution. Within one
// slot the serving unit and cluster key are read once: a slot replaced in the
// same epoch, after an attribute or custom assignment change, keeps the
// previous resolution's cluster unless no linked unit was set then. The
// caller holds ContextLock_ for writing.
func (c *Context) newSwitchbackSlot(indexed *indexedExperiment, previous *cachedAssignment, slot assignmentSlot, at *clockReading, serving **readServing) *switchbackSlot {
	var now = at.now()
	var resolved = &switchbackSlot{generation: indexed.switchback.generation, period: slot.period,
		hasPeriod: slot.hasPeriod, resolvedAt: now}
	if slot.hasPeriod {
		resolved.startAt, resolved.periodMs = *indexed.experiment.Data.SwitchbackStartAt, *indexed.experiment.Data.SwitchbackPeriodMs
	}
	if previous != nil && previous.switchback != nil && previous.switchback.generation == resolved.generation &&
		previous.switchback.serving.status != switchbackAbsent {
		resolved.serving = previous.switchback.serving
	} else if kept := *serving; kept != nil && kept.generation == resolved.generation {
		// A retry of the same read, after a cold audience parse.
		resolved.serving = kept.serving
	} else {
		resolved.serving = c.resolveSwitchbackServing(indexed, now)
		*serving = &readServing{generation: resolved.generation, serving: resolved.serving}
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
	if indexed.switchback.sources == nil {
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

// javaTrim is Java's String.trim: leading and trailing characters at or
// below U+0020 are removed, and nothing else (a non-breaking space stays).
func javaTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
}

// switchbackServingUnitSet reports whether a unit that switchback serving
// reads is set: one of the readable cluster map's linked types, or on the
// legacy path the declared unitType. The caller holds ContextLock_.
func (c *Context) switchbackServingUnitSet(indexed *indexedExperiment) bool {
	if indexed.switchback.sources == nil {
		return legacySwitchbackServing(indexed.experiment.Data.UnitType, c.Units_).status != switchbackAbsent
	}
	for _, source := range indexed.switchback.sources {
		if _, set := c.Units_[source.unitType]; set {
			return true
		}
	}
	return false
}

// latestAttribute is the attribute the Collector records with an exposure at
// now (ContextAPI.extractAttributes): among the attributes set at or before
// now whose published, trimmed name matches, the greatest setAt wins, and a tie goes to
// the one set last. Only attributes set on the context are read, never the
// system attributes. The caller holds ContextLock_.
func (c *Context) latestAttribute(name string, now int64) (jsonmodels.Attribute, bool) {
	var selected jsonmodels.Attribute
	var found = false
	for _, value := range c.Attributes_ {
		var attribute, ok = value.(jsonmodels.Attribute)
		if ok && attribute.SetAt <= now && javaTrim(publishedString(attribute.Name)) == name && (!found || attribute.SetAt >= selected.SetAt) {
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
// full-on are unaffected. It returns the experiment when its audience must
// first be parsed with no lock held (see evaluateAudience). The caller holds
// ContextLock_ for writing.
func (c *Context) resolveSwitchback(st *contextState, entry *cachedAssignment, indexed *indexedExperiment, slot assignmentSlot,
	last bool, parse *audienceParse) *jsonmodels.Experiment {
	var data, assignment, resolved = &indexed.experiment.Data, entry.assignment, entry.switchback
	assignment.Id = data.Id
	assignment.Iteration = data.Iteration
	assignment.TrafficSplit = data.TrafficSplit
	assignment.FullOnVariant = data.FullOnVariant
	assignment.Variant = 0

	var serving = resolved.serving
	if serving.status == switchbackAbsent {
		// No linked unit: the experiment does not apply, like an unset unit.
		assignment.UnitType = resolved.unitType(indexed)
		assignment.Eligible = false
		resolved.silent = true
		return nil
	}
	assignment.UnitType = serving.unitType
	assignment.Assigned = true
	if serving.status == switchbackIneligible {
		assignment.Eligible = false
		resolved.silent = true
		return nil
	}

	if rule := c.resolveRule(st, *data, indexed.rules, serving.unitType, false, true); rule != nil {
		// Recorded like a unit experiment's rule win: not assigned.
		assignment.Assigned = false
		entry.ruleOverride = true
		assignment.Variant = rule.variant
		entry.ruleMissingUnit = rule.missingUnit
		return nil
	}
	if len(data.Audience) > 0 {
		if unparsed := c.evaluateAudience(st, assignment.Name, entry, data, last, parse); unparsed != nil {
			return unparsed
		}
	}
	if data.AudienceStrict && assignment.AudienceMismatch {
		// Recorded like a unit experiment's strict mismatch: control, not
		// assigned.
		assignment.Assigned = false
		return nil
	}
	if data.FullOnVariant != 0 {
		assignment.Variant = data.FullOnVariant
		assignment.FullOn = true
		return nil
	}
	if !slot.hasPeriod {
		assignment.Eligible = false
		resolved.silent = true
		return nil
	}

	var hash [22]byte
	var assigner = c.GetVariantAssigner(serving.unitType, c.GetUnitHash(serving.unitType, serving.uid, hash[:], false), false)
	var buf [12]int8
	if assigner.Assign(data.TrafficSplit, data.TrafficSeedHi, data.TrafficSeedLo, buf[:]) != 1 {
		// A traffic miss on an eligible cluster is recorded, as for a unit
		// experiment.
		assignment.Eligible = false
		return nil
	}
	if resolved.customSet {
		assignment.Variant = resolved.custom
		assignment.Custom = true
		return nil
	}
	assignment.Variant = switchbackAssign(serving.clusterKey, slot.period, switchbackSeed(data.SeedHi, data.SeedLo), len(data.Split))
	return nil
}

// assignSwitchbackGenerations gives each installed switchback definition a
// generation: the previous definition's while it was a switchback with the
// same identity, a fresh one otherwise. Because a removal or a unit
// definition breaks the chain, a switchback that returns later with an
// identical definition gets a new generation. The caller holds ContextLock_
// for writing.
func (c *Context) assignSwitchbackGenerations(st *contextState, previous *contextIndex, installed map[string]*indexedExperiment) {
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
			st.switchbackGeneration_++
			experiment.switchback.generation = st.switchbackGeneration_
		}
	}
}

// the start, the period length, the design and the arm grid, plus the
// unitType when either side takes the legacy path. Analysis-only fields are
// left out because they do not invalidate unit assignments.
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
		sameSplit(a.Split, b.Split) &&
		((previous.switchback.sources != nil && installed.switchback.sources != nil) || a.UnitType == b.UnitType)
}

func equalInt64Pointers(a *int64, b *int64) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}
