package main

import (
	"context"
	"errors"
	"github.com/absmartly/go-sdk/main/future"
	"github.com/absmartly/go-sdk/main/internal"
	"github.com/absmartly/go-sdk/main/jsonmodels"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Context struct {
	PublishDelay_    int64
	RefreshInterval_ int64
	EventHandler_    ContextEventHandler
	EventLogger_     ContextEventLogger
	DataProvider_    ContextDataProvider
	VariableParser_  VariableParser
	AudienceMatcher_ AudienceMatcher
	Units_           map[string]string
	Failed_          bool
	DataLock         *sync.RWMutex
	Data_            jsonmodels.ContextData
	Index_           map[string]ExperimentVariables
	IndexVariables_  map[interface{}]interface{}
	ContextLock_     *sync.RWMutex
	HashedUnits_     map[interface{}]interface{}
	Assigners_       map[interface{}]interface{}
	AssignmentCache  map[string]Assignment
	EventLock_       *sync.Mutex
	Exposures_       []jsonmodels.Exposure
	Achievements_    []jsonmodels.GoalAchievement
	Attributes_      []interface{}
	Overrides_       map[interface{}]interface{}
	Cassignments_    map[interface{}]interface{}
	PendingCount_    *atomic.Int32
	Closing_         *atomic.Bool
	Closed_          *atomic.Bool
	Refreshing_      *atomic.Bool
	ReadyFuture_     *future.Future
	ClosingFuture_   *future.Future
	RefreshFuture_   *future.Future
	TimeoutLock_     *sync.Mutex
	Timeout_         chan bool
	RefreshTimer_    chan bool
	Clock_           internal.Clock
}

type ExperimentVariables struct {
	Data      jsonmodels.Experiment
	Variables []map[string]interface{}
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
	Exposed          *atomic.Bool
}

func CreateContext(clock internal.Clock, config ContextConfig, dataFuture *future.Future, dataProvider ContextDataProvider,
	eventHandler ContextEventHandler, eventLogger ContextEventLogger, variableParser VariableParser,
	audienceMatcher AudienceMatcher, buff [512]byte, block [16]int32, st [4]int32) Context {
	var cntx = Context{Clock_: clock, PublishDelay_: config.PublishDelay_, RefreshInterval_: config.RefreshInterval_,
		EventHandler_: eventHandler, DataProvider_: dataProvider, VariableParser_: variableParser,
		AudienceMatcher_: audienceMatcher, Units_: map[string]string{}}

	if config.EventLogger_ != nil {
		cntx.EventLogger_ = config.EventLogger_
	} else {
		cntx.EventLogger_ = eventLogger
	}

	cntx.Closed_ = &atomic.Bool{}
	cntx.Closing_ = &atomic.Bool{}
	cntx.Refreshing_ = &atomic.Bool{}
	cntx.PendingCount_ = &atomic.Int32{}
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
		cntx.SetUnits(units)
	}

	cntx.Assigners_ = map[interface{}]interface{}{}
	cntx.HashedUnits_ = map[interface{}]interface{}{}

	var attributes = config.Attributes_
	if attributes != nil {
		cntx.SetAttributes(attributes)
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
		dataFuture.Listen(func(val future.Value, err error) {
			if err == nil {
				var result = val.(*jsonmodels.ContextData)
				cntx.SetData(result)
				cntx.LogEvent(Ready, result)
			} else {
				cntx.SetDataFailed(err)
				cntx.LogError(err)
			}

		})
	} else {
		var tmp = &cntx
		var tempFuture, readyFutureDone = future.New()
		cntx.ReadyFuture_ = tempFuture
		dataFuture.Listen(func(val future.Value, err error) {
			if err == nil {
				var result = val.(*jsonmodels.ContextData)
				tmp.SetData(result)
				readyFutureDone(nil, nil)
				tmp.LogEvent(Ready, result)

				if tmp.GetPendingCount() > 0 {
					tmp.SetTimeout(buff, block, st)
				}
			} else {
				tmp.Data_ = jsonmodels.ContextData{}
				tmp.SetDataFailed(err)
				readyFutureDone(nil, err)
				tmp.LogError(err)
			}

		})
	}

	return cntx
}

func (c Context) IsReady() bool {
	return reflect.DeepEqual(c.Data_, jsonmodels.ContextData{})
}

func (c Context) IsFailed() bool {
	return c.Failed_
}

func (c Context) IsClosed() bool {
	return c.Closed_.Load()
}

func (c Context) IsClosing() bool {
	return !c.Closed_.Load() && c.Closing_.Load()
}

func (c Context) WaitUntilReadyAsync() *future.Future {
	if reflect.DeepEqual(c.Data_, jsonmodels.ContextData{}) {
		return future.Call(func() (future.Value, error) {
			return c, nil
		})
	} else {
		c.ReadyFuture_.Listen(func(val future.Value, err error) {
			c.ReadyFuture_.SetResult(val, err)
		})
		return c.ReadyFuture_
	}
}

func (c Context) WaitUntilReady() Context {
	if !reflect.DeepEqual(c.Data_, jsonmodels.ContextData{}) {
		var ft = c.ReadyFuture_
		if ft != nil && !ft.Ready() {
			ft.Join(context.Background())
		}
	}
	return c
}

func (c Context) GetExperiments() ([]string, error) {
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

func (c Context) GetData() (*jsonmodels.ContextData, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return &jsonmodels.ContextData{}, err
	}

	return &c.Data_, nil

}

func (c Context) SetOverride(experimentName string, variant int) error {
	var err = c.CheckNotClosed()
	if err != nil {
		return err
	}

	PutRW(c.ContextLock_, c.Overrides_, experimentName, variant)
	return nil
}

func (c Context) GetOverride(experimentName string) int {
	return GetRW(c.ContextLock_, c.Overrides_, experimentName).(int)
}

func (c Context) SetOverrides(overrides map[string]int) error {
	for key, value := range overrides {
		var err = c.SetOverride(key, value)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c Context) SetCustomAssignment(experimentName string, variant int) error {
	var err = c.CheckNotClosed()
	if err != nil {
		return err
	}

	PutRW(c.ContextLock_, c.Cassignments_, experimentName, variant)
	return nil
}

func (c Context) GetCustomAssignment(experimentName string) int {
	return GetRW(c.ContextLock_, c.Cassignments_, experimentName).(int)
}

func (c Context) SetCustomAssignments(customAssignments map[string]int) error {
	for key, value := range customAssignments {
		var err = c.SetCustomAssignment(key, value)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c Context) SetUnit(unitType string, uid string) error {
	var err = c.CheckNotClosed()
	if err != nil {
		return err
	}

	c.ContextLock_.Lock()
	var previous = c.Units_[unitType]
	if len(previous) > 0 && previous != uid {
		return errors.New("unit already set")
	}

	var trimmed = strings.TrimSpace(uid)
	if len(trimmed) <= 0 {
		return errors.New("unit  UID must not be blank.")
	}

	c.Units_[unitType] = trimmed
	c.ContextLock_.Unlock()
	return nil
}

func (c Context) SetUnits(units map[string]string) error {
	for key, value := range units {
		var err = c.SetUnit(key, value)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c Context) SetAttribute(name string, value interface{}) error {
	var err = c.CheckNotClosed()
	if err != nil {
		return err
	}

	AddRW(c.ContextLock_, c.Attributes_, jsonmodels.Attribute{Name: name, Value: value, SetAt: c.Clock_.Millis()})
	return nil
}

func (c Context) SetAttributes(attributes map[string]interface{}) error {
	for key, value := range attributes {
		var err = c.SetAttribute(key, value)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c Context) GetTreatment(experimentName string, buff [512]byte, block [16]int32, st [4]int32, assignBuff [12]int8) (int, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return -1, err
	}

	var assignment = c.GetAssignment(experimentName, buff, block, st, assignBuff)
	if !assignment.Exposed.Load() {
		c.QueueExposure(assignment, buff, block, st)
	}

	return assignment.Variant, nil
}

func (c Context) QueueExposure(assignment Assignment, buff [512]byte, block [16]int32, st [4]int32) {
	if assignment.Exposed.CompareAndSwap(false, true) {
		var exposure = jsonmodels.Exposure{Id: assignment.Id, Name: assignment.Name, Unit: assignment.UnitType,
			Variant: assignment.Variant, ExposedAt: c.Clock_.Millis(), Assigned: assignment.Assigned,
			Eligible: assignment.Eligible, Overridden: assignment.Overridden, FullOn: assignment.FullOn,
			Custom: assignment.Custom, AudienceMismatch: assignment.AudienceMismatch}

		c.EventLock_.Lock()
		c.PendingCount_.Add(1)
		c.Exposures_ = append(c.Exposures_, exposure)
		c.EventLock_.Unlock()

		c.LogEvent(Exposure, exposure)

		c.SetTimeout(buff, block, st)
	}
}

func (c Context) PeekTreatent(experimentName string, buff [512]byte, block [16]int32, st [4]int32, assignBuff [12]int8) (int, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return -1, err
	}

	return c.GetAssignment(experimentName, buff, block, st, assignBuff).Variant, nil
}

func (c Context) GetVariableKeys() (map[string]string, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var variableKeys = map[string]string{}

	c.DataLock.Lock()
	for key, value := range c.IndexVariables_ {
		variableKeys[key.(string)] = value.(ExperimentVariables).Data.Name
	}
	c.DataLock.Unlock()
	return variableKeys, nil
}

func (c Context) GetVariableValue(key string, defaultValue interface{}, buff [512]byte, block [16]int32, st [4]int32, assignBuff [12]int8) (interface{}, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var assignment, errres = c.GetVariableAssignment(key, buff, block, st, assignBuff)
	if errres == nil {
		if assignment.Variables != nil {
			if !assignment.Exposed.Load() {
				c.QueueExposure(assignment, buff, block, st)
			}

			var value, exist = assignment.Variables[key]
			if exist {
				return value, nil
			}
		}
	}
	return defaultValue, nil
}

func (c Context) PeekVariableValue(key string, defaultValue interface{}, buff [512]byte, block [16]int32, st [4]int32, assignBuff [12]int8) (interface{}, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var assignment, errres = c.GetVariableAssignment(key, buff, block, st, assignBuff)
	if errres == nil {
		if assignment.Variables != nil {
			var value, exist = assignment.Variables[key]
			if exist {
				return value, nil
			}
		}
	}
	return defaultValue, nil
}

func (c Context) Track(goalName string, properties map[string]interface{}, buff [512]byte, block [16]int32, st [4]int32) error {
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
	c.PendingCount_.Add(1)
	c.Achievements_ = append(c.Achievements_, achievement)
	c.EventLock_.Unlock()

	c.LogEvent(Goal, achievement)

	c.SetTimeout(buff, block, st)
	return nil
}

func (c Context) PublishAsync(buff [512]byte, block [16]int32, st [4]int32) (*future.Future, error) {
	var err = c.CheckNotClosed()
	if err != nil {
		return nil, err
	}

	return c.Flush(buff, block, st), nil
}

func (c Context) Publish(buff [512]byte, block [16]int32, st [4]int32) error {
	var result, err = c.PublishAsync(buff, block, st)
	if err == nil {
		result.Join(context.Background())
		return nil
	} else {
		return err
	}
}

func (c Context) GetPendingCount() int32 {
	return c.PendingCount_.Load()
}

func (c Context) RefreshAsync() *future.Future {

	var err = c.CheckNotClosed()
	if err != nil {
		return nil
	}

	if c.Refreshing_.CompareAndSwap(false, true) {
		var tempfuture, donefun = future.New()
		c.RefreshFuture_ = tempfuture

		c.DataProvider_.GetContextData().Listen(func(value future.Value, err error) {
			if err == nil {
				c.SetData(value.(*jsonmodels.ContextData))
				c.Refreshing_.Store(false)
				donefun(nil, nil)

				c.LogEvent(Refresh, value.(*jsonmodels.ContextData))
			} else {
				c.Refreshing_.Store(false)
				donefun(nil, err)

				c.LogError(err)
			}
		})
	}

	var ft = c.RefreshFuture_
	if ft != nil {
		return ft
	}
	var tempfuture, donefun = future.New()
	donefun(nil, nil)
	return tempfuture
}

func (c Context) Refresh() {
	c.RefreshAsync().Join(context.Background())
}

func (c Context) CloseAsync(buff [512]byte, block [16]int32, st [4]int32) (*future.Future, error) {
	if !c.Closed_.Load() {
		if c.Closing_.CompareAndSwap(false, true) {
			c.ClearRefreshTimer()

			if c.PendingCount_.Load() > 0 {
				var tempFuture, done = future.New()
				c.ClosingFuture_ = tempFuture

				c.Flush(buff, block, st).Listen(func(value future.Value, err error) {
					if err == nil {
						c.Closed_.Store(true)
						c.Closing_.Store(false)
						done(nil, nil)

						c.LogEvent(Close, nil)
					} else {
						c.Closed_.Store(true)
						c.Closing_.Store(false)
						done(nil, err)
						// event logger gets this error during publish
					}
				})
				return c.ClosingFuture_, nil
			} else {
				c.Closed_.Store(true)
				c.Closing_.Store(false)

				c.LogEvent(Close, nil)
			}
		}

		var future = c.ClosingFuture_
		if future != nil {
			return future, nil
		}
	}

	var tempFuture, done = future.New()
	done(nil, nil)
	return tempFuture, nil
}

func (c Context) Close(buff [512]byte, block [16]int32, st [4]int32) {
	var fut, err = c.CloseAsync(buff, block, st)
	if err == nil {
		fut.Join(context.Background())
	}
}

func (c Context) GetAssignment(experimentName string, buff [512]byte, block [16]int32, st [4]int32, assignBuff [12]int8) Assignment {

	c.ContextLock_.RLock()
	if assignment, found := c.AssignmentCache[experimentName]; found {
		var custom, cfound = c.Cassignments_[experimentName]
		var override, ofound = c.Overrides_[experimentName]
		var experiment, efound = c.GetExperiment(experimentName)

		if ofound {
			if assignment.Overridden && assignment.Variant == override.(int) {
				return assignment
			}
		} else if !efound {
			if !assignment.Assigned {
				return assignment
			}
		} else if !cfound || custom.(int) == assignment.Variant {
			if c.ExperimentMatches(experiment.Data, assignment) {
				return assignment
			}
		}
	}

	c.ContextLock_.RUnlock()

	// cache miss or out-dated
	c.ContextLock_.Lock()

	var custom, cfound = c.Cassignments_[experimentName]
	var override, ofound = c.Overrides_[experimentName]
	var experiment, efound = c.GetExperiment(experimentName)

	var assignment = Assignment{Exposed: &atomic.Bool{}}
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

			if experiment.Data.AudienceStrict && assignment.AudienceMismatch {
				assignment.Variant = 0
			} else if experiment.Data.FullOnVariant == 0 {
				var uid, ufound = c.Units_[experiment.Data.UnitType]
				if ufound {
					var unitHash = c.GetUnitHash(unitType, uid, buff, block, st)

					var assigner = c.GetVariantAssigner(unitType, unitHash)
					var eligible = assigner.Assign(experiment.Data.TrafficSplit, experiment.Data.TrafficSeedHi,
						experiment.Data.TrafficSeedLo, assignBuff[:]) == 1
					if eligible {
						if cfound {
							assignment.Variant = custom.(int)
							assignment.Custom = true
						} else {
							assignment.Variant = assigner.Assign(experiment.Data.Split, experiment.Data.SeedHi,
								experiment.Data.SeedLo, assignBuff[:])
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
		}
	}

	if efound && (assignment.Variant < len(experiment.Data.Variants)) {
		assignment.Variables = experiment.Variables[assignment.Variant]
	}

	c.AssignmentCache[experimentName] = assignment
	c.ContextLock_.Unlock()
	return assignment
}

func (c Context) ClearRefreshTimer() {
	if c.RefreshTimer_ != nil {
		c.RefreshTimer_ <- true
		c.RefreshTimer_ = nil
	}
}

func (c Context) GetExperiment(experimentName string) (ExperimentVariables, bool) {
	var result, found = c.Index_[experimentName]
	return result, found
}

func (c Context) ExperimentMatches(experiment jsonmodels.Experiment, assignment Assignment) bool {
	return experiment.Id == assignment.Id &&
		experiment.UnitType == assignment.UnitType &&
		experiment.Iteration == assignment.Iteration &&
		experiment.FullOnVariant == assignment.FullOnVariant &&
		reflect.DeepEqual(experiment.TrafficSplit, assignment.TrafficSplit)
}

func (c Context) CheckNotClosed() error {
	if c.Closed_.Load() {
		return errors.New("ABSmartly Context is closed")
	} else if c.Closing_.Load() {
		return errors.New("ABSmartly Context is closing")
	}
	return nil
}

func (c Context) CheckReady(expectNotClosed bool) error {
	if !c.IsReady() {
		return errors.New("ABSmartly Context is not yet ready")
	} else if expectNotClosed {
		return c.CheckNotClosed()
	}
	return nil
}

func (c Context) SetData(data *jsonmodels.ContextData) {
	var index = map[string]ExperimentVariables{}
	var indexVariables = map[interface{}]interface{}{}

	for _, experiment := range data.Experiments {
		var experiemntVariables = ExperimentVariables{}
		experiemntVariables.Data = experiment
		experiemntVariables.Variables = make([]map[string]interface{}, len(experiment.Variants))

		for _, variant := range experiment.Variants {
			if len(variant.Config) > 0 {
				var variables = c.VariableParser_.Parse(c, experiment.Name, variant.Name, variant.Config)
				for key := range variables {
					indexVariables[key] = experiemntVariables
				}
				experiemntVariables.Variables = append(experiemntVariables.Variables, variables)
			} else {
				experiemntVariables.Variables = append(experiemntVariables.Variables, map[string]interface{}{})
			}
		}

		index[experiment.Name] = experiemntVariables
	}

	c.DataLock.Lock()

	c.Index_ = index
	c.IndexVariables_ = indexVariables
	c.Data_ = *data

	c.SetRefreshTimer()
	c.DataLock.Unlock()
}

func (c Context) LogEvent(event EventType, data interface{}) {
	if c.EventLogger_ != nil {
		c.EventLogger_.HandleEvent(c, event, data)
	}
}

func (c Context) LogError(err error) {
	if c.EventLogger_ != nil {
		c.EventLogger_.HandleEvent(c, Error, err)
	}
}

func (c Context) SetDataFailed(err error) {
	c.DataLock.Lock()
	c.Index_ = map[string]ExperimentVariables{}
	c.IndexVariables_ = map[interface{}]interface{}{}
	c.Data_ = jsonmodels.ContextData{}
	c.Failed_ = true
	c.DataLock.Unlock()
}

func (c Context) Flush(buff [512]byte, block [16]int32, st [4]int32) *future.Future {
	c.ClearTimeout()

	if !c.Failed_ {
		if c.PendingCount_.Load() > 0 {
			var exposures = make([]jsonmodels.Exposure, 0)
			var achievements = make([]jsonmodels.GoalAchievement, 0)
			var eventCount int32

			c.EventLock_.Lock()
			eventCount = c.PendingCount_.Load()

			if eventCount > 0 {
				if len(c.Exposures_) > 0 {
					copy(exposures, c.Exposures_)
					c.Exposures_ = nil
				}

				if len(c.Achievements_) > 0 {
					copy(achievements, c.Achievements_)
					c.Achievements_ = nil
				}

				c.PendingCount_.Store(0)
			}
			c.EventLock_.Unlock()

			if eventCount > 0 {
				var event = jsonmodels.PublishEvent{}
				event.Hashed = true
				event.PublishedAt = c.Clock_.Millis()
				var entrySet []interface{}
				for key, value := range c.Units_ {
					entrySet = append(entrySet, Pair{a: key, b: value})
				}

				var mapper = FlushMapper{Context: c, buff: buff, block: block, st: st}
				var temp = MapSetToArray(entrySet, make([]interface{}, 0), mapper)
				event.Units = make([]jsonmodels.Unit, 0)
				for _, value := range temp {
					event.Units = append(event.Units, value.(jsonmodels.Unit))
				}
				if len(c.Attributes_) == 0 {
					event.Attributes = nil
				} else {
					for key, value := range c.Attributes_ {
						event.Attributes[key] = value.(jsonmodels.Attribute)
					}
				}
				event.Goals = achievements

				result, done := future.New()

				c.EventHandler_.Publish(c, event).Listen(
					func(value future.Value, err error) {
						if err != nil {
							c.LogEvent(Publish, event)
							done(nil, nil)
						} else {
							c.LogError(err)
							done(nil, err)
						}
					})

				return result
			}
		}
	} else {
		c.EventLock_.Lock()
		c.Exposures_ = nil
		c.Achievements_ = nil
		c.PendingCount_.Store(0)
		c.EventLock_.Unlock()
	}
	result, done := future.New()
	done(nil, nil)
	return result
}

type FlushMapper struct {
	MapperInt
	Context Context
	buff    [512]byte
	block   [16]int32
	st      [4]int32
}

func (f FlushMapper) Apply(value interface{}) interface{} {
	var key = value.(Pair).a
	var val = value.(Pair).b
	var uid = Context.GetUnitHash(f.Context, key, val, f.buff, f.block, f.st)
	var dst = make([]byte, len(uid))
	var res = strconv.QuoteToASCII(string(dst))
	return jsonmodels.Unit{Type: value.(Pair).a, Uid: res}
}

func (c Context) ClearTimeout() {
	if c.Timeout_ != nil {
		c.TimeoutLock_.Lock()
		if c.Timeout_ != nil {
			c.Timeout_ <- true
			c.Timeout_ = nil
		}
		c.TimeoutLock_.Unlock()
	}
}

func (c Context) SetTimeout(buff [512]byte, block [16]int32, st [4]int32) {
	if c.IsReady() {
		if c.Timeout_ == nil {
			c.TimeoutLock_.Lock()
			if c.Timeout_ == nil {
				c.Timeout_ = make(chan bool)
				go func() {
					var delay = c.PublishDelay_ * int64(time.Millisecond)
					time.Sleep(time.Duration(delay))
					for {
						select {
						case <-c.Timeout_:
							return
						default:
							c.Flush(buff, block, st)
						}
					}
				}()
			}
			c.TimeoutLock_.Unlock()
		}
	}

}

func (c Context) SetRefreshTimer() {
	if c.RefreshInterval_ > 0 && c.RefreshTimer_ == nil {
		c.RefreshTimer_ = make(chan bool)
		go func() {
			var delay = c.RefreshInterval_ * int64(time.Millisecond)
			for {
				time.Sleep(time.Duration(delay))
				select {
				case <-c.RefreshTimer_:
					return
				default:
					c.RefreshAsync()
				}
			}
		}()
	}
}

func (c Context) GetUnitHash(unitType string, unitUID string, buff [512]byte, block [16]int32, st [4]int32) []byte {
	var computer = ComputerUnitHash{St: st, Block: block, Buff: buff, UnitUID: unitUID}
	var result = ComputeIfAbsentRW(c.ContextLock_, c.HashedUnits_, unitType, computer).([]int8)
	var data = make([]byte, len(result))
	for i, val := range result {
		data[i] = byte(val)
	}
	return data
}

func (c Context) GetVariantAssigner(unitType string, hash []byte) VariantAssigner {
	var computer = ComputerVariantAssigner{Hash: hash}
	return ComputeIfAbsentRW(c.ContextLock_, c.Assigners_, unitType, computer).(VariantAssigner)
}

func (c Context) GetVariableAssignment(key string, buff [512]byte, block [16]int32, st [4]int32, assignBuff [12]int8) (Assignment, error) {
	var experiment, err = c.GetVariableExperiment(key)

	if err == nil {
		return c.GetAssignment(experiment.Data.Name, buff, block, st, assignBuff), nil
	}
	return Assignment{Exposed: &atomic.Bool{}}, err
}

func (c Context) GetVariableExperiment(key string) (ExperimentVariables, error) {
	var result = GetRW(c.DataLock, c.IndexVariables_, key)
	if result == nil {
		return ExperimentVariables{}, errors.New("result is nil")
	} else {
		return result.(ExperimentVariables), nil
	}
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
	Buff    [512]byte
	Block   [16]int32
	St      [4]int32
	UnitUID string
}

func (c ComputerUnitHash) Apply(value interface{}) interface{} {
	return HashUnit(c.UnitUID, c.Buff[:], c.Block[:], c.St[:])
}

type Pair struct {
	a, b string
}
