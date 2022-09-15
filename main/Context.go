package main

import (
	"github.com/absmartly/go-sdk/main/future"
	"github.com/absmartly/go-sdk/main/jsonmodels"
	"sync"
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
	Data_            *jsonmodels.ContextData
	Index_           map[string]ExperimentVariables
	IndexVariables_  map[string]ExperimentVariables
	ContextLock_     *sync.RWMutex
	HashedUnits_     map[string][]byte
	Assigners_       map[string]VariantAssigner
	AssignmentCache  map[string]Assignment
	EventLock_       *sync.Locker
	Exposures_       []jsonmodels.Exposure
	Achievements_    []jsonmodels.GoalAchievement
	Attributes_      []jsonmodels.Attribute
	Overrides_       map[string]int
	Cassignments_    map[string]int
	PendingCount_    int
	Closing_         bool
	CLosed_          bool
	Refreshing_      bool
	ReadyFuture_     *future.Future
	ClosingFuture_   *future.Future
	RefreshFuture_   *future.Future
	TimeoutLock_     *sync.Locker
	Timeout_         chan bool
	RefreshTimer_    chan bool
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
	Exposed          bool
}

func CreateContext(config ContextConfig, dataFuture *future.Future, dataProvider ContextDataProvider,
	eventHandler ContextEventHandler, eventLogger ContextEventLogger, variableParser VariableParser,
	audienceMatcher AudienceMatcher) Context {
	var cntx = Context{PublishDelay_: config.PublishDelay_, RefreshInterval_: config.RefreshInterval_,
		EventHandler_: eventHandler, DataProvider_: dataProvider, VariableParser_: variableParser,
		AudienceMatcher_: audienceMatcher, Units_: map[string]string{}}

	if config.EventLogger_ != nil {
		cntx.EventLogger_ = config.EventLogger_
	} else {
		cntx.EventLogger_ = eventLogger
	}

	var units = config.Units_
	if units != nil {
		cntx.SetUnits(units)
	}

	cntx.Assigners_ = map[string]VariantAssigner{}
	cntx.HashedUnits_ = map[string][]byte{}

	var attributes = config.Attributes_
	if attributes != nil {
		cntx.SetAttributes(attributes)
	}

	var overrides = config.Overrides_
	if overrides != nil {
		for key, value := range overrides {
			cntx.Overrides_[key] = value
		}
	} else {
		cntx.Overrides_ = map[string]int{}
	}

	var cassignments = config.Cassigmnents_
	if cassignments != nil {
		for key, value := range cassignments {
			cntx.Cassignments_[key] = value
		}
	} else {
		cntx.Cassignments_ = map[string]int{}
	}

	if dataFuture.Ready() {
		dataFuture.Listen(func(val future.Value, err error) {
			if err == nil {
				var result = val.(*jsonmodels.ContextData)
				cntx.setData(result)
				cntx.logEvent(Ready, result)
			} else {
				cntx.setDataFailed(err)
				cntx.logError(err)
			}

		})
	} else {
		cntx.ReadyFuture_ = dataFuture
		cntx.ReadyFuture_.Listen(func(val future.Value, err error) {
			if err == nil {
				var result = val.(*jsonmodels.ContextData)
				cntx.setData(result)
				cntx.ReadyFuture_.SetResult(nil, nil)
				cntx.logEvent(Ready, result)

				if cntx.GetPendingCount() > 0 {
					cntx.setTimeout()
				}
			} else {
				cntx.setDataFailed(err)
				cntx.ReadyFuture_.SetResult(nil, err)
				cntx.logError(err)
			}

		})
	}

	return cntx
}

func (c Context) SetUnits(units map[string]string) {
	for k, v := range units {
		c.Units_[k] = v
	}
}

func (c Context) SetAttributes(attributes map[string]interface{}) {

}

func (c Context) setData(result *jsonmodels.ContextData) {

}

func (c Context) logEvent(ready EventType, result *jsonmodels.ContextData) {

}

func (c Context) setDataFailed(err error) {

}

func (c Context) logError(err error) {

}

func (c Context) GetPendingCount() int {
	return 0
}

func (c Context) setTimeout() {

}
