package sdk

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/absmartly/go-sdk/pkg/absmartly"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type Context struct {
	uc *absmartly.UnitContext

	PublishDelay_    int64
	RefreshInterval_ int64
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

func (c *Context) IsReady() bool { return true }

func (c *Context) IsFailed() bool { return false }

func (c *Context) IsClosed() bool { return false }

func (c *Context) IsClosing() bool { return false }

func (c *Context) WaitUntilReady() Context {
	return *c
}

func (c *Context) GetExperiments() ([]string, error) {
	return make([]string, 0), nil
}

func (c *Context) GetData() (jsonmodels.ContextData, error) {
	return jsonmodels.ContextData{}, errors.New("data is not a public SDK API")
}

func (c *Context) SetOverride(experimentName string, variant int) error {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) GetOverride(experimentName string) (int, error) {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) SetOverrides(overrides map[string]int) error {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) SetCustomAssignment(experimentName string, variant int) error {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) GetCustomAssignment(experimentName string) (int, error) {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) SetCustomAssignments(customAssignments map[string]int) error {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) SetUnit(unitType string, uid string) error {
	return errors.New("provide unit in UnitConfig or switch to v2 SDK")
}

func (c *Context) SetUnits(units map[string]string) error {
	return errors.New("provide unit in UnitConfig or switch to v2 SDK")
}

func (c *Context) SetAttribute(name string, value interface{}) error {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) SetAttributes(attributes map[string]interface{}) error {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) GetTreatment(experimentName string) (int, error) {
	return c.uc.GetTreatment(experimentName)
}

func (c *Context) QueueExposure(assignment absmartly.Assignment) {
	c.uc.QueueExposure(assignment)
}

func (c *Context) PeekTreatment(experimentName string) (int, error) {
	variant, _, err := c.uc.PeekTreatment(experimentName)
	return variant, err
}

func (c *Context) GetVariableKeys() (map[string]string, error) {
	panic("not yet implemented in v2 SDK")

}

func (c *Context) GetVariableValue(key string, defaultValue interface{}) (interface{}, error) {
	panic("not yet implemented in v2 SDK")

}

func (c *Context) PeekVariableValue(key string, defaultValue interface{}) (interface{}, error) {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) Track(goalName string, properties map[string]interface{}) error {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) Publish() error {
	return c.uc.Flush(context.Background())
}

func (c *Context) GetPendingCount() int32 { return 0 }

func (c *Context) Refresh() {}

func (c *Context) Close() {}

func (c *Context) GetAssignment(experimentName string) absmartly.Assignment {
	a, _ := c.uc.GetAssignment(experimentName)
	return a
}

func (c *Context) ClearRefreshTimer() {}

func (c *Context) CheckNotClosed() error { return nil }

func (c *Context) CheckReady(expectNotClosed bool) error { return nil }

func (c *Context) Flush() int {
	c.uc.Flush(context.Background())
	return 0
}

func (c *Context) ClearTimeout() {}

func (c *Context) SetTimeout() {}

func (c *Context) SetRefreshTimer() {}

func (c *Context) GetVariableAssignment(key string) (*Assignment, error) {
	panic("not yet implemented in v2 SDK")
}

func (c *Context) GetVariableExperiment(key string) (ExperimentVariables, error) {
	panic("not yet implemented in v2 SDK")
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
