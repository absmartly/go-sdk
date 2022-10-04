package sdk

import (
	"context"
	"errors"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"reflect"
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
	Failed_          *atomic.Value
	Ready_           *atomic.Value
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
	PendingCount_    *atomic.Value
	Closing_         *atomic.Value
	Closed_          *atomic.Value
	Refreshing_      *atomic.Value
	ReadyFuture_     *future.Future
	ClosingFuture_   *future.Future
	RefreshFuture_   *future.Future
	TimeoutLock_     *sync.Mutex
	Timeout_         *time.Ticker
	RefreshTimer_    *time.Ticker
	RefreshDone_     chan bool
	TimeoutDone_     chan bool
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
	Exposed          *atomic.Value
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

	cntx.AssignmentCache = map[string]Assignment{}
	cntx.Achievements_ = make([]jsonmodels.GoalAchievement, 0)
	cntx.Exposures_ = make([]jsonmodels.Exposure, 0)
	cntx.Attributes_ = make([]interface{}, 0)

	var units = config.Units_
	if units != nil {
		var _ = cntx.SetUnits(units)
	}

	cntx.Assigners_ = map[interface{}]interface{}{}
	cntx.HashedUnits_ = map[interface{}]interface{}{}

	var attributes = config.Attributes_
	if attributes != nil {
		var _ = cntx.SetAttributes(attributes)
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
				tmp.SetData(result)
				tmp.LogEvent(Ready, result)
			} else {
				tmp.SetDataFailed(err)
				tmp.LogError(err)
			}

		})
	} else {
		var tmp = &cntx
		var tempFuture, readyFutureDone = future.New()
		cntx.ReadyFuture_ = tempFuture
		dataFuture.Listen(func(val future.Value, err error) {
			if err == nil {
				var result = val.(jsonmodels.ContextData)
				tmp.SetData(result)
				readyFutureDone(result, nil)
				cntx.ReadyFuture_ = nil
				tmp.LogEvent(Ready, result)

				if tmp.GetPendingCount() > 0 {
					tmp.SetTimeout()
				}
			} else {
				tmp.SetDataFailed(err)
				readyFutureDone(nil, err)
				cntx.ReadyFuture_ = nil
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
	} else {
		c.ReadyFuture_.Listen(func(val future.Value, err error) {
			c.ReadyFuture_.SetResult(val, err)
		})
		return c.ReadyFuture_
	}
}

func (c *Context) WaitUntilReady() Context {
	if !c.Ready_.Load().(bool) {
		var ft = c.ReadyFuture_
		if ft != nil && !ft.Ready() {
			ft.Join(context.Background())
		}
	}
	return *c
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
		return errors.New("unit already set")
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

	c.Attributes_ = AddRW(c.ContextLock_, c.Attributes_, jsonmodels.Attribute{Name: name, Value: value, SetAt: c.Clock_.Millis()}).([]interface{})
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
	var res = assignment.Exposed.Load().(bool)
	if !res {
		assignment.Exposed.Store(true)
	}
	if !res {
		var exposure = jsonmodels.Exposure{Id: assignment.Id, Name: assignment.Name, Unit: assignment.UnitType,
			Variant: assignment.Variant, ExposedAt: c.Clock_.Millis(), Assigned: assignment.Assigned,
			Eligible: assignment.Eligible, Overridden: assignment.Overridden, FullOn: assignment.FullOn,
			Custom: assignment.Custom, AudienceMismatch: assignment.AudienceMismatch}

		c.EventLock_.Lock()
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

func (c *Context) GetVariableKeys() (map[string]string, error) {
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

func (c *Context) GetVariableValue(key string, defaultValue interface{}) (interface{}, error) {
	var err = c.CheckReady(true)
	if err != nil {
		return nil, err
	}

	var assignment, errres = c.GetVariableAssignment(key)
	if errres == nil {
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
	c.PendingCount_.Store(c.PendingCount_.Load().(int32) + 1)
	c.Achievements_ = append(c.Achievements_, achievement)
	c.EventLock_.Unlock()

	c.LogEvent(Goal, achievement)

	c.SetTimeout()
	return nil
}

func (c *Context) PublishAsync() (*future.Future, error) {
	var err = c.CheckNotClosed()
	if err != nil {
		return nil, err
	}

	return c.Flush(), nil
}

func (c *Context) Publish() error {
	var result, err = c.PublishAsync()
	if err == nil {
		result.Join(context.Background())
		return nil
	} else {
		return err
	}
}

func (c *Context) GetPendingCount() int32 {
	return c.PendingCount_.Load().(int32)
}

func (c *Context) RefreshAsync() *future.Future {

	var err = c.CheckNotClosed()
	if err != nil {
		return nil
	}

	var res = c.Refreshing_.Load().(bool)
	if !res {
		c.Refreshing_.Store(true)
	}
	if !res {
		var tempfuture, donefun = future.New()
		c.RefreshFuture_ = tempfuture

		c.DataProvider_.GetContextData().Listen(func(value future.Value, err error) {
			if err == nil {
				var result = value.(jsonmodels.ContextData)
				c.SetData(result)
				c.Refreshing_.Store(false)
				donefun(nil, nil)

				c.LogEvent(Refresh, result)
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

func (c *Context) Refresh() {
	c.RefreshAsync().Join(context.Background())
}

func (c *Context) CloseAsync() (*future.Future, error) {
	if !c.Closed_.Load().(bool) {
		var res = c.Closing_.Load().(bool)
		if !res {
			c.Closing_.Store(true)
		}
		if !res {
			c.ClearRefreshTimer()

			if c.PendingCount_.Load().(int32) > 0 {
				var tempFuture, done = future.New()
				c.ClosingFuture_ = tempFuture
				c.Flush().Listen(func(value future.Value, err error) {
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

func (c *Context) Close() {
	var fut, err = c.CloseAsync()
	if err == nil {
		fut.Join(context.Background())
	}
}

func (c *Context) GetAssignment(experimentName string) *Assignment {

	c.ContextLock_.RLock()
	if assignment, found := c.AssignmentCache[experimentName]; found {
		var custom, cfound = c.Cassignments_[experimentName]
		var override, ofound = c.Overrides_[experimentName]
		var experiment, efound = c.GetExperiment(experimentName)

		if ofound {
			if assignment.Overridden && assignment.Variant == override.(int) {
				c.ContextLock_.RUnlock()
				return &assignment
			}
		} else if !efound {
			if !assignment.Assigned {
				c.ContextLock_.RUnlock()
				return &assignment
			}
		} else if !cfound || custom.(int) == assignment.Variant {
			if c.ExperimentMatches(experiment.Data, assignment) {
				c.ContextLock_.RUnlock()
				return &assignment
			}
		}
	}
	c.ContextLock_.RUnlock()
	// cache miss or out-dated
	c.ContextLock_.Lock()

	var custom, cfound = c.Cassignments_[experimentName]
	var override, ofound = c.Overrides_[experimentName]
	var experiment, efound = c.GetExperiment(experimentName)

	var exposed = &atomic.Value{}
	exposed.Store(false)
	var assignment = Assignment{Exposed: exposed}
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
		}
	}

	if efound && (assignment.Variant < len(experiment.Data.Variants)) {
		assignment.Variables = experiment.Variables[assignment.Variant]
	}

	c.AssignmentCache[experimentName] = assignment
	c.ContextLock_.Unlock()
	return &assignment
}

func (c *Context) ClearRefreshTimer() {
	if c.RefreshTimer_ != nil {
		c.RefreshTimer_.Stop()
		c.RefreshDone_ <- true
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

func (c *Context) CheckNotClosed() error {
	if c.Closed_.Load().(bool) {
		return errors.New("ABSmartly Context is closed")
	} else if c.Closing_.Load().(bool) {
		return errors.New("ABSmartly Context is closing")
	}
	return nil
}

func (c *Context) CheckReady(expectNotClosed bool) error {
	if !c.IsReady() {
		return errors.New("ABSmartly Context is not yet ready")
	} else if expectNotClosed {
		return c.CheckNotClosed()
	}
	return nil
}

func (c *Context) SetData(data jsonmodels.ContextData) {
	var index = map[string]ExperimentVariables{}
	var indexVariables = map[interface{}]interface{}{}

	for _, experiment := range data.Experiments {
		var experiemntVariables = ExperimentVariables{}
		experiemntVariables.Data = experiment
		experiemntVariables.Variables = make([]map[string]interface{}, 0)

		for _, variant := range experiment.Variants {
			if len(variant.Config) > 0 {
				var variables = c.VariableParser_.Parse(*c, experiment.Name, variant.Name, variant.Config)
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
	c.Data_ = data
	c.Ready_.Store(true)

	c.SetRefreshTimer()
	c.DataLock.Unlock()
}

func (c *Context) LogEvent(event EventType, data interface{}) {
	if c.EventLogger_ != nil {
		c.EventLogger_.HandleEvent(*c, event, data)
	}
}

func (c *Context) LogError(err error) {
	if c.EventLogger_ != nil {
		c.EventLogger_.HandleEvent(*c, Error, err)
	}
}

func (c *Context) SetDataFailed(err error) {
	c.DataLock.Lock()
	c.Index_ = map[string]ExperimentVariables{}
	c.IndexVariables_ = map[interface{}]interface{}{}
	c.Data_ = jsonmodels.ContextData{}
	c.Ready_.Store(true)
	c.Failed_.Store(true)
	c.DataLock.Unlock()
}

func (c *Context) Flush() *future.Future {
	c.ClearTimeout()

	if !c.Failed_.Load().(bool) {
		if c.PendingCount_.Load().(int32) > 0 {
			var eventCount int32

			c.EventLock_.Lock()
			eventCount = c.PendingCount_.Load().(int32)
			var exposures = make([]jsonmodels.Exposure, len(c.Exposures_))
			var achievements = make([]jsonmodels.GoalAchievement, len(c.Achievements_))

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

				var mapper = FlushMapper{Context: *c}
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
				event.Goals = achievements
				event.Exposures = exposures

				result, done := future.New()

				c.EventHandler_.Publish(*c, event).Listen(
					func(value future.Value, err error) {
						if err == nil {
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
		c.PendingCount_.Store(int32(0))
		c.EventLock_.Unlock()
	}

	result, done := future.New()
	done(nil, nil)
	return result
}

type FlushMapper struct {
	MapperInt
	Context Context
}

func (f FlushMapper) Apply(value interface{}) interface{} {
	var key = value.(Pair).a
	var val = value.(Pair).b
	var cntx = &f.Context
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
	if c.Timeout_ != nil {
		c.TimeoutLock_.Lock()
		if c.Timeout_ != nil {
			c.Timeout_.Stop()
			c.TimeoutDone_ <- true
			c.Timeout_ = nil
		}
		c.TimeoutLock_.Unlock()
	}
}

func (c *Context) SetTimeout() {
	if c.IsReady() {
		if c.Timeout_ == nil {
			c.TimeoutLock_.Lock()
			if c.Timeout_ == nil {
				var delay = uint64(c.PublishDelay_ * int64(time.Millisecond))
				c.Timeout_ = time.NewTicker(time.Duration(delay))
				c.TimeoutDone_ = make(chan bool)
				go func() {
					for {
						select {
						case <-c.TimeoutDone_:
							return
						case <-c.Timeout_.C:
							c.Flush()
							c.Timeout_.Stop()
							c.TimeoutDone_ <- true
						}
					}
				}()
			}
			c.TimeoutLock_.Unlock()
		}
	}

}

func (c *Context) SetRefreshTimer() {
	if c.RefreshInterval_ > 0 && c.RefreshTimer_ == nil {
		var rate = time.Duration(uint64(c.RefreshInterval_ * int64(time.Millisecond)))
		c.RefreshTimer_ = time.NewTicker(rate)
		c.RefreshDone_ = make(chan bool)
		go func() {
			for {
				select {
				case <-c.RefreshDone_:
					return
				case <-c.RefreshTimer_.C:
					c.RefreshAsync()
				}
			}
		}()
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
	UnitUID string
}

func (c ComputerUnitHash) Apply(value interface{}) interface{} {
	return HashUnit(c.UnitUID)
}

type Pair struct {
	a, b string
}
