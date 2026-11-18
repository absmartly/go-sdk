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
	// manualRoot is set only by the literal fallback; its copies share this
	// owner through Assigners_, just as constructed copies share a carrier.
	manualRoot *Context
	// finalizedFuture_ includes logger delivery; ClosingFuture_ only includes
	// transport/sealing, allowing loggers to re-enter Close safely.
	finalizedFuture_ *future.Future
	publishLogs_     []*future.Future
	// stateLock_ guards ReadyFuture_, RefreshFuture_, ClosingFuture_, RefreshTimer_ and closeStarted_.
	stateLock_ *sync.Mutex
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
	var state = newContextState()
	state.manualRoot = c
	var indexed = make(map[string]*indexedExperiment, len(c.Index_))
	for name, experiment := range c.Index_ {
		// Manually supplied v1.1.1 indexes have no parsed-rule metadata.
		// SetData installs it; don't call a user deserializer under this lock.
		indexed[name] = &indexedExperiment{experiment: experiment}
	}
	state.index_.Store(&contextIndex{experiments: indexed})
	state.indexVariables_ = map[string][]ExperimentVariables{}
	for key, value := range c.IndexVariables_ {
		if key, ok := key.(string); ok {
			if experiment, ok := value.(ExperimentVariables); ok {
				state.indexVariables_[key] = []ExperimentVariables{experiment}
			}
		}
	}
	for name, assignment := range c.AssignmentCache {
		var assignment = assignment
		state.assignmentCache_[name] = &cachedAssignment{assignment: &assignment}
	}
	if c.Assigners_ == nil {
		c.Assigners_ = map[interface{}]interface{}{}
	}
	c.Assigners_[contextStateKey{}] = state
	return state
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
}

// indexedExperiment is an installed experiment with its parsed assignment
// rules (nil when it has none), so a read finds both with one lookup.
type indexedExperiment struct {
	experiment ExperimentVariables
	rules      *experimentRules
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
	// Also register the state where the literal fallback looks for it, so a
	// caller that replaces the exported Clock_ (as v1.1.1 allowed) keeps the
	// context's rules, caches, system attributes and lifecycle.
	state.manualRoot = &cntx
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
				var result = val.(jsonmodels.ContextData)
				var diagnostics, panicErr = tmp.applyRefreshedData(result)
				if panicErr != nil {
					tmp.SetDataFailed(panicErr)
					tmp.dispatchDiagnostics(diagnostics)
					tmp.LogError(panicErr)
					return
				}
				tmp.dispatchDiagnostics(diagnostics)
				tmp.LogEvent(Ready, result)
			} else {
				tmp.SetDataFailed(err)
				tmp.LogError(err)
			}

		})
	} else {
		var tmp = &cntx
		var tempFuture, readyFutureDone = future.New()
		cntx.setReadyFuture(tempFuture)
		dataFuture.Listen(func(val future.Value, err error) {
			atomic.StoreUint64(tmp.state().readyDeliverer_, goroutineID())
			defer atomic.StoreUint64(tmp.state().readyDeliverer_, 0)
			if err == nil {
				result, ok := val.(jsonmodels.ContextData)
				if !ok {
					err = &TypeError{Expected: "jsonmodels.ContextData", Actual: val}
					tmp.SetDataFailed(err)
					tmp.LogError(err)
					readyFutureDone(nil, err)
					cntx.setReadyFuture(nil)
					return
				}
				var diagnostics, panicErr = tmp.applyRefreshedData(result)
				if panicErr != nil {
					tmp.SetDataFailed(panicErr)
					tmp.dispatchDiagnostics(diagnostics)
					tmp.LogError(panicErr)
					readyFutureDone(nil, panicErr)
					cntx.setReadyFuture(nil)
					return
				}
				// Log before settling, so WaitUntilReady returns after the
				// logger has seen the ready event.
				tmp.dispatchDiagnostics(diagnostics)
				tmp.LogEvent(Ready, result)
				readyFutureDone(result, nil)
				cntx.setReadyFuture(nil)

				if tmp.GetPendingCount() > 0 {
					tmp.SetTimeout()
				}
			} else {
				tmp.SetDataFailed(err)
				tmp.LogError(err)
				readyFutureDone(nil, err)
				cntx.setReadyFuture(nil)
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
	var st = c.state()
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

func (c *Context) live() *Context {
	if clock := c.ownClock(); clock != nil {
		return clock.root
	}
	if c.ContextLock_ != nil {
		return c.state().manualRoot
	}
	return c
}

func (c *Context) GetExperiments() ([]string, error) {
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
	var err = c.CheckReady(true)
	if err != nil {
		return jsonmodels.ContextData{}, err
	}

	c.DataLock.RLock()
	defer c.DataLock.RUnlock()
	return c.Data_, nil

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
	c.evictRuleAssignmentsMissingUnit(st, unitType)
	return nil
}

// evictRuleAssignmentsMissingUnit drops the cached split-rule assignments that
// were resolved without unitType's unit, so they are hashed with the unit now
// that it is set. Assign rules never need the unit and keep their assignment.
// An exposed assignment is kept: re-resolving it would switch the user's
// variant and queue a second, conflicting exposure. An evicted assignment is
// marked exposed so a caller still holding it cannot queue it. The caller
// holds ContextLock_ for writing.
func (c *Context) evictRuleAssignmentsMissingUnit(st *contextState, unitType string) {
	for name, entry := range st.assignmentCache_ {
		var assignment = entry.assignment
		if entry.ruleOverride && entry.ruleMissingUnit && assignment.UnitType == unitType &&
			assignment.Exposed.CompareAndSwap(false, true) {
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
	var entry = c.assignmentEntry(experimentName)
	if !entry.assignment.Exposed.Load().(bool) {
		c.queueExposure(entry.assignment, entry.ruleOverride)
	}

	return entry.assignment.Variant, nil
}

func (c *Context) QueueExposure(assignment *Assignment) {
	c = c.live()
	var st = c.state()
	// A copy returned by GetAssignment shares the cached assignment's
	// exposed flag, which identifies the cache entry its rule flag is on.
	var ruleOverride = st.ruleIdentities.contains(assignment.Exposed)
	c.ContextLock_.RLock()
	if entry, found := st.assignmentCache_[assignment.Name]; found && entry.assignment.Exposed == assignment.Exposed {
		ruleOverride = entry.ruleOverride
	}
	c.ContextLock_.RUnlock()
	c.queueExposure(assignment, ruleOverride)
}

func (c *Context) queueExposure(assignment *Assignment, ruleOverride bool) {
	c = c.live()
	if assignment.Exposed.CompareAndSwap(false, true) {
		var exposure = jsonmodels.Exposure{Id: assignment.Id, Name: assignment.Name, Unit: assignment.UnitType,
			Variant: assignment.Variant, ExposedAt: c.Clock_.Millis(), Assigned: assignment.Assigned,
			Eligible: assignment.Eligible, Overridden: assignment.Overridden, FullOn: assignment.FullOn,
			Custom: assignment.Custom, AudienceMismatch: assignment.AudienceMismatch, RuleOverride: ruleOverride}

		c.EventLock_.Lock()
		// Close seals the context under EventLock_, so this check and the
		// append cannot interleave with the seal: an event is queued before it
		// or rejected after it, never stranded behind it.
		if c.CheckNotClosed() != nil {
			c.EventLock_.Unlock()
			// The exposure was not queued, so it must not stay marked as
			// exposed: a failed close reopens the context, and the next
			// GetTreatment has to queue it.
			assignment.Exposed.Store(false)
			return
		}
		c.PendingCount_.Store(c.PendingCount_.Load().(int32) + 1)
		c.Exposures_ = append(c.Exposures_, exposure)
		c.EventLock_.Unlock()

		c.LogEvent(Exposure, exposure)

		c.SetTimeout()
	}
}

func (c *Context) PeekTreatment(experimentName string) (int, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return -1, err
	}

	return c.assignmentEntry(experimentName).assignment.Variant, nil
}

// GetVariableKeys returns, for each variable key, the name of the first
// experiment that defines it. Use GetVariableKeysAll to get every experiment.
func (c *Context) GetVariableKeys() (map[string]string, error) {
	var st = c.state()
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var variableKeys = map[string]string{}

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
	var st = c.state()
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var variableKeys = map[string][]string{}

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

// resolveVariableValue implements the JS SDK's _resolveVariableValue rule so
// shared variable keys behave the same across SDKs. It walks the experiments
// that define key in experiment-id order (see indexVariables_), and returns
// the value from the first experiment the unit is actually assigned to or
// overridden in. Each experiment visited on the way is exposed when expose is
// set, so a fall-through still records that the unit saw the earlier
// experiment. When no experiment along the way assigns the key, the caller's
// default is returned. v1.1.1 read only the last experiment in the payload.
func (c *Context) resolveVariableValue(key string, defaultValue interface{}, expose bool) (interface{}, error) {
	var st = c.state()
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	c.DataLock.RLock()
	var experiments = st.indexVariables_[key]
	c.DataLock.RUnlock()

	for _, experiment := range experiments {
		var entry = c.assignmentEntry(experiment.Data.Name)
		var assignment = entry.assignment
		if assignment.Variables == nil {
			continue
		}

		if expose && !assignment.Exposed.Load().(bool) {
			c.queueExposure(assignment, entry.ruleOverride)
		}

		if value, exist := assignment.Variables[key]; exist && (assignment.Assigned || assignment.Overridden || entry.ruleOverride) {
			return value, nil
		}
	}
	return defaultValue, nil
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

	return c.Flush(), nil
}

func (c *Context) Publish() error {
	var result, err = c.PublishAsync()
	if err != nil {
		return err
	}
	_, err = result.Get(context.Background())
	return err
}

func (c *Context) GetPendingCount() int32 {
	return c.PendingCount_.Load().(int32)
}

func (c *Context) RefreshAsync() *future.Future {
	c = c.live()
	var st = c.state()
	var err = c.CheckNotClosed()
	if err != nil {
		var tempfuture, donefun = future.New()
		donefun(nil, err)
		return tempfuture
	}

	// Decide and publish under one lock so a concurrent caller that sees the
	// refresh as running also sees its future.
	st.stateLock_.Lock()
	if c.Refreshing_.Load().(bool) {
		var active = c.RefreshFuture_
		st.stateLock_.Unlock()
		if active != nil {
			return active
		}
		var tempfuture, donefun = future.New()
		donefun(nil, nil)
		return tempfuture
	}
	c.Refreshing_.Store(true)
	var tempfuture, donefun = future.New()
	c.RefreshFuture_ = tempfuture
	st.stateLock_.Unlock()

	c.callDataProvider().Listen(func(value future.Value, err error) {
		// Whatever happens below, the refresh ends: Refreshing_ is cleared and
		// the future settles, so later refreshes are not blocked for ever.
		var outcome = err
		var result jsonmodels.ContextData
		var diagnostics []bufferedEvent
		if outcome == nil {
			var ok bool
			result, ok = value.(jsonmodels.ContextData)
			if !ok {
				outcome = &TypeError{Expected: "jsonmodels.ContextData", Actual: value}
			} else {
				var panicErr error
				diagnostics, panicErr = c.applyRefreshedData(result)
				if panicErr != nil {
					outcome = panicErr
				}
			}
		}
		c.Refreshing_.Store(false)

		// The load is over, so a logger that calls Refresh starts a new one.
		// Log before settling the future, so a caller that waits on Refresh
		// returns after the logger has seen the refresh or error event.
		c.dispatchDiagnostics(diagnostics)
		if outcome == nil {
			c.LogEvent(Refresh, result)
		} else {
			c.LogError(outcome)
		}
		donefun(nil, outcome)
	})
	return tempfuture
}

func (c *Context) applyRefreshedData(data jsonmodels.ContextData) (diagnostics []bufferedEvent, panicErr error) {
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

func (c *Context) Refresh() {
	c.RefreshAsync().Join(context.Background())
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
	closing, finalized := c.closeAttempt()
	if st.loggers_ != nil && st.loggers_.current() {
		closing.Join(context.Background())
		return
	}
	finalized.Join(context.Background())
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

// validCachedAssignment returns the cached assignment for experimentName and
// whether it is still valid, with the rule set it was validated against. It
// only reads, so it may run under ContextLock_ held for reading or writing.
func (c *Context) validCachedAssignment(st *contextState, experimentName string) (*cachedAssignment, *experimentRules, assignmentState) {
	var entry, found = st.assignmentCache_[experimentName]
	if !found {
		return nil, nil, assignmentStale
	}
	var assignment = entry.assignment
	var custom, cfound = c.Cassignments_[experimentName]
	var override, ofound = c.Overrides_[experimentName]
	var experiment, rules, efound = st.experimentAndRules(experimentName)

	if ofound {
		if assignment.Overridden && assignment.Variant == override.(int) {
			return entry, rules, assignmentValid
		}
	} else if !efound {
		// A rule-won assignment is not assigned either, but its variant came
		// from the experiment's rules, so it is rebuilt once the experiment
		// is gone.
		if !assignment.Assigned && !entry.ruleOverride {
			return entry, rules, assignmentValid
		}
	} else if entry.ruleOverride || !cfound || custom.(int) == assignment.Variant {
		// A matching rule wins over a custom assignment, so a custom
		// assignment that differs from a rule-won variant is not staleness.
		if !c.ExperimentMatches(experiment.Data, *assignment) {
			return entry, rules, assignmentStale
		}
		if rules == entry.rules && atomic.LoadInt32(&st.attrsSeq_) <= entry.attrsSeq {
			return entry, rules, assignmentValid
		}
		if c.ruleMatches(st, experiment.Data, rules, entry) && c.audienceMatches(st, experiment.Data, entry) {
			return entry, rules, assignmentRevalidated
		}
	}
	return entry, rules, assignmentStale
}

// ruleMatches reports whether the experiment's assignment rules still give the
// cached assignment's outcome. It never updates the entry; a changed outcome
// is reported as stale for GetAssignment to rebuild under the write lock.
func (c *Context) ruleMatches(st *contextState, data jsonmodels.Experiment, rules *experimentRules, entry *cachedAssignment) bool {
	var assignment = entry.assignment
	// An exposed split resolved without its unit keeps resolving as if the
	// unit were still missing, so a unit set afterwards cannot switch the
	// variant the user already saw.
	var pinnedMissing = entry.ruleMissingUnit && assignment.Exposed.Load().(bool)
	var rule = c.resolveRule(st, data, rules, pinnedMissing, false)
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
// assignment's audienceMismatch. A rule-won assignment skips the audience, as
// in the JS SDK.
func (c *Context) audienceMatches(st *contextState, data jsonmodels.Experiment, entry *cachedAssignment) bool {
	var assignment = entry.assignment
	if entry.ruleOverride || len(data.Audience) == 0 {
		return true
	}
	var match, err = c.AudienceMatcher_.Evaluate(data.Audience, c.evaluationAttributesWithState(st))
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
// The caller holds ContextLock_. cache is false on the read path, where the
// hashed-unit and assigner caches must not be written.
func (c *Context) resolveRule(st *contextState, data jsonmodels.Experiment, rules *experimentRules, unitPinnedMissing bool, cache bool) *ruleResolution {
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

	var uid, ufound = c.Units_[data.UnitType]
	if len(data.UnitType) == 0 || !ufound || unitPinnedMissing {
		return &ruleResolution{variant: data.FullOnVariant, missingUnit: true}
	}

	var assigner *VariantAssigner
	if cache {
		var hash [22]byte
		assigner = c.GetVariantAssigner(data.UnitType, c.GetUnitHash(data.UnitType, uid, hash[:], false), false)
	} else if cached, found := c.Assigners_[data.UnitType]; found {
		assigner = cached.(*VariantAssigner)
	} else if hash, found := c.HashedUnits_[data.UnitType]; found {
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
func (c *Context) keepExposedRuleAssignment(data jsonmodels.Experiment, previous *cachedAssignment, entry *cachedAssignment) {
	if previous == nil {
		return
	}
	var old = previous.assignment
	var fresh = entry.assignment
	if !entry.ruleOverride || !previous.ruleOverride || old.Overridden || old.Variant != fresh.Variant ||
		!old.Exposed.Load().(bool) || !c.ExperimentMatches(data, *old) {
		return
	}
	fresh.Exposed = previous.assignment.Exposed
}

// GetAssignment returns a copy of the experiment's assignment, as in v1.1.1:
// changing it does not change the context's cached assignment. The copy
// shares the cached assignment's Exposed flag.
func (c *Context) GetAssignment(experimentName string) *Assignment {
	var assignment = *c.assignmentEntry(experimentName).assignment
	return &assignment
}

// assignmentEntry returns the experiment's cached assignment entry, resolving
// it first when it is missing or out of date. The entry's assignment is
// shared and never mutated once cached.
func (c *Context) assignmentEntry(experimentName string) *cachedAssignment {
	var st = c.state()
	// A valid cached assignment only needs the read lock, so concurrent reads
	// of a shared context do not serialise.
	c.ContextLock_.RLock()
	if entry, _, state := c.validCachedAssignment(st, experimentName); state == assignmentValid {
		c.ContextLock_.RUnlock()
		return entry
	}
	c.ContextLock_.RUnlock()

	// Cache miss, out of date, or revalidated. Check again under the write
	// lock: another goroutine may have rebuilt the assignment meanwhile, and
	// replacing it would lose its exposed flag and queue a second exposure.
	c.ContextLock_.Lock()
	var previous, validated, state = c.validCachedAssignment(st, experimentName)
	if state == assignmentValid {
		c.ContextLock_.Unlock()
		return previous
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
		c.ContextLock_.Unlock()
		return previous
	}

	var custom, cfound = c.Cassignments_[experimentName]
	var override, ofound = c.Overrides_[experimentName]
	var experiment, rules, efound = st.experimentAndRules(experimentName)

	var exposed = &atomic.Value{}
	exposed.Store(false)
	var assignment = &Assignment{Exposed: exposed}
	assignment.Name = experimentName
	assignment.Eligible = true
	var entry = &cachedAssignment{assignment: assignment, rules: rules, attrsSeq: atomic.LoadInt32(&st.attrsSeq_)}

	if ofound {
		if efound {
			assignment.Id = experiment.Data.Id
			assignment.UnitType = experiment.Data.UnitType
		}

		assignment.Overridden = true
		assignment.Variant = override.(int)
	} else {
		if efound {
			var unitType = experiment.Data.UnitType

			// A matching rule is an explicit assignment, like an override: it
			// wins over the audience, audienceStrict, traffic eligibility,
			// full-on and custom assignments.
			if rule := c.resolveRule(st, experiment.Data, rules, false, true); rule != nil {
				assignment.Variant = rule.variant
				entry.ruleOverride = true
				entry.ruleMissingUnit = rule.missingUnit
			}

			if !entry.ruleOverride && len(experiment.Data.Audience) > 0 {
				var attrs = c.evaluationAttributesWithState(st)

				var match, err = c.AudienceMatcher_.Evaluate(experiment.Data.Audience, attrs)
				if err == nil {
					assignment.AudienceMismatch = !match.Get()
				}
			}

			if !entry.ruleOverride {
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
		c.keepExposedRuleAssignment(experiment.Data, previous, entry)
	}

	if entry.ruleOverride {
		st.ruleIdentities.remember(assignment.Exposed)
	}
	st.assignmentCache_[experimentName] = entry
	c.AssignmentCache[experimentName] = *assignment
	c.ContextLock_.Unlock()
	return entry
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
// data cannot race it, and it takes no lock.
func (c *Context) GetExperiment(experimentName string) (ExperimentVariables, bool) {
	if c.ContextLock_ == nil {
		result, found := c.Index_[experimentName]
		return result, found
	}
	var st = c.state()
	var result, _, found = st.experimentAndRules(experimentName)
	return result, found
}

// loadIndex returns the installed index, or nil before any data is set.
func (st *contextState) loadIndex() *contextIndex {
	return st.index_.Load().(*contextIndex)
}

// experimentAndRules is GetExperiment plus the experiment's parsed assignment
// rules, read from the same installed index.
func (st *contextState) experimentAndRules(experimentName string) (ExperimentVariables, *experimentRules, bool) {
	var index = st.loadIndex()
	if index == nil {
		return ExperimentVariables{}, nil, false
	}
	if entry, found := index.experiments[experimentName]; found {
		return entry.experiment, entry.rules, true
	}
	return ExperimentVariables{}, nil, false
}

func (c *Context) ExperimentMatches(experiment jsonmodels.Experiment, assignment Assignment) bool {
	return experiment.Id == assignment.Id &&
		experiment.UnitType == assignment.UnitType &&
		experiment.Iteration == assignment.Iteration &&
		experiment.FullOnVariant == assignment.FullOnVariant &&
		reflect.DeepEqual(experiment.TrafficSplit, assignment.TrafficSplit)
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
	var buffer = &diagnosticBuffer{}
	c.setData(data, buffer)
	c.dispatchDiagnostics(buffer.take())
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

func (c *Context) setData(data jsonmodels.ContextData, buffer *diagnosticBuffer) {
	var st = c.state()
	var self = c.snapshot()
	self.EventLogger_ = buffer
	var index = map[string]ExperimentVariables{}
	var indexVariables = map[string][]ExperimentVariables{}
	var contextCustomFields = map[string]map[string]ContextCustomFieldValue{}
	var rules = map[string]*experimentRules{}
	var previous = st.loadIndex()
	var previousRules = func(name string) *experimentRules {
		if previous != nil {
			if entry, found := previous.experiments[name]; found {
				return entry.rules
			}
		}
		return nil
	}

	for _, experiment := range data.Experiments {
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

		contextCustomFields[experiment.Name] = experimentCustomFields
		index[experiment.Name] = experiemntVariables
	}

	// Keep each key's experiments sorted and unique by experiment id, matching
	// the JS SDK's insertUniqueSorted. GetVariableValue/PeekVariableValue walk
	// this list in id order, so a shared key resolves to the lowest-id
	// experiment the unit is assigned to or overridden in, regardless of the
	// experiment order in the payload. GetVariableKeys, GetVariableKeysAll and
	// IndexVariables_ follow the same id order.
	for key := range indexVariables {
		var experiments = indexVariables[key]
		sort.SliceStable(experiments, func(i, j int) bool {
			return experiments[i].Data.Id < experiments[j].Data.Id
		})
	}

	c.DataLock.Lock()

	c.Index_ = index
	var indexed = make(map[string]*indexedExperiment, len(index))
	for name, experiment := range index {
		indexed[name] = &indexedExperiment{experiment: experiment, rules: rules[name]}
	}
	st.index_.Store(&contextIndex{experiments: indexed})
	c.ContextCustomFields_ = contextCustomFields
	st.indexVariables_ = indexVariables
	c.IndexVariables_ = legacyIndexVariables(indexVariables)
	c.Data_ = data
	c.Ready_.Store(true)

	c.setRefreshTimer(st)
	c.DataLock.Unlock()
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
	var parsed, _ = c.AudienceMatcher_.parseRules(text)
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
	var st = c.state()
	c.DataLock.Lock()
	c.Index_ = map[string]ExperimentVariables{}
	st.index_.Store(&contextIndex{})
	st.indexVariables_ = map[string][]ExperimentVariables{}
	c.IndexVariables_ = map[interface{}]interface{}{}
	c.Data_ = jsonmodels.ContextData{}
	c.Ready_.Store(true)
	c.Failed_.Store(true)
	c.DataLock.Unlock()
}

func (c *Context) Flush() *future.Future {
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
			st.stateLock_.Unlock()
		}
		c.EventLock_.Unlock()

		if eventCount > 0 {
			var event = jsonmodels.PublishEvent{}
			event.Hashed = true
			event.PublishedAt = c.Clock_.Millis()

			c.ContextLock_.Lock()
			var entrySet []interface{}
			for key, value := range c.Units_ {
				entrySet = append(entrySet, Pair{a: key, b: value})
			}

			var mapper = liveFlushMapper{ctx: c}
			var temp = MapSetToArray(entrySet, make([]interface{}, 0), mapper)
			event.Units = make([]jsonmodels.Unit, 0)
			for _, value := range temp {
				event.Units = append(event.Units, value.(jsonmodels.Unit))
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
						runLogger(done, nil, func() { c.logEvent(Publish, event) })
					} else {
						c.requeueEvents(exposures, achievements, eventCount)
						runLogger(done, err, func() { c.logEvent(Error, err) })
					}
					c.removeInflight(settled)
					settledDone(nil, err)
				})

			return result
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
	return result
}

func (st *contextState) addInflight(f *future.Future) {
	st.stateLock_.Lock()
	st.inflight_ = append(st.inflight_, f)
	st.stateLock_.Unlock()
}

func (c *Context) removeInflight(f *future.Future) {
	var st = c.state()
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
func runLogger(done future.SetResultFunc, result error, log func()) {
	go func() {
		log()
		done(nil, result)
	}()
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
	loaded = c.DataProvider_.GetContextData()
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
	return flushUnit(&f.Context, value)
}

// liveFlushMapper is FlushMapper for the live context the SDK flushes, which
// must not be copied: it holds the locks the flush takes.
type liveFlushMapper struct {
	ctx *Context
}

func (f liveFlushMapper) Apply(value interface{}) interface{} {
	return flushUnit(f.ctx, value)
}

func flushUnit(cntx *Context, value interface{}) interface{} {
	var key = value.(Pair).a
	var val = value.(Pair).b
	var data [22]byte
	var uid = cntx.GetUnitHash(key, val, data[:], false)
	var res = asciiOnly(string(uid))
	return jsonmodels.Unit{Type: value.(Pair).a, Uid: res}
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
	c = c.live()
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
	var st = c.state()
	c.DataLock.RLock()
	var experiments = st.indexVariables_[key]
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
