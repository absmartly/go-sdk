package sdk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"math"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TypeError represents a type assertion failure
type TypeError struct {
	Expected string
	Actual   interface{}
}

func (e *TypeError) Error() string {
	return fmt.Sprintf("type assertion failed: expected %s, got %T", e.Expected, e.Actual)
}

type Context struct {
	PublishDelay_        int64
	RefreshInterval_     int64
	EventHandler_        ContextEventHandler
	EventLogger_         ContextEventLogger
	DataProvider_        ContextDataProvider
	VariableParser_      VariableParser
	AudienceMatcher_     AudienceMatcher
	Units_               map[string]string
	Failed_              *atomic.Value
	Ready_               *atomic.Value
	DataLock             *sync.RWMutex
	Data_                jsonmodels.ContextData
	Index_               map[string]ExperimentVariables
	ContextCustomFields_ map[string]map[string]ContextCustomFieldValue
	IndexVariables_      map[interface{}]interface{}
	ContextLock_         *sync.RWMutex
	HashedUnits_         map[interface{}]interface{}
	Assigners_           map[interface{}]interface{}
	AssignmentCache      map[string]Assignment
	EventLock_           *sync.Mutex
	Exposures_           []jsonmodels.Exposure
	Achievements_        []jsonmodels.GoalAchievement
	Attributes_          []interface{}
	Overrides_           map[interface{}]interface{}
	Cassignments_        map[interface{}]interface{}
	PendingCount_        *atomic.Value
	Closing_             *atomic.Value
	Closed_              *atomic.Value
	Refreshing_          *atomic.Value
	ReadyFuture_         *future.Future
	ClosingFuture_       *future.Future
	RefreshFuture_       *future.Future
	TimeoutLock_         *sync.Mutex
	Timeout_             *time.Timer
	RefreshTimer_        *time.Timer
	Clock_               internal.Clock
}

// contextState is owned by a context, never by a process-wide registry.
// Context keeps its v1.1.1 layout so external unkeyed literals remain valid.
type contextState struct {
	// manualRoot is the context createContext built. A copy of it whose
	// Clock_ was replaced finds this state through Assigners_ and redirects to
	// manualRoot, as a copy with the carrier does. It is nil for literals.
	manualRoot *Context
	// finalizedFuture_ includes logger delivery; ClosingFuture_ only includes
	// transport/sealing, allowing loggers to re-enter Close safely.
	finalizedFuture_ *future.Future
	publishLogs_     []*future.Future
	// publishOps_ are the gate operations of the publishes whose loggers have
	// not returned, so a blocking Close or Finalize can lend them the custom
	// audience gate. Guarded by stateLock_.
	publishOps_ map[*gateOperation]struct{}
	// closeLenders_ are the lends of holders waiting in Close: each lends the
	// gate to every publish in publishOps_, including those registered while
	// it waits. Guarded by stateLock_.
	closeLenders_ map[*publishLender]struct{}
	// indexLock_ makes a literal's Index_ write and index install in setData
	// atomic for syncLiteralExperiment. It is taken last, with no other lock
	// taken inside it.
	indexLock_ sync.Mutex
	// stateLock_ guards ReadyFuture_, RefreshFuture_, ClosingFuture_, RefreshTimer_, refresh_ and closeStarted_.
	stateLock_ *sync.Mutex
	refresh_   *refreshOperation
	// inflight_ holds the publishes that have been sent and not yet settled. Guarded by stateLock_.
	inflight_ []*future.Future

	attrsSeq_ int32

	// application_ and environment_ are the application and environment names
	// from the client config. They are injected into the attribute map that
	// audiences and assignment-rule conditions are evaluated against (system
	// values first, user attributes overlaid on top), and environment_ scopes
	// assignment rules. They are never published as attributes and are set
	// once at creation, so they need no locking.
	application_ string
	environment_ string

	// unitValues_ keeps each unit as it was set (string or float64); Units_
	// holds the string form that is hashed. Guarded by ContextLock_.
	unitValues_ map[string]interface{}

	// readyDeliverer_ is the goroutine that is logging the initial ready or
	// load-error event, or 0. A readiness wait from that goroutine is the
	// logger waiting for itself, so it returns at once.
	readyDeliverer_ *uint64

	// loggers_ records the goroutines that are inside an event logger call
	// on this context, so a blocking Close from a logger does not wait for
	// logger delivery that cannot finish until it returns.
	loggers_ *loggerGoroutines

	// closeStarted_ is set once Close has been called. The refresh timer
	// that Close stops is never installed again, even if the close fails and
	// leaves the context open for a retry. Guarded by stateLock_.
	closeStarted_ bool

	// indexVariables_ lists, per variable key, every experiment that defines
	// it, in experiment-id order. Guarded by DataLock.
	indexVariables_ map[string][]ExperimentVariables

	// payloadExperiments_ and payloadHoldouts_ are the installed payload, as
	// GetPayload returns it, when payloadInstalled_ is set; it is clear when a
	// plain ContextData was installed. Guarded by DataLock.
	payloadInstalled_   bool
	payloadExperiments_ []jsonmodels.ExperimentData
	payloadHoldouts_    []jsonmodels.Holdout

	// index_ is the installed experiment index and parsed assignment rules,
	// replaced as a whole by setData under DataLock. The assignment read path
	// loads it atomically instead of taking DataLock, so a cached read costs
	// what it did before the index was guarded. It always holds a
	// *contextIndex (an atomic.Value rather than atomic.Pointer, which needs
	// Go 1.19, so the module keeps v1.1.1's Go floor); read it with
	// loadIndex.
	index_ *atomic.Value

	// assignmentCache_ holds the assignments by experiment name. Guarded by
	// ContextLock_: entries are only written under the write lock.
	assignmentCache_ map[string]*cachedAssignment
	ruleIdentities   *ruleAssignmentIdentities

	// holdoutArms_ holds each holdout's arm per unit type, so every
	// experiment a holdout covers shares one arm and one exposure. An arm is
	// kept until the holdout's iteration changes. Created with the first arm,
	// so a context without holdouts never allocates it. Guarded by
	// ContextLock_.
	holdoutArms_ map[holdoutKey]*holdoutArm

	// switchbackEpochs_ records, by experiment name, the period slots a
	// switchback experiment has cached and the generation they were
	// resolved under. Created with the first switchback slot, so a context
	// without switchbacks never allocates it. Guarded by ContextLock_.
	switchbackEpochs_ map[string]*switchbackEpoch

	// switchbackGeneration_ is the last generation setData handed to a
	// switchback definition. Guarded by ContextLock_ held for writing.
	switchbackGeneration_ uint64

	// retiredSwitchbackSlots_ holds, per experiment, the last switchback
	// slots removed from the cache (at most switchbackRetiredSlots), so
	// QueueExposure still finds the slot of an assignment a caller holds.
	// Guarded by ContextLock_.
	retiredSwitchbackSlots_ map[string][]*cachedAssignment

	// audiences caches the usable parses of the built-in deserializer, whose
	// output is fresh on every call. A custom deserializer's result is used
	// by the build that requested it and never kept. Deserializers run
	// without context locks. audiencePanics guards same-experiment logger
	// reads while a panic is reported; it is protected by ContextLock_.
	audiences      sync.Map
	audiencePanics map[string][]*audienceParse

	// customAudiences serialises, within this context, each custom
	// deserializer call with the locked evaluation of its result, as v1.1.1
	// did by holding ContextLock_ across both. It is taken with no context
	// lock held and released after ContextLock_ is. It is reentrant for its
	// holder, so a deserializer or a panic logger can read other experiments,
	// and it is lent to a Refresh, initial load or publish the holder waits for, so
	// that operation's variable parser and loggers can read too. Such nested
	// reads decode before the outer deserializer returns: a deserializer
	// reusing storage across its own nested calls is responsible for it.
	// See buildAssignment.
	customAudiences reentrantGate

	// loadOp_ is the initial load, when it was pending at creation, for
	// lending customAudiences to it. Set before the context escapes.
	loadOp_ *gateOperation
}

// reentrantGate is a mutex that the goroutines holding it may take again.
// The first goroutine takes it when it is free and holds it until its
// matching unlock. Another goroutine is admitted only while it is the
// registered executor of an operation that a holder has lent the gate to
// (see lend). A holder lends only while it is blocked waiting for that
// operation, so the executor runs as a nested call of the holder, as v1.1.1
// ran refresh callbacks on the goroutine joining the refresh. The zero value
// is ready to use.
type reentrantGate struct {
	mu   sync.Mutex
	free sync.Cond
	// holders counts, per goroutine, the locks it holds; the gate is free
	// when it is empty. More than one goroutine holds it only through a lend.
	holders map[uint64]int
	// lent counts, per operation, the holders currently waiting for it.
	lent map[*gateOperation]int
	// lenders counts, per holder, the lends it has in force (it is blocked
	// waiting for those operations).
	lenders map[uint64]int
	// waiting counts goroutines blocked in lock, and ending lend ends
	// waiting for their executor, for tests.
	waiting, ending int
}

// gateOperation is an SDK operation (a refresh, the initial load, or a
// publish's logger) that a
// gate holder can wait for. executor is the goroutine running the
// operation's completion (installing its data, calling the variable parser
// and event loggers for it), or 0. It is guarded by the gate's mu.
type gateOperation struct {
	executor uint64
}

func (g *reentrantGate) init() {
	if g.free.L == nil {
		g.free.L = &g.mu
		g.holders = map[uint64]int{}
		g.lent = map[*gateOperation]int{}
		g.lenders = map[uint64]int{}
	}
}

// admits reports whether goroutine id may take the gate now. g.mu is held.
func (g *reentrantGate) admits(id uint64) bool {
	if len(g.holders) == 0 || g.holders[id] > 0 {
		return true
	}
	if !g.executingLent(id) {
		return false
	}
	// A lent executor stands in for the holder that waits for it, but two
	// executors (two publishes a Close waits for, say) must not decode at
	// the same time: admit it only while every other holder is blocked
	// waiting for an operation the gate is lent to.
	for holder := range g.holders {
		if !g.waitingOnLend(holder) {
			return false
		}
	}
	return true
}

// waitingOnLend reports whether holder is blocked waiting for an operation
// the gate is lent to on its behalf. g.mu is held.
func (g *reentrantGate) waitingOnLend(holder uint64) bool {
	return g.lenders[holder] > 0
}

// executingLent reports whether goroutine id executes an operation the gate
// is lent to: it acts for a waiting holder, so it may lend in turn (a
// Refresh called from a Refresh logger). g.mu is held.
func (g *reentrantGate) executingLent(id uint64) bool {
	for op := range g.lent {
		if op.executor == id {
			return true
		}
	}
	return false
}

// lock takes the gate, waiting until it is free, already held by the calling
// goroutine, or lent to an operation the calling goroutine is executing.
func (g *reentrantGate) lock() {
	var id = goroutineID()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.init()
	for !g.admits(id) {
		g.waiting++
		g.free.Wait()
		g.waiting--
	}
	g.holders[id]++
}

func (g *reentrantGate) unlock() {
	var id = goroutineID()
	g.mu.Lock()
	if g.holders[id]--; g.holders[id] <= 0 {
		delete(g.holders, id)
		// Wake waiters whenever a holder leaves: a lent executor waits for
		// the other admitted executors, not only for the gate to be free.
		g.free.Broadcast()
	}
	g.mu.Unlock()
}

// lend is called by a goroutine about to block until op settles. If that
// goroutine holds the gate, op's executor is admitted until the returned
// function is called, which the caller does once its wait returns. op
// settles only after its completion has returned from the parser and every
// logger it calls, so an admitted executor has released the gate by then.
// Whether the executor registers before or after the lend, or is already
// waiting in lock, it is admitted: both sides broadcast under g.mu.
func (g *reentrantGate) lend(op *gateOperation) (end func()) {
	return g.lendAs(goroutineID(), op)
}

// lendAs is lend on behalf of goroutine id, which is blocked waiting for op.
func (g *reentrantGate) lendAs(id uint64, op *gateOperation) (end func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.init()
	if g.holders[id] == 0 && !g.executingLent(id) {
		return func() {}
	}
	g.lent[op]++
	g.lenders[id]++
	g.free.Broadcast()
	return func() {
		g.mu.Lock()
		// The holder resumes only once op's executor is out of the gate: a
		// lend that ends early (an operation registered after the wait
		// snapshot) must not leave the two decoding at the same time. The
		// lend stays in force while it waits, so the holder still counts
		// as blocked and operations that executor waits for are admitted.
		for op.executor != 0 && g.holders[op.executor] > 0 && op.executor != id {
			g.ending++
			g.free.Wait()
			g.ending--
		}
		if g.lent[op]--; g.lent[op] <= 0 {
			delete(g.lent, op)
		}
		if g.lenders[id]--; g.lenders[id] <= 0 {
			delete(g.lenders, id)
		}
		g.mu.Unlock()
	}
}

// execute registers the calling goroutine as op's executor until the
// returned function is first called. It is called at the start of op's
// completion, whether or not a holder is (or will be) waiting for op, and
// ended before op's future settles: once it settles the holder resumes, and
// the future's own listeners, which may run on this goroutine, are not part
// of the operation.
func (g *reentrantGate) execute(op *gateOperation) (end func()) {
	var id = goroutineID()
	g.mu.Lock()
	g.init()
	op.executor = id
	g.free.Broadcast()
	g.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			op.executor = 0
			g.free.Broadcast()
			g.mu.Unlock()
		})
	}
}

// contextClock carries private state in an existing interface field without
// changing Context's public layout. Millis still delegates to the supplied
// clock. Ordinary value copies share state; callback snapshots additionally
// point at the live context. All references are GC-owned, including cycles:
// neither abandoned open contexts nor retained snapshots need a global map or
// finalizer. The carrier is installed before a constructed context escapes.
type contextClock struct {
	internal.Clock
	state    *contextState
	root     *Context
	snapshot bool
}

type contextStateKey struct{}

func newContextState() *contextState {
	var state = &contextState{
		stateLock_:       &sync.Mutex{},
		unitValues_:      map[string]interface{}{},
		readyDeliverer_:  new(uint64),
		loggers_:         &loggerGoroutines{ids: map[uint64]int{}},
		index_:           &atomic.Value{},
		assignmentCache_: map[string]*cachedAssignment{},
		ruleIdentities:   &ruleAssignmentIdentities{ids: map[uintptr]struct{}{}},
	}
	state.index_.Store((*contextIndex)(nil))
	return state
}

// state takes no lock on constructor-created contexts, including assignment
// cache hits. A manually initialized v1.1.1 literal has no carrier; its existing
// assigner map owns the state under a private, non-string key. That fallback is
// protected by ContextLock_, and must be resolved before taking other locks.
// It does not replace the caller's Clock_ or require process-lifetime storage.
func (c *Context) state() *contextState {
	if clock := c.ownClock(); clock != nil {
		return clock.state
	}
	return c.literalState()
}

// ownClock returns the context's state carrier, or nil when Clock_ holds none
// or holds another context's (a caller assigned b.Clock_ = a.Clock_, which in
// v1.1.1 only shared the clock). Copies and snapshots of a context share its
// ContextLock_, so the lock tells the two apart.
func (c *Context) ownClock() *contextClock {
	if clock, ok := c.Clock_.(*contextClock); ok && clock.root != nil && clock.root.ContextLock_ == c.ContextLock_ {
		return clock
	}
	return nil
}

// literalState is the cold fallback for a context with no carrier of its own
// (a literal, or one whose Clock_ was replaced), kept out of state so the
// carrier lookup stays a short call.
func (c *Context) literalState() *contextState {
	if c.ContextLock_ == nil {
		return newContextState()
	}
	c.ContextLock_.Lock()
	defer c.ContextLock_.Unlock()
	if state, ok := c.Assigners_[contextStateKey{}].(*contextState); ok {
		return state
	}
	// A literal has no root: as in v1.1.1, each value copy acts on its own
	// fields (sharing their maps and locks). Only a context the SDK created
	// and whose Clock_ was replaced redirects to its root (createContext sets
	// manualRoot for those).
	var state = newContextState()
	var indexed = make(map[string]*indexedExperiment, len(c.Index_))
	for name, experiment := range c.Index_ {
		// Manually supplied v1.1.1 indexes have no parsed-rule metadata.
		// SetData installs it; don't call a user deserializer under this lock.
		indexed[name] = &indexedExperiment{experiment: experiment}
	}
	state.index_.Store(&contextIndex{experiments: indexed})
	for name, assignment := range c.AssignmentCache {
		var assignment = assignment
		state.assignmentCache_[name] = &cachedAssignment{assignment: &assignment, retirement: &atomicFlag{}}
	}
	if c.Assigners_ == nil {
		c.Assigners_ = map[interface{}]interface{}{}
	}
	c.Assigners_[contextStateKey{}] = state
	return state
}

// legacyVariableReader returns the context whose v1.1.1 IndexVariables_
// answers variable-key reads, or nil when the private multi-experiment index
// does: a Context{...} literal reads its own, and a snapshot of a literal (the
// context its event logger receives) reads the literal it was taken from, so
// edits to the mapping are seen in callbacks too. SDK-created contexts and
// their copies use the private index.
func (c *Context) legacyVariableReader() *Context {
	if clock := c.ownClock(); clock != nil {
		if clock.state.manualRoot == nil {
			return clock.root
		}
		return nil
	}
	if c.ContextLock_ == nil || c.literalState().manualRoot == nil {
		return c
	}
	return nil
}

func (c *Context) isSnapshot() bool {
	var clock = c.ownClock()
	return clock != nil && clock.snapshot
}

// ruleAssignmentIdentities remembers the rule flag of assignments handed to
// callers even after their cache entries are replaced. Keys are numeric, not
// pointers: the registry must not keep an abandoned assignment alive. Each
// flag's finalizer removes its key and captures only this standalone registry,
// never the context or its cache (which would create a retention cycle).
// A retained Assignment copy keeps Exposed alive, so its rule flag remains
// available for as long as QueueExposure can use it. No global table is used.
type ruleAssignmentIdentities struct {
	mu  sync.Mutex
	ids map[uintptr]struct{}
}

func (r *ruleAssignmentIdentities) remember(flag *atomic.Value) {
	var id = reflect.ValueOf(flag).Pointer()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.ids[id]; found {
		return
	}
	r.ids[id] = struct{}{}
	runtime.SetFinalizer(flag, func(*atomic.Value) {
		r.mu.Lock()
		delete(r.ids, id)
		r.mu.Unlock()
	})
}

func (r *ruleAssignmentIdentities) contains(flag *atomic.Value) bool {
	var id = reflect.ValueOf(flag).Pointer()
	r.mu.Lock()
	_, found := r.ids[id]
	r.mu.Unlock()
	runtime.KeepAlive(flag)
	return found
}

// contextIndex is an installed experiment index and, by experiment name, the
// parsed assignment rules of the experiments that have any. It is never
// modified once published in index_.
type contextIndex struct {
	experiments map[string]*indexedExperiment
	holdouts    map[int]*jsonmodels.Holdout
	// keyed is set once any install had holdouts, assignment rules or
	// switchbacks: from then on every install builds its variable key set,
	// since an entry resolved under them may be retired by any later
	// replacement.
	keyed bool
	// heldOut is set once any install had holdouts or switchbacks. Only then
	// is an unexposed decision resolved again when its audience definition
	// or, for an override, its experiment's identity changes, so a context
	// with neither (rules only included) keeps v1.2.0's cached decisions, as
	// the JS and Java SDKs do.
	heldOut bool

	// audiences is the set of audience texts in experiments, built once per
	// snapshot on first use: a snapshot is never changed after it is stored.
	audiencesOnce sync.Once
	audiences     map[string]struct{}
}

// audienceSetBuilds counts audience-set builds, for tests.
var audienceSetBuilds int64

// audienceTexts returns the snapshot's set of audience texts.
func (index *contextIndex) audienceTexts() map[string]struct{} {
	index.audiencesOnce.Do(func() {
		atomic.AddInt64(&audienceSetBuilds, 1)
		index.audiences = map[string]struct{}{}
		for _, entry := range index.experiments {
			if text := entry.experiment.Data.Audience; text != "" {
				index.audiences[text] = struct{}{}
			}
		}
	})
	return index.audiences
}

// indexedExperiment is an installed experiment with its parsed assignment
// rules (nil when it has none) and the holdouts covering it, sorted and
// unique by id (nil when none), so a read finds them with one lookup.
// variableKeySet holds the variable keys one experiment defines in any
// variant.
type variableKeySet map[string]struct{}

type indexedExperiment struct {
	variableKeys variableKeySet
	experiment   ExperimentVariables
	rules        *experimentRules
	holdouts     []*jsonmodels.Holdout
	// switchback is set for a switchback experiment only. Holdouts never
	// cover one.
	switchback *switchbackDefinition
	// payload holds the experiment's holdout and switchback fields, which
	// jsonmodels.Experiment does not carry; nil when it has none.
	payload *jsonmodels.ExperimentData
}

type holdoutKey struct {
	id       int
	unitType string
}

// holdoutArm is a unit's arm in a holdout. armCount is the split length the
// arm was drawn from: a same-iteration refresh may change the live split,
// and the arm must keep the meaning it was drawn with.
type holdoutArm struct {
	assignment *Assignment
	armCount   int
}

// assignmentHoldouts pins the holdout decisions an assignment was resolved
// with, so its exposures fire the arms that decided it even if a refresh
// lands in between. holdouts identifies the covering set (by id and
// iteration) for cache validation; arms has one entry per holdout, nil when
// the experiment's unit was not set. suppressed is set when an arm holds the
// experiment out and no override or assignment rule chose the variant.
type assignmentHoldouts struct {
	holdouts   []*jsonmodels.Holdout
	arms       []*holdoutArm
	suppressed bool
}

// experimentRules is an experiment's assignmentRules text and its parsed
// rules (nil when they could not be parsed). It is built once when context
// data is installed and reused by a refresh that leaves the text unchanged,
// so the pointer identifies the rules a cached assignment was resolved
// against.
type experimentRules struct {
	text   string
	parsed *assignmentRules
}

// cachedAssignment is an assignment plus the state it was last validated
// against. The assignment itself is shared with callers and never mutated;
// the stamps are advanced under the write lock once a revalidation confirms
// the assignment, so later reads take the fast path again.
type cachedAssignment struct {
	assignment *Assignment
	rules      *experimentRules
	attrsSeq   int32
	// ruleOverride is set when a matching assignment rule chose the
	// variant. The exposure carries it so the collector leaves the unit out
	// of the analysis, as it does for overrides. It is kept here rather than
	// on Assignment, so Assignment keeps its v1.1.1 fields.
	ruleOverride bool
	// ruleMissingUnit is set when a split rule matched while the
	// experiment's unit was not set, so the variant is the experiment's
	// no-unit result rather than a hash of the unit.
	ruleMissingUnit bool
	// holdouts are the holdout decisions the assignment was resolved with,
	// nil when no holdout covers the experiment. Never modified once the
	// entry is installed. It lives here rather than on the public
	// Assignment, so that struct keeps its layout.
	holdouts *assignmentHoldouts
	// retirement is shared by entries that carry the same Exposed identity.
	retirement *atomicFlag
	switchback *switchbackSlot
	// resolvedId, resolvedIteration and resolvedUnitType identify the
	// definition used to resolve this entry, including overrides whose public
	// Iteration stays zero. A switchback slot's unit identity is its
	// generation instead (sameSwitchbackIdentity), so its unitType is not
	// compared.
	resolvedId        int
	resolvedIteration int
	resolvedUnitType  string
	// resolvedAudience and resolvedAudienceStrict are the audience definition
	// the decision was evaluated against; ExperimentMatches does not compare
	// them, so an audience edit is otherwise only seen once attributes change.
	resolvedAudience       string
	resolvedAudienceStrict bool
	// resolvedSplit identifies the arm grid used by a switchback slot.
	resolvedSplit []float64
	// failed is assignment-local: revalidation keeps the original fallback,
	// but another experiment (or deserializer) gets its own parse.
	failed *audienceParse
}

// audienceChanged reports whether the audience definition a decision was
// evaluated against has changed. A rule win or a holdout suppression never
// evaluated the audience, so an edit leaves it the same decision. The cache
// (for an unexposed decision in a keyed context) and the variable claim treat
// a change as staleness; exposed decisions and other contexts keep the
// released behaviour.
func (e *cachedAssignment) audienceChanged(data jsonmodels.Experiment) bool {
	if e.ruleOverride || e.holdouts.isSuppressed() {
		return false
	}
	return e.resolvedAudience != data.Audience || e.resolvedAudienceStrict != data.AudienceStrict
}

func (e *cachedAssignment) resolvedExperimentMatches(data jsonmodels.Experiment) bool {
	return e.resolvedId == data.Id && e.resolvedIteration == data.Iteration &&
		(e.resolvedUnitType == data.UnitType || e.switchback != nil)
}

type loggerGoroutines struct {
	mu  sync.Mutex
	ids map[uint64]int
}

func (l *loggerGoroutines) enter() uint64 {
	var id = goroutineID()
	l.mu.Lock()
	l.ids[id]++
	l.mu.Unlock()
	return id
}

func (l *loggerGoroutines) exit(id uint64) {
	l.mu.Lock()
	if l.ids[id]--; l.ids[id] <= 0 {
		delete(l.ids, id)
	}
	l.mu.Unlock()
}

func (l *loggerGoroutines) current() bool {
	var id = goroutineID()
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ids[id] > 0
}

type ExperimentVariables struct {
	Data      jsonmodels.Experiment
	Variables []map[string]interface{}
}

type ContextCustomFieldValue struct {
	Name  string
	Type  string
	Value interface{}
}

type Assignment struct {
	Id               int
	Iteration        int
	FullOnVariant    int
	Name             string
	UnitType         string
	TrafficSplit     []float64
	Variant          int
	Assigned         bool
	Overridden       bool
	Eligible         bool
	FullOn           bool
	Custom           bool
	AudienceMismatch bool
	Variables        map[string]interface{}
	Exposed          *atomic.Value
}

// suppressed reports whether a holdout holds the unit out of the experiment
// and neither an override nor an assignment rule chose the variant. A
// suppressed assignment gets control, is not assigned, and publishes no
// exposure of its own; its holdouts' exposures still fire.
func (h *assignmentHoldouts) isSuppressed() bool {
	return h != nil && h.suppressed
}

// isHeldOutBy reports whether a holdout arm holds a covered experiment out:
// arm 0 always does, and in a three-arm holdout arm 1 holds out experiments
// that are not full-on. The arm count comes from the split, never from the
// holdout type. JS: isHeldOutBy in context.ts.
func isHeldOutBy(variant int, armCount int, fullOnVariant int) bool {
	if variant == 0 {
		return true
	}
	if armCount == 3 && variant == 1 {
		return fullOnVariant == 0
	}
	return false
}

// holdoutSetMatches reports whether the holdouts covering an experiment are
// still the ones an assignment was resolved with. Only id and iteration
// count: a seed, split or name edit must not re-resolve, and so re-expose,
// the assignment.
func holdoutSetMatches(live []*jsonmodels.Holdout, pinned *assignmentHoldouts) bool {
	var resolved []*jsonmodels.Holdout
	if pinned != nil {
		resolved = pinned.holdouts
	}
	if len(live) != len(resolved) {
		return false
	}
	for i, holdout := range live {
		if holdout.Id != resolved[i].Id || holdout.Iteration != resolved[i].Iteration {
			return false
		}
	}
	return true
}

func CreateContext(clock internal.Clock, config ContextConfig, dataFuture *future.Future, dataProvider ContextDataProvider,
	eventHandler ContextEventHandler, eventLogger ContextEventLogger, variableParser VariableParser,
	audienceMatcher AudienceMatcher) *Context {
	return createContext(clock, config, dataFuture, dataProvider, eventHandler, eventLogger, variableParser,
		audienceMatcher, "", "")
}

// createContext is CreateContext with the client's application and
// environment names, which ABsmartly.CreateContext(With) passes. They are set
// before the data future can deliver its Ready callback (which may evaluate an
// audience), so that callback already sees them.
func createContext(clock internal.Clock, config ContextConfig, dataFuture *future.Future, dataProvider ContextDataProvider,
	eventHandler ContextEventHandler, eventLogger ContextEventLogger, variableParser VariableParser,
	audienceMatcher AudienceMatcher, application string, environment string) *Context {
	var cntx = Context{Clock_: clock, PublishDelay_: config.PublishDelay_, RefreshInterval_: config.RefreshInterval_,
		EventHandler_: eventHandler, DataProvider_: dataProvider, VariableParser_: variableParser,
		AudienceMatcher_: audienceMatcher, Units_: map[string]string{}}
	var state = newContextState()
	state.application_, state.environment_ = application, environment
	cntx.Clock_ = &contextClock{Clock: clock, state: state, root: &cntx}
	// manualRoot marks the state as an SDK-created context's (literals have
	// none) before anything can take a snapshot of it. A caller that later
	// replaces the exported Clock_ (as v1.1.1 allowed) finds the state through
	// Assigners_ and keeps the context's rules, caches and lifecycle.
	state.manualRoot = &cntx

	if config.EventLogger_ != nil {
		cntx.EventLogger_ = config.EventLogger_
	} else {
		cntx.EventLogger_ = eventLogger
	}

	cntx.Closed_ = &atomic.Value{}
	cntx.Closed_.Store(false)
	cntx.Closing_ = &atomic.Value{}
	cntx.Closing_.Store(false)
	cntx.Refreshing_ = &atomic.Value{}
	cntx.Refreshing_.Store(false)
	cntx.Ready_ = &atomic.Value{}
	cntx.Ready_.Store(false)
	cntx.Failed_ = &atomic.Value{}
	cntx.Failed_.Store(false)
	cntx.PendingCount_ = &atomic.Value{}
	cntx.PendingCount_.Store(int32(0))
	cntx.DataLock = &sync.RWMutex{}
	cntx.ContextLock_ = &sync.RWMutex{}
	cntx.EventLock_ = &sync.Mutex{}
	cntx.TimeoutLock_ = &sync.Mutex{}
	cntx.AssignmentCache = map[string]Assignment{}
	cntx.Achievements_ = make([]jsonmodels.GoalAchievement, 0)
	cntx.Exposures_ = make([]jsonmodels.Exposure, 0)
	cntx.Attributes_ = make([]interface{}, 0)

	var units = config.Units_
	if units != nil {
		_ = cntx.SetUnits(units)
	}

	cntx.Assigners_ = map[interface{}]interface{}{}
	cntx.Assigners_[contextStateKey{}] = state
	cntx.HashedUnits_ = map[interface{}]interface{}{}

	var attributes = config.Attributes_
	if attributes != nil {
		if err := cntx.SetAttributes(attributes); err != nil && cntx.EventLogger_ != nil {
			notifyLogger(cntx.EventLogger_, cntx, Error, errors.New("CreateContext: Failed to set attributes: "+err.Error()))
		}
	}

	var overrides = config.Overrides_
	if overrides != nil {
		if cntx.Overrides_ == nil {
			cntx.Overrides_ = map[interface{}]interface{}{}
		}
		for key, value := range overrides {
			cntx.Overrides_[key] = value
		}
	} else {
		cntx.Overrides_ = map[interface{}]interface{}{}
	}

	var cassignments = config.Cassigmnents_
	if cassignments != nil {
		if cntx.Cassignments_ == nil {
			cntx.Cassignments_ = map[interface{}]interface{}{}
		}
		for key, value := range cassignments {
			cntx.Cassignments_[key] = value
		}
	} else {
		cntx.Cassignments_ = map[interface{}]interface{}{}
	}

	if dataFuture.Ready() {
		var tmp = &cntx
		dataFuture.Listen(func(val future.Value, err error) {
			if err == nil {
				var result, ok = contextInputOf(val)
				if !ok {
					var invalid = &TypeError{Expected: "jsonmodels.ContextData or jsonmodels.ContextPayload", Actual: val}
					tmp.SetDataFailed(invalid)
					tmp.LogError(invalid)
					return
				}
				var diagnostics, panicErr = tmp.applyRefreshedData(result)
				if panicErr != nil {
					tmp.SetDataFailed(panicErr)
					tmp.dispatchDiagnostics(diagnostics)
					tmp.LogError(panicErr)
					return
				}
				tmp.dispatchDiagnostics(diagnostics)
				tmp.LogEvent(Ready, result.data)
			} else {
				tmp.SetDataFailed(err)
				tmp.LogError(err)
			}

		})
	} else {
		var tmp = &cntx
		var tempFuture, readyFutureDone = future.New()
		var loadOp = &gateOperation{}
		state.loadOp_ = loadOp
		cntx.setReadyFuture(tempFuture)
		// fail reports a failed initial load. It ends the load's executor
		// registration (executed) and settles the failed future before
		// clearing it, so a waiter that arrives in between joins it and sees
		// the error. Nothing after it reads Clock_.
		var fail = func(err error, diagnostics []bufferedEvent, executed func()) {
			tmp.SetDataFailed(err)
			tmp.dispatchDiagnostics(diagnostics)
			tmp.LogError(err)
			executed()
			readyFutureDone(nil, err)
			cntx.setReadyFutureWith(state, nil)
		}
		dataFuture.Listen(func(val future.Value, err error) {
			var executed = state.customAudiences.execute(loadOp)
			defer executed()
			atomic.StoreUint64(state.readyDeliverer_, goroutineID())
			defer atomic.StoreUint64(state.readyDeliverer_, 0)
			if err == nil {
				result, ok := contextInputOf(val)
				if !ok {
					fail(&TypeError{Expected: "jsonmodels.ContextData or jsonmodels.ContextPayload", Actual: val}, nil, executed)
					return
				}
				var diagnostics, panicErr = tmp.applyRefreshedData(result)
				if panicErr != nil {
					fail(panicErr, diagnostics, executed)
					return
				}
				// Log before settling, so WaitUntilReady returns after the
				// logger has seen the ready event.
				tmp.dispatchDiagnostics(diagnostics)
				tmp.LogEvent(Ready, result.data)
				// Finish everything that reads the context before releasing
				// waiters: once WaitUntilReady returns, the caller may replace
				// Clock_. ReadyFuture_ stays installed until then, so a waiter
				// that arrives meanwhile waits on it too.
				if tmp.GetPendingCount() > 0 {
					tmp.armTimeout()
				}
				cntx.setReadyFutureWith(state, nil)
				executed()
				readyFutureDone(result.data, nil)
			} else {
				fail(err, nil, executed)
			}

		})
	}

	return &cntx
}

func (c *Context) IsReady() bool {
	return c.Ready_.Load().(bool)
}

func (c *Context) IsFailed() bool {
	return c.Failed_.Load().(bool)
}

func (c *Context) IsClosed() bool {
	return c.Closed_.Load().(bool)
}

func (c *Context) IsClosing() bool {
	return !c.Closed_.Load().(bool) && c.Closing_.Load().(bool)
}

// WaitUntilReadyAsync and WaitUntilReady wait on the load future while one is
// pending, not on the ready flag: the flag is set before the ready event is
// logged, and the future settles after it. The Context a logger receives is a
// copy; on it they go by the flag, so a ready or error callback that waits for
// readiness does not wait for itself.
func (c *Context) WaitUntilReadyAsync() *future.Future {
	if ft := c.pendingReadyFuture(); ft != nil {
		return ft
	}
	return future.Call(func() (future.Value, error) {
		return c, nil
	})
}

func (c *Context) WaitUntilReady() Context {
	if ft := c.pendingReadyFuture(); ft != nil && !ft.Ready() {
		// A goroutine holding the custom audience gate (a deserializer
		// waiting for readiness) lends it to the initial load meanwhile.
		if op := c.live().state().loadOp_; op != nil {
			defer c.live().state().customAudiences.lend(op)()
		}
		ft.Join(context.Background())
	}
	return c.snapshot()
}

func (c *Context) pendingReadyFuture() *future.Future {
	if c.Ready_.Load().(bool) && (c.isSnapshot() || atomic.LoadUint64(c.live().state().readyDeliverer_) == goroutineID()) {
		return nil
	}
	return c.live().getReadyFuture()
}

func (c *Context) getReadyFuture() *future.Future {
	var st = c.state()
	st.stateLock_.Lock()
	defer st.stateLock_.Unlock()
	return c.ReadyFuture_
}

func (c *Context) setReadyFuture(f *future.Future) {
	c.setReadyFutureWith(c.state(), f)
}

// setReadyFutureWith is setReadyFuture with the state already resolved, for
// the ready callback: it runs after WaitUntilReady has returned, when the
// caller may be replacing Clock_, so it must not read Clock_ again.
func (c *Context) setReadyFutureWith(st *contextState, f *future.Future) {
	st.stateLock_.Lock()
	c.ReadyFuture_ = f
	st.stateLock_.Unlock()
}

// snapshot returns a copy of the context taken while holding every lock that
// guards a mutable field, so the copy handed to a callback does not race with
// writers. Lock order is ContextLock_, DataLock, EventLock_, TimeoutLock_,
// stateLock_. It must not be called while holding any of them.
func (c *Context) snapshot() Context {
	c = c.live()
	var st = c.state()
	if c.ContextLock_ != nil {
		c.ContextLock_.RLock()
		defer c.ContextLock_.RUnlock()
	}
	if c.DataLock != nil {
		c.DataLock.RLock()
		defer c.DataLock.RUnlock()
	}
	if c.EventLock_ != nil {
		c.EventLock_.Lock()
		defer c.EventLock_.Unlock()
	}
	if c.TimeoutLock_ != nil {
		c.TimeoutLock_.Lock()
		defer c.TimeoutLock_.Unlock()
	}
	st.stateLock_.Lock()
	defer st.stateLock_.Unlock()
	var cp = *c
	cp.Clock_ = &contextClock{Clock: c.Clock_, state: st, root: c, snapshot: true}
	return cp
}

// live returns the context SetData installs data on: the root of a copy or
// snapshot of an SDK-created context (also after its Clock_ was replaced),
// or c itself for a literal. Data, attribute and experiment getters read it,
// so reads through a copy agree with assignment evaluation.
func (c *Context) live() *Context {
	if clock := c.ownClock(); clock != nil {
		return clock.root
	}
	if c.ContextLock_ != nil {
		if root := c.state().manualRoot; root != nil {
			return root
		}
	}
	return c
}

func (c *Context) GetExperiments() ([]string, error) {
	c = c.live()
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	c.DataLock.RLock()
	var experimentNames = make([]string, len(c.Data_.Experiments))

	for i := 0; i < len(c.Data_.Experiments); i++ {
		experimentNames[i] = c.Data_.Experiments[i].Name
	}

	c.DataLock.RUnlock()
	return experimentNames, nil
}

func (c *Context) GetData() (jsonmodels.ContextData, error) {
	c = c.live()
	var err = c.CheckReady(true)
	if err != nil {
		return jsonmodels.ContextData{}, err
	}

	c.DataLock.RLock()
	defer c.DataLock.RUnlock()
	return c.Data_, nil

}

// GetPayload is GetData for the full context payload, with holdouts and each
// experiment's holdout and switchback fields, as installed: pass it to
// CreateContextWithPayload or SetPayload to hand the context's data over
// without losing them. After a plain ContextData was installed (SetData,
// CreateContextWith, or a provider without GetContextPayload) it is that
// data with no holdouts or switchbacks. It fails as GetData does.
//
// The experiment and holdout slices are copies, but, as with GetData, the
// values inside them (variants, splits, holdout ids, schedule numbers and
// cluster maps) are shared with the context and must not be modified.
func (c *Context) GetPayload() (jsonmodels.ContextPayload, error) {
	c = c.live()
	if err := c.CheckReady(true); err != nil {
		return jsonmodels.ContextPayload{}, err
	}
	var st = c.state()
	c.DataLock.RLock()
	defer c.DataLock.RUnlock()
	if !st.payloadInstalled_ {
		return jsonmodels.PayloadOf(c.Data_), nil
	}
	// An installed empty list stays empty, so it encodes as [] and not null.
	return jsonmodels.ContextPayload{Experiments: copyExperimentData(st.payloadExperiments_),
		Holdouts: copyHoldouts(st.payloadHoldouts_)}, nil
}

// copyFloat64, copyExperimentData and copyHoldouts copy a slice, keeping nil
// nil and empty empty: a copied payload encodes as the original did, and a
// switchback's split snapshot compares equal to the split it was taken from.
func copyFloat64(values []float64) []float64 {
	if values == nil {
		return nil
	}
	return append(make([]float64, 0, len(values)), values...)
}

func copyExperimentData(experiments []jsonmodels.ExperimentData) []jsonmodels.ExperimentData {
	if experiments == nil {
		return nil
	}
	return append(make([]jsonmodels.ExperimentData, 0, len(experiments)), experiments...)
}

func copyHoldouts(holdouts []jsonmodels.Holdout) []jsonmodels.Holdout {
	if holdouts == nil {
		return nil
	}
	return append(make([]jsonmodels.Holdout, 0, len(holdouts)), holdouts...)
}

// SetOverride is allowed after the context is finalized, like the other
// ABsmartly SDKs: it only affects later reads and queues nothing.
func (c *Context) SetOverride(experimentName string, variant int) error {
	PutRW(c.ContextLock_, c.Overrides_, experimentName, variant)
	return nil
}

func (c *Context) GetOverride(experimentName string) (int, error) {
	var result = GetRW(c.ContextLock_, c.Overrides_, experimentName)
	if result != nil {
		return result.(int), nil
	} else {
		return -1, errors.New("override not found")
	}

}

func (c *Context) SetOverrides(overrides map[string]int) error {
	for key, value := range overrides {
		var err = c.SetOverride(key, value)
		if err != nil {
			return err
		}
	}
	return nil
}

// SetCustomAssignment returns the closed or closing error after Close, as in
// v1.1.1 and the JS and Java SDKs. Unlike SetOverride, it is not allowed then.
func (c *Context) SetCustomAssignment(experimentName string, variant int) error {
	if err := c.CheckNotClosed(); err != nil {
		return err
	}
	PutRW(c.ContextLock_, c.Cassignments_, experimentName, variant)
	return nil
}

func (c *Context) GetCustomAssignment(experimentName string) (int, error) {
	var result = GetRW(c.ContextLock_, c.Cassignments_, experimentName)
	if result != nil {
		return result.(int), nil
	} else {
		return -1, errors.New("customAssignment not found")
	}
}

func (c *Context) SetCustomAssignments(customAssignments map[string]int) error {
	for key, value := range customAssignments {
		var err = c.SetCustomAssignment(key, value)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Context) SetUnit(unitType string, uid string) error {
	return c.setUnit(unitType, uid)
}

// SetUnitValue sets a unit from a string or a number. The value is kept as
// given, so GetUnit and GetUnits return a number for a numeric unit. A number
// is hashed and published as its rounded integer, like the other ABsmartly SDKs.
func (c *Context) SetUnitValue(unitType string, uid interface{}) error {
	switch v := uid.(type) {
	case string:
		return c.setUnit(unitType, v)
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return c.setUnit(unitType, v)
	default:
		var err = c.CheckNotClosed()
		if err != nil {
			return err
		}
		return fmt.Errorf("Unit '%s' must be a string or a number.", unitType) //nolint:staticcheck // canonical SDK error text
	}
}

func (c *Context) setUnit(unitType string, uid interface{}) error {
	c = c.live()
	var st = c.state()
	var err = c.CheckNotClosed()
	if err != nil {
		return err
	}

	var value interface{}
	var uidString string
	if s, ok := uid.(string); ok {
		uidString = strings.TrimSpace(s)
		value = uidString
	} else {
		var number = toFloat64(uid)
		uidString = numericUnitString(number)
		value = number
	}

	c.ContextLock_.Lock()
	defer c.ContextLock_.Unlock()
	var previous, exist = st.unitValues_[unitType]
	if !exist {
		if s, found := c.Units_[unitType]; found {
			previous, exist = s, true
		}
	}
	// v1.1.1 order: a unit that is already set is reported before a blank UID.
	if exist && previous != value {
		return &UnitError{UnitType: unitType, Err: ErrUnitAlreadySet}
	}
	if len(uidString) == 0 {
		return &UnitError{UnitType: unitType, Err: ErrUnitBlank}
	}

	c.Units_[unitType] = uidString
	st.unitValues_[unitType] = value
	c.evictAssignmentsMissingUnit(st, unitType)
	return nil
}

// evictAssignmentsMissingUnit drops the cached assignments that were resolved
// without unitType's unit, so they are resolved with the unit now that it is
// set: split-rule assignments, which are then hashed with the unit, and
// assignments with a holdout arm that could not be drawn. Assign rules never
// need the unit and keep their assignment. An exposed assignment is kept:
// re-resolving it would switch the user's variant and queue a second,
// conflicting exposure. An evicted assignment is marked exposed so a caller
// still holding it cannot queue it. The caller holds ContextLock_ for
// writing.
func (c *Context) evictAssignmentsMissingUnit(st *contextState, unitType string) {
	for name, entry := range st.assignmentCache_ {
		var assignment = entry.assignment
		if entry.switchback != nil && entry.switchback.serving.status == switchbackAbsent {
			// A unit no serving source reads leaves the slot as it is.
			if indexed := st.indexedExperiment(assignment.Name); indexed == nil || indexed.switchback == nil ||
				!c.switchbackServingUnitSet(indexed) {
				continue
			}
			// A switchback resolved before any linked unit was set is
			// resolved again now that one may be. One that is not overridden
			// never published anything, so it is dropped even if exposed. An
			// overridden one is evicted like any entry missing its unit:
			// retired before it is claimed, under EventLock_, so a read that
			// resolved it and loses the claim resolves again and exposes the
			// replacement; an exposed one is kept.
			var evict = !assignment.Overridden
			if !evict {
				c.EventLock_.Lock()
				var retired = entry.retirement.Swap(true)
				if beforeEvictionClaim != nil {
					beforeEvictionClaim(entry)
				}
				evict = assignment.Exposed.CompareAndSwap(false, true)
				if evict && afterEvictionClaim != nil {
					afterEvictionClaim(entry)
				}
				if !evict {
					entry.retirement.Store(retired)
				}
				c.EventLock_.Unlock()
			}
			if evict {
				c.retireSwitchbackSlot(st, assignment.Name, name)
				delete(c.AssignmentCache, assignment.Name)
			}
			continue
		}
		if assignment.UnitType != unitType {
			continue
		}
		if !entry.missingUnit() {
			continue
		}
		// Retired before the exposed flag is set, as a replacement does,
		// so a read whose claim loses to this eviction always sees the
		// retirement and resolves again with the unit. An exposed or
		// claimed assignment is kept, and stays unretired.
		var evict = !assignment.Exposed.Load().(bool)
		if evict {
			entry.retirement.Store(true)
			if beforeEvictionClaim != nil {
				beforeEvictionClaim(entry)
			}
			evict = assignment.Exposed.CompareAndSwap(false, true)
			if evict && afterEvictionClaim != nil {
				afterEvictionClaim(entry)
			}
			if !evict {
				// Claimed meanwhile by a read already past the lock-free CAS:
				// that read holds the claim and admits it; keep the entry.
				entry.retirement.Store(false)
			}
		}
		if evict {
			delete(st.assignmentCache_, name)
			delete(c.AssignmentCache, name)
		}
	}
}

// numericUnitString is the string a numeric UID is hashed as: the number
// rounded to an integer, like JavaScript's toFixed(0).
func numericUnitString(number float64) string {
	return strconv.FormatFloat(math.Round(number), 'f', 0, 64)
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int8:
		return float64(n)
	case int16:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case uint:
		return float64(n)
	case uint8:
		return float64(n)
	case uint16:
		return float64(n)
	case uint32:
		return float64(n)
	case uint64:
		return float64(n)
	}
	return 0
}

// GetUnit returns the UID of a unit type, or nil when it is not set.
func (c *Context) GetUnit(unitType string) interface{} {
	c = c.live()
	var st = c.state()
	c.ContextLock_.RLock()
	defer c.ContextLock_.RUnlock()
	if v, ok := st.unitValues_[unitType]; ok {
		return v
	}
	if s, ok := c.Units_[unitType]; ok {
		return s
	}
	return nil
}

// GetUnits returns a copy of all units, each as it was set.
func (c *Context) GetUnits() map[string]interface{} {
	c = c.live()
	var st = c.state()
	c.ContextLock_.RLock()
	defer c.ContextLock_.RUnlock()
	var result = make(map[string]interface{}, len(c.Units_))
	for k, s := range c.Units_ {
		if v, ok := st.unitValues_[k]; ok {
			result[k] = v
		} else {
			result[k] = s
		}
	}
	return result
}

func (c *Context) SetUnits(units map[string]string) error {
	for key, value := range units {
		var err = c.SetUnit(key, value)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Context) SetAttribute(name string, value interface{}) error {
	c = c.live()
	var st = c.state()
	var err = c.CheckNotClosed()
	if err != nil {
		return err
	}

	var attribute = jsonmodels.Attribute{Name: name, Value: value, SetAt: c.Clock_.Millis()}
	c.ContextLock_.Lock()
	c.Attributes_ = append(c.Attributes_, attribute)
	atomic.AddInt32(&st.attrsSeq_, 1)
	c.ContextLock_.Unlock()
	return nil
}

// GetAttribute returns the last value set for an attribute, or nil.
func (c *Context) GetAttribute(name string) interface{} {
	c = c.live()
	c.ContextLock_.RLock()
	defer c.ContextLock_.RUnlock()
	for i := len(c.Attributes_) - 1; i >= 0; i-- {
		if a, ok := c.Attributes_[i].(jsonmodels.Attribute); ok && a.Name == name {
			return a.Value
		}
	}
	return nil
}

// GetAttributes returns the last value set for each attribute.
func (c *Context) GetAttributes() map[string]interface{} {
	c = c.live()
	c.ContextLock_.RLock()
	defer c.ContextLock_.RUnlock()
	var result = map[string]interface{}{}
	for _, attr := range c.Attributes_ {
		if a, ok := attr.(jsonmodels.Attribute); ok {
			result[a.Name] = a.Value
		}
	}
	return result
}

// evaluationAttributes builds the attribute map audiences are evaluated
// against. The system attributes application and environment go in first, and
// user-set attributes are overlaid on top, so a user-set value of the same
// name always wins, even one set after an assignment was cached (the attrsSeq_
// bump invalidates it). Empty system values are left out rather than set to an
// empty string, so a filter on a name that is not configured does not match.
// These values are never published as attributes; the collector adds them to
// exposures server-side. The caller must hold ContextLock_.
func (c *Context) evaluationAttributes() map[string]interface{} {
	return c.evaluationAttributesWithState(c.state())
}

func (c *Context) evaluationAttributesWithState(st *contextState) map[string]interface{} {
	var attrs = map[string]interface{}{}
	if len(st.application_) > 0 {
		attrs["application"] = st.application_
	}
	if len(st.environment_) > 0 {
		attrs["environment"] = st.environment_
	}
	for _, v := range c.Attributes_ {
		var attribute = v.(jsonmodels.Attribute)
		attrs[attribute.Name] = attribute.Value
	}
	return attrs
}

func (c *Context) SetAttributes(attributes map[string]interface{}) error {
	for key, value := range attributes {
		var err = c.SetAttribute(key, value)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Context) GetTreatment(experimentName string) (int, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return -1, err
	}
	var at = clockReading{clock: c.Clock_}
	var entry = c.getAssignmentAt(experimentName, &at)
	if afterReadResolve != nil {
		afterReadResolve()
	}
	if entry.needsSettling() {
		entry, _ = c.settleExposureAt(experimentName, entry, nil, &at)
	}
	return entry.assignment.Variant, nil
}

// maxExposureAttempts bounds how many assignments a read tries to expose when
// each is retired by a concurrent replacement before the read claims it, as
// in the Java SDK.
const maxExposureAttempts = 3

// settleExposureAt exposes entry on a read. If a replacement or an eviction
// retired the assignment before the read claimed it, nothing of it was
// published, so the current assignment is resolved again and exposed
// instead, up to maxExposureAttempts times. It returns the entry the read
// must serve: the replacement that was exposed, or on exhaustion the last
// one tried. A non-nil eligible is checked with each new claim, atomically
// with it; when it fails nothing is claimed and settleExposureAt reports false.
// Each retry resolves at a fresh clock reading, which stamps a switchback
// exposure, as in the Java SDK. Loggers run after admit released its locks.
func (c *Context) settleExposureAt(experimentName string, entry *cachedAssignment,
	eligible func(*cachedAssignment) exposureStatus, at *clockReading) (*cachedAssignment, bool) {
	for attempt := 1; ; attempt++ {
		if beforeSettleClaim != nil {
			beforeSettleClaim()
		}
		var status = c.queueEligibleExposureAt(entry, eligible, at)
		if status == exposureIneligible {
			return entry, false
		}
		if status != exposureRetired || attempt >= maxExposureAttempts {
			return entry, true
		}
		// Reset in place: the caller's later candidates (a shared variable
		// key's fall-through) resolve and stamp at the retry's reading, never
		// at the one the retry discarded.
		*at = clockReading{clock: c.Clock_}
		entry = c.getAssignmentAt(experimentName, at)
	}
}

// queueEligibleExposureAt is queueExposureAt for a read: a non-nil eligible is
// checked under ContextLock_ together with the claim, so a refresh can never
// land between the check and the claim. A claim already taken is honoured.
func (c *Context) queueEligibleExposureAt(entry *cachedAssignment, eligible func(*cachedAssignment) exposureStatus, at *clockReading) exposureStatus {
	if eligible == nil {
		return c.queueExposureAt(entry.assignment, entry, at)
	}
	c = c.live()
	if status := c.claimEligible(entry, eligible); status != exposureClaimed {
		return status
	}
	return c.admitExposureAt(entry.assignment, entry, at)
}

// claimEligible checks eligibility and claims the exposure in one step under
// ContextLock_, so no refresh can land between the check and the claim. The
// lock is released on unwind too: the audience evaluator can panic. A claim
// already taken is honoured.
func (c *Context) claimEligible(entry *cachedAssignment, eligible func(*cachedAssignment) exposureStatus) exposureStatus {
	c.ContextLock_.RLock()
	defer c.ContextLock_.RUnlock()
	if beforeEligibleClaim != nil {
		beforeEligibleClaim()
	}
	if entry.retirement.Load() {
		return exposureRetired
	}
	if entry.assignment.Exposed.Load().(bool) {
		return exposureQueued
	}
	if status := eligible(entry); status != exposureClaimed {
		return status
	}
	var claimed = entry.assignment.Exposed.CompareAndSwap(false, true)
	if afterEligibleCAS != nil {
		afterEligibleCAS()
	}
	if claimed {
		return exposureClaimed
	}
	return exposureQueued
}

// sameDecision compares the resolved experiment run as well as its exposures:
// the wire record alone cannot distinguish experiment iterations. Variables
// may be refreshed without changing the variant or its measurement identity.
func (c *Context) sameDecision(a *cachedAssignment, b *cachedAssignment) bool {
	if a.retirement.Load() || a.resolvedId != b.resolvedId || a.resolvedIteration != b.resolvedIteration ||
		a.resolvedUnitType != b.resolvedUnitType || a.assignment.FullOnVariant != b.assignment.FullOnVariant ||
		!sameSplit(a.assignment.TrafficSplit, b.assignment.TrafficSplit) ||
		c.exposureFor(a.assignment, a.ruleOverride, 0) != c.exposureFor(b.assignment, b.ruleOverride, 0) ||
		(a.holdouts == nil) != (b.holdouts == nil) {
		return false
	}
	if a.holdouts == nil {
		return true
	}
	if a.holdouts.suppressed != b.holdouts.suppressed || len(a.holdouts.arms) != len(b.holdouts.arms) {
		return false
	}
	for i, arm := range a.holdouts.arms {
		if arm != b.holdouts.arms[i] {
			return false
		}
	}
	return true
}

// needsSettling reports whether an exposing read must settle the entry's
// exposure: it is not exposed yet, or it was retired (marked exposed by a
// replacement or an eviction without being recorded). A warmed, exposed,
// unretired read skips settling, as before holdouts.
func (e *cachedAssignment) needsSettling() bool {
	return !e.assignment.Exposed.Load().(bool) || e.retirement.Load()
}

// variableEligible is the variable read's claim check: once the context has
// seen holdouts, assignment rules or switchbacks (sources of retirement), a read
// of key claims an exposure only for an experiment that, as installed now,
// still exists and defines key in some variant, and only while the entry is
// still the current decision for that definition by the cache's own rules
// (identity, full-on variant, traffic, holdout epochs, overrides, assignment
// rules and audience). It reports exposureClaimed when the claim may be taken,
// exposureIneligible when the experiment is no longer a candidate for key, and
// exposureRetired when it still is but this decision is no longer current, so
// the read resolves again. Contexts with none of them keep the released behaviour.
// The caller holds ContextLock_.
func (c *Context) variableEligible(experimentName string, key string) func(*cachedAssignment) exposureStatus {
	// The live root, so a readiness copy is validated against the live
	// attributes, and state resolved before the caller takes ContextLock_.
	c = c.live()
	var st = c.state()
	return func(e *cachedAssignment) exposureStatus {
		return c.keyEligible(st, experimentName, key, e)
	}
}

// keyEligible is variableEligible's check, as a method so the arm-only read
// can run it without building a closure. The caller holds ContextLock_.
func (c *Context) keyEligible(st *contextState, experimentName string, key string, e *cachedAssignment) exposureStatus {
	var index = st.loadIndex()
	if index == nil || !index.keyed {
		return exposureClaimed
	}
	var indexed = index.experiments[experimentName]
	if indexed == nil {
		return exposureIneligible
	}
	if _, defined := indexed.variableKeys[key]; !defined {
		return exposureIneligible
	}
	if _, state := c.validEntry(st, experimentName, indexed, e, true, nil); state == assignmentStale {
		// Still a candidate, but this decision is no longer the current one:
		// resolve again rather than claim it or skip the experiment.
		return exposureRetired
	}
	return exposureClaimed
}

// afterEvictionClaim, when set by a test, runs right after an eviction marks
// an entry exposed; beforeEvictionClaim runs between
// its retirement store and that claim. beforeReplacementClaim and
// afterReplacementClaim do the same for a replacement.
var (
	afterReadResolve       func()
	afterEvictionClaim     func(*cachedAssignment)
	beforeEvictionClaim    func(*cachedAssignment)
	beforeReplacementClaim func(*cachedAssignment)
	afterReplacementClaim  func(*cachedAssignment)
)

// beforeSettleClaim, when set by a test, runs before each claim in
// settleExposure, between resolving the assignment and claiming its exposure.
// beforeEligibleClaim and afterEligibleCAS run in claimEligible, under
// ContextLock_, before the eligibility check and right after the claim.
// beforeHoldoutArms runs as queueHoldoutArms starts, before it takes
// ContextLock_.
var (
	afterEligibleCAS    func()
	beforeSettleClaim   func()
	beforeEligibleClaim func()
	beforeHoldoutArms   func()
)

// QueueExposure queues the assignment's exposure once, then the exposures of
// the holdouts covering it that are not exposed yet, in holdout id order. A
// suppressed assignment queues only its holdouts' exposures.
//
// A switchback assignment is stamped with the current clock reading while it
// is inside the period the assignment was resolved for, and with the reading
// that resolved it otherwise, so analysis places it in the period whose
// variant it carries.
func (c *Context) QueueExposure(assignment *Assignment) {
	if assignment.Exposed.Load().(bool) {
		return
	}
	c = c.live()
	var entry *cachedAssignment
	// Public Assignment copies use Exposed as their identity to recover the
	// cached retirement token, pinned holdouts and switchback slot. This
	// lookup is required even before the first holdout arrives: a refresh may
	// add coverage while a caller-held exposure is in flight. A literal
	// without ContextLock_ has no cache to look in, as in v1.1.1.
	if c.ContextLock_ != nil {
		var st = c.state()
		c.ContextLock_.RLock()
		entry = st.cachedEntryFor(assignment)
		c.ContextLock_.RUnlock()
	}
	c.queueExposure(assignment, entry)
}

// cachedEntryFor finds the cache entry holding assignment, or nil: the
// experiment's slot, one of its switchback period slots, or a removed
// switchback slot a caller may still hold. The caller holds ContextLock_.
func (st *contextState) cachedEntryFor(assignment *Assignment) *cachedAssignment {
	if entry, found := st.assignmentCache_[assignment.Name]; found && entry.assignment.Exposed == assignment.Exposed {
		return entry
	}
	if epoch := st.switchbackEpochs_[assignment.Name]; epoch != nil {
		for _, period := range epoch.periods {
			var entry = st.assignmentCache_[switchbackSlotKey(assignment.Name, period)]
			if entry != nil && entry.assignment.Exposed == assignment.Exposed {
				return entry
			}
		}
	}
	for _, entry := range st.retiredSwitchbackSlots_[assignment.Name] {
		if entry.assignment.Exposed == assignment.Exposed {
			return entry
		}
	}
	return nil
}

// exposureStatus is the outcome of admitting a claimed exposure.
type exposureStatus int

const (
	// exposureQueued: the decision was queued, or there was nothing to queue.
	exposureQueued exposureStatus = iota
	exposureRejected
	// exposureIneligible: the read's eligibility check failed, so nothing was
	// claimed.
	exposureIneligible
	exposureClaimed
	// exposureRetired: the claim failed because a replacement or an eviction
	// retired the assignment and marked it exposed; nothing of it was
	// published, so the read must resolve again.
	exposureRetired
)

func (c *Context) queueExposure(assignment *Assignment, entry *cachedAssignment) exposureStatus {
	return c.queueExposureAt(assignment, entry, nil)
}

// queueExposureAt is queueExposure with the clock reading the assignment was
// resolved at, which stamps a switchback exposure; nil when the caller has
// none (QueueExposure).
func (c *Context) queueExposureAt(assignment *Assignment, entry *cachedAssignment, at *clockReading) exposureStatus {
	c = c.live()
	if !assignment.Exposed.CompareAndSwap(false, true) {
		// Already exposed, or retired by a replacement that landed after the
		// read resolved this entry and before it claimed it.
		if entry != nil && entry.retirement.Load() {
			return exposureRetired
		}
		return exposureQueued
	}
	return c.admitExposureAt(assignment, entry, at)
}

// admitExposure queues a claimed decision or rolls it back after a rejected
// close. The entry supplies the pinned holdouts and its retirement.
func (c *Context) admitExposure(assignment *Assignment, entry *cachedAssignment) exposureStatus {
	return c.admitExposureAt(assignment, entry, nil)
}

func (c *Context) admitExposureAt(assignment *Assignment, entry *cachedAssignment, at *clockReading) exposureStatus {
	var holdouts *assignmentHoldouts
	var slot *switchbackSlot
	var ruleOverride bool
	var st = c.state()
	if entry != nil {
		holdouts, slot, ruleOverride = entry.holdouts, entry.switchback, entry.ruleOverride
	} else {
		// No cache entry holds it any more: the rule flag of every assignment
		// handed out is remembered by its exposed flag.
		ruleOverride = st.ruleIdentities.contains(assignment.Exposed)
	}
	var status = c.admit(assignment, ruleOverride, holdouts, slot, at)
	if status == exposureRejected {
		if c.ContextLock_ == nil {
			// A literal without ContextLock_ has no cache to update.
			assignment.Exposed.Store(false)
			return status
		}
		c.ContextLock_.Lock()
		c.reopenExposure(st, assignment, entry)
		c.ContextLock_.Unlock()
	}
	return status
}

// admit queues the claimed exposures, and reports exposureRejected when a
// close rejected them. A claim always commits, even if a replacement retired
// its assignment meanwhile, as in the Java SDK: another read may already be
// serving that decision because the claim marked it exposed.
//
// ruleOverride is whether an assignment rule chose the variant. slot and at
// are the switchback slot (nil for a unit experiment) and the reading that
// resolved it: a silent switchback resolution publishes nothing, and a
// switchback exposure carries the reading of its period.
func (c *Context) admit(assignment *Assignment, ruleOverride bool, holdouts *assignmentHoldouts, slot *switchbackSlot,
	at *clockReading) exposureStatus {
	if holdouts == nil {
		if slot.isSilent() {
			return exposureQueued
		}
		if !c.enqueueExposure(assignment, ruleOverride, c.exposedAt(slot, at)) {
			return exposureRejected
		}
		return exposureQueued
	}

	var exposedAt = c.Clock_.Millis()
	// Admit the entire decision before any logger can close the context. Arm
	// marks are made under the same lock as their append, so another decision
	// can never confuse an in-flight arm with an accepted exposure.
	c.EventLock_.Lock()
	if c.CheckNotClosed() != nil {
		c.EventLock_.Unlock()
		return exposureRejected
	}
	var exposures []jsonmodels.Exposure
	if !holdouts.isSuppressed() {
		exposures = append(exposures, c.exposureFor(assignment, ruleOverride, exposedAt))
	}
	for _, arm := range holdouts.arms {
		if arm != nil && arm.assignment.Exposed.CompareAndSwap(false, true) {
			exposures = append(exposures, c.exposureFor(arm.assignment, false, exposedAt))
		}
	}
	c.PendingCount_.Store(c.PendingCount_.Load().(int32) + int32(len(exposures)))
	c.Exposures_ = append(c.Exposures_, exposures...)
	c.EventLock_.Unlock()
	for _, exposure := range exposures {
		c.LogEvent(Exposure, exposure)
	}
	if len(exposures) != 0 {
		c.SetTimeout()
	}
	return exposureQueued
}

// reopenExposure rolls back a rejected admission unless a replacement retired
// its assignment meanwhile: the replacement, not the superseded decision, is
// exposed by the next read. A decision resolved without its unit
// is resolved again instead if the unit arrived meanwhile. The caller holds
// ContextLock_ for writing.
func (c *Context) reopenExposure(st *contextState, assignment *Assignment, claimed *cachedAssignment) {
	if claimed != nil && claimed.retirement.Load() {
		return
	}
	if claimed != nil && claimed.switchback != nil && claimed.switchback.serving.status == switchbackAbsent {
		// Resolved before any serving unit was set: if one has arrived, the
		// next read resolves again under it. The serving sources are the
		// readable cluster map's, or the declared unitType on the legacy path.
		var indexed = st.indexedExperiment(assignment.Name)
		if indexed != nil && indexed.switchback != nil && c.switchbackServingUnitSet(indexed) {
			claimed.retirement.Store(true)
			return
		}
	}
	var entry = st.assignmentCache_[assignment.Name]
	if entry != nil && entry.assignment.Exposed == assignment.Exposed {
		if _, unitSet := c.Units_[assignment.UnitType]; unitSet && entry.missingUnit() {
			entry.retirement.Store(true)
			delete(st.assignmentCache_, assignment.Name)
			delete(c.AssignmentCache, assignment.Name)
			return
		}
	}
	assignment.Exposed.Store(false)
}

// missingUnit reports whether the assignment was resolved without its unit:
// a split rule that matched without it, or a holdout arm it could not draw.
func (e *cachedAssignment) missingUnit() bool {
	if e.ruleOverride && e.ruleMissingUnit {
		return true
	}
	if e.holdouts != nil {
		for _, arm := range e.holdouts.arms {
			if arm == nil {
				return true
			}
		}
	}
	return false
}

func (c *Context) exposureFor(assignment *Assignment, ruleOverride bool, exposedAt int64) jsonmodels.Exposure {
	return jsonmodels.Exposure{Id: assignment.Id, Name: assignment.Name, Unit: assignment.UnitType,
		Variant: assignment.Variant, ExposedAt: exposedAt, Assigned: assignment.Assigned,
		Eligible: assignment.Eligible, Overridden: assignment.Overridden, FullOn: assignment.FullOn,
		Custom: assignment.Custom, AudienceMismatch: assignment.AudienceMismatch, RuleOverride: ruleOverride}
}

// exposedAt is the time an exposure is stamped with. A unit experiment's is
// the clock reading at enqueue time. A switchback's is the reading that
// selected its period (see QueueExposure when there is none): analysis
// derives the period from exposedAt, so a later reading could put the
// variant in the next period.
func (c *Context) exposedAt(slot *switchbackSlot, at *clockReading) int64 {
	if slot == nil {
		return c.Clock_.Millis()
	}
	if at != nil {
		return at.now()
	}
	return slot.stamp(c.Clock_.Millis())
}

// enqueueExposure queues an exposure for an assignment already marked
// exposed. When the context is closed it reports false, and the caller
// clears the mark so a later read can retry.
func (c *Context) enqueueExposure(assignment *Assignment, ruleOverride bool, exposedAt int64) bool {
	var exposure = c.exposureFor(assignment, ruleOverride, exposedAt)

	c.EventLock_.Lock()
	// Close seals the context under EventLock_, so this check and the
	// append cannot interleave with the seal: an event is queued before it
	// or rejected after it, never stranded behind it.
	if c.CheckNotClosed() != nil {
		c.EventLock_.Unlock()
		return false
	}
	c.PendingCount_.Store(c.PendingCount_.Load().(int32) + 1)
	c.Exposures_ = append(c.Exposures_, exposure)
	c.EventLock_.Unlock()

	c.LogEvent(Exposure, exposure)

	c.SetTimeout()
	return true
}

func (c *Context) PeekTreatment(experimentName string) (int, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return -1, err
	}

	return c.getAssignment(experimentName).assignment.Variant, nil
}

// GetVariableKeys returns, for each variable key, the name of the first
// experiment that defines it. Use GetVariableKeysAll to get every experiment.
func (c *Context) GetVariableKeys() (map[string]string, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var variableKeys = map[string]string{}
	if literal := c.legacyVariableReader(); literal != nil {
		literal.DataLock.RLock()
		for key, value := range literal.IndexVariables_ {
			if key, ok := key.(string); ok {
				if experiment, ok := value.(ExperimentVariables); ok {
					variableKeys[key] = experiment.Data.Name
				}
			}
		}
		literal.DataLock.RUnlock()
		return variableKeys, nil
	}
	var st = c.state()

	c.DataLock.RLock()
	for key, experiments := range st.indexVariables_ {
		if len(experiments) > 0 {
			variableKeys[key] = experiments[0].Data.Name
		}
	}
	c.DataLock.RUnlock()
	return variableKeys, nil
}

// GetVariableKeysAll returns, for each variable key, the names of all
// experiments that define it.
func (c *Context) GetVariableKeysAll() (map[string][]string, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var variableKeys = map[string][]string{}
	if literal := c.legacyVariableReader(); literal != nil {
		literal.DataLock.RLock()
		for key, value := range literal.IndexVariables_ {
			if key, ok := key.(string); ok {
				if experiment, ok := value.(ExperimentVariables); ok {
					variableKeys[key] = []string{experiment.Data.Name}
				}
			}
		}
		literal.DataLock.RUnlock()
		return variableKeys, nil
	}
	var st = c.state()

	c.DataLock.RLock()
	for key, experiments := range st.indexVariables_ {
		var names = make([]string, 0, len(experiments))
		for _, value := range experiments {
			names = append(names, value.Data.Name)
		}
		variableKeys[key] = names
	}
	c.DataLock.RUnlock()
	return variableKeys, nil
}

func removeDuplicateValues(intSlice []string) []string {
	keys := make(map[string]bool)
	var list []string

	for _, entry := range intSlice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func (c *Context) GetCustomFieldValueKeys() ([]string, error) {
	c = c.live()
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var keys []string

	c.DataLock.Lock()

	for _, experiment := range c.Data_.Experiments {
		var customFieldValues = experiment.CustomFieldValues
		if customFieldValues != nil {
			for _, customFieldValue := range customFieldValues {
				keys = append(keys, customFieldValue.Name)
			}
		}
	}
	c.DataLock.Unlock()

	sort.Strings(keys)

	return removeDuplicateValues(keys), nil
}

// GetCustomFieldValue is the legacy convenience API; it returns nil on errors.
// Use GetCustomFieldValueE to receive readiness and finalization errors.
func (c *Context) GetCustomFieldValue(experimentName string, key string) interface{} {
	var value, _ = c.GetCustomFieldValueE(experimentName, key)
	return value
}

// GetCustomFieldValueE returns a custom field value or the context read error.
func (c *Context) GetCustomFieldValueE(experimentName string, key string) (interface{}, error) {
	c = c.live()
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}
	c.DataLock.Lock()
	var customFieldValues = c.ContextCustomFields_[experimentName]

	var value interface{} = nil
	if customFieldValues != nil {
		field, ok := customFieldValues[key]
		if ok {
			value = field.Value
		}
	}
	c.DataLock.Unlock()

	return value, nil
}

func (c *Context) GetCustomFieldValueType(experimentName string, key string) string {
	c = c.live()
	c.DataLock.Lock()
	defer c.DataLock.Unlock()

	var customFieldValues = c.ContextCustomFields_[experimentName]
	var fieldType string
	if customFieldValues != nil {
		field, ok := customFieldValues[key]
		if ok {
			fieldType = field.Type
		}
	}

	return fieldType
}

func (c *Context) GetVariableValue(key string, defaultValue interface{}) (interface{}, error) {
	return c.resolveVariableValue(key, defaultValue, true)
}

func (c *Context) PeekVariableValue(key string, defaultValue interface{}) (interface{}, error) {
	return c.resolveVariableValue(key, defaultValue, false)
}

// resolveVariableValue resolves a shared variable key as the Java SDK's
// Context.getVariableAssignment does. The candidates are the experiments any
// of whose variants define key, in experiment-id order (see indexVariables_).
// The winner is the first candidate the unit is assigned to, overridden in or
// rule-assigned to; its selected variant supplies the value, or the caller's
// default when that variant lacks key. Later candidates are never consulted,
// so which variant the unit got cannot move it to another experiment. Only
// the winner is exposed, and only when expose is set: the candidates checked
// before it were never shown to the unit. With no winner the caller's default
// is returned and nothing is exposed.
//
// Holdouts follow the Java SDK too. A candidate a holdout suppresses is not
// assigned, so it never wins; when nothing wins, the first suppressed
// candidate met supplies its control value and is exposed, which publishes
// only its holdouts. When expose is set, every candidate visited fires the
// holdouts covering it, since each was evaluated.
//
// A true Context{...} literal keeps v1.1.1's rule instead: it reads the one
// experiment its current IndexVariables_ maps the key to, exposes it, and
// returns the value whenever the assignment's variables have the key.
func (c *Context) resolveVariableValue(key string, defaultValue interface{}, expose bool) (interface{}, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}
	if literal := c.legacyVariableReader(); literal != nil {
		return literal.resolveLiteralVariableValue(key, defaultValue, expose)
	}
	c = c.live()
	var st = c.state()

	c.DataLock.RLock()
	var experiments = st.indexVariables_[key]
	c.DataLock.RUnlock()

	var suppressedFallback *cachedAssignment
	// One clock reading resolves and stamps every experiment on the key.
	var at = clockReading{clock: c.Clock_}
	for i := range experiments {
		var name = experiments[i].Data.Name
		var entry = c.getAssignmentAt(name, &at)
		if expose {
			if afterReadResolve != nil {
				afterReadResolve()
			}
			var current bool
			if entry, current = c.exposeVariableCandidate(name, key, entry, &at); !current {
				// No longer a candidate for key: the next one is.
				continue
			}
		}
		if entry.wins() {
			return variableOf(entry, key, defaultValue), nil
		}
		if suppressedFallback == nil && entry.holdouts.isSuppressed() {
			suppressedFallback = entry
		}
	}
	if suppressedFallback != nil {
		return variableOf(suppressedFallback, key, defaultValue), nil
	}
	return defaultValue, nil
}

// exposeVariableCandidate exposes a candidate the exposing variable read
// visits, and returns the decision the read must use and whether the
// experiment is still a candidate for key. A winner publishes its own
// exposure and its holdouts'; a suppressed candidate only its holdouts'. A
// candidate that neither wins nor is suppressed claims nothing: only its
// holdouts' exposures fire, as in the Java SDK, so a later GetTreatment still
// exposes it. Once the context has seen holdouts, switchbacks or assignment
// rules, a decision a refresh, SetUnit or attribute change superseded during
// the read is resolved again and the current one used instead, up to
// maxExposureAttempts times, each retry at a fresh clock reading.
func (c *Context) exposeVariableCandidate(experimentName string, key string, entry *cachedAssignment,
	at *clockReading) (*cachedAssignment, bool) {
	// Built only once a claim or an arm check needs it, so a warmed read that
	// returns at once allocates no closure.
	var eligible func(*cachedAssignment) exposureStatus
	for attempt := 1; ; attempt++ {
		var status exposureStatus
		if entry.wins() || entry.holdouts.isSuppressed() {
			if !entry.needsSettling() {
				return entry, true
			}
			if eligible == nil {
				eligible = c.variableEligible(experimentName, key)
			}
			var demoted = false
			var current bool
			entry, current = c.settleExposureAt(experimentName, entry, func(e *cachedAssignment) exposureStatus {
				if !e.wins() && !e.holdouts.isSuppressed() {
					// Resolved again into a non-participant, which claims
					// nothing: only its holdouts fire, below.
					demoted = true
					return exposureIneligible
				}
				return eligible(e)
			}, at)
			if !demoted {
				return entry, current
			}
			status = exposureRetired
		} else {
			// Until the context has seen holdouts or switchbacks nothing can
			// retire or rebuild a non-participant during the read (as in
			// v1.2.0), so one without unexposed arms costs no lock.
			if !entry.hasUnexposedArms() && !c.state().loadIndex().heldOut {
				return entry, true
			}
			var exposures []jsonmodels.Exposure
			status, exposures = c.queueHoldoutArms(experimentName, key, entry)
			c.logExposures(exposures)
			if status == exposureIneligible {
				return entry, false
			}
		}
		if status != exposureRetired || attempt >= maxExposureAttempts {
			return entry, true
		}
		*at = clockReading{clock: c.Clock_}
		entry = c.getAssignmentAt(experimentName, at)
	}
}

// hasUnexposedArms reports whether any holdout arm the entry pinned is still
// unexposed.
func (e *cachedAssignment) hasUnexposedArms() bool {
	if e.holdouts == nil {
		return false
	}
	for _, arm := range e.holdouts.arms {
		if arm != nil && !arm.assignment.Exposed.Load().(bool) {
			return true
		}
	}
	return false
}

// queueHoldoutArms queues entry's unexposed holdout arms for a read of key.
// The caller's clock is read first, with no context lock held, since a clock
// may call back into the context, and only when an arm is still unexposed:
// a warmed read has nothing to timestamp. Every mutable read then happens
// under ContextLock_: the entry must still be the cached, unretired decision,
// and keyEligible validates it against the installed definitions, so a
// refresh that lands while the clock runs (a holdout iteration bump, for one)
// is seen and no superseded arm is admitted. EventLock_ is taken inside
// ContextLock_, in the snapshot lock order. The caller logs the returned
// exposures with no lock held.
func (c *Context) queueHoldoutArms(experimentName string, key string, entry *cachedAssignment) (exposureStatus, []jsonmodels.Exposure) {
	if beforeHoldoutArms != nil {
		beforeHoldoutArms()
	}
	c = c.live()
	var st = c.state()
	var exposedAt int64
	if entry.hasUnexposedArms() {
		exposedAt = c.Clock_.Millis()
	}
	c.ContextLock_.RLock()
	defer c.ContextLock_.RUnlock()
	if entry.retirement.Load() || st.assignmentCache_[experimentName] != entry {
		return exposureRetired, nil
	}
	if status := c.keyEligible(st, experimentName, key, entry); status != exposureClaimed {
		return status, nil
	}
	if !entry.hasUnexposedArms() {
		return exposureQueued, nil
	}
	c.EventLock_.Lock()
	defer c.EventLock_.Unlock()
	if c.CheckNotClosed() != nil {
		return exposureRejected, nil
	}
	var exposures []jsonmodels.Exposure
	for _, arm := range entry.holdouts.arms {
		if arm != nil && arm.assignment.Exposed.CompareAndSwap(false, true) {
			exposures = append(exposures, c.exposureFor(arm.assignment, false, exposedAt))
		}
	}
	c.PendingCount_.Store(c.PendingCount_.Load().(int32) + int32(len(exposures)))
	c.Exposures_ = append(c.Exposures_, exposures...)
	return exposureQueued, exposures
}

// logExposures logs queued exposures and arms the publish timeout. The caller
// holds no context lock.
func (c *Context) logExposures(exposures []jsonmodels.Exposure) {
	for _, exposure := range exposures {
		c.LogEvent(Exposure, exposure)
	}
	if len(exposures) != 0 {
		c.SetTimeout()
	}
}

// wins reports whether the assignment can win a variable key: the unit is
// assigned to, overridden in or rule-assigned to its experiment.
func (e *cachedAssignment) wins() bool {
	return e.assignment.Assigned || e.assignment.Overridden || e.ruleOverride
}

// variableOf is the assignment's value for key, or defaultValue when its
// variables lack key.
func variableOf(entry *cachedAssignment, key string, defaultValue interface{}) interface{} {
	if value, exist := entry.assignment.Variables[key]; exist {
		return value
	}
	return defaultValue
}

// resolveLiteralVariableValue is v1.1.1's GetVariableValue/PeekVariableValue
// for a Context{...} literal.
func (c *Context) resolveLiteralVariableValue(key string, defaultValue interface{}, expose bool) (interface{}, error) {
	var experiment, err = c.GetVariableExperiment(key)
	if err != nil {
		return defaultValue, nil
	}
	var entry = c.getAssignment(experiment.Data.Name)
	if expose && !entry.assignment.Exposed.Load().(bool) {
		c.queueExposure(entry.assignment, entry)
	}
	return variableOf(entry, key, defaultValue), nil
}

func (c *Context) Track(goalName string, properties map[string]interface{}) error {
	c = c.live()
	var err = c.CheckNotClosed()
	if err != nil {
		return err
	}

	var achievement = jsonmodels.GoalAchievement{}
	achievement.AchievedAt = c.Clock_.Millis()
	achievement.Name = goalName
	if properties == nil {
		achievement.Properties = nil
	} else {
		achievement.Properties = map[string]interface{}{}
		for key, value := range properties {
			achievement.Properties[key] = value
		}
	}

	c.EventLock_.Lock()
	// See QueueExposure: check and append under the lock Close seals with.
	if err := c.CheckNotClosed(); err != nil {
		c.EventLock_.Unlock()
		return err
	}
	c.PendingCount_.Store(c.PendingCount_.Load().(int32) + 1)
	c.Achievements_ = append(c.Achievements_, achievement)
	c.EventLock_.Unlock()

	c.LogEvent(Goal, achievement)

	c.SetTimeout()
	return nil
}

func (c *Context) PublishAsync() (*future.Future, error) {
	c = c.live()
	var err = c.CheckNotClosed()
	if err != nil {
		return nil, err
	}

	var result, _ = c.flush()
	return result, nil
}

func (c *Context) Publish() error {
	c = c.live()
	if err := c.CheckNotClosed(); err != nil {
		return err
	}
	var result, op = c.flush()
	if op != nil && !result.Ready() {
		// A deserializer calling Publish lends the gate to the publish's
		// logger, as Refresh does to its own.
		defer c.state().customAudiences.lend(op)()
	}
	var _, err = result.Get(context.Background())
	return err
}

func (c *Context) GetPendingCount() int32 {
	return c.PendingCount_.Load().(int32)
}

// refreshOperation owns copy adoption independently of Future's user listeners.
// Its mutex is never held while invoking customer code or taking stateLock_.
// A late join adopts synchronously, before RefreshAsync returns its future.
// Every caller shares that future, so listeners can wait on other joiners.
type refreshOperation struct {
	gate      gateOperation
	mu        sync.Mutex
	receivers map[*Context]struct{}
	completed bool
	settled   bool
}

func (op *refreshOperation) join(receiver, root *Context) {
	if !receiver.needsLiveDataFrom(root) {
		return
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if !op.settled {
		if op.receivers == nil {
			op.receivers = map[*Context]struct{}{}
		}
		op.receivers[receiver] = struct{}{}
	}
	if op.completed {
		receiver.adoptLiveDataFrom(root)
	}
}

func (op *refreshOperation) complete(root *Context, succeeded bool) {
	op.mu.Lock()
	defer op.mu.Unlock()
	if succeeded {
		for receiver := range op.receivers {
			receiver.adoptLiveDataFrom(root)
		}
	}
	op.completed = true
}

// settle re-adopts the live data into every joined copy just before the
// operation's future settles: a completion logger may have refreshed (or set
// data) again meanwhile, and the copy's reads must agree with the live data
// when its caller sees the outcome, success or failure.
func (op *refreshOperation) settle(root *Context) {
	op.mu.Lock()
	defer op.mu.Unlock()
	for receiver := range op.receivers {
		receiver.adoptLiveDataFrom(root)
	}
	op.receivers = nil
	op.settled = true
}

func (c *Context) RefreshAsync() *future.Future {
	var ft, _ = c.refreshAsync()
	return ft
}

// refreshAsync is RefreshAsync, also returning the operation the future
// belongs to, or nil when there is none to wait for.
func (c *Context) refreshAsync() (*future.Future, *refreshOperation) {
	var receiver = c
	c = c.live()
	var st = c.state()
	var err = c.CheckNotClosed()
	if err != nil {
		var tempfuture, donefun = future.New()
		donefun(nil, err)
		return tempfuture, nil
	}

	st.stateLock_.Lock()
	if c.Refreshing_.Load().(bool) {
		var active, op = c.RefreshFuture_, st.refresh_
		st.stateLock_.Unlock()
		if active != nil {
			if op != nil {
				op.join(receiver, c)
			}
			return active, op
		}
		var tempfuture, donefun = future.New()
		donefun(nil, nil)
		return tempfuture, nil
	}
	c.Refreshing_.Store(true)
	var tempfuture, donefun = future.New()
	var op = &refreshOperation{}
	c.RefreshFuture_, st.refresh_ = tempfuture, op
	st.stateLock_.Unlock()
	op.join(receiver, c)

	var loaded = c.callDataProvider()
	var complete = func(value future.Value, err error) {
		// Register this goroutine as the operation's executor, so a gate
		// holder waiting for this refresh in Refresh admits its parser and
		// loggers.
		var executed = st.customAudiences.execute(&op.gate)
		defer executed()
		var outcome = err
		var result contextInput
		var diagnostics []bufferedEvent
		if outcome == nil {
			var ok bool
			result, ok = contextInputOf(value)
			if !ok {
				outcome = &TypeError{Expected: "jsonmodels.ContextData or jsonmodels.ContextPayload", Actual: value}
			} else {
				var panicErr error
				diagnostics, panicErr = c.applyRefreshedData(result)
				if panicErr != nil {
					outcome = panicErr
				}
			}
		}
		// Adopt every registered copy before loggers or future listeners can
		// observe success. Registration racing completion adopts on its caller.
		op.complete(c, outcome == nil)
		st.stateLock_.Lock()
		c.Refreshing_.Store(false)
		if st.refresh_ == op {
			st.refresh_ = nil
		}
		st.stateLock_.Unlock()

		// A logger may start another refresh. Keep this operation's future and
		// settlement local, and preserve logger-before-return ordering.
		c.dispatchDiagnostics(diagnostics)
		if outcome == nil {
			c.LogEvent(Refresh, result.data)
		} else {
			c.LogError(outcome)
		}
		op.settle(c)
		executed()
		donefun(nil, outcome)
	}
	loaded.Listen(complete)
	return tempfuture, op
}

// contextInput is data to install: the v1.2.0 ContextData, and for a
// ContextPayload its experiments with the holdout and switchback fields
// (experiments[i].Experiment is data.Experiments[i]) and its holdouts. A
// plain ContextData has neither and installs with no conversion.
type contextInput struct {
	payload     bool // a ContextPayload, which GetPayload returns as installed
	data        jsonmodels.ContextData
	experiments []jsonmodels.ExperimentData
	holdouts    []jsonmodels.Holdout
}

func payloadInput(payload jsonmodels.ContextPayload) contextInput {
	return contextInput{payload: true, data: payload.ContextData(), experiments: payload.Experiments,
		holdouts: payload.Holdouts}
}

// contextInputOf normalises a value a data future delivered: a
// jsonmodels.ContextData (v1.2.0 and custom providers) or a
// jsonmodels.ContextPayload (holdouts and switchbacks). It reports false for
// anything else.
func contextInputOf(value future.Value) (contextInput, bool) {
	switch data := value.(type) {
	case jsonmodels.ContextPayload:
		return payloadInput(data), true
	case jsonmodels.ContextData:
		return contextInput{data: data}, true
	}
	return contextInput{}, false
}

func (c *Context) applyRefreshedData(data contextInput) (diagnostics []bufferedEvent, panicErr error) {
	var buffer = &diagnosticBuffer{}
	defer func() {
		if r := recover(); r != nil {
			panicErr = panicToError(r)
		}
		diagnostics = buffer.take()
	}()
	c.setData(data, buffer)
	return nil, nil
}

// Refresh waits for the refresh. A goroutine holding the custom audience
// gate (a deserializer calling Refresh) lends it to that refresh meanwhile.
func (c *Context) Refresh() {
	var ft, op = c.refreshAsync()
	if op != nil && !ft.Ready() {
		defer c.live().state().customAudiences.lend(&op.gate)()
	}
	ft.Join(context.Background())
}

func (c *Context) CloseAsync() (*future.Future, error) {
	closing, _ := c.live().closeAttempt()
	return closing, nil
}

// closeAttempt starts or joins a close and returns that attempt's close and
// finalization futures together, so a later retry cannot replace them.
func (c *Context) closeAttempt() (*future.Future, *future.Future) {
	var st = c.state()
	// Claim the close and publish its future in one critical section, so a
	// concurrent caller that sees the context closing also gets the future of
	// the close that is running.
	st.stateLock_.Lock()
	if c.Closed_.Load().(bool) || c.Closing_.Load().(bool) {
		var existing = c.ClosingFuture_
		var existingFinalized = st.finalizedFuture_
		st.stateLock_.Unlock()
		if existingFinalized == nil {
			var resolved, resolve = future.New()
			resolve(nil, nil)
			existingFinalized = resolved
		}
		// A logger that re-enters a close that has already succeeded gets a
		// settled future, as in v1.1.1: the close future may only settle once
		// that logger returns.
		if existing == nil || (c.Closed_.Load().(bool) && st.loggers_ != nil && st.loggers_.current()) {
			var resolved, resolve = future.New()
			resolve(nil, nil)
			existing = resolved
		}
		return existing, existingFinalized
	}
	c.Closing_.Store(true)
	st.closeStarted_ = true
	var tempFuture, done = future.New()
	c.ClosingFuture_ = tempFuture
	var finalized, finalizeDone = future.New()
	st.finalizedFuture_ = finalized
	st.stateLock_.Unlock()

	c.ClearRefreshTimer()

	c.Flush()

	var failureLock sync.Mutex
	var failure error
	var recordFailure = func(err error) {
		if err == nil {
			return
		}
		failureLock.Lock()
		if failure == nil {
			failure = err
		}
		failureLock.Unlock()
	}

	// settle waits for every publish in flight and then decides, under
	// EventLock_, whether the context can be sealed. A publish that fails puts
	// its events back in the queue, and another publish may drain them again
	// after a snapshot was taken, so the loop repeats until nothing is in
	// flight. Only then is the queue checked and the context sealed, in one
	// critical section. A close that finds events still queued fails and leaves
	// the context open for a retry.
	var settle func(synchronous bool)
	settle = func(synchronous bool) {
		c.EventLock_.Lock()
		var waitFor = st.inflightSnapshot()
		if len(waitFor) > 0 {
			c.EventLock_.Unlock()
			var remaining = int32(len(waitFor))
			for _, f := range waitFor {
				f.Listen(func(value future.Value, err error) {
					recordFailure(err)
					if atomic.AddInt32(&remaining, -1) == 0 {
						settle(false)
					}
				})
			}
			return
		}

		failureLock.Lock()
		var err = failure
		failureLock.Unlock()
		if err == nil && c.PendingCount_.Load().(int32) > 0 {
			err = errors.New("close left unsent events: an earlier publish failed")
		}
		if err == nil {
			c.Closed_.Store(true)
		}
		c.EventLock_.Unlock()

		if err != nil {
			// The failed batch is back in the queue. Leave the context open so
			// Publish or Close can retry it. The refresh timer stays stopped:
			// Close returns nothing, so the caller cannot know it must stop it.
			st.stateLock_.Lock()
			c.ClosingFuture_ = nil
			c.Closing_.Store(false)
			st.stateLock_.Unlock()
			// Start delivery before notifying Close listeners, which run
			// synchronously and may wait for finalization.
			go func() {
				c.waitPublishLogs()
				finalizeDone(nil, err)
			}()
			done(nil, err)
			return
		}
		c.Closing_.Store(false)
		// With nothing in flight when the close started, the close completes
		// in this call and, like v1.1.1, the Close event is logged before the
		// future settles. A logger that re-enters Close is the exception: it
		// cannot wait for logger delivery that only finishes once it returns.
		// So is a close that would have to wait for an earlier publish logger.
		// Each future's listeners run synchronously and may wait for the
		// other future, so finalization settles on its own goroutine.
		if synchronous && c.publishLogsDone() && (st.loggers_ == nil || !st.loggers_.current()) {
			c.LogEvent(Finalize, nil)
			go finalizeDone(nil, nil)
			done(nil, nil)
			return
		}
		go func() {
			c.waitPublishLogs()
			c.LogEvent(Finalize, nil)
			finalizeDone(nil, nil)
		}()
		done(nil, nil)
	}
	settle(true)
	return tempFuture, finalized
}

// FinalizeAsync closes the context and waits for publish/error and finalize
// logger callbacks to return. Unlike CloseAsync, this is an observer-delivery
// barrier. Do not wait on it from a logger; loggers may re-enter Close instead.
func (c *Context) FinalizeAsync() (*future.Future, error) {
	c = c.live()
	_, finalized := c.closeAttempt()
	return finalized, nil
}

func (c *Context) publishLogsDone() bool {
	var st = c.state()
	st.stateLock_.Lock()
	defer st.stateLock_.Unlock()
	for _, f := range st.publishLogs_ {
		if !f.Ready() {
			return false
		}
	}
	return true
}

func (c *Context) waitPublishLogs() {
	var st = c.state()
	st.stateLock_.Lock()
	logs := append([]*future.Future(nil), st.publishLogs_...)
	st.stateLock_.Unlock()
	for _, f := range logs {
		f.Join(context.Background())
	}
}

// Close closes the context and, like v1.1.x, returns after the Close event
// has been logged. Called from inside an event logger of this context, it
// only waits for the close itself: logger delivery cannot finish until that
// logger returns.
func (c *Context) Close() {
	c = c.live()
	var st = c.state()
	if st.loggers_ != nil && st.loggers_.current() {
		closing, _ := c.closeAttempt()
		closing.Join(context.Background())
		return
	}
	// A deserializer calling Close lends the gate to the publishes it waits
	// for, as Publish does to its own, from before the close starts its
	// final publish until finalization (which waits for their loggers) ends.
	var wait, end = st.lendPublishes()
	defer end()
	_, finalized := c.closeAttempt()
	wait(func() { finalized.Join(context.Background()) })
}

// assignmentState is what validCachedAssignment found for a cached assignment.
type assignmentState int

const (
	assignmentStale assignmentState = iota
	assignmentValid
	// assignmentRevalidated means the rules or attributes changed since the
	// assignment was last validated, but they still give its outcome. Its
	// stamps must be advanced under the write lock, or every later read would
	// re-evaluate it.
	assignmentRevalidated
)

// validCachedAssignment returns the assignment cached in slot for
// experimentName and whether it is still valid, with the rule set it was
// validated against. indexed is the installed experiment slot was computed
// from, nil when there is none. It only reads, so it may run under
// ContextLock_ held for reading or writing. parse is the build's own fresh
// parse, or nil (see buildAssignmentLocked).
func (c *Context) validCachedAssignment(st *contextState, experimentName string, indexed *indexedExperiment, slot assignmentSlot,
	parse *audienceParse) (*cachedAssignment, *experimentRules, assignmentState) {
	return c.validCachedAssignmentFor(st, experimentName, indexed, slot, false, parse)
}

// validCachedAssignmentFor is validCachedAssignment; with ignoreCustom a
// custom assignment that differs from the cached one is not staleness, and
// every other check still applies. The variable claim check uses it: a
// custom assignment the build could not apply (full-on, traffic-ineligible,
// strict audience mismatch, an ineligible switchback cluster) makes every
// rebuild stale, as in v1.1.1, but it is no newer definition, so the read
// claims the decision it serves rather than retrying it. A custom assignment
// set or changed during the read is ignored by the claim the same way, as
// main and v1.1.1 expose what the read resolved; the next read applies it.
func (c *Context) validCachedAssignmentFor(st *contextState, experimentName string, indexed *indexedExperiment, slot assignmentSlot,
	ignoreCustom bool, parse *audienceParse) (*cachedAssignment, *experimentRules, assignmentState) {
	var entry, found = st.assignmentCache_[slot.key]
	if !found {
		return nil, nil, assignmentStale
	}
	var rules, state = c.validEntry(st, experimentName, indexed, entry, ignoreCustom, parse)
	return entry, rules, state
}

// validEntry reports whether entry, cached or not, is still the decision the
// installed data gives for experimentName. indexed is the installed
// experiment, nil when there is none. A variable read's claim checks its own
// entry, so an equivalent rebuild by a concurrent read (an unapplied custom
// assignment rebuilds on every lookup) does not make its decision stale.
// parse is as for validCachedAssignment.
func (c *Context) validEntry(st *contextState, experimentName string, indexed *indexedExperiment, entry *cachedAssignment,
	ignoreCustom bool, parse *audienceParse) (*experimentRules, assignmentState) {
	if st.switchbackEpochStale(experimentName, indexed) {
		return nil, assignmentStale
	}
	if entry.retirement.Load() {
		return nil, assignmentStale
	}
	var assignment = entry.assignment
	var custom, cfound = c.Cassignments_[experimentName]
	var override, ofound = c.Overrides_[experimentName]
	var efound = indexed != nil
	// A pointer: the experiment is too large to copy on every cached read.
	var experiment *ExperimentVariables
	var rules *experimentRules
	var holdouts []*jsonmodels.Holdout
	if efound {
		experiment, rules, holdouts = &indexed.experiment, indexed.rules, indexed.holdouts
	}

	if ofound {
		// A removed experiment keeps its override, as in the JS SDK
		// (experiment == null || holdoutSetMatches). The switchback identity
		// is checked too, so a grid change, or the removal of a switchback,
		// resolves an overridden slot again like any other.
		// A still-unclaimed override needs current variables and resolved
		// identity before a variable read can claim it in the new epoch.
		if efound && !assignment.Exposed.Load().(bool) && st.loadIndex().heldOut &&
			!entry.resolvedExperimentMatches(experiment.Data) {
			return rules, assignmentStale
		}
		if assignment.Overridden && assignment.Variant == override.(int) && (!efound ||
			holdoutSetMatches(holdouts, entry.holdouts) && (entry.holdouts == nil || entry.resolvedUnitType == experiment.Data.UnitType)) &&
			switchbackSlotMatches(indexed, entry) {
			return rules, assignmentValid
		}
	} else if !efound {
		// A rule-won assignment is not assigned either, but its variant came
		// from the experiment's rules, so it is rebuilt once the experiment
		// is gone.
		if !assignment.Assigned && !entry.ruleOverride {
			return rules, assignmentValid
		}
	} else if entry.ruleOverride || entry.holdouts.isSuppressed() || ignoreCustom || entry.switchback.customMatches(custom, cfound, assignment) {
		// A matching rule wins over a custom assignment, and a holdout
		// suppression forces control, so a custom assignment that differs
		// from either variant is not staleness.
		if !c.entryMatches(indexed, entry) || !holdoutSetMatches(holdouts, entry.holdouts) {
			return rules, assignmentStale
		}
		if (ignoreCustom || !assignment.Exposed.Load().(bool)) && st.loadIndex().heldOut &&
			entry.audienceChanged(experiment.Data) {
			return rules, assignmentStale
		}
		if rules == entry.rules && atomic.LoadInt32(&st.attrsSeq_) <= entry.attrsSeq {
			return rules, assignmentValid
		}
		if c.ruleMatches(st, experiment.Data, rules, entry) && c.audienceMatches(st, experiment.Data, entry, parse) {
			return rules, assignmentRevalidated
		}
	}
	return rules, assignmentStale
}

// ruleMatches reports whether the experiment's assignment rules still give the
// cached assignment's outcome. It never updates the entry; a changed outcome
// is reported as stale for GetAssignment to rebuild under the write lock.
func (c *Context) ruleMatches(st *contextState, data jsonmodels.Experiment, rules *experimentRules, entry *cachedAssignment) bool {
	var assignment = entry.assignment
	// A switchback without an eligible cluster never consulted its rules,
	// as the Collector evaluates them only for a cluster key, so a rule
	// change cannot make it stale.
	if entry.switchback != nil && entry.switchback.serving.status != switchbackEligible {
		return true
	}
	// An exposed split resolved without its unit keeps resolving as if the
	// unit were still missing, so a unit set afterwards cannot switch the
	// variant the user already saw.
	var pinnedMissing = entry.ruleMissingUnit && assignment.Exposed.Load().(bool)
	var rule = c.resolveRule(st, data, rules, ruleUnitType(data, entry), pinnedMissing, false)
	if (rule != nil) != entry.ruleOverride {
		return false
	}
	if rule == nil {
		return true
	}
	// Same variant, but now resolved by a split without its unit, or no
	// longer so: stale, so GetAssignment refreshes the flag. It keeps an
	// exposed assignment's exposed flag (see keepExposedRuleAssignment).
	return rule.variant == assignment.Variant && rule.missingUnit == entry.ruleMissingUnit
}

// audienceMatches reports whether the audience still gives the cached
// assignment's audienceMismatch. A rule-won or holdout-suppressed assignment
// never evaluated the audience, as in the JS SDK, so it skips it. parse is
// used when it was made for this experiment run; otherwise only the built-in
// deserializer's cached parse is.
func (c *Context) audienceMatches(st *contextState, data jsonmodels.Experiment, entry *cachedAssignment, parse *audienceParse) bool {
	var assignment = entry.assignment
	if entry.ruleOverride || entry.holdouts.isSuppressed() || len(data.Audience) == 0 {
		return true
	}
	// Nor did a switchback without an eligible cluster: it is resolved
	// again, from the same cluster, only when the period or the definition
	// changes.
	if entry.switchback != nil && entry.switchback.serving.status != switchbackEligible {
		return true
	}
	if entry.failed.matches(data, c.AudienceMatcher_.Deserializer_) {
		// Unparseable when built, so the audience was ignored. It stays
		// ignored for this assignment, as v1.1.1's single assignment kept
		// its fallback, while other experiments parse the text again.
		return !assignment.AudienceMismatch
	}
	var parsed parsedAudience
	if guarded := st.audiencePanic(data.Name, data, c.AudienceMatcher_.Deserializer_); guarded != nil {
		// A logger is reporting this experiment's panic: revalidate with that
		// failure, as the read that hit it does, rather than rebuilding.
		parsed = guarded.parsed
	} else if parse.matches(data, c.AudienceMatcher_.Deserializer_) {
		// A fresh failure evaluates as an error, so the audience is ignored,
		// as a rebuild would ignore it; an assignment that already ignored
		// it is kept.
		parsed = parse.parsed
	} else if cached, ok := c.cachedAudience(st, data.Audience, c.AudienceMatcher_.Deserializer_); ok {
		parsed = cached
	} else {
		// Not parsed yet: force the rebuild, which parses with no lock held.
		return false
	}
	var match, err = parsed.evaluate(c.evaluationAttributesWithState(st))
	var newAudienceMismatch = false
	if err == nil {
		newAudienceMismatch = !match.Get()
	}
	return newAudienceMismatch == assignment.AudienceMismatch
}

type ruleResolution struct {
	variant     int
	missingUnit bool
}

// resolveRule resolves the experiment's assignment rules to a variant, or nil
// when no rule applies. An assign rule whose variant is out of range, or a
// split whose value count differs from the variant count, stops rule
// evaluation and falls back to normal assignment. A split with no unit to hash
// gives the experiment's own no-unit result (the full-on variant, or 0) and is
// flagged missingUnit. The unit is hashed with the experiment's own seed, so
// a split equal to the experiment split keeps every unit on its variant.
// Rules are scoped by the configured environment, never by a user-set
// "environment" attribute, which only conditions see.
//
// unitType is the unit a split hashes: the experiment's, or a switchback's
// serving unit. The caller holds ContextLock_. cache is false on the read
// path, where the hashed-unit and assigner caches must not be written.
func (c *Context) resolveRule(st *contextState, data jsonmodels.Experiment, rules *experimentRules, unitType string, unitPinnedMissing bool, cache bool) *ruleResolution {
	if rules == nil || rules.parsed == nil {
		return nil
	}

	var action = c.AudienceMatcher_.evaluateRules(rules.parsed, st.environment_, c.evaluationAttributesWithState(st))
	if action == nil {
		return nil
	}

	var variantCount = len(data.Variants)
	if action.split == nil {
		if action.variant >= 0 && action.variant < variantCount {
			return &ruleResolution{variant: action.variant}
		}
		return nil
	}
	if len(action.split) != variantCount {
		return nil
	}

	var uid, ufound = c.Units_[unitType]
	if len(unitType) == 0 || !ufound || unitPinnedMissing {
		return &ruleResolution{variant: data.FullOnVariant, missingUnit: true}
	}

	var assigner *VariantAssigner
	if cache {
		var hash [22]byte
		assigner = c.GetVariantAssigner(unitType, c.GetUnitHash(unitType, uid, hash[:], false), false)
	} else if cached, found := c.Assigners_[unitType]; found {
		assigner = cached.(*VariantAssigner)
	} else if hash, found := c.HashedUnits_[unitType]; found {
		assigner = NewVariantAssigner(hash.([]int8))
	} else {
		assigner = NewVariantAssigner(HashUnit(uid))
	}
	var buf [12]int8
	return &ruleResolution{variant: assigner.Assign(action.split, data.SeedHi, data.SeedLo, buf[:])}
}

// keepExposedRuleAssignment carries the exposed flag over to the rebuilt
// assignment when both are rule wins on the same variant for the same
// experiment run. A fresh exposed flag would publish the same decision a
// second time. The rebuilt assignment is still the one installed, so it keeps
// the refreshed variables and rule bookkeeping, and the cached one is never
// mutated while a reader may hold it. The caller holds ContextLock_ for
// writing.
func (c *Context) keepExposedRuleAssignment(indexed *indexedExperiment, previous *cachedAssignment, entry *cachedAssignment) {
	if previous == nil {
		return
	}
	var old, fresh = previous.assignment, entry.assignment
	if !entry.ruleOverride || !previous.ruleOverride || old.Overridden || old.Variant != fresh.Variant ||
		!old.Exposed.Load().(bool) || !c.entryMatches(indexed, previous) || old.UnitType != fresh.UnitType {
		return
	}
	// Changed holdout coverage is a new decision, whose holdout exposures
	// must fire: it is exposed again, as in the JS SDK.
	var covering []*jsonmodels.Holdout
	if entry.holdouts != nil {
		covering = entry.holdouts.holdouts
	}
	if !holdoutSetMatches(covering, previous.holdouts) {
		return
	}
	fresh.Exposed = previous.assignment.Exposed
	entry.retirement = previous.retirement
}

// GetAssignment returns a shallow copy of the experiment's assignment, as in
// v1.1.1: changing its fields does not change the context's cached
// assignment. The copy shares the cached Exposed flag (QueueExposure relies
// on it), the Variables map and the TrafficSplit slice; treat those as
// read-only.
func (c *Context) GetAssignment(experimentName string) *Assignment {
	var assignment = *c.getAssignment(experimentName).assignment
	return &assignment
}

func (c *Context) getAssignment(experimentName string) *cachedAssignment {
	var at = clockReading{clock: c.Clock_}
	return c.getAssignmentAt(experimentName, &at)
}

// getAssignmentAt resolves the assignment at the clock reading at, which is
// read only for a switchback experiment, and at most once. The entry's
// assignment is shared with callers, so it is never mutated once cached.
func (c *Context) getAssignmentAt(experimentName string, at *clockReading) *cachedAssignment {
	var st *contextState
	if clock := c.ownClock(); clock != nil {
		c, st = clock.root, clock.state
	} else if st = c.literalState(); st.manualRoot != nil {
		c = st.manualRoot
	} else {
		c.syncLiteralExperiment(st, experimentName)
	}
	// A valid cached assignment only needs the read lock, so concurrent reads
	// of a shared context do not serialise. An ordinary experiment's lookup
	// calls no injected code, so it stays inline with explicit unlocks; a
	// switchback's reads the clock, so it goes through cachedAssignmentAt,
	// which releases the lock on unwind.
	c.ContextLock_.RLock()
	var indexed = st.indexedExperiment(experimentName)
	if indexed == nil || indexed.switchback == nil {
		if entry, _, state := c.validCachedAssignment(st, experimentName, indexed, assignmentSlot{key: experimentName}, nil); state == assignmentValid {
			c.ContextLock_.RUnlock()
			return entry
		}
		c.ContextLock_.RUnlock()
	} else {
		c.ContextLock_.RUnlock()
		if entry := c.cachedAssignmentAt(st, experimentName, at); entry != nil {
			return entry
		}
	}
	return c.buildAssignment(st, experimentName, at)
}

// cachedAssignmentAt is the valid cached assignment, or nil. The read lock
// is released on unwind too: the slot lookup reads an injected clock, which
// may panic.
func (c *Context) cachedAssignmentAt(st *contextState, experimentName string, at *clockReading) *cachedAssignment {
	c.ContextLock_.RLock()
	defer c.ContextLock_.RUnlock()
	var indexed = st.indexedExperiment(experimentName)
	if entry, _, state := c.validCachedAssignment(st, experimentName, indexed, assignmentSlotFor(experimentName, indexed, at), nil); state == assignmentValid {
		return entry
	}
	return nil
}

// buildAssignment validates or rebuilds experimentName's assignment under the
// write lock. The audience is parsed only when the assignment needs it (an
// override or a matching assignment rule skips it, as in v1.1.1): the locked
// path then releases the lock, the text is parsed with no lock held, and it
// tries again with that parse. Only new data installed during each parse
// makes it parse again; after maxAudienceAttempts parses the audience counts
// as unparseable for this evaluation.
//
// A custom deserializer's parse holds st.customAudiences from before the call
// until the locked attempt that evaluates it returns, so on one context no
// other custom parse runs while a result may still be in use, as in v1.1.1.
// The built-in deserializer's parses, whose results are fresh, skip it. Any
// read from another goroutine, an event logger's included, waits for the
// gate, except the executor of a refresh, initial load or publish that the
// holder waits for in Refresh, WaitUntilReady, Publish or Close: the holder
// lent it the gate, so that operation's parser and loggers run nested, as
// v1.1.1 ran refresh callbacks on the joining goroutine.
func (c *Context) buildAssignment(st *contextState, experimentName string, at *clockReading) *cachedAssignment {
	var entry, unparsed = c.buildAssignmentLocked(st, experimentName, at, false, nil)
	for attempt := 0; unparsed != nil; attempt++ {
		entry, unparsed = c.buildAudienceAttempt(st, experimentName, at, attempt >= maxAudienceAttempts)
	}
	return entry
}

// buildAudienceAttempt owns the gate for one decode and its consuming locked
// evaluation only. Waiting readers recheck the assignment before decoding:
// another reader may have installed it while they waited.
func (c *Context) buildAudienceAttempt(st *contextState, experimentName string, at *clockReading, last bool) (*cachedAssignment, *jsonmodels.Experiment) {
	if !builtinDeserializer(c.AudienceMatcher_.Deserializer_) {
		st.customAudiences.lock()
		defer st.customAudiences.unlock()
	}
	var entry, unparsed = c.buildAssignmentLocked(st, experimentName, at, last, nil)
	if unparsed == nil {
		return entry, nil
	}
	var parsed = c.parseAudienceUnlocked(st, experimentName, *unparsed)
	return c.buildAssignmentLocked(st, experimentName, at, last, parsed)
}

// audienceParse is the parse one build made of an experiment run's audience,
// successful or not (parsed.err set). It belongs to that build alone: a
// custom deserializer's filter is evaluated by the next locked attempt and
// then dropped, and a failure is kept only by the assignment it was made for
// (cachedAssignment.failed), so other experiments with the same text parse
// it themselves.
type audienceParse struct {
	id, iteration int
	text          string
	deserializer  AudienceDeserializer
	parsed        parsedAudience
}

// matches reports whether f was made for this run of the experiment (its id
// and iteration), with this text and deserializer: a replacement experiment
// installed under the same name never inherits it.
func (f *audienceParse) matches(data jsonmodels.Experiment, deserializer AudienceDeserializer) bool {
	return f != nil && f.id == data.Id && f.iteration == data.Iteration && f.text == data.Audience &&
		data.Audience != "" && sameDeserializer(f.deserializer, deserializer)
}

// audiencePanic is called with ContextLock_ held. It prevents a logger's read
// of the experiment reporting a panic from recursively invoking its parser.
func (st *contextState) audiencePanic(name string, data jsonmodels.Experiment, deserializer AudienceDeserializer) *audienceParse {
	for _, failed := range st.audiencePanics[name] {
		if failed.matches(data, deserializer) {
			return failed
		}
	}
	return nil
}

const maxAudienceAttempts = 4

// buildAssignmentLocked returns the assignment, or the audience text that has
// to be parsed (with no lock held) before it can be built. parse is this
// build's own parse, from the previous attempt; it is used, for revalidating
// or building, only when it was made for the experiment run found now. On the
// last attempt a text not parsed yet counts as unparseable too.
//
// The unlock is deferred: resolving a switchback cluster can call an
// attribute's own JSON or text marshaler, and the audience evaluator can
// panic; neither may strand the lock.
func (c *Context) buildAssignmentLocked(st *contextState, experimentName string, at *clockReading, last bool,
	parse *audienceParse) (*cachedAssignment, *jsonmodels.Experiment) {
	// Cache miss, out of date, or revalidated. Check again under the write
	// lock: another goroutine may have rebuilt the assignment meanwhile, and
	// replacing it would lose its exposed flag and queue a second exposure.
	c.ContextLock_.Lock()
	defer c.ContextLock_.Unlock()
	// The index may have been replaced between the locks; setData publishes
	// it under this lock, so it is stable from here on.
	var indexed = st.indexedExperiment(experimentName)
	var slot = assignmentSlotFor(experimentName, indexed, at)
	if st.switchbackEpochStale(experimentName, indexed) {
		c.dropSwitchbackEpoch(st, experimentName)
	}
	var previous, validated, state = c.validCachedAssignment(st, experimentName, indexed, slot, parse)
	if state == assignmentValid {
		return previous, nil
	}
	if state == assignmentRevalidated {
		// Still the same outcome: acknowledge the rules and attributes it was
		// validated against, so later reads take the fast path. The shared
		// assignment, and so its exposed flag, is kept as is. SetAttribute
		// needs the write lock held here, so the sequence is the one just
		// validated; a refresh racing this records the old rule set, which
		// only makes the next read revalidate.
		previous.rules = validated
		previous.attrsSeq = atomic.LoadInt32(&st.attrsSeq_)
		return previous, nil
	}

	var custom, cfound = c.Cassignments_[experimentName]
	var override, ofound = c.Overrides_[experimentName]
	var efound = indexed != nil
	var experiment ExperimentVariables
	var rules *experimentRules
	if efound {
		experiment, rules = indexed.experiment, indexed.rules
	}

	var exposed = &atomic.Value{}
	exposed.Store(false)
	var assignment = &Assignment{Exposed: exposed}
	assignment.Name = experimentName
	assignment.Eligible = true
	var entry = &cachedAssignment{assignment: assignment, rules: rules, attrsSeq: atomic.LoadInt32(&st.attrsSeq_), retirement: &atomicFlag{}}
	if efound {
		entry.resolvedId = experiment.Data.Id
		entry.resolvedIteration = experiment.Data.Iteration
		entry.resolvedUnitType = experiment.Data.UnitType
		entry.resolvedAudience = experiment.Data.Audience
		entry.resolvedAudienceStrict = experiment.Data.AudienceStrict
		if indexed.switchback != nil {
			entry.resolvedSplit = copyFloat64(experiment.Data.Split)
		}
	}

	// Resolved for overrides and rule wins too: the holdouts' exposures fire
	// whatever chose the variant.
	var held = false
	if efound && len(indexed.holdouts) > 0 {
		entry.holdouts, held = c.resolveHoldouts(st, indexed.holdouts, experiment.Data)
	}

	if efound && indexed.switchback != nil {
		entry.switchback = c.newSwitchbackSlot(indexed, previous, slot, at)
	}

	if ofound {
		if entry.switchback != nil {
			// The override applies even without a cluster, under the
			// serving unit or, failing that, the first linked unit type.
			assignment.Id = experiment.Data.Id
			assignment.UnitType = entry.switchback.unitType(indexed)
		} else if efound {
			assignment.Id = experiment.Data.Id
			assignment.UnitType = experiment.Data.UnitType
		}

		assignment.Overridden = true
		assignment.Variant = override.(int)
	} else if entry.switchback != nil {
		if cfound {
			entry.switchback.customSet, entry.switchback.custom = true, custom.(int)
		}
		if unparsed := c.resolveSwitchback(st, entry, indexed, slot, last, parse); unparsed != nil {
			return nil, unparsed
		}
	} else {
		if efound {
			var unitType = experiment.Data.UnitType

			// A matching rule is an explicit assignment, like an override: it
			// wins over the audience, audienceStrict, traffic eligibility,
			// full-on and custom assignments.
			if rule := c.resolveRule(st, experiment.Data, rules, unitType, false, true); rule != nil {
				assignment.Variant = rule.variant
				entry.ruleOverride = true
				entry.ruleMissingUnit = rule.missingUnit
			} else if held {
				entry.holdouts.suppressed = true
			}

			if !entry.ruleOverride && !entry.holdouts.isSuppressed() && len(experiment.Data.Audience) > 0 {
				if unparsed := c.evaluateAudience(st, experimentName, entry, &experiment.Data, last, parse); unparsed != nil {
					return nil, unparsed
				}
			}

			if !entry.ruleOverride && !entry.holdouts.isSuppressed() {
				var assignBuf [12]int8
				if experiment.Data.AudienceStrict && assignment.AudienceMismatch {
					assignment.Variant = 0
				} else if experiment.Data.FullOnVariant == 0 {
					var uid, ufound = c.Units_[experiment.Data.UnitType]
					if ufound {
						var data [22]byte
						var unitHash = c.GetUnitHash(unitType, uid, data[:], false)
						var assigner = c.GetVariantAssigner(unitType, unitHash, false)
						var eligible = assigner.Assign(experiment.Data.TrafficSplit, experiment.Data.TrafficSeedHi,
							experiment.Data.TrafficSeedLo, assignBuf[:]) == 1
						if eligible {
							if cfound {
								assignment.Variant = custom.(int)
								assignment.Custom = true
							} else {
								assignment.Variant = assigner.Assign(experiment.Data.Split, experiment.Data.SeedHi,
									experiment.Data.SeedLo, assignBuf[:])
							}
						} else {
							assignment.Eligible = false
							assignment.Variant = 0
						}
						assignment.Assigned = true
					}
				} else {
					assignment.Assigned = true
					assignment.Variant = experiment.Data.FullOnVariant
					assignment.FullOn = true
				}
			}

			assignment.UnitType = unitType
			assignment.Id = experiment.Data.Id
			assignment.Iteration = experiment.Data.Iteration
			assignment.TrafficSplit = experiment.Data.TrafficSplit
			assignment.FullOnVariant = experiment.Data.FullOnVariant
		}
	}

	if efound && assignment.Variant >= 0 && (assignment.Variant < len(experiment.Data.Variants)) {
		assignment.Variables = experiment.Variables[assignment.Variant]
	}

	if efound {
		c.keepExposedRuleAssignment(indexed, previous, entry)
	}
	var keyed, heldOut = st.loadIndex().keyed, st.loadIndex().heldOut
	// Share before publishing the entry: retained pointers and Assignment copies
	// must observe the same claim and any later retirement. An already exposed
	// unapplied custom assignment still starts a new exposure, as in v1.1.1.
	if previous != nil && keyed && !previous.assignment.Exposed.Load().(bool) &&
		c.sameDecision(previous, entry) {
		assignment.Exposed = previous.assignment.Exposed
		entry.retirement = previous.retirement
	}
	var rebuiltOverride = previous != nil && previous.assignment.Overridden && efound &&
		heldOut && !previous.resolvedExperimentMatches(experiment.Data)
	// An unexposed keyed decision rebuilt because its audience was edited is
	// superseded the same way: a caller still holding it must not publish it.
	var rebuiltAudience = previous != nil && !previous.assignment.Exposed.Load().(bool) && efound &&
		heldOut && previous.audienceChanged(experiment.Data)
	if previous != nil && (previous.holdouts != nil || entry.holdouts != nil || rebuiltOverride || rebuiltAudience) &&
		previous.assignment.Exposed != assignment.Exposed {
		// Retire the replaced assignment, as eviction does: a caller still
		// holding it must not claim a decision, or holdout arms, that the
		// replacement has superseded. Retired before it is marked exposed,
		// so a claim losing to this, or a read finding it exposed, sees the
		// retirement and resolves again. A claim already taken still
		// commits: the decision is served by the read that claimed it.
		previous.retirement.Store(true)
		if beforeReplacementClaim != nil {
			beforeReplacementClaim(previous)
		}
		previous.assignment.Exposed.CompareAndSwap(false, true)
		if afterReplacementClaim != nil {
			afterReplacementClaim(previous)
		}
	}

	if previous != nil && previous.switchback != nil {
		// Same-period revalidation replaces a slot too. Preserve its metadata
		// for QueueExposure on an Assignment the caller still holds.
		c.retireSwitchbackSlot(st, experimentName, slot.key)
	}
	c.retireAcrossRandomization(st, experimentName, slot, entry)
	if entry.ruleOverride {
		st.ruleIdentities.remember(assignment.Exposed)
	}
	st.assignmentCache_[slot.key] = entry
	if entry.switchback != nil {
		c.retainSwitchbackSlot(st, experimentName, indexed, slot)
	}
	c.AssignmentCache[experimentName] = *assignment
	return entry, nil
}

// evaluateAudience sets the assignment's audienceMismatch from the
// experiment's audience, with this build's own parse, the built-in
// deserializer's cached parse, or the failure a logger is reporting. When
// none is available it returns the experiment, whose audience the caller
// parses with no lock held before building again; on the last attempt the
// audience is ignored for this build instead, and the next revalidation
// parses it again. The caller holds ContextLock_ for writing.
func (c *Context) evaluateAudience(st *contextState, experimentName string, entry *cachedAssignment,
	data *jsonmodels.Experiment, last bool, parse *audienceParse) *jsonmodels.Experiment {
	var assignment = entry.assignment
	var attrs = c.evaluationAttributesWithState(st)
	var deserializer = c.AudienceMatcher_.Deserializer_
	if guarded := st.audiencePanic(experimentName, *data, deserializer); guarded != nil {
		entry.failed = guarded
	} else if parse.matches(*data, deserializer) {
		// This build's own parse, evaluated as soon as the lock is taken: a
		// custom deserializer's result is never kept.
		if match, err := parse.parsed.evaluate(attrs); err == nil {
			assignment.AudienceMismatch = !match.Get()
		} else {
			entry.failed = parse
		}
	} else if parsed, ok := c.cachedAudience(st, data.Audience, deserializer); ok {
		// The built-in deserializer's cache holds only usable parses, so
		// this evaluation cannot fail.
		var match, _ = parsed.evaluate(attrs)
		assignment.AudienceMismatch = !match.Get()
	} else if !last {
		// The data changed between the unlocked parse and this lock: parse
		// the new audience unlocked and start over.
		var unparsed = *data
		return &unparsed
	}
	return nil
}

// resolveHoldouts draws, or reuses, the unit's arm in each holdout covering
// the experiment, hashing the experiment's unit type (never the holdout's
// own), and reports whether any arm holds the experiment out. An arm is nil
// when the experiment's unit is not set. The caller holds ContextLock_ for
// writing.
func (c *Context) resolveHoldouts(st *contextState, holdouts []*jsonmodels.Holdout, data jsonmodels.Experiment) (*assignmentHoldouts, bool) {
	var resolved = &assignmentHoldouts{holdouts: holdouts, arms: make([]*holdoutArm, len(holdouts))}
	var held = false
	for i, holdout := range holdouts {
		var arm = c.holdoutArm(st, holdout, data.UnitType)
		resolved.arms[i] = arm
		if arm != nil && isHeldOutBy(arm.assignment.Variant, arm.armCount, data.FullOnVariant) {
			held = true
		}
	}
	return resolved, held
}

// holdoutArm returns the unit's arm in a holdout for unitType, drawing it on
// first use. The arm is kept while the installed holdout's iteration is
// unchanged, so a same-iteration seed or split edit neither moves the unit
// nor re-exposes it. The definition comes from the same index as coverage.
// The caller holds ContextLock_ for writing.
func (c *Context) holdoutArm(st *contextState, holdout *jsonmodels.Holdout, unitType string) *holdoutArm {
	if index := st.loadIndex(); index != nil {
		if live, found := index.holdouts[holdout.Id]; found {
			holdout = live
		}
	}
	var key = holdoutKey{id: holdout.Id, unitType: unitType}
	if cached, found := st.holdoutArms_[key]; found && cached.assignment.Iteration == holdout.Iteration {
		return cached
	}
	var uid, ufound = c.Units_[unitType]
	if len(unitType) == 0 || !ufound {
		return nil
	}

	var hash [22]byte
	var assigner = c.GetVariantAssigner(unitType, c.GetUnitHash(unitType, uid, hash[:], false), false)
	var buf [12]int8
	var exposed = &atomic.Value{}
	exposed.Store(false)
	var arm = &holdoutArm{
		assignment: &Assignment{Id: holdout.Id, Name: holdout.Name, Iteration: holdout.Iteration, UnitType: unitType,
			Variant: assigner.Assign(holdout.Split, holdout.SeedHi, holdout.SeedLo, buf[:]), Assigned: true,
			Eligible: true, Exposed: exposed},
		armCount: len(holdout.Split),
	}
	if st.holdoutArms_ == nil {
		st.holdoutArms_ = map[holdoutKey]*holdoutArm{}
	}
	st.holdoutArms_[key] = arm
	return arm
}

func (c *Context) ClearRefreshTimer() {
	c = c.live()
	var st = c.state()
	st.stateLock_.Lock()
	defer st.stateLock_.Unlock()
	if c.RefreshTimer_ != nil {
		c.RefreshTimer_.Stop()
		c.RefreshTimer_ = nil
	}
}

// GetExperiment returns the experiment from the installed context data. It
// reads the index setData published atomically, so a refresh installing new
// data cannot race it.
//
// A context with no state carrier (a literal, or a copy of a context whose
// Clock_ was replaced) reads its own Index_ under DataLock, as v1.1.1 read
// it, without ContextLock_. SetData and Refresh keep it current, including
// through such a copy (see adoptLiveDataFrom).
func (c *Context) GetExperiment(experimentName string) (ExperimentVariables, bool) {
	var clock = c.ownClock()
	if clock == nil {
		if c.DataLock != nil {
			c.DataLock.RLock()
			defer c.DataLock.RUnlock()
		}
		result, found := c.Index_[experimentName]
		return result, found
	}
	var result, _, found = clock.state.experimentAndRules(experimentName)
	return result, found
}

func (st *contextState) loadIndex() *contextIndex {
	return st.index_.Load().(*contextIndex)
}

// audienceEntry is a cached parse and the deserializer that made it: the same
// text through a replaced AudienceMatcher_ is parsed again.
type audienceEntry struct {
	deserializer AudienceDeserializer
	parsed       parsedAudience
}

// sameDeserializer reports whether a cached parse was made by b. Values of a
// type Go cannot compare (a func, a struct holding a map) or that compare
// unequal to themselves (a struct holding a NaN, an interface field holding
// a func) cannot be told apart, so for those any value of the same type
// counts as the same deserializer. It never panics: it runs under
// ContextLock_.
func sameDeserializer(a, b AudienceDeserializer) (same bool) {
	var ta, tb = reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	if ta == nil || !ta.Comparable() {
		return true
	}
	defer func() {
		if recover() != nil {
			same = true
		}
	}()
	return a == b || b != b
}

// builtinDeserializer reports whether d is the SDK's own deserializer, whose
// output is a fresh json.Unmarshal result on every call and so can be cached
// as is.
func builtinDeserializer(d AudienceDeserializer) bool {
	switch d.(type) {
	case nil, DefaultAudienceDeserializer, *DefaultAudienceDeserializer:
		return true
	}
	return false
}

// parseAudienceUnlocked runs customer code without context locks and returns
// the parse for the requesting build, successful or not. Only the built-in
// deserializer's usable parses enter the shared cache: a custom deserializer
// may reuse its result storage between calls, so its result is evaluated by
// the next locked attempt and never kept. A panic is detected by whether the
// parse returned, not by recover's value, so panic(nil) on Go versions where
// recover returns nil for it counts as a panic too.
func (c *Context) parseAudienceUnlocked(st *contextState, name string, data jsonmodels.Experiment) *audienceParse {
	var deserializer = c.AudienceMatcher_.Deserializer_
	var text = data.Audience
	var parse = &audienceParse{id: data.Id, iteration: data.Iteration, text: text, deserializer: deserializer}
	var builtin = builtinDeserializer(deserializer)
	if builtin {
		if parsed, ok := c.cachedAudience(st, text, deserializer); ok {
			parse.parsed = parsed
			return parse
		}
	}
	var returned bool
	var panicked interface{}
	parse.parsed = func() (parsed parsedAudience) {
		defer func() {
			if !returned {
				panicked = recover()
				parsed = parsedAudience{err: errAudienceUnparseable}
			}
		}()
		parsed = AudienceMatcher{deserializer}.parseAudience(text)
		returned = true
		return parsed
	}()
	if !returned {
		c.logAudiencePanic(st, name, parse, panicked)
		return parse
	}
	if builtin && parse.parsed.err == nil {
		st.audiences.Store(text, &audienceEntry{deserializer: deserializer, parsed: parse.parsed})
		if !st.hasAudience(text) {
			// Data installed during parsing may already have pruned this text.
			st.audiences.Delete(text)
		}
	}
	return parse
}

func (c *Context) logAudiencePanic(st *contextState, name string, failed *audienceParse, panicked interface{}) {
	c.ContextLock_.Lock()
	if st.audiencePanics == nil {
		st.audiencePanics = map[string][]*audienceParse{}
	}
	st.audiencePanics[name] = append(st.audiencePanics[name], failed)
	c.ContextLock_.Unlock()
	defer func() {
		c.ContextLock_.Lock()
		defer c.ContextLock_.Unlock()
		var active = st.audiencePanics[name]
		for i, entry := range active {
			if entry == failed {
				active = append(active[:i], active[i+1:]...)
				break
			}
		}
		if len(active) == 0 {
			delete(st.audiencePanics, name)
		} else {
			st.audiencePanics[name] = active
		}
	}()
	c.LogError(fmt.Errorf("AudienceDeserializer panicked: %v", panicked))
}

// cachedAudience returns a parse the built-in deserializer made earlier in
// parseAudienceUnlocked, when deserializer is the one that made it. It may be
// called with ContextLock_ held, so it never runs the deserializer; it reports
// false when the audience was not parsed yet, and always for a custom
// deserializer, whose parses are never cached.
func (c *Context) cachedAudience(st *contextState, text string, deserializer AudienceDeserializer) (parsedAudience, bool) {
	if entry, ok := st.audiences.Load(text); ok {
		var entry = entry.(*audienceEntry)
		if sameDeserializer(entry.deserializer, deserializer) {
			return entry.parsed, true
		}
	}
	// A miss carries the unparseable error, so evaluating it by mistake can
	// never read a nil filter.
	return parsedAudience{err: errAudienceUnparseable}, false
}

// pruneAudiences drops the parses of audience texts the installed data no
// longer has, so the cache does not grow with every refresh.
func (st *contextState) pruneAudiences() {
	st.audiences.Range(func(text, _ interface{}) bool {
		if !st.hasAudience(text.(string)) {
			st.audiences.Delete(text)
		}
		return true
	})
}

// hasAudience reports whether an installed experiment has audience text.
func (st *contextState) hasAudience(text string) bool {
	if index := st.loadIndex(); index != nil {
		var _, found = index.audienceTexts()[text]
		return found
	}
	return false
}

// syncLiteralExperiment makes the private index entry of a context built as
// a literal follow its Index_ entry, which a caller may edit, so its
// assignments read Index_ as v1.1.1 did. Like v1.1.1's GetExperiment it reads
// Index_ without DataLock, so a caller holding DataLock can still read
// treatments. No customer code runs here: an edited entry keeps the rules
// SetData parsed for it only while its assignmentRules text is unchanged,
// and otherwise has none, as an entry present at the first read has none
// (v1.1.1 had no rules). Contexts the SDK created keep their own index.
func (c *Context) syncLiteralExperiment(st *contextState, experimentName string) {
	// indexLock_, not DataLock: setData holds it while it writes Index_ and
	// installs the index, so the two are read consistently and race-free,
	// and a caller holding DataLock can still read treatments.
	st.indexLock_.Lock()
	defer st.indexLock_.Unlock()
	var experiment, found = c.Index_[experimentName]
	var installedEntry = st.indexedExperiment(experimentName)
	var installed = installedEntry != nil
	var current ExperimentVariables
	var rules *experimentRules
	if installed {
		current, rules = installedEntry.experiment, installedEntry.rules
	}
	if found == installed && (!found || sameLiteralEntry(current, experiment)) {
		return
	}
	var indexed = map[string]*indexedExperiment{}
	// The edited entry keeps the installed holdouts, its payload's coverage
	// and the gates, so an Index_ edit never drops holdout coverage.
	// jsonmodels.Experiment carries no HoldoutIds: coverage comes from the
	// payload SetPayload installed for the experiment.
	var next = &contextIndex{experiments: indexed}
	if previous := st.loadIndex(); previous != nil {
		for name, entry := range previous.experiments {
			indexed[name] = entry
		}
		next.holdouts, next.keyed, next.heldOut = previous.holdouts, previous.keyed, previous.heldOut
	}
	if !installed || current.Data.AssignmentRules != experiment.Data.AssignmentRules {
		rules = nil
	}
	if found {
		var entry = &indexedExperiment{experiment: experiment, rules: rules}
		if installed {
			entry.payload = installedEntry.payload
		}
		if entry.payload != nil && !isSwitchback(entry.payload) && len(next.holdouts) > 0 {
			entry.holdouts = coveringHoldouts(entry.payload.HoldoutIds, next.holdouts)
		}
		if next.keyed {
			entry.variableKeys = variableKeysOf(experiment)
		}
		indexed[experimentName] = entry
	} else {
		delete(indexed, experimentName)
	}
	st.index_.Store(next)
}

// sameLiteralEntry reports whether an Index_ entry still matches the private
// copy in the fields an assignment reads, without reflection on the cached
// read path. Slices compare by identity: an entry copied from Index_ shares
// them, and a replaced one has new ones.
func sameLiteralEntry(a, b ExperimentVariables) bool {
	var x, y = a.Data, b.Data
	return x.Id == y.Id && x.Name == y.Name && x.UnitType == y.UnitType && x.Iteration == y.Iteration &&
		x.SeedHi == y.SeedHi && x.SeedLo == y.SeedLo && x.TrafficSeedHi == y.TrafficSeedHi &&
		x.TrafficSeedLo == y.TrafficSeedLo && x.FullOnVariant == y.FullOnVariant &&
		x.Audience == y.Audience && x.AudienceStrict == y.AudienceStrict &&
		sameSplit(x.Split, y.Split) && sameSplit(x.TrafficSplit, y.TrafficSplit) &&
		x.AssignmentRules == y.AssignmentRules &&
		len(x.Variants) == len(y.Variants) && (len(x.Variants) == 0 || &x.Variants[0] == &y.Variants[0]) &&
		len(a.Variables) == len(b.Variables) && (len(a.Variables) == 0 || &a.Variables[0] == &b.Variables[0])
}

// variableKeysOf returns the variable keys an experiment defines in any
// variant. The keys are copied, so the set never retains the parsed variant
// maps.
func variableKeysOf(experiment ExperimentVariables) variableKeySet {
	var keys = variableKeySet{}
	for _, variables := range experiment.Variables {
		for key := range variables {
			if _, found := keys[key]; !found {
				keys[string(append([]byte(nil), key...))] = struct{}{}
			}
		}
	}
	return keys
}

// experimentAndRules is GetExperiment plus the experiment's parsed assignment
// rules, read from the same installed index.
func (st *contextState) experimentAndRules(experimentName string) (ExperimentVariables, *experimentRules, bool) {
	if entry := st.indexedExperiment(experimentName); entry != nil {
		return entry.experiment, entry.rules, true
	}
	return ExperimentVariables{}, nil, false
}

func (st *contextState) indexedExperiment(experimentName string) *indexedExperiment {
	var index = st.loadIndex()
	if index == nil {
		return nil
	}
	return index.experiments[experimentName]
}

func (c *Context) ExperimentMatches(experiment jsonmodels.Experiment, assignment Assignment) bool {
	return experimentMatches(&experiment, &assignment)
}

// experimentMatches reads the installed definition and cached assignment in
// place; neither needs to be copied on every warmed cache hit.
func experimentMatches(experiment *jsonmodels.Experiment, assignment *Assignment) bool {
	return experiment.Id == assignment.Id &&
		experiment.UnitType == assignment.UnitType &&
		experiment.Iteration == assignment.Iteration &&
		experiment.FullOnVariant == assignment.FullOnVariant &&
		sameSplit(experiment.TrafficSplit, assignment.TrafficSplit)
}

// sameSplit is reflect.DeepEqual for two []float64, without its reflection
// and allocation on the cached-read path. As DeepEqual: nil and empty differ,
// the same backing array is equal, and other elements compare with == (so a
// NaN in two different arrays differs).
func sameSplit(a []float64, b []float64) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	if len(a) == 0 || &a[0] == &b[0] {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Errors returned by context reads and writes. The texts are the ones
// v1.1.x returned, so callers comparing err.Error() keep working; new code
// should use errors.Is.
var (
	ErrContextClosed   = errors.New("ABSmartly Context is closed")
	ErrContextClosing  = errors.New("ABSmartly Context is closing")
	ErrContextNotReady = errors.New("ABSmartly Context is not yet ready")
)

// ErrUnitAlreadySet and ErrUnitBlank keep the v1.1.1 error texts. SetUnit
// and SetUnitValue return them wrapped in a *UnitError, so they can be
// matched with errors.Is.
var (
	ErrUnitAlreadySet = errors.New("unit already set")
	ErrUnitBlank      = errors.New("unit  UID must not be blank.") //nolint:staticcheck // v1.1.1 error text
)

// UnitError is returned when a unit cannot be set. Error returns the v1.1.1
// text; Canonical returns the text the other ABsmartly SDKs use.
type UnitError struct {
	UnitType string
	Err      error
}

func (e *UnitError) Error() string {
	return e.Err.Error()
}

func (e *UnitError) Unwrap() error {
	return e.Err
}

// Canonical returns the cross-SDK error text, for example
// "Unit 'user_id' UID already set.".
func (e *UnitError) Canonical() string {
	switch e.Err {
	case ErrUnitAlreadySet:
		return fmt.Sprintf("Unit '%s' UID already set.", e.UnitType)
	case ErrUnitBlank:
		return fmt.Sprintf("Unit '%s' UID must not be blank.", e.UnitType)
	}
	return e.Err.Error()
}

func (c *Context) CheckNotClosed() error {
	if c.Closed_.Load().(bool) {
		return ErrContextClosed
	} else if c.Closing_.Load().(bool) {
		return ErrContextClosing
	}
	return nil
}

func (c *Context) CheckReady(expectNotClosed bool) error {
	if !c.IsReady() {
		return ErrContextNotReady
	} else if expectNotClosed {
		return c.CheckNotClosed()
	}
	return nil
}

func (c *Context) SetData(data jsonmodels.ContextData) {
	c.installData(contextInput{data: data})
}

// SetPayload is SetData for a full context payload, with holdouts and each
// experiment's holdout and switchback fields.
func (c *Context) SetPayload(data jsonmodels.ContextPayload) {
	c.installData(payloadInput(data))
}

func (c *Context) installData(data contextInput) {
	var receiver, root = c, c.live()
	var buffer = &diagnosticBuffer{}
	root.setData(data, buffer)
	receiver.adoptLiveDataFrom(root)
	receiver.dispatchDiagnostics(buffer.take())
	// A diagnostics logger may have installed newer data: adopt again, so the
	// copy's reads agree with the live data when SetData returns.
	receiver.adoptLiveDataFrom(root)
}

// adoptLiveDataFrom copies the data installed on root into c's own v1.1.1
// data fields when c is a copy without a state carrier, so its direct index
// lookups agree with reads through the live context. Resolve root before
// taking DataLock: live may take ContextLock_ on a clock-replaced context.
func (c *Context) adoptLiveDataFrom(root *Context) {
	if !c.needsLiveDataFrom(root) {
		return
	}
	c.DataLock.Lock()
	c.Index_ = root.Index_
	c.IndexVariables_ = root.IndexVariables_
	c.Data_ = root.Data_
	c.ContextCustomFields_ = root.ContextCustomFields_
	c.DataLock.Unlock()
}

func (c *Context) needsLiveDataFrom(root *Context) bool {
	return c != root && c.ownClock() == nil && c.ContextLock_ != nil
}

// diagnosticBuffer collects the events the parser and SetData emit while a
// load is running. They are delivered only after the load has finished, so a
// logger that reacts to a diagnostic by calling Refresh, Close or Publish
// never waits on the load that is reporting to it.
type diagnosticBuffer struct {
	mu     sync.Mutex
	events []bufferedEvent
}

type bufferedEvent struct {
	event EventType
	data  interface{}
}

func (b *diagnosticBuffer) HandleEvent(_ Context, event EventType, data interface{}) {
	b.mu.Lock()
	b.events = append(b.events, bufferedEvent{event, data})
	b.mu.Unlock()
}

func (b *diagnosticBuffer) take() []bufferedEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	var events = b.events
	b.events = nil
	return events
}

func (c *Context) dispatchDiagnostics(events []bufferedEvent) {
	for _, e := range events {
		c.logEvent(e.event, e.data)
	}
}

func (c *Context) setData(input contextInput, buffer *diagnosticBuffer) {
	var data = input.data
	c = c.live()
	var st = c.state()
	var self = c.snapshot()
	self.EventLogger_ = buffer
	var index = map[string]ExperimentVariables{}
	var indexVariables = map[string][]ExperimentVariables{}
	var contextCustomFields = map[string]map[string]ContextCustomFieldValue{}
	var rules = map[string]*experimentRules{}
	var holdoutsById map[int]*jsonmodels.Holdout
	// A copy, so assignments and GetPayload keep the holdouts as installed
	// even if the caller reuses its slice.
	var installedHoldouts = copyHoldouts(input.holdouts)
	if len(input.holdouts) > 0 {
		var installed = installedHoldouts
		holdoutsById = make(map[int]*jsonmodels.Holdout, len(installed))
		// An id defined more than once with definitions that draw different
		// arms (iteration, seeds or split) covers nothing, so the outcome never
		// depends on the entries' order; duplicates that draw the same arm
		// count as one (the first one's name is published). The collector
		// never sends duplicates.
		// A conflicting id is kept as a nil sentinel until the end, so a later
		// duplicate can never reinstate it.
		var conflicts = false
		for i := range installed {
			if len(installed[i].Split) == 0 {
				continue
			}
			if seen, found := holdoutsById[installed[i].Id]; found {
				if seen != nil && !sameHoldoutDraw(seen, &installed[i]) {
					holdoutsById[installed[i].Id] = nil
					conflicts = true
				}
				continue
			}
			installed[i].Split = append([]float64(nil), installed[i].Split...)
			holdoutsById[installed[i].Id] = &installed[i]
		}
		if conflicts {
			for id, holdout := range holdoutsById {
				if holdout == nil {
					delete(holdoutsById, id)
				}
			}
		}
	}
	var coverage map[string][]*jsonmodels.Holdout
	var switchbacks map[string]*switchbackDefinition
	var previous = st.loadIndex()
	var previousRules = func(name string) *experimentRules {
		if previous != nil {
			if entry, found := previous.experiments[name]; found {
				return entry.rules
			}
		}
		return nil
	}

	// The holdout and switchback fields of each experiment, attached to its
	// index entry below; none for a plain ContextData. The copy keeps the
	// installed definitions apart from the caller's slice.
	var extendedAt = copyExperimentData(input.experiments)
	var payloads map[string]*jsonmodels.ExperimentData
	for i, experiment := range data.Experiments {
		var extended *jsonmodels.ExperimentData
		if extendedAt != nil {
			extended = &extendedAt[i]
			// A private copy, so an in-place edit of the caller's slice never
			// changes the coverage installed (or recomputed) from it.
			extended.HoldoutIds = append([]int(nil), extended.HoldoutIds...)
			// By name as index is, so a repeated name keeps the last.
			if payloads == nil {
				payloads = make(map[string]*jsonmodels.ExperimentData, len(extendedAt))
			}
			payloads[experiment.Name] = extended
		}
		var experiemntVariables = ExperimentVariables{}
		var experimentCustomFields = map[string]ContextCustomFieldValue{}
		experiemntVariables.Data = experiment
		experiemntVariables.Variables = make([]map[string]interface{}, 0)

		for _, variant := range experiment.Variants {
			if len(variant.Config) > 0 {
				var variables = c.VariableParser_.Parse(self, experiment.Name, variant.Name, variant.Config)
				for key := range variables {
					var existing = indexVariables[key]
					var already = false
					for _, e := range existing {
						if e.Data.Name == experiment.Name {
							already = true
							break
						}
					}
					if !already {
						indexVariables[key] = append(existing, experiemntVariables)
					}
				}
				experiemntVariables.Variables = append(experiemntVariables.Variables, variables)
			} else {
				experiemntVariables.Variables = append(experiemntVariables.Variables, map[string]interface{}{})
			}
		}

		for _, customFieldValue := range experiment.CustomFieldValues {
			var value = ContextCustomFieldValue{}
			value.Type = customFieldValue.Type
			value.Name = customFieldValue.Name
			if len(customFieldValue.Value) > 0 {
				var customValue = customFieldValue.Value
				if strings.HasPrefix(customFieldValue.Type, "json") {
					if parser, ok := customFieldValueParser(c.VariableParser_); ok {
						value.Value, _ = parser.ParseCustomFieldValue(self, experiment.Name, customFieldValue.Name, customValue)
					} else {
						value.Value = c.VariableParser_.Parse(self, experiment.Name, customFieldValue.Name, customValue)
					}
				} else if strings.HasPrefix(customFieldValue.Type, "boolean") {
					value.Value, _ = strconv.ParseBool(customValue)
				} else if strings.HasPrefix(customFieldValue.Type, "number") {
					value.Value, _ = strconv.ParseInt(customValue, 10, 64)
				} else {
					value.Value = customValue
				}

				experimentCustomFields[value.Name] = value
			}
		}
		if len(experiment.AssignmentRules) > 0 {
			rules[experiment.Name] = c.experimentRulesFor(previousRules(experiment.Name), experiment.AssignmentRules)
		}
		if isSwitchback(extended) {
			if switchbacks == nil {
				switchbacks = map[string]*switchbackDefinition{}
			}
			switchbacks[experiment.Name] = newSwitchbackDefinition(extended.SwitchbackClusters)
		} else if extended != nil && len(extended.HoldoutIds) > 0 && len(holdoutsById) > 0 {
			if covering := coveringHoldouts(extended.HoldoutIds, holdoutsById); len(covering) > 0 {
				if coverage == nil {
					coverage = map[string][]*jsonmodels.Holdout{}
				}
				coverage[experiment.Name] = covering
			}
		}

		contextCustomFields[experiment.Name] = experimentCustomFields
		index[experiment.Name] = experiemntVariables
	}

	// Keep each key's experiments sorted and unique by experiment id, as the
	// Java and JS SDKs do. GetVariableValue/PeekVariableValue walk this list in
	// id order (see resolveVariableValue), regardless of the experiment order
	// in the payload. GetVariableKeys, GetVariableKeysAll and
	// IndexVariables_ follow the same id order.
	for key := range indexVariables {
		var experiments = indexVariables[key]
		sort.SliceStable(experiments, func(i, j int) bool {
			return experiments[i].Data.Id < experiments[j].Data.Id
		})
	}

	// Serialize publication with resolution to keep coverage and arms in one generation.
	c.ContextLock_.Lock()
	c.DataLock.Lock()

	var indexed = make(map[string]*indexedExperiment, len(index))
	// Each installed experiment's key set is owned by the index, not by
	// cached assignments, so old decisions retain neither variant values
	// nor key sets. Only a holdout, a SetUnit eviction of a split rule or a
	// switchback slot retirement retires an assignment, so the sets are built
	// only once the context has seen holdouts, assignment rules or
	// switchbacks; payloads with none of them cost nothing more.
	// The index installed now is read under the lock, so an install that
	// finished while this one was parsing does not lose the gate.
	var current = st.loadIndex()
	var keyed = len(holdoutsById) > 0 || len(rules) > 0 || len(switchbacks) > 0 || (current != nil && current.keyed)
	var heldOut = len(holdoutsById) > 0 || len(switchbacks) > 0 || (current != nil && current.heldOut)
	for name, experiment := range index {
		var keys variableKeySet
		if keyed {
			keys = variableKeysOf(experiment)
		}
		indexed[name] = &indexedExperiment{experiment: experiment, rules: rules[name], holdouts: coverage[name], variableKeys: keys,
			switchback: switchbacks[name], payload: payloads[name]}
	}
	if switchbacks != nil {
		// Under ContextLock_, so the generations follow the index this
		// replaces, whatever other setData ran meanwhile.
		c.assignSwitchbackGenerations(st, st.loadIndex(), indexed)
	}
	st.indexLock_.Lock()
	c.Index_ = index
	st.index_.Store(&contextIndex{experiments: indexed, holdouts: holdoutsById, keyed: keyed, heldOut: heldOut})
	st.indexLock_.Unlock()
	st.pruneAudiences()
	c.ContextCustomFields_ = contextCustomFields
	st.indexVariables_ = indexVariables
	c.IndexVariables_ = legacyIndexVariables(indexVariables)
	c.Data_ = data
	st.payloadInstalled_ = input.payload
	st.payloadExperiments_, st.payloadHoldouts_ = extendedAt, installedHoldouts
	c.Ready_.Store(true)

	c.setRefreshTimer(st)
	c.DataLock.Unlock()
	c.ContextLock_.Unlock()
}

// sameHoldoutDraw reports whether two definitions of one holdout id draw the
// same arm for every unit: same iteration, seeds and split.
func sameHoldoutDraw(a *jsonmodels.Holdout, b *jsonmodels.Holdout) bool {
	return a.Iteration == b.Iteration && a.SeedHi == b.SeedHi && a.SeedLo == b.SeedLo &&
		reflect.DeepEqual(a.Split, b.Split)
}

// coveringHoldouts resolves an experiment's holdoutIds to the installed
// holdouts, sorted and unique by id; ids with no holdout are ignored. JS:
// insertUniqueSorted in _init.
func coveringHoldouts(ids []int, holdoutsById map[int]*jsonmodels.Holdout) []*jsonmodels.Holdout {
	var covering []*jsonmodels.Holdout
	for _, id := range ids {
		if holdout, found := holdoutsById[id]; found {
			covering = append(covering, holdout)
		}
	}
	if len(covering) < 2 {
		return covering
	}
	sort.Slice(covering, func(i, j int) bool { return covering[i].Id < covering[j].Id })
	var unique = covering[:0]
	for _, holdout := range covering {
		if len(unique) == 0 || unique[len(unique)-1].Id != holdout.Id {
			unique = append(unique, holdout)
		}
	}
	return unique
}

// experimentRulesFor parses an experiment's assignmentRules text, or returns
// previous when a refresh left the text unchanged, so cached assignments keep
// validating against the same rule set. Rules that cannot be parsed give no
// rules for that experiment only. As in the JS SDK, which only writes them to
// the console, they are not reported to the event logger as errors: the
// context and its other experiments are unaffected.
func (c *Context) experimentRulesFor(previous *experimentRules, text string) *experimentRules {
	if previous != nil && previous.text == text {
		return previous
	}
	var parsed, _ = parseRules(text)
	return &experimentRules{text: text, parsed: parsed}
}

// LogEvent and LogError pass an event to the event logger. A logger is an
// observer: a panic in it is contained here, so it can neither end the host
// process from an SDK goroutine nor leave the SDK's own state half updated.
func (c *Context) LogEvent(event EventType, data interface{}) {
	c.logEvent(event, data)
}

func (c *Context) LogError(err error) {
	c.logEvent(Error, err)
}

func (c *Context) logEvent(event EventType, data interface{}) {
	if c.EventLogger_ != nil {
		var snapshot = c.snapshot()
		if loggers := c.live().state().loggers_; loggers != nil {
			var id = loggers.enter()
			defer loggers.exit(id)
		}
		notifyLogger(c.EventLogger_, snapshot, event, data)
	}
}

// notifyLogger calls an event logger and contains its panic. Every call into a
// logger goes through here, including the ones made while a
// context is being created or its data parsed, so a logger can never end the
// host process or abort the SDK code that reports to it.
func notifyLogger(logger ContextEventLogger, ctx Context, event EventType, data interface{}) {
	if logger == nil {
		return
	}
	defer func() { _ = recover() }()
	logger.HandleEvent(ctx, event, data)
}

func panicToError(r interface{}) error {
	if err, ok := r.(error); ok {
		return err
	}
	return &future.PanicError{Value: r}
}

func (c *Context) SetDataFailed(err error) {
	var receiver = c
	c = c.live()
	var st = c.state()
	c.ContextLock_.Lock()
	defer c.ContextLock_.Unlock()
	c.DataLock.Lock()
	st.indexLock_.Lock()
	c.Index_ = map[string]ExperimentVariables{}
	var keyed, heldOut = false, false
	if previous := st.loadIndex(); previous != nil {
		keyed, heldOut = previous.keyed, previous.heldOut
	}
	st.index_.Store(&contextIndex{keyed: keyed, heldOut: heldOut})
	st.indexLock_.Unlock()
	st.indexVariables_ = map[string][]ExperimentVariables{}
	c.IndexVariables_ = map[interface{}]interface{}{}
	c.Data_ = jsonmodels.ContextData{}
	st.payloadInstalled_, st.payloadExperiments_, st.payloadHoldouts_ = false, nil, nil
	c.Ready_.Store(true)
	c.Failed_.Store(true)
	c.DataLock.Unlock()
	receiver.adoptLiveDataFrom(c)
}

func (c *Context) Flush() *future.Future {
	var result, _ = c.flush()
	return result
}

// flush is Flush, also returning the operation that runs the publish's event
// logger, or nil when nothing was sent.
func (c *Context) flush() (*future.Future, *gateOperation) {
	c = c.live()
	var st = c.state()
	c.ClearTimeout()

	if !c.Failed_.Load().(bool) {
		// Read the count under EventLock_: a Track or exposure that passed its
		// closed check before Close began appends under this lock, and must be
		// drained by this flush rather than left queued behind it.
		var eventCount int32

		c.EventLock_.Lock()
		eventCount = c.PendingCount_.Load().(int32)
		var exposures = make([]jsonmodels.Exposure, len(c.Exposures_))
		var achievements = make([]jsonmodels.GoalAchievement, len(c.Achievements_))
		// Close waits on transport settlement independently of logger delivery.
		// Publish callers wait on result, which includes logger delivery.
		var settled, settledDone = future.New()
		var result, done = future.New()
		var op = &gateOperation{}

		if eventCount > 0 {
			if len(c.Exposures_) > 0 {
				copy(exposures, c.Exposures_)
				c.Exposures_ = make([]jsonmodels.Exposure, 0)
			}

			if len(c.Achievements_) > 0 {
				copy(achievements, c.Achievements_)
				c.Achievements_ = nil
			}

			c.PendingCount_.Store(int32(0))
			// Register the publish in the same critical section that drains
			// the queue, so CloseAsync cannot miss it.
			st.addInflight(settled)
			st.stateLock_.Lock()
			var activeLogs []*future.Future
			for _, f := range st.publishLogs_ {
				if !f.Ready() {
					activeLogs = append(activeLogs, f)
				}
			}
			st.publishLogs_ = append(activeLogs, result)
			if st.publishOps_ == nil {
				st.publishOps_ = map[*gateOperation]struct{}{}
			}
			st.publishOps_[op] = struct{}{}
			for lender := range st.closeLenders_ {
				lender.add(op)
			}
			st.stateLock_.Unlock()
		}
		c.EventLock_.Unlock()

		if eventCount > 0 {
			var event = jsonmodels.PublishEvent{}
			event.Hashed = true
			event.PublishedAt = c.Clock_.Millis()

			c.ContextLock_.Lock()
			event.Units = make([]jsonmodels.Unit, 0, len(c.Units_))
			for key, value := range c.Units_ {
				event.Units = append(event.Units, flushUnit(c, key, value))
			}
			if len(c.Attributes_) == 0 {
				event.Attributes = make([]jsonmodels.Attribute, 0)
			} else {
				event.Attributes = make([]jsonmodels.Attribute, len(c.Attributes_))
				for key, value := range c.Attributes_ {
					event.Attributes[key] = value.(jsonmodels.Attribute)
				}
			}
			c.ContextLock_.Unlock()
			event.Goals = achievements
			event.Exposures = exposures

			c.callPublisher(event).Listen(
				func(value future.Value, err error) {
					// The publisher may return a future that is already complete,
					// which runs this callback on the caller's stack, for example
					// inside CloseAsync. Run the logger and settle the caller's
					// future on their own goroutine, so a logger that calls Close
					// cannot block the call that started the publish.
					if err == nil {
						// Start the logger first: settledDone runs Close's listeners
						// synchronously, and a panic there must not leave the
						// caller's future unsettled.
						runLogger(st, op, done, nil, func() { c.logEvent(Publish, event) })
					} else {
						c.requeueEvents(exposures, achievements, eventCount)
						runLogger(st, op, done, err, func() { c.logEvent(Error, err) })
					}
					st.removeInflight(settled)
					settledDone(nil, err)
				})

			return result, op
		}
	} else {
		c.EventLock_.Lock()
		c.Exposures_ = nil
		c.Achievements_ = nil
		c.PendingCount_.Store(int32(0))
		c.EventLock_.Unlock()
	}

	result, done := future.New()
	done(nil, nil)
	return result, nil
}

func (st *contextState) addInflight(f *future.Future) {
	st.stateLock_.Lock()
	st.inflight_ = append(st.inflight_, f)
	st.stateLock_.Unlock()
}

// removeInflight drops a settled publish from the state that registered it
// (Flush captured it), without reading Clock_ again.
func (st *contextState) removeInflight(f *future.Future) {
	st.stateLock_.Lock()
	for i, g := range st.inflight_ {
		if g == f {
			st.inflight_ = append(st.inflight_[:i:i], st.inflight_[i+1:]...)
			break
		}
	}
	st.stateLock_.Unlock()
}

func (c *Context) inflightCount() int {
	var st = c.state()
	st.stateLock_.Lock()
	defer st.stateLock_.Unlock()
	return len(st.inflight_)
}

func (st *contextState) inflightSnapshot() []*future.Future {
	st.stateLock_.Lock()
	defer st.stateLock_.Unlock()
	return append([]*future.Future(nil), st.inflight_...)
}

// runLogger runs an observer on its own goroutine so lifecycle re-entry cannot
// block the publisher callback. Logger panics never replace the publish outcome.
//
// The logger goroutine is op's executor while it runs, so a Publish waiting
// for it can lend the custom audience gate.
func runLogger(st *contextState, op *gateOperation, done future.SetResultFunc, result error, log func()) {
	go func() {
		var end = st.customAudiences.execute(op)
		func() {
			defer end()
			log()
		}()
		st.stateLock_.Lock()
		delete(st.publishOps_, op)
		st.stateLock_.Unlock()
		done(nil, result)
	}()
}

// publishLender is a holder's lend of the custom audience gate to the
// publishes a Close it is waiting in depends on.
type publishLender struct {
	gate   *reentrantGate
	id     uint64
	active bool
	ops    []*gateOperation
	ends   []func()
}

// add records op, and lends the gate to it at once if the holder is already
// blocked waiting. stateLock_ is held; lendAs only takes the gate's mutex.
func (l *publishLender) add(op *gateOperation) {
	l.ops = append(l.ops, op)
	if l.active {
		l.ends = append(l.ends, l.gate.lendAs(l.id, op))
	}
}

// lendPublishes registers the caller, if it holds the custom audience gate,
// for every publish whose logger is running now or starts before the
// returned end is called: a Close cannot end before those loggers return,
// and on a failed close its finalization waits for publishes started
// meanwhile. The returned wait must wrap the blocking wait itself: the gate
// is lent only while the holder is blocked there, so it never decodes at the
// same time as an admitted logger.
func (st *contextState) lendPublishes() (wait func(func()), end func()) {
	var lender = &publishLender{gate: &st.customAudiences, id: goroutineID()}
	st.stateLock_.Lock()
	if st.closeLenders_ == nil {
		st.closeLenders_ = map[*publishLender]struct{}{}
	}
	st.closeLenders_[lender] = struct{}{}
	for op := range st.publishOps_ {
		lender.add(op)
	}
	st.stateLock_.Unlock()
	wait = func(block func()) {
		st.stateLock_.Lock()
		lender.active = true
		for _, op := range lender.ops {
			lender.ends = append(lender.ends, lender.gate.lendAs(lender.id, op))
		}
		st.stateLock_.Unlock()
		defer func() {
			st.stateLock_.Lock()
			lender.active = false
			var ends = lender.ends
			lender.ends = nil
			st.stateLock_.Unlock()
			for _, e := range ends {
				e()
			}
		}()
		block()
	}
	end = func() {
		st.stateLock_.Lock()
		delete(st.closeLenders_, lender)
		st.stateLock_.Unlock()
	}
	return wait, end
}

// callPublisher calls the event handler and always returns a future. A
// handler that panics or returns nil would otherwise leave the drained batch
// in flight forever, and Close would wait for it for ever.
func (c *Context) callPublisher(event jsonmodels.PublishEvent) (published *future.Future) {
	defer func() {
		if r := recover(); r != nil {
			var failed, fail = future.New()
			fail(nil, panicToError(r))
			published = failed
		}
	}()
	published = c.EventHandler_.Publish(c.snapshot(), event)
	if published == nil {
		var failed, fail = future.New()
		fail(nil, errors.New("event handler returned no future"))
		published = failed
	}
	return published
}

// callDataProvider is callPublisher for the data provider.
func (c *Context) callDataProvider() (loaded *future.Future) {
	defer func() {
		if r := recover(); r != nil {
			var failed, fail = future.New()
			fail(nil, panicToError(r))
			loaded = failed
		}
	}()
	loaded = contextPayloadFuture(c.DataProvider_)
	if loaded == nil {
		var failed, fail = future.New()
		fail(nil, errors.New("data provider returned no future"))
		loaded = failed
	}
	return loaded
}

// requeueEvents puts the events of a failed publish back in the queue so a
// later Publish or Close can send them again. Flush empties the queue before
// it calls the publisher, so events recorded while the publish was in flight
// are already queued. The failed batch goes in front of them to keep the
// queue in the order the events happened.
func (c *Context) requeueEvents(exposures []jsonmodels.Exposure, achievements []jsonmodels.GoalAchievement, count int32) {
	c.EventLock_.Lock()
	defer c.EventLock_.Unlock()

	// Build new slices. The publisher can still hold the event that references
	// the failed batch, so the batch slices must not be changed.
	var restoredExposures = make([]jsonmodels.Exposure, 0, len(exposures)+len(c.Exposures_))
	restoredExposures = append(restoredExposures, exposures...)
	c.Exposures_ = append(restoredExposures, c.Exposures_...)

	var restoredAchievements = make([]jsonmodels.GoalAchievement, 0, len(achievements)+len(c.Achievements_))
	restoredAchievements = append(restoredAchievements, achievements...)
	c.Achievements_ = append(restoredAchievements, c.Achievements_...)

	c.PendingCount_.Store(c.PendingCount_.Load().(int32) + count)
}

type FlushMapper struct {
	MapperInt
	Context Context
}

func (f FlushMapper) Apply(value interface{}) interface{} {
	return flushUnit(&f.Context, value.(Pair).a, value.(Pair).b)
}

// flushUnit is a published unit: its type and hashed UID. Flush calls it on
// the live context directly, which must not be copied into a FlushMapper.
func flushUnit(cntx *Context, unitType string, uid string) jsonmodels.Unit {
	var data [22]byte
	var hash = cntx.GetUnitHash(unitType, uid, data[:], false)
	return jsonmodels.Unit{Type: unitType, Uid: asciiOnly(string(hash))}
}

func asciiOnly(s string) string {
	rs := make([]rune, 0, len(s))
	for _, r := range s {
		if r <= 127 {
			rs = append(rs, r)
		}
	}
	return string(rs)
}

func (c *Context) ClearTimeout() {
	c = c.live()
	c.TimeoutLock_.Lock()
	if c.Timeout_ != nil {
		c.Timeout_.Stop()
		c.Timeout_ = nil
	}
	c.TimeoutLock_.Unlock()
}

func (c *Context) SetTimeout() {
	c.live().armTimeout()
}

// armTimeout arms the publish timeout on c, which must be the live context.
// It does not read Clock_, so the ready callback can call it on the context it
// created while a caller may be replacing Clock_.
func (c *Context) armTimeout() {
	if c.IsReady() {
		c.TimeoutLock_.Lock()
		if c.Timeout_ == nil && c.PublishDelay_ >= 0 {
			var delay = uint64(c.PublishDelay_ * int64(time.Millisecond))
			if delay < 1 {
				delay = 1
			}

			c.Timeout_ = time.AfterFunc(time.Duration(delay), func() {
				c.Flush()
			})
		}
		c.TimeoutLock_.Unlock()
	}
}

func (c *Context) SetRefreshTimer() {
	c = c.live()
	c.setRefreshTimer(c.state())
}

func (c *Context) setRefreshTimer(st *contextState) {
	st.stateLock_.Lock()
	defer st.stateLock_.Unlock()
	// A refresh that finishes after Close began calls SetData, which gets here.
	// Installing the timer then would leave a timer on a closed context.
	if c.Closed_.Load().(bool) || c.Closing_.Load().(bool) || st.closeStarted_ {
		return
	}
	if c.RefreshInterval_ > 0 && c.RefreshTimer_ == nil {
		var rate = time.Duration(uint64(c.RefreshInterval_ * int64(time.Millisecond)))

		c.RefreshTimer_ = time.AfterFunc(rate, func() {
			st.stateLock_.Lock()
			var timer = c.RefreshTimer_
			if timer != nil {
				timer.Reset(rate)
			}
			st.stateLock_.Unlock()
			if timer != nil {
				c.RefreshAsync()
			}
		})
	}
}

func (c *Context) GetUnitHash(unitType string, unitUID string, data []byte, needlock bool) []byte {
	var computer = ComputerUnitHash{UnitUID: unitUID}
	var result = ComputeIfAbsentRW(c.ContextLock_, needlock, c.HashedUnits_, unitType, computer).([]int8)
	for i, val := range result {
		data[i] = byte(val)
	}
	return data
}

func (c *Context) GetVariantAssigner(unitType string, hash []byte, needlock bool) *VariantAssigner {
	var computer = ComputerVariantAssigner{Hash: hash}
	return ComputeIfAbsentRW(c.ContextLock_, needlock, c.Assigners_, unitType, computer).(*VariantAssigner)
}

func (c *Context) GetVariableAssignment(key string) (*Assignment, error) {
	// Look the key up on the context whose index GetAssignment reads, so a
	// stale copy cannot pair an old experiment with the live index.
	c = c.live()
	var experiment, err = c.GetVariableExperiment(key)

	if err == nil {
		return c.GetAssignment(experiment.Data.Name), nil
	}
	var exposed = &atomic.Value{}
	exposed.Store(false)
	return &Assignment{Exposed: exposed}, err
}

func legacyIndexVariables(index map[string][]ExperimentVariables) map[interface{}]interface{} {
	var legacy = make(map[interface{}]interface{}, len(index))
	for key, experiments := range index {
		if len(experiments) > 0 {
			legacy[key] = experiments[0]
		}
	}
	return legacy
}

func (c *Context) GetVariableExperiment(key string) (ExperimentVariables, error) {
	var clock = c.ownClock()
	if clock != nil && clock.state.manualRoot == nil {
		c, clock = clock.root, nil
	}
	if clock == nil {
		if c.DataLock != nil {
			c.DataLock.RLock()
			defer c.DataLock.RUnlock()
		}
		if experiment, ok := c.IndexVariables_[key].(ExperimentVariables); ok {
			return experiment, nil
		}
		return ExperimentVariables{}, errors.New("result is nil")
	}
	c.DataLock.RLock()
	var experiments = clock.state.indexVariables_[key]
	c.DataLock.RUnlock()
	if len(experiments) == 0 {
		return ExperimentVariables{}, errors.New("result is nil")
	}
	return experiments[0], nil
}

type ComputerVariantAssigner struct {
	MapperInt
	Hash []byte
}

func (c ComputerVariantAssigner) Apply(value interface{}) interface{} {
	var dst = make([]int8, len(c.Hash))
	for i := 0; i < len(c.Hash); i++ {
		dst[i] = int8(c.Hash[i])
	}
	return NewVariantAssigner(dst)
}

type ComputerUnitHash struct {
	MapperInt
	UnitUID string
}

func (c ComputerUnitHash) Apply(value interface{}) interface{} {
	return HashUnit(c.UnitUID)
}

type Pair struct {
	a, b string
}

// goroutineID returns the id of the calling goroutine, read from its stack
// header ("goroutine N [...").
func goroutineID() uint64 {
	var buf [64]byte
	var b = buf[:runtime.Stack(buf[:], false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i >= 0 {
		b = b[:i]
	}
	id, _ := strconv.ParseUint(string(b), 10, 64)
	return id
}
