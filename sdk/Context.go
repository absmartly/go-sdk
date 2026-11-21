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
	// Deprecated: IndexVariables_ keeps its v1.1.1 type; each key maps to the
	// first ExperimentVariables that defines it. Use GetVariableKeysAll for
	// every experiment per key.
	IndexVariables_ map[interface{}]interface{}
	ContextLock_    *sync.RWMutex
	HashedUnits_    map[interface{}]interface{}
	Assigners_      map[interface{}]interface{}
	// Deprecated: AssignmentCache keeps its v1.1.1 type and is a read-only
	// copy of the internal cache, written under ContextLock_.
	AssignmentCache map[string]Assignment
	EventLock_      *sync.Mutex
	Exposures_      []jsonmodels.Exposure
	Achievements_   []jsonmodels.GoalAchievement
	Attributes_     []interface{}
	Overrides_      map[interface{}]interface{}
	Cassignments_   map[interface{}]interface{}
	PendingCount_   *atomic.Value
	Closing_        *atomic.Value
	Closed_         *atomic.Value
	Refreshing_     *atomic.Value
	ReadyFuture_    *future.Future
	ClosingFuture_  *future.Future
	// FinalizedFuture_ includes logger delivery; ClosingFuture_ only includes
	// transport/sealing, allowing loggers to re-enter Close safely.
	FinalizedFuture_ *future.Future
	publishLogs_     []*future.Future
	RefreshFuture_   *future.Future
	TimeoutLock_     *sync.Mutex
	// StateLock_ guards ReadyFuture_, RefreshFuture_, ClosingFuture_, RefreshTimer_ and closeStarted_.
	StateLock_ *sync.Mutex
	// inflight_ holds the publishes that have been sent and not yet settled. Guarded by StateLock_.
	inflight_ []*future.Future

	// root_ is set on the copies of the Context that callbacks receive. It
	// points at the live context, which owns the lifecycle state (the
	// closing/refresh futures, timers, in-flight publishes and event queue).
	// The lifecycle methods run on the live context, so calling Close, Publish
	// or Refresh on the copy a logger was given acts on the real context, not
	// on stale copied fields.
	root_         *Context
	Timeout_      *time.Timer
	RefreshTimer_ *time.Timer
	Clock_        internal.Clock
	AttrsSeq_     int32

	// application_ and environment_ are the application and environment names
	// from the client config. They are injected into the attribute map that
	// audiences and assignment-rule conditions are evaluated against (system
	// values first, user attributes overlaid on top), and environment_ scopes
	// assignment rules. They are never published as attributes and are set
	// once at creation, so they need no locking.
	application_ string
	environment_ string

	// customFieldErrors_ holds, per experiment and field name, the error from
	// parsing a "json" custom field. GetCustomFieldValue reports it through the
	// event logger when the field is read. Guarded by DataLock.
	customFieldErrors_ map[string]map[string]error

	// unitValues_ keeps each unit as it was set (string or float64); Units_
	// holds the string form that is hashed. Guarded by ContextLock_.
	unitValues_ map[string]interface{}

	// readyDeliverer_ is the goroutine that is logging the initial ready or
	// load-error event, or 0. A readiness wait from that goroutine is the
	// logger waiting for itself, so it returns at once.
	readyDeliverer_ *atomic.Uint64

	// loggers_ records the goroutines that are inside an event logger call
	// on this context, so a blocking Close from a logger does not wait for
	// logger delivery that cannot finish until it returns.
	loggers_ *loggerGoroutines

	// closeStarted_ is set once Close has been called. The refresh timer
	// that Close stops is never installed again, even if the close fails and
	// leaves the context open for a retry. Guarded by StateLock_.
	closeStarted_ bool

	// indexVariables_ lists, per variable key, every experiment that defines
	// it, in experiment-id order. Guarded by DataLock.
	indexVariables_ map[string][]ExperimentVariables

	// index_ is the installed experiment index and parsed assignment rules,
	// replaced as a whole by setData under DataLock. The assignment read path
	// loads it atomically instead of taking DataLock, so a cached read costs
	// what it did before the index was guarded.
	index_ *atomic.Pointer[contextIndex]

	// assignmentCache_ holds the assignments by experiment name. Guarded by
	// ContextLock_: entries are only written under the write lock.
	assignmentCache_ map[string]*cachedAssignment

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
	retirement *atomic.Bool
	switchback *switchbackSlot
	// resolvedId, resolvedIteration and resolvedUnitType identify the
	// definition used to resolve this entry, including overrides whose public
	// Iteration stays zero. A switchback slot's unit identity is its
	// generation instead (sameSwitchbackIdentity), so its unitType is not
	// compared.
	resolvedId        int
	resolvedIteration int
	resolvedUnitType  string
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
	// RuleOverride is set when a matching assignment rule chose the variant.
	// The exposure carries it so the collector leaves the unit out of the
	// analysis, as it does for overrides.
	RuleOverride bool
	Variables    map[string]interface{}
	Exposed      *atomic.Value
	AttrsSeq     int32
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
		AudienceMatcher_: audienceMatcher, Units_: map[string]string{},
		application_: application, environment_: environment}

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
	cntx.StateLock_ = &sync.Mutex{}

	cntx.AssignmentCache = map[string]Assignment{}
	cntx.assignmentCache_ = map[string]*cachedAssignment{}
	cntx.index_ = &atomic.Pointer[contextIndex]{}
	cntx.unitValues_ = map[string]interface{}{}
	cntx.readyDeliverer_ = &atomic.Uint64{}
	cntx.loggers_ = &loggerGoroutines{ids: map[uint64]int{}}
	cntx.Achievements_ = make([]jsonmodels.GoalAchievement, 0)
	cntx.Exposures_ = make([]jsonmodels.Exposure, 0)
	cntx.Attributes_ = make([]interface{}, 0)

	var units = config.Units_
	if units != nil {
		if err := cntx.SetUnits(units); err != nil && cntx.EventLogger_ != nil {
			notifyLogger(cntx.EventLogger_, cntx, Error, errors.New("CreateContext: Failed to set units: "+err.Error()))
		}
	}

	cntx.Assigners_ = map[interface{}]interface{}{}
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
			tmp.readyDeliverer_.Store(goroutineID())
			defer tmp.readyDeliverer_.Store(0)
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
	if c.Ready_.Load().(bool) && (c.root_ != nil || c.live().readyDeliverer_.Load() == goroutineID()) {
		return nil
	}
	return c.live().getReadyFuture()
}

func (c *Context) getReadyFuture() *future.Future {
	c.StateLock_.Lock()
	defer c.StateLock_.Unlock()
	return c.ReadyFuture_
}

func (c *Context) setReadyFuture(f *future.Future) {
	c.StateLock_.Lock()
	c.ReadyFuture_ = f
	c.StateLock_.Unlock()
}

// snapshot returns a copy of the context taken while holding every lock that
// guards a mutable field, so the copy handed to a callback does not race with
// writers. Lock order is ContextLock_, DataLock, EventLock_, TimeoutLock_,
// StateLock_. It must not be called while holding any of them.
func (c *Context) snapshot() Context {
	c = c.live()
	c.ContextLock_.RLock()
	defer c.ContextLock_.RUnlock()
	c.DataLock.RLock()
	defer c.DataLock.RUnlock()
	c.EventLock_.Lock()
	defer c.EventLock_.Unlock()
	c.TimeoutLock_.Lock()
	defer c.TimeoutLock_.Unlock()
	c.StateLock_.Lock()
	defer c.StateLock_.Unlock()
	var cp = *c
	cp.root_ = c
	return cp
}

func (c *Context) live() *Context {
	if c.root_ != nil {
		return c.root_
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

// SetCustomAssignment is allowed after the context is finalized, like SetOverride.
func (c *Context) SetCustomAssignment(experimentName string, variant int) error {
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
	var previous, exist = c.unitValues_[unitType]
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
	c.unitValues_[unitType] = value
	c.evictAssignmentsMissingUnit(unitType)
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
func (c *Context) evictAssignmentsMissingUnit(unitType string) {
	for name, entry := range c.assignmentCache_ {
		var assignment = entry.assignment
		if entry.switchback != nil && entry.switchback.serving.status == switchbackAbsent {
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
				c.retireSwitchbackSlot(assignment.Name, name)
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
			delete(c.assignmentCache_, name)
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
	c.ContextLock_.RLock()
	defer c.ContextLock_.RUnlock()
	if v, ok := c.unitValues_[unitType]; ok {
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
	c.ContextLock_.RLock()
	defer c.ContextLock_.RUnlock()
	var result = make(map[string]interface{}, len(c.Units_))
	for k, s := range c.Units_ {
		if v, ok := c.unitValues_[k]; ok {
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
	var err = c.CheckNotClosed()
	if err != nil {
		return err
	}

	var attribute = jsonmodels.Attribute{Name: name, Value: value, SetAt: c.Clock_.Millis()}
	c.ContextLock_.Lock()
	c.Attributes_ = append(c.Attributes_, attribute)
	atomic.AddInt32(&c.AttrsSeq_, 1)
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
// name always wins, even one set after an assignment was cached (the AttrsSeq_
// bump invalidates it). Empty system values are left out rather than set to an
// empty string, so a filter on a name that is not configured does not match.
// These values are never published as attributes; the collector adds them to
// exposures server-side. The caller must hold ContextLock_.
func (c *Context) evaluationAttributes() map[string]interface{} {
	var attrs = map[string]interface{}{}
	if len(c.application_) > 0 {
		attrs["application"] = c.application_
	}
	if len(c.environment_) > 0 {
		attrs["environment"] = c.environment_
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
		at = &clockReading{clock: c.Clock_}
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
	c.ContextLock_.RLock()
	var status = claimEligible(entry, eligible)
	c.ContextLock_.RUnlock()
	if status != exposureClaimed {
		return status
	}
	return c.admitExposureAt(entry.assignment, entry, at)
}

// claimEligible checks eligibility and claims the exposure in one step; the
// caller holds ContextLock_ across the whole call, so no refresh can land
// between the check and the claim. A claim already taken is honoured.
func claimEligible(entry *cachedAssignment, eligible func(*cachedAssignment) exposureStatus) exposureStatus {
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
	return func(e *cachedAssignment) exposureStatus {
		if e.assignment.Variables == nil {
			return exposureIneligible
		}
		var index = c.index_.Load()
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
		// The entry's own slot: a read resolved in a period claims that
		// period's decision, even if the clock has since moved on.
		var slot = assignmentSlot{key: experimentName}
		if e.switchback != nil && e.switchback.hasPeriod {
			slot = assignmentSlot{key: switchbackSlotKey(experimentName, e.switchback.period), period: e.switchback.period, hasPeriod: true}
		}
		if current, _, state := c.validCachedAssignmentFor(experimentName, indexed, slot, true); current != e || state == assignmentStale {
			// Still a candidate, but this decision is no longer the current
			// one: resolve again rather than claim it or skip the experiment.
			return exposureRetired
		}
		return exposureClaimed
	}
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
var (
	afterEligibleCAS    func()
	beforeSettleClaim   func()
	beforeEligibleClaim func()
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
	// add coverage while a caller-held exposure is in flight.
	c.ContextLock_.RLock()
	entry = c.cachedEntryFor(assignment)
	c.ContextLock_.RUnlock()
	c.queueExposure(assignment, entry)
}

// cachedEntryFor finds the cache entry holding assignment, or nil: the
// experiment's slot, one of its switchback period slots, or a removed
// switchback slot a caller may still hold. The caller holds ContextLock_.
func (c *Context) cachedEntryFor(assignment *Assignment) *cachedAssignment {
	if entry, found := c.assignmentCache_[assignment.Name]; found && entry.assignment.Exposed == assignment.Exposed {
		return entry
	}
	if epoch := c.switchbackEpochs_[assignment.Name]; epoch != nil {
		for _, period := range epoch.periods {
			var entry = c.assignmentCache_[switchbackSlotKey(assignment.Name, period)]
			if entry != nil && entry.assignment.Exposed == assignment.Exposed {
				return entry
			}
		}
	}
	for _, entry := range c.retiredSwitchbackSlots_[assignment.Name] {
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
	if entry != nil {
		holdouts, slot = entry.holdouts, entry.switchback
	}
	var status = c.admit(assignment, holdouts, slot, at)
	if status == exposureRejected {
		c.ContextLock_.Lock()
		c.reopenExposure(assignment, entry)
		c.ContextLock_.Unlock()
	}
	return status
}

// admit queues the claimed exposures, and reports exposureRejected when a
// close rejected them. A claim always commits, even if a replacement retired
// its assignment meanwhile, as in the Java SDK: another read may already be
// serving that decision because the claim marked it exposed.
//
// slot and at are the switchback slot (nil for a unit experiment) and the
// reading that resolved it: a silent switchback resolution publishes
// nothing, and a switchback exposure carries the reading of its period.
func (c *Context) admit(assignment *Assignment, holdouts *assignmentHoldouts, slot *switchbackSlot, at *clockReading) exposureStatus {
	if holdouts == nil {
		if slot.isSilent() {
			return exposureQueued
		}
		if !c.enqueueExposure(assignment, c.exposedAt(slot, at)) {
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
		exposures = append(exposures, c.exposureFor(assignment, exposedAt))
	}
	for _, arm := range holdouts.arms {
		if arm != nil && arm.assignment.Exposed.CompareAndSwap(false, true) {
			exposures = append(exposures, c.exposureFor(arm.assignment, exposedAt))
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
func (c *Context) reopenExposure(assignment *Assignment, claimed *cachedAssignment) {
	if claimed != nil && claimed.retirement.Load() {
		return
	}
	if claimed != nil && claimed.switchback != nil && claimed.switchback.serving.status == switchbackAbsent {
		// Resolved before any serving unit was set: if one has arrived, the
		// next read resolves again under it. The serving sources are the
		// readable cluster map's, or the declared unitType on the legacy path.
		var indexed = c.indexedExperiment(assignment.Name)
		if indexed != nil && indexed.switchback != nil && c.switchbackServingUnitSet(indexed) {
			claimed.retirement.Store(true)
			return
		}
	}
	var entry = c.assignmentCache_[assignment.Name]
	if entry != nil && entry.assignment.Exposed == assignment.Exposed {
		if _, unitSet := c.Units_[assignment.UnitType]; unitSet && entry.missingUnit() {
			// Retired, as an eviction is: it stays marked exposed, and a
			// read already holding it resolves again with the unit.
			entry.retirement.Store(true)
			delete(c.assignmentCache_, assignment.Name)
			delete(c.AssignmentCache, assignment.Name)
			return
		}
	}
	assignment.Exposed.Store(false)
}

// missingUnit reports whether the assignment was resolved without its unit:
// a split rule that matched without it, or a holdout arm it could not draw.
func (e *cachedAssignment) missingUnit() bool {
	if e.assignment.RuleOverride && e.ruleMissingUnit {
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

func (c *Context) exposureFor(assignment *Assignment, exposedAt int64) jsonmodels.Exposure {
	return jsonmodels.Exposure{Id: assignment.Id, Name: assignment.Name, Unit: assignment.UnitType,
		Variant: assignment.Variant, ExposedAt: exposedAt, Assigned: assignment.Assigned,
		Eligible: assignment.Eligible, Overridden: assignment.Overridden, FullOn: assignment.FullOn,
		Custom: assignment.Custom, AudienceMismatch: assignment.AudienceMismatch, RuleOverride: assignment.RuleOverride}
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
func (c *Context) enqueueExposure(assignment *Assignment, exposedAt int64) bool {
	var exposure = c.exposureFor(assignment, exposedAt)

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

	return c.GetAssignment(experimentName).Variant, nil
}

// GetVariableKeys returns, for each variable key, the name of the first
// experiment that defines it. Use GetVariableKeysAll to get every experiment.
func (c *Context) GetVariableKeys() (map[string]string, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var variableKeys = map[string]string{}

	c.DataLock.RLock()
	for key, experiments := range c.indexVariables_ {
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

	c.DataLock.RLock()
	for key, experiments := range c.indexVariables_ {
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
	var parseErr = c.customFieldErrors_[experimentName][key]
	c.DataLock.Unlock()

	if parseErr != nil {
		c.LogError(errors.New("Failed to parse JSON custom field value '" + key + "' for experiment '" +
			experimentName + "': " + parseErr.Error()))
	}

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
// An experiment a holdout suppresses never wins the key, but when nothing
// else does, the first one met gives its control value rather than the
// default, as in the JS SDK.
func (c *Context) resolveVariableValue(key string, defaultValue interface{}, expose bool) (interface{}, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	c.DataLock.RLock()
	var experiments = c.indexVariables_[key]
	c.DataLock.RUnlock()

	var suppressedFallback map[string]interface{}
	// One clock reading resolves and stamps every experiment on the key.
	var at = clockReading{clock: c.Clock_}
	for _, experiment := range experiments {
		var entry = c.getAssignmentAt(experiment.Data.Name, &at)
		var assignment = entry.assignment
		if assignment.Variables == nil {
			continue
		}

		if expose && afterReadResolve != nil {
			afterReadResolve()
		}
		if expose && entry.needsSettling() {
			var eligible bool
			entry, eligible = c.settleExposureAt(experiment.Data.Name, entry, c.variableEligible(experiment.Data.Name, key), &at)
			if !eligible {
				continue
			}
			assignment = entry.assignment
		}

		if value, exist := assignment.Variables[key]; exist && (assignment.Assigned || assignment.Overridden || assignment.RuleOverride) {
			return value, nil
		}
		if suppressedFallback == nil && entry.holdouts.isSuppressed() {
			suppressedFallback = assignment.Variables
		}
	}
	if value, exist := suppressedFallback[key]; exist {
		return value, nil
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
	var err = c.CheckNotClosed()
	if err != nil {
		var tempfuture, donefun = future.New()
		donefun(nil, err)
		return tempfuture
	}

	if c.IsFailed() {
		var resolved, done = future.New()
		done(nil, nil)
		return resolved
	}

	// Decide and publish under one lock so a concurrent caller that sees the
	// refresh as running also sees its future.
	c.StateLock_.Lock()
	if c.Refreshing_.Load().(bool) {
		var active = c.RefreshFuture_
		c.StateLock_.Unlock()
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
	c.StateLock_.Unlock()

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
	// Claim the close and publish its future in one critical section, so a
	// concurrent caller that sees the context closing also gets the future of
	// the close that is running.
	c.StateLock_.Lock()
	if c.Closed_.Load().(bool) || c.Closing_.Load().(bool) {
		var existing = c.ClosingFuture_
		var existingFinalized = c.FinalizedFuture_
		c.StateLock_.Unlock()
		if existingFinalized == nil {
			var resolved, resolve = future.New()
			resolve(nil, nil)
			existingFinalized = resolved
		}
		// A logger that re-enters a close that has already succeeded gets a
		// settled future, as in v1.1.1: the close future may only settle once
		// that logger returns.
		if existing == nil || (c.Closed_.Load().(bool) && c.loggers_ != nil && c.loggers_.current()) {
			var resolved, resolve = future.New()
			resolve(nil, nil)
			existing = resolved
		}
		return existing, existingFinalized
	}
	c.Closing_.Store(true)
	c.closeStarted_ = true
	var tempFuture, done = future.New()
	c.ClosingFuture_ = tempFuture
	var finalized, finalizeDone = future.New()
	c.FinalizedFuture_ = finalized
	c.StateLock_.Unlock()

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
		var waitFor = c.inflightSnapshot()
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
			c.StateLock_.Lock()
			c.ClosingFuture_ = nil
			c.Closing_.Store(false)
			c.StateLock_.Unlock()
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
		if synchronous && c.publishLogsDone() && (c.loggers_ == nil || !c.loggers_.current()) {
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
	c.StateLock_.Lock()
	defer c.StateLock_.Unlock()
	for _, f := range c.publishLogs_ {
		if !f.Ready() {
			return false
		}
	}
	return true
}

func (c *Context) waitPublishLogs() {
	c.StateLock_.Lock()
	logs := append([]*future.Future(nil), c.publishLogs_...)
	c.StateLock_.Unlock()
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
	closing, finalized := c.closeAttempt()
	if c.loggers_ != nil && c.loggers_.current() {
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

// validCachedAssignment returns the assignment cached in slot for
// experimentName and whether it is still valid, with the rule set it was
// validated against. indexed is the installed experiment slot was computed
// from, nil when there is none. It only reads, so it may run under
// ContextLock_ held for reading or writing.
func (c *Context) validCachedAssignment(experimentName string, indexed *indexedExperiment, slot assignmentSlot) (*cachedAssignment, *experimentRules, assignmentState) {
	return c.validCachedAssignmentFor(experimentName, indexed, slot, false)
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
func (c *Context) validCachedAssignmentFor(experimentName string, indexed *indexedExperiment, slot assignmentSlot,
	ignoreCustom bool) (*cachedAssignment, *experimentRules, assignmentState) {
	var entry, found = c.assignmentCache_[slot.key]
	if !found {
		return nil, nil, assignmentStale
	}
	if c.switchbackEpochStale(experimentName, indexed) {
		return entry, nil, assignmentStale
	}
	if entry.retirement.Load() {
		return entry, nil, assignmentStale
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
		if efound && !assignment.Exposed.Load().(bool) && c.index_.Load().keyed &&
			!entry.resolvedExperimentMatches(experiment.Data) {
			return entry, rules, assignmentStale
		}
		if assignment.Overridden && assignment.Variant == override.(int) && (!efound ||
			holdoutSetMatches(holdouts, entry.holdouts) && (entry.holdouts == nil || entry.resolvedUnitType == experiment.Data.UnitType)) &&
			switchbackSlotMatches(indexed, entry) {
			return entry, rules, assignmentValid
		}
	} else if !efound {
		// A rule-won assignment is not assigned either, but its variant came
		// from the experiment's rules, so it is rebuilt once the experiment
		// is gone.
		if !assignment.Assigned && !assignment.RuleOverride {
			return entry, rules, assignmentValid
		}
	} else if assignment.RuleOverride || entry.holdouts.isSuppressed() || ignoreCustom || entry.switchback.customMatches(custom, cfound, assignment) {
		// A matching rule wins over a custom assignment, and a holdout
		// suppression forces control, so a custom assignment that differs
		// from either variant is not staleness.
		if !c.entryMatches(indexed, entry) || !holdoutSetMatches(holdouts, entry.holdouts) {
			return entry, rules, assignmentStale
		}
		if rules == entry.rules && atomic.LoadInt32(&c.AttrsSeq_) <= entry.attrsSeq {
			return entry, rules, assignmentValid
		}
		if c.ruleMatches(experiment.Data, rules, entry) && c.audienceMatches(experiment.Data, entry) {
			return entry, rules, assignmentRevalidated
		}
	}
	return entry, rules, assignmentStale
}

// ruleMatches reports whether the experiment's assignment rules still give the
// cached assignment's outcome. It never updates the entry; a changed outcome
// is reported as stale for GetAssignment to rebuild under the write lock.
func (c *Context) ruleMatches(data jsonmodels.Experiment, rules *experimentRules, entry *cachedAssignment) bool {
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
	var rule = c.resolveRule(data, rules, ruleUnitType(data, entry), pinnedMissing, false)
	if (rule != nil) != assignment.RuleOverride {
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
// never evaluated the audience, as in the JS SDK, so it skips it.
func (c *Context) audienceMatches(data jsonmodels.Experiment, entry *cachedAssignment) bool {
	var assignment = entry.assignment
	if assignment.RuleOverride || entry.holdouts.isSuppressed() || len(data.Audience) == 0 {
		return true
	}
	// Nor did a switchback without an eligible cluster: it is resolved
	// again, from the same cluster, only when the period or the definition
	// changes.
	if entry.switchback != nil && entry.switchback.serving.status != switchbackEligible {
		return true
	}
	var match, err = c.AudienceMatcher_.Evaluate(data.Audience, c.evaluationAttributes())
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
func (c *Context) resolveRule(data jsonmodels.Experiment, rules *experimentRules, unitType string, unitPinnedMissing bool, cache bool) *ruleResolution {
	if rules == nil || rules.parsed == nil {
		return nil
	}

	var action = c.AudienceMatcher_.evaluateRules(rules.parsed, c.environment_, c.evaluationAttributes())
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
	if !fresh.RuleOverride || !old.RuleOverride || old.Overridden || old.Variant != fresh.Variant ||
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

func (c *Context) GetAssignment(experimentName string) *Assignment {
	return c.getAssignment(experimentName).assignment
}

func (c *Context) getAssignment(experimentName string) *cachedAssignment {
	var at = clockReading{clock: c.Clock_}
	return c.getAssignmentAt(experimentName, &at)
}

// getAssignmentAt resolves the assignment at the clock reading at, which is
// read only for a switchback experiment, and at most once.
func (c *Context) getAssignmentAt(experimentName string, at *clockReading) *cachedAssignment {
	c = c.live()
	// A valid cached assignment only needs the read lock, so concurrent reads
	// of a shared context do not serialise.
	c.ContextLock_.RLock()
	var indexed = c.indexedExperiment(experimentName)
	if entry, _, state := c.validCachedAssignment(experimentName, indexed, assignmentSlotFor(experimentName, indexed, at)); state == assignmentValid {
		c.ContextLock_.RUnlock()
		return entry
	}
	c.ContextLock_.RUnlock()

	return c.resolveAssignmentLocked(experimentName, at)
}

// resolveAssignmentLocked rebuilds an assignment under the write lock. The
// unlock is deferred: resolving a switchback cluster can call an attribute's
// own JSON or text marshaler, and a panic there must not strand the lock.
func (c *Context) resolveAssignmentLocked(experimentName string, at *clockReading) *cachedAssignment {
	// Cache miss, out of date, or revalidated. Check again under the write
	// lock: another goroutine may have rebuilt the assignment meanwhile, and
	// replacing it would lose its exposed flag and queue a second exposure.
	c.ContextLock_.Lock()
	defer c.ContextLock_.Unlock()
	var indexed *indexedExperiment
	// The index may have been replaced between the locks; setData publishes
	// it under this lock, so it is stable from here on.
	indexed = c.indexedExperiment(experimentName)
	var slot = assignmentSlotFor(experimentName, indexed, at)
	if c.switchbackEpochStale(experimentName, indexed) {
		c.dropSwitchbackEpoch(experimentName)
	}
	var previous, validated, state = c.validCachedAssignment(experimentName, indexed, slot)
	if state == assignmentValid {
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
		previous.attrsSeq = atomic.LoadInt32(&c.AttrsSeq_)
		return previous
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
	var entry = &cachedAssignment{assignment: assignment, rules: rules, attrsSeq: atomic.LoadInt32(&c.AttrsSeq_), retirement: &atomic.Bool{}}
	if efound {
		entry.resolvedId = experiment.Data.Id
		entry.resolvedIteration = experiment.Data.Iteration
		entry.resolvedUnitType = experiment.Data.UnitType
	}

	// Resolved for overrides and rule wins too: the holdouts' exposures fire
	// whatever chose the variant.
	var held = false
	if efound && len(indexed.holdouts) > 0 {
		entry.holdouts, held = c.resolveHoldouts(indexed.holdouts, experiment.Data)
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
		c.resolveSwitchback(entry, indexed, slot)
	} else {
		if efound {
			var unitType = experiment.Data.UnitType

			// A matching rule is an explicit assignment, like an override: it
			// wins over the audience, audienceStrict, traffic eligibility,
			// full-on and custom assignments.
			if rule := c.resolveRule(experiment.Data, rules, unitType, false, true); rule != nil {
				assignment.Variant = rule.variant
				assignment.RuleOverride = true
				entry.ruleMissingUnit = rule.missingUnit
			} else if held {
				entry.holdouts.suppressed = true
			}

			if !assignment.RuleOverride && !entry.holdouts.isSuppressed() && len(experiment.Data.Audience) > 0 {
				var attrs = c.evaluationAttributes()

				var match, err = c.AudienceMatcher_.Evaluate(experiment.Data.Audience, attrs)
				if err == nil {
					assignment.AudienceMismatch = !match.Get()
				}
			}

			if !assignment.RuleOverride && !entry.holdouts.isSuppressed() {
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
			assignment.AttrsSeq = atomic.LoadInt32(&c.AttrsSeq_)
		}
	}

	if efound && assignment.Variant >= 0 && (assignment.Variant < len(experiment.Data.Variants)) {
		assignment.Variables = experiment.Variables[assignment.Variant]
	}

	if efound {
		c.keepExposedRuleAssignment(indexed, previous, entry)
	}
	var rebuiltOverride = previous != nil && previous.assignment.Overridden && efound &&
		c.index_.Load().keyed && !previous.resolvedExperimentMatches(experiment.Data)
	if previous != nil && (previous.holdouts != nil || entry.holdouts != nil || rebuiltOverride) && previous.assignment.Exposed != assignment.Exposed {
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
		c.retireSwitchbackSlot(experimentName, slot.key)
	}
	c.retireAcrossRandomization(experimentName, slot, entry)
	c.assignmentCache_[slot.key] = entry
	if entry.switchback != nil {
		c.retainSwitchbackSlot(experimentName, indexed, slot)
	}
	c.AssignmentCache[experimentName] = *assignment
	return entry
}

// resolveHoldouts draws, or reuses, the unit's arm in each holdout covering
// the experiment, hashing the experiment's unit type (never the holdout's
// own), and reports whether any arm holds the experiment out. An arm is nil
// when the experiment's unit is not set. The caller holds ContextLock_ for
// writing.
func (c *Context) resolveHoldouts(holdouts []*jsonmodels.Holdout, data jsonmodels.Experiment) (*assignmentHoldouts, bool) {
	var resolved = &assignmentHoldouts{holdouts: holdouts, arms: make([]*holdoutArm, len(holdouts))}
	var held = false
	for i, holdout := range holdouts {
		var arm = c.holdoutArm(holdout, data.UnitType)
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
func (c *Context) holdoutArm(holdout *jsonmodels.Holdout, unitType string) *holdoutArm {
	if index := c.index_.Load(); index != nil {
		if live, found := index.holdouts[holdout.Id]; found {
			holdout = live
		}
	}
	var key = holdoutKey{id: holdout.Id, unitType: unitType}
	if cached, found := c.holdoutArms_[key]; found && cached.assignment.Iteration == holdout.Iteration {
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
	if c.holdoutArms_ == nil {
		c.holdoutArms_ = map[holdoutKey]*holdoutArm{}
	}
	c.holdoutArms_[key] = arm
	return arm
}

func (c *Context) ClearRefreshTimer() {
	c = c.live()
	c.StateLock_.Lock()
	defer c.StateLock_.Unlock()
	if c.RefreshTimer_ != nil {
		c.RefreshTimer_.Stop()
		c.RefreshTimer_ = nil
	}
}

// GetExperiment returns the experiment from the installed context data. It
// reads the index setData published atomically, so a refresh installing new
// data cannot race it, and it takes no lock.
func (c *Context) GetExperiment(experimentName string) (ExperimentVariables, bool) {
	if entry := c.indexedExperiment(experimentName); entry != nil {
		return entry.experiment, true
	}
	return ExperimentVariables{}, false
}

// indexedExperiment returns the installed experiment with its parsed
// assignment rules and covering holdouts, or nil.
func (c *Context) indexedExperiment(experimentName string) *indexedExperiment {
	var index = c.index_.Load()
	if index == nil {
		return nil
	}
	return index.experiments[experimentName]
}

func (c *Context) ExperimentMatches(experiment jsonmodels.Experiment, assignment Assignment) bool {
	return experiment.Id == assignment.Id &&
		experiment.UnitType == assignment.UnitType &&
		experiment.Iteration == assignment.Iteration &&
		experiment.FullOnVariant == assignment.FullOnVariant &&
		reflect.DeepEqual(experiment.TrafficSplit, assignment.TrafficSplit)
}

func (c *Context) AudienceMatches(experiment jsonmodels.Experiment, assignment *Assignment) bool {
	if len(experiment.Audience) > 0 {
		if atomic.LoadInt32(&c.AttrsSeq_) > assignment.AttrsSeq {
			var attrs = c.evaluationAttributes()

			var match, err = c.AudienceMatcher_.Evaluate(experiment.Audience, attrs)
			var newAudienceMismatch = false
			if err == nil {
				newAudienceMismatch = !match.Get()
			}

			if newAudienceMismatch != assignment.AudienceMismatch {
				return false
			}
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
	c = c.live()
	var self = c.snapshot()
	self.EventLogger_ = buffer
	var index = map[string]ExperimentVariables{}
	var indexVariables = map[string][]ExperimentVariables{}
	var contextCustomFields = map[string]map[string]ContextCustomFieldValue{}
	var customFieldErrors = map[string]map[string]error{}
	var rules = map[string]*experimentRules{}
	var holdoutsById map[int]*jsonmodels.Holdout
	if len(data.Holdouts) > 0 {
		// A copy, so assignments pin the holdouts as installed even if the
		// caller reuses its slice.
		var installed = append([]jsonmodels.Holdout(nil), data.Holdouts...)
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
	var previous = c.index_.Load()
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
					if parser, ok := c.VariableParser_.(CustomFieldValueParser); ok {
						parsed, err := parser.ParseCustomFieldValue(self, experiment.Name, customFieldValue.Name, customValue)
						value.Value = parsed
						if err != nil {
							if customFieldErrors[experiment.Name] == nil {
								customFieldErrors[experiment.Name] = map[string]error{}
							}
							customFieldErrors[experiment.Name][customFieldValue.Name] = err
						}
					} else {
						value.Value = c.VariableParser_.Parse(self, experiment.Name, customFieldValue.Name, customValue)
					}
				} else if strings.HasPrefix(customFieldValue.Type, "boolean") {
					parsed, err := strconv.ParseBool(customValue)
					if err != nil && c.EventLogger_ != nil {
						buffer.HandleEvent(self, Error,
							errors.New("SetData: Failed to parse boolean custom field '"+customFieldValue.Name+
								"' in experiment '"+experiment.Name+"': "+err.Error()))
					}
					value.Value = parsed
				} else if strings.HasPrefix(customFieldValue.Type, "number") {
					parsed, err := strconv.ParseInt(customValue, 10, 64)
					if err != nil && c.EventLogger_ != nil {
						buffer.HandleEvent(self, Error,
							errors.New("SetData: Failed to parse number custom field '"+customFieldValue.Name+
								"' in experiment '"+experiment.Name+"': "+err.Error()))
					}
					value.Value = parsed
				} else {
					value.Value = customValue
				}

				experimentCustomFields[value.Name] = value
			}
		}
		if len(experiment.AssignmentRules) > 0 {
			rules[experiment.Name] = c.experimentRulesFor(previousRules(experiment.Name), experiment.AssignmentRules)
		}
		if isSwitchback(experiment) {
			if switchbacks == nil {
				switchbacks = map[string]*switchbackDefinition{}
			}
			switchbacks[experiment.Name] = newSwitchbackDefinition(experiment.SwitchbackClusters)
		} else if len(experiment.HoldoutIds) > 0 && len(holdoutsById) > 0 {
			if covering := coveringHoldouts(experiment.HoldoutIds, holdoutsById); len(covering) > 0 {
				if coverage == nil {
					coverage = map[string][]*jsonmodels.Holdout{}
				}
				coverage[experiment.Name] = covering
			}
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

	// Serialize publication with resolution to keep coverage and arms in one generation.
	c.ContextLock_.Lock()
	c.DataLock.Lock()

	c.Index_ = index
	var indexed = make(map[string]*indexedExperiment, len(index))
	// Each installed experiment's key set is owned by the index, not by
	// cached assignments, so old decisions retain neither variant values
	// nor key sets. Only a holdout, a SetUnit eviction of a split rule or a
	// switchback slot retirement retires an assignment, so the sets are built
	// only once the context has seen holdouts, assignment rules or
	// switchbacks; payloads with none of them cost nothing more.
	// The index installed now is read under the lock, so an install that
	// finished while this one was parsing does not lose the gate.
	var current = c.index_.Load()
	var keyed = len(holdoutsById) > 0 || len(rules) > 0 || len(switchbacks) > 0 || (current != nil && current.keyed)
	for name, experiment := range index {
		var keys variableKeySet
		if keyed {
			keys = variableKeySet{}
			for _, variables := range experiment.Variables {
				for key := range variables {
					if _, found := keys[key]; !found {
						keys[strings.Clone(key)] = struct{}{}
					}
				}
			}
		}
		indexed[name] = &indexedExperiment{experiment: experiment, rules: rules[name], holdouts: coverage[name], variableKeys: keys,
			switchback: switchbacks[name]}
	}
	if switchbacks != nil {
		// Under ContextLock_, so the generations follow the index this
		// replaces, whatever other setData ran meanwhile.
		c.assignSwitchbackGenerations(c.index_.Load(), indexed)
	}
	c.index_.Store(&contextIndex{experiments: indexed, holdouts: holdoutsById, keyed: keyed})
	c.ContextCustomFields_ = contextCustomFields
	c.customFieldErrors_ = customFieldErrors
	c.indexVariables_ = indexVariables
	c.IndexVariables_ = legacyIndexVariables(indexVariables)
	c.Data_ = data
	c.Ready_.Store(true)

	c.SetRefreshTimer()
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
		if loggers := c.live().loggers_; loggers != nil {
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
	c = c.live()
	c.ContextLock_.Lock()
	defer c.ContextLock_.Unlock()
	c.DataLock.Lock()
	c.Index_ = map[string]ExperimentVariables{}
	var keyed = false
	if previous := c.index_.Load(); previous != nil {
		keyed = previous.keyed
	}
	c.index_.Store(&contextIndex{keyed: keyed})
	c.indexVariables_ = map[string][]ExperimentVariables{}
	c.IndexVariables_ = map[interface{}]interface{}{}
	c.customFieldErrors_ = nil
	c.Data_ = jsonmodels.ContextData{}
	c.Ready_.Store(true)
	c.Failed_.Store(true)
	c.DataLock.Unlock()
}

func (c *Context) Flush() *future.Future {
	c = c.live()
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
			c.addInflight(settled)
			c.StateLock_.Lock()
			var activeLogs []*future.Future
			for _, f := range c.publishLogs_ {
				if !f.Ready() {
					activeLogs = append(activeLogs, f)
				}
			}
			c.publishLogs_ = append(activeLogs, result)
			c.StateLock_.Unlock()
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

			var mapper = FlushMapper{ctx: c}
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

func (c *Context) addInflight(f *future.Future) {
	c.StateLock_.Lock()
	c.inflight_ = append(c.inflight_, f)
	c.StateLock_.Unlock()
}

func (c *Context) removeInflight(f *future.Future) {
	c.StateLock_.Lock()
	for i, g := range c.inflight_ {
		if g == f {
			c.inflight_ = append(c.inflight_[:i:i], c.inflight_[i+1:]...)
			break
		}
	}
	c.StateLock_.Unlock()
}

func (c *Context) inflightCount() int {
	c.StateLock_.Lock()
	defer c.StateLock_.Unlock()
	return len(c.inflight_)
}

func (c *Context) inflightSnapshot() []*future.Future {
	c.StateLock_.Lock()
	defer c.StateLock_.Unlock()
	return append([]*future.Future(nil), c.inflight_...)
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
	// ctx is the live context the SDK flushes; when nil, Context is used.
	ctx *Context
}

func (f FlushMapper) Apply(value interface{}) interface{} {
	var key = value.(Pair).a
	var val = value.(Pair).b
	var cntx = f.ctx
	if cntx == nil {
		cntx = &f.Context
	}
	var data [22]byte
	var uid = cntx.GetUnitHash(key, val, data[:], false)
	var res = f.forceASCII(string(uid))
	return jsonmodels.Unit{Type: value.(Pair).a, Uid: res}
}

func (f FlushMapper) forceASCII(s string) string {
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
	c.StateLock_.Lock()
	defer c.StateLock_.Unlock()
	// A refresh that finishes after Close began calls SetData, which gets here.
	// Installing the timer then would leave a timer on a closed context.
	if c.Closed_.Load().(bool) || c.Closing_.Load().(bool) || c.closeStarted_ {
		return
	}
	if c.RefreshInterval_ > 0 && c.RefreshTimer_ == nil {
		var rate = time.Duration(uint64(c.RefreshInterval_ * int64(time.Millisecond)))

		c.RefreshTimer_ = time.AfterFunc(rate, func() {
			c.StateLock_.Lock()
			var timer = c.RefreshTimer_
			if timer != nil {
				timer.Reset(rate)
			}
			c.StateLock_.Unlock()
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
	c.DataLock.RLock()
	var experiments = c.indexVariables_[key]
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
