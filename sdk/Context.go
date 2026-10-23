package sdk

import (
	"context"
	"errors"
	"fmt"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"reflect"
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
	IndexVariables_      map[string][]ExperimentVariables
	ContextLock_         *sync.RWMutex
	HashedUnits_         map[interface{}]interface{}
	Assigners_           map[interface{}]interface{}
	AssignmentCache      map[string]*Assignment
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
	// FinalizedFuture_ includes logger delivery; ClosingFuture_ only includes
	// transport/sealing, allowing loggers to re-enter Close safely.
	FinalizedFuture_ *future.Future
	publishLogs_     []*future.Future
	RefreshFuture_   *future.Future
	TimeoutLock_     *sync.Mutex
	// StateLock_ guards ReadyFuture_, RefreshFuture_, ClosingFuture_ and RefreshTimer_.
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

	// customFieldErrors_ holds, per experiment and field name, the error from
	// parsing a "json" custom field. GetCustomFieldValue reports it through the
	// event logger when the field is read. Guarded by DataLock.
	customFieldErrors_ map[string]map[string]error
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
	AttrsSeq         int32
}

func CreateContext(clock internal.Clock, config ContextConfig, dataFuture *future.Future, dataProvider ContextDataProvider,
	eventHandler ContextEventHandler, eventLogger ContextEventLogger, variableParser VariableParser,
	audienceMatcher AudienceMatcher) *Context {
	var cntx = Context{Clock_: clock, PublishDelay_: config.PublishDelay_, RefreshInterval_: config.RefreshInterval_,
		EventHandler_: eventHandler, DataProvider_: dataProvider, VariableParser_: variableParser,
		AudienceMatcher_: audienceMatcher, Units_: map[string]string{}}

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

	cntx.AssignmentCache = map[string]*Assignment{}
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
			if err == nil {
				result, ok := val.(jsonmodels.ContextData)
				if !ok {
					err = &TypeError{Expected: "jsonmodels.ContextData", Actual: val}
					tmp.SetDataFailed(err)
					readyFutureDone(nil, err)
					cntx.setReadyFuture(nil)
					tmp.LogError(err)
					return
				}
				var diagnostics, panicErr = tmp.applyRefreshedData(result)
				if panicErr != nil {
					tmp.SetDataFailed(panicErr)
					readyFutureDone(nil, panicErr)
					cntx.setReadyFuture(nil)
					tmp.dispatchDiagnostics(diagnostics)
					tmp.LogError(panicErr)
					return
				}
				readyFutureDone(result, nil)
				cntx.setReadyFuture(nil)
				tmp.dispatchDiagnostics(diagnostics)
				tmp.LogEvent(Ready, result)

				if tmp.GetPendingCount() > 0 {
					tmp.SetTimeout()
				}
			} else {
				tmp.SetDataFailed(err)
				readyFutureDone(nil, err)
				cntx.setReadyFuture(nil)
				tmp.LogError(err)
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

func (c *Context) WaitUntilReadyAsync() *future.Future {
	if c.Ready_.Load().(bool) {
		return future.Call(func() (future.Value, error) {
			return c, nil
		})
	}
	return c.getReadyFuture()
}

func (c *Context) WaitUntilReady() Context {
	if !c.Ready_.Load().(bool) {
		var ft = c.getReadyFuture()
		if ft != nil && !ft.Ready() {
			ft.Join(context.Background())
		}
	}
	return c.snapshot()
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

// live returns the context that owns the lifecycle state: the context itself,
// or, for a copy handed to a callback, the context it was copied from.
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

func (c *Context) SetOverride(experimentName string, variant int) error {
	var err = c.CheckNotClosed()
	if err != nil {
		return err
	}

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

func (c *Context) SetCustomAssignment(experimentName string, variant int) error {
	var err = c.CheckNotClosed()
	if err != nil {
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
	var err = c.CheckNotClosed()
	if err != nil {
		return err
	}

	c.ContextLock_.Lock()
	var previous, exist = c.Units_[unitType]
	if exist && previous != uid {
		c.ContextLock_.Unlock()
		return fmt.Errorf("Unit '%s' UID already set.", unitType) //nolint:staticcheck // canonical SDK error text
	}

	var trimmed = strings.TrimSpace(uid)
	if len(trimmed) <= 0 {
		c.ContextLock_.Unlock()
		return errors.New("unit  UID must not be blank.")
	}

	c.Units_[unitType] = trimmed
	c.ContextLock_.Unlock()
	return nil
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
	var assignment = c.GetAssignment(experimentName)
	if !assignment.Exposed.Load().(bool) {
		c.QueueExposure(assignment)
	}

	return assignment.Variant, nil
}

func (c *Context) QueueExposure(assignment *Assignment) {
	c = c.live()
	if assignment.Exposed.CompareAndSwap(false, true) {
		var exposure = jsonmodels.Exposure{Id: assignment.Id, Name: assignment.Name, Unit: assignment.UnitType,
			Variant: assignment.Variant, ExposedAt: c.Clock_.Millis(), Assigned: assignment.Assigned,
			Eligible: assignment.Eligible, Overridden: assignment.Overridden, FullOn: assignment.FullOn,
			Custom: assignment.Custom, AudienceMismatch: assignment.AudienceMismatch}

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

	return c.GetAssignment(experimentName).Variant, nil
}

func (c *Context) GetVariableKeys() (map[string][]string, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var variableKeys = map[string][]string{}

	c.DataLock.RLock()
	for key, experiments := range c.IndexVariables_ {
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
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var assignment, errres = c.GetVariableAssignment(key)
	if errres == nil && assignment.Variables != nil {
		if !assignment.Exposed.Load().(bool) {
			c.QueueExposure(assignment)
		}

		var value, exist = assignment.Variables[key]
		if exist {
			return value, nil
		}
	}
	return defaultValue, nil
}

func (c *Context) PeekVariableValue(key string, defaultValue interface{}) (interface{}, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var assignment, errres = c.GetVariableAssignment(key)
	if errres == nil {
		var value, exist = assignment.Variables[key]
		if exist {
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
		donefun(nil, outcome)

		// The load is over; only now tell the logger what the parser found.
		c.dispatchDiagnostics(diagnostics)
		if outcome == nil {
			c.LogEvent(Refresh, result)
		} else {
			c.LogError(outcome)
		}
	})
	return tempfuture
}

// applyRefreshedData sets refreshed data and returns a panic from the variable
// parser, or any other code SetData calls, as an error.
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
	closing, _, err := c.live().closeAttempt()
	return closing, err
}

// closeAttempt starts or joins a close and returns that attempt's close and
// finalization futures together, so a later retry cannot replace them.
func (c *Context) closeAttempt() (*future.Future, *future.Future, error) {
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
		if existing != nil {
			return existing, existingFinalized, nil
		}
		var resolved, resolve = future.New()
		resolve(nil, nil)
		return resolved, existingFinalized, nil
	}
	c.Closing_.Store(true)
	var tempFuture, done = future.New()
	c.ClosingFuture_ = tempFuture
	var finalized, finalizeDone = future.New()
	c.FinalizedFuture_ = finalized
	c.StateLock_.Unlock()

	c.ClearRefreshTimer()

	// Send what is queued. The publish is registered in inflight_ in the same
	// critical section that drains the queue.
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
	var settle func()
	settle = func() {
		c.EventLock_.Lock()
		var waitFor = c.inflightSnapshot()
		if len(waitFor) > 0 {
			c.EventLock_.Unlock()
			var remaining = int32(len(waitFor))
			for _, f := range waitFor {
				f.Listen(func(value future.Value, err error) {
					recordFailure(err)
					if atomic.AddInt32(&remaining, -1) == 0 {
						settle()
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
			// Publish or Close can retry it, and bring back the refresh timer
			// that Close stopped.
			c.StateLock_.Lock()
			c.ClosingFuture_ = nil
			c.Closing_.Store(false)
			c.StateLock_.Unlock()
			c.SetRefreshTimer()
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
		go func() {
			c.waitPublishLogs()
			c.LogEvent(Finalize, nil)
			finalizeDone(nil, nil)
		}()
		done(nil, nil)
	}
	settle()
	return tempFuture, finalized, nil
}

// FinalizeAsync closes the context and waits for publish/error and finalize
// logger callbacks to return. Unlike CloseAsync, this is an observer-delivery
// barrier. Do not wait on it from a logger; loggers may re-enter Close instead.
func (c *Context) FinalizeAsync() (*future.Future, error) {
	c = c.live()
	_, finalized, err := c.closeAttempt()
	if err != nil {
		return nil, err
	}
	return finalized, nil
}

func (c *Context) waitPublishLogs() {
	c.StateLock_.Lock()
	logs := append([]*future.Future(nil), c.publishLogs_...)
	c.StateLock_.Unlock()
	for _, f := range logs {
		f.Join(context.Background())
	}
}

func (c *Context) Close() {
	c = c.live()
	c.ClearRefreshTimer()
	var fut, err = c.CloseAsync()
	if err == nil {
		fut.Join(context.Background())
	}
}

func (c *Context) GetAssignment(experimentName string) *Assignment {

	c.ContextLock_.Lock()
	if assignment, found := c.AssignmentCache[experimentName]; found {
		var custom, cfound = c.Cassignments_[experimentName]
		var override, ofound = c.Overrides_[experimentName]
		var experiment, efound = c.GetExperiment(experimentName)

		if ofound {
			if assignment.Overridden && assignment.Variant == override.(int) {
				c.ContextLock_.Unlock()
				return assignment
			}
		} else if !efound {
			if !assignment.Assigned {
				c.ContextLock_.Unlock()
				return assignment
			}
		} else if !cfound || custom.(int) == assignment.Variant {
			if c.ExperimentMatches(experiment.Data, *assignment) && c.AudienceMatches(experiment.Data, assignment) {
				c.ContextLock_.Unlock()
				return assignment
			}
		}
	}
	c.ContextLock_.Unlock()
	// cache miss or out-dated
	c.ContextLock_.Lock()

	var custom, cfound = c.Cassignments_[experimentName]
	var override, ofound = c.Overrides_[experimentName]
	var experiment, efound = c.GetExperiment(experimentName)

	var exposed = &atomic.Value{}
	exposed.Store(false)
	var assignment = &Assignment{Exposed: exposed}
	assignment.Name = experimentName
	assignment.Eligible = true

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

			if len(experiment.Data.Audience) > 0 {
				var attrs = map[string]interface{}{}
				for _, v := range c.Attributes_ {
					attrs[v.(jsonmodels.Attribute).Name] = v.(jsonmodels.Attribute).Value
				}

				var match, err = c.AudienceMatcher_.Evaluate(experiment.Data.Audience, attrs)
				if err == nil {
					assignment.AudienceMismatch = !match.Get()
				}
			}

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

	c.AssignmentCache[experimentName] = assignment
	c.ContextLock_.Unlock()
	return assignment
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

func (c *Context) GetExperiment(experimentName string) (ExperimentVariables, bool) {
	var result, found = c.Index_[experimentName]
	return result, found
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
			var attrs = map[string]interface{}{}
			for _, v := range c.Attributes_ {
				attrs[v.(jsonmodels.Attribute).Name] = v.(jsonmodels.Attribute).Value
			}

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

func (c *Context) CheckNotClosed() error {
	if c.Closed_.Load().(bool) {
		return errors.New("ABsmartly Context is finalized.")
	} else if c.Closing_.Load().(bool) {
		return errors.New("ABsmartly Context is finalizing.")
	}
	return nil
}

func (c *Context) CheckReady(expectNotClosed bool) error {
	if !c.IsReady() {
		return errors.New("ABsmartly Context is not yet ready.")
	} else if expectNotClosed {
		return c.CheckNotClosed()
	}
	return nil
}

// SetData sets the experiment data and then reports what it found while
// parsing (malformed configs and custom fields) to the event logger.
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
		_ = c.logEvent(e.event, e.data)
	}
}

// setData does the work of SetData. Parse diagnostics go to buffer instead of
// the event logger.
func (c *Context) setData(data jsonmodels.ContextData, buffer *diagnosticBuffer) {
	var self = c.snapshot()
	self.EventLogger_ = buffer
	var index = map[string]ExperimentVariables{}
	var indexVariables = map[string][]ExperimentVariables{}
	var contextCustomFields = map[string]map[string]ContextCustomFieldValue{}
	var customFieldErrors = map[string]map[string]error{}

	for _, experiment := range data.Experiments {
		var experiemntVariables = ExperimentVariables{}
		var experimentCustomFields = map[string]ContextCustomFieldValue{}
		experiemntVariables.Data = experiment
		experiemntVariables.Variables = make([]map[string]interface{}, 0)

		for _, variant := range experiment.Variants {
			if len(variant.Config) > 0 {
				var variables = c.VariableParser_.Parse(self, experiment.Name, variant.Name, variant.Config)
				for key := range variables {
					// Keep a list of experiments per variable key (matching the
					// canonical SDKs). Guard against adding the same experiment
					// twice when multiple variants define the same key.
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
		contextCustomFields[experiment.Name] = experimentCustomFields
		index[experiment.Name] = experiemntVariables
	}

	c.DataLock.Lock()

	c.Index_ = index
	c.ContextCustomFields_ = contextCustomFields
	c.customFieldErrors_ = customFieldErrors
	c.IndexVariables_ = indexVariables
	c.Data_ = data
	c.Ready_.Store(true)

	c.SetRefreshTimer()
	c.DataLock.Unlock()
}

// LogEvent and LogError pass an event to the event logger. A logger is an
// observer: a panic in it is contained here, so it can neither end the host
// process from an SDK goroutine nor leave the SDK's own state half updated.
func (c *Context) LogEvent(event EventType, data interface{}) {
	_ = c.logEvent(event, data)
}

func (c *Context) LogError(err error) {
	_ = c.logEvent(Error, err)
}

// logEvent returns the logger's panic as an error, for the callers that can
// report it through a future.
func (c *Context) logEvent(event EventType, data interface{}) (panicErr error) {
	if c.EventLogger_ == nil {
		return nil
	}
	return notifyLogger(c.EventLogger_, c.snapshot(), event, data)
}

// notifyLogger calls an event logger and returns a panic from it as an error.
// Every call into a logger goes through here, including the ones made while a
// context is being created or its data parsed, so a logger can never end the
// host process or abort the SDK code that reports to it.
func notifyLogger(logger ContextEventLogger, ctx Context, event EventType, data interface{}) (panicErr error) {
	if logger == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			panicErr = panicToError(r)
		}
	}()
	logger.HandleEvent(ctx, event, data)
	return nil
}

func panicToError(r interface{}) error {
	if err, ok := r.(error); ok {
		return err
	}
	return &future.PanicError{Value: r}
}

func (c *Context) SetDataFailed(err error) {
	c.DataLock.Lock()
	c.Index_ = map[string]ExperimentVariables{}
	c.IndexVariables_ = map[string][]ExperimentVariables{}
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
		if c.PendingCount_.Load().(int32) > 0 {
			var eventCount int32

			c.EventLock_.Lock()
			eventCount = c.PendingCount_.Load().(int32)
			var exposures = make([]jsonmodels.Exposure, len(c.Exposures_))
			var achievements = make([]jsonmodels.GoalAchievement, len(c.Achievements_))
			// settled is the future Close waits on. It settles before the event
			// logger runs, so a logger that calls Close cannot wait on its own
			// publish. result is the future callers get; it settles after the
			// logger has run.
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

				var mapper = FlushMapper{Context: c}
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
							runLogger(done, nil, func() error { return c.logEvent(Publish, event) })
							c.removeInflight(settled)
							settledDone(nil, nil)
						} else {
							c.requeueEvents(exposures, achievements, eventCount)
							runLogger(done, err, func() error { return c.logEvent(Error, err) })
							c.removeInflight(settled)
							settledDone(nil, err)
						}
					})

				return result
			}
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
func runLogger(done future.SetResultFunc, result error, log func() error) {
	go func() {
		_ = log()
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
	Context *Context
}

func (f FlushMapper) Apply(value interface{}) interface{} {
	var key = value.(Pair).a
	var val = value.(Pair).b
	var cntx = f.Context
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
		{
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
}

func (c *Context) SetRefreshTimer() {
	c = c.live()
	c.StateLock_.Lock()
	defer c.StateLock_.Unlock()
	// A refresh that finishes after Close began calls SetData, which gets here.
	// Installing the timer then would leave a timer on a closed context.
	if c.Closed_.Load().(bool) || c.Closing_.Load().(bool) {
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

func (c *Context) GetVariableExperiment(key string) (ExperimentVariables, error) {
	c.DataLock.RLock()
	var experiments = c.IndexVariables_[key]
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
