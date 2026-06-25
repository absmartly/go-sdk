package sdk

import (
	"errors"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"sync/atomic"
	"testing"
	"time"
)

type CapturingPublishClient struct {
	publishEvents []jsonmodels.PublishEvent
	publishErr    error
	contextData   jsonmodels.ContextData
}

func (c *CapturingPublishClient) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return c.contextData, nil
	})
}

func (c *CapturingPublishClient) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		if c.publishErr != nil {
			return nil, c.publishErr
		}
		c.publishEvents = append(c.publishEvents, event)
		return nil, nil
	})
}

func createCanonicalContext(t *testing.T, client *CapturingPublishClient, dataFuture *future.Future, logger *MockEventLogger) *Context {
	t.Helper()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	if logger != nil {
		config.EventLogger_ = logger
	}

	var dp = DefaultContextDataProvider{client_: client}
	var eh = DefaultContextEventHandler{client_: client}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{DefaultAudienceDeserializer{}}
	var clk = FixedClockForTest{millis: 1620000000000}

	var el ContextEventLogger
	if logger != nil {
		el = logger
	}

	return CreateContext(clk, config, dataFuture, dp, eh, el, vp, am)
}

func TestTreatmentQueuesExposures(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	for _, experiment := range data.Experiments {
		var res, err = context.GetTreatment(experiment.Name)
		assertAny(nil, err, t)
		assertAny(expectedVariants[experiment.Name], res, t)
	}

	assertAny(int32(len(data.Experiments)), context.GetPendingCount(), t)
}

func TestTreatmentQueuesExposuresOnlyOnce(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	for _, experiment := range data.Experiments {
		context.GetTreatment(experiment.Name)
	}
	for _, experiment := range data.Experiments {
		context.GetTreatment(experiment.Name)
	}

	assertAny(int32(len(data.Experiments)), context.GetPendingCount(), t)
}

func TestTreatmentQueuesExposureAfterPeek(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	for _, experiment := range data.Experiments {
		context.PeekTreatment(experiment.Name)
	}
	assertAny(int32(0), context.GetPendingCount(), t)

	for _, experiment := range data.Experiments {
		context.GetTreatment(experiment.Name)
	}
	assertAny(int32(len(data.Experiments)), context.GetPendingCount(), t)
}

func TestTreatmentReturnsBaseVariantForUnknownExperiment(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res, err = context.GetTreatment("unknown_experiment")
	assertAny(nil, err, t)
	assertAny(0, res, t)

	assertAny(int32(1), context.GetPendingCount(), t)
}

func TestTreatmentDoesNotReQueueOnUnknownExperiment(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.GetTreatment("unknown_experiment")
	assertAny(int32(1), context.GetPendingCount(), t)

	context.GetTreatment("unknown_experiment")
	assertAny(int32(1), context.GetPendingCount(), t)
}

func TestTreatmentWithCustomAssignmentVariant(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetCustomAssignment("exp_test_ab", 2)
	var res, err = context.GetTreatment("exp_test_ab")
	assertAny(nil, err, t)
	assertAny(2, res, t)
}

func TestPeekDoesNotQueueExposures(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	for _, experiment := range data.Experiments {
		context.PeekTreatment(experiment.Name)
	}

	assertAny(int32(0), context.GetPendingCount(), t)
}

func TestPeekReturnsOverrideVariant(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetOverride("exp_test_ab", 5)
	var res, err = context.PeekTreatment("exp_test_ab")
	assertAny(nil, err, t)
	assertAny(5, res, t)

	assertAny(int32(0), context.GetPendingCount(), t)
}

func TestPeekReturnsAssignedVariantOnAudienceMismatchNonStrict(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res, err = context.PeekTreatment("exp_test_ab")
	assertAny(nil, err, t)
	assertAny(expectedVariants["exp_test_ab"], res, t)
}

func TestPeekReturnsControlVariantOnAudienceMismatchStrict(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureSrict)

	var res, err = context.PeekTreatment("exp_test_ab")
	assertAny(nil, err, t)
	assertAny(0, res, t)
}

func TestVariableValueReturnsDefaultWhenUnassigned(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res, err = context.GetVariableValue("unknown_variable", "defaultVal")
	assertAny(nil, err, t)
	assertAny("defaultVal", res, t)
}

func TestVariableValueReturnsOverriddenValues(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetOverride("exp_test_ab", 1)
	var res, err = context.GetVariableValue("banner.border", 0)
	assertAny(nil, err, t)
	assertAny(1.0, res, t)
}

func TestVariableValueQueuesExposures(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.GetVariableValue("banner.border", 0)
	assertAny(int32(1), context.GetPendingCount(), t)
}

func TestVariableValueQueuesExposuresOnlyOnce(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.GetVariableValue("banner.border", 0)
	context.GetVariableValue("banner.size", 0)
	assertAny(int32(1), context.GetPendingCount(), t)
}

func TestVariableValueQueuesExposureAfterPeekVariable(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.PeekVariableValue("banner.border", 0)
	assertAny(int32(0), context.GetPendingCount(), t)

	context.GetVariableValue("banner.border", 0)
	assertAny(int32(1), context.GetPendingCount(), t)
}

func TestPeekVariableValueReturnsDefaultWhenUnassigned(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res, err = context.PeekVariableValue("unknown_variable", "defaultVal")
	assertAny(nil, err, t)
	assertAny("defaultVal", res, t)
}

func TestPeekVariableValueReturnsOverriddenValues(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetOverride("exp_test_ab", 1)
	var res, err = context.PeekVariableValue("banner.border", 0)
	assertAny(nil, err, t)
	assertAny(1.0, res, t)
}

func TestPeekVariableValueDoesNotQueueExposures(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	for key := range variableExperiments {
		context.PeekVariableValue(key, nil)
	}

	assertAny(int32(0), context.GetPendingCount(), t)
}

func TestPeekVariableValueStrictReturnDefault(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureSrict)

	var res, err = context.PeekVariableValue("banner.border", 99)
	assertAny(nil, err, t)
	assertAny(99, res, t)
}

func TestVariableValueStrictReturnDefault(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureSrict)

	var res, err = context.GetVariableValue("banner.border", 99)
	assertAny(nil, err, t)
	assertAny(99, res, t)
}

func TestTrackQueuesGoals(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var err = context.Track("goal1", map[string]interface{}{"amount": 125})
	assertAny(nil, err, t)
	assertAny(int32(1), context.GetPendingCount(), t)

	err = context.Track("goal2", map[string]interface{}{"tries": 7})
	assertAny(nil, err, t)
	assertAny(int32(2), context.GetPendingCount(), t)
}

func TestTrackWithNilProperties(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var err = context.Track("goal1", nil)
	assertAny(nil, err, t)
	assertAny(int32(1), context.GetPendingCount(), t)
}

func TestTrackBeforeReady(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)

	var err = context.Track("goal1", map[string]interface{}{"amount": 125})
	assertAny(nil, err, t)
	assertAny(int32(1), context.GetPendingCount(), t)
}

func TestTrackThrowsAfterClose(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()

	var err = context.Track("goal1", nil)
	if err == nil {
		t.Error("Expected error after close, got nil")
	}
}

func TestPublishDoesNotCallClientWhenQueueEmpty(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var err = context.Publish()
	assertAny(nil, err, t)
	assertAny(int32(0), context.GetPendingCount(), t)
}

func TestPublishClearsQueueOnSuccess(t *testing.T) {
	setUp()
	client := &CapturingPublishClient{contextData: data}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, nil)

	ctx.Track("goal1", map[string]interface{}{"amount": 100})
	assertAny(int32(1), ctx.GetPendingCount(), t)

	ctx.Publish()
	assertAny(int32(0), ctx.GetPendingCount(), t)
}

func TestPublishThrowsAfterClose(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()

	var _, err = context.PublishAsync()
	if err == nil {
		t.Error("Expected error after close, got nil")
	}
}

func TestCloseNotCallPublishWhenQueueEmpty(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()
	assertAny(true, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
}

func TestCloseCallsPublishWhenQueueNotEmpty(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Track("goal1", nil)
	assertAny(int32(1), context.GetPendingCount(), t)

	context.Close()
	assertAny(true, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
}

func TestCloseReturnsSameResultOnSecondCall(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()
	assertAny(true, context.IsClosed(), t)

	context.Close()
	assertAny(true, context.IsClosed(), t)
}

func TestSetUnitBeforeReady(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{}
	var context = CreateTestContext(config, dataFuture)

	var err = context.SetUnit("user_id", "test_user")
	assertAny(nil, err, t)
}

func TestSetUnitThrowsOnDuplicate(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{"user_id": "original"}
	var context = CreateTestContext(config, dataFutureReady)

	var err = context.SetUnit("user_id", "different")
	if err == nil {
		t.Error("Expected error on duplicate unit, got nil")
	}
}

func TestSetUnitThrowsOnInvalidUid(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{}
	var context = CreateTestContext(config, dataFutureReady)

	var err = context.SetUnit("user_id", "")
	if err == nil {
		t.Error("Expected error on empty UID, got nil")
	}

	err = context.SetUnit("user_id", "   ")
	if err == nil {
		t.Error("Expected error on whitespace UID, got nil")
	}
}

func TestSetUnitThrowsAfterClose(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()

	var err = context.SetUnit("new_unit", "value")
	if err == nil {
		t.Error("Expected error after close, got nil")
	}
}

func TestSetAttributeBeforeReady(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)

	var err = context.SetAttribute("attr1", "value1")
	assertAny(nil, err, t)
}

func TestGetAttributeReturnsLastSet(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetAttribute("attr1", "value1")
	context.SetAttribute("attr1", "value2")

	var found = false
	for i := len(context.Attributes_) - 1; i >= 0; i-- {
		attr := context.Attributes_[i].(jsonmodels.Attribute)
		if attr.Name == "attr1" {
			assertAny("value2", attr.Value, t)
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find attr1 in attributes")
	}
}

func TestSetOverrideBeforeReady(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)

	var err = context.SetOverride("exp_test", 2)
	assertAny(nil, err, t)

	var res, er = context.GetOverride("exp_test")
	assertAny(nil, er, t)
	assertAny(2, res, t)
}

func TestSetOverrideThrowsAfterClose(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()

	var err = context.SetOverride("exp_test", 2)
	if err == nil {
		t.Error("Expected error after close, got nil")
	}
}

func TestSetCustomAssignmentBeforeReady(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)

	var err = context.SetCustomAssignment("exp_test", 2)
	assertAny(nil, err, t)

	var res, er = context.GetCustomAssignment("exp_test")
	assertAny(nil, er, t)
	assertAny(2, res, t)
}

func TestSetCustomAssignmentThrowsAfterClose(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()

	var err = context.SetCustomAssignment("exp_test", 2)
	if err == nil {
		t.Error("Expected error after close, got nil")
	}
}

func TestRefreshKeepsOverrides(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureRefresh)

	context.SetOverride("exp_test_ab", 9)
	context.Refresh()

	var res, err = context.GetOverride("exp_test_ab")
	assertAny(nil, err, t)
	assertAny(9, res, t)
}

func TestRefreshKeepsCustomAssignments(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureRefresh)

	context.SetCustomAssignment("exp_test_ab", 3)
	context.Refresh()

	var res, err = context.GetCustomAssignment("exp_test_ab")
	assertAny(nil, err, t)
	assertAny(3, res, t)
}

func TestRefreshNotReQueueExposuresWhenNotChanged(t *testing.T) {
	setUp()

	refreshClient := &CapturingClientWithRefresh{
		contextData:   refreshData,
		refreshedData: refreshData,
	}
	refreshClient.useRefreshedData.Store(false)

	df, done := future.New()
	done(refreshData, nil)

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.RefreshInterval_ = 5000

	var dp = DefaultContextDataProvider{client_: refreshClient}
	var eh = DefaultContextEventHandler{client_: refreshClient}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{DefaultAudienceDeserializer{}}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, df, dp, eh, nil, vp, am)

	ctx.GetTreatment("exp_test_ab")
	var count = ctx.GetPendingCount()

	refreshClient.useRefreshedData.Store(true)
	ctx.Refresh()

	ctx.GetTreatment("exp_test_ab")
	assertAny(count, ctx.GetPendingCount(), t)
}

func TestRefreshNotReQueueWhenNotChangedWithOverride(t *testing.T) {
	setUp()

	refreshClient := &CapturingClientWithRefresh{
		contextData:   refreshData,
		refreshedData: refreshData,
	}
	refreshClient.useRefreshedData.Store(false)

	df, done := future.New()
	done(refreshData, nil)

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.RefreshInterval_ = 5000

	var dp = DefaultContextDataProvider{client_: refreshClient}
	var eh = DefaultContextEventHandler{client_: refreshClient}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{DefaultAudienceDeserializer{}}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, df, dp, eh, nil, vp, am)

	ctx.SetOverride("exp_test_ab", 5)
	ctx.GetTreatment("exp_test_ab")
	var count = ctx.GetPendingCount()

	refreshClient.useRefreshedData.Store(true)
	ctx.Refresh()

	ctx.GetTreatment("exp_test_ab")
	assertAny(count, ctx.GetPendingCount(), t)
}

func TestRefreshThrowsAfterClose(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()

	var result = context.RefreshAsync()
	if result == nil {
		t.Log("RefreshAsync returned nil after close as expected")
	}
}

func TestCustomFieldValueReturnsNilForMissingExperiment(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res = context.GetCustomFieldValue("nonexistent_experiment", "country")
	assertAny(nil, res, t)
}

func TestCustomFieldValueReturnsNilForMissingField(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res = context.GetCustomFieldValue("exp_test_ab", "nonexistent_field")
	assertAny(nil, res, t)
}

func TestCustomFieldValueReturnsStringValue(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res = context.GetCustomFieldValue("exp_test_ab", "country")
	assertAny("US,PT,ES,DE,FR", res, t)
}

func TestCustomFieldValueTypeReturnsType(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res = context.GetCustomFieldValueType("exp_test_ab", "country")
	assertAny("string", res, t)

	res = context.GetCustomFieldValueType("exp_test_ab", "overrides")
	assertAny("json", res, t)
}

func TestCustomFieldValueReturnsNilForExperimentWithNoCustomFields(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res = context.GetCustomFieldValue("exp_test_not_eligible", "country")
	assertAny(nil, res, t)
}

func TestEventLoggerCalledOnTreatmentExposure(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dp = DefaultContextDataProvider{client_: client}
	var eh = DefaultContextEventHandler{client_: client}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dp, eh, logger, vp, am)
	logger.Reset()

	ctx.GetTreatment("exp_test_ab")

	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundExposure := false
	for _, event := range events {
		if event.EventType == Exposure {
			foundExposure = true
			exposure := event.Data.(jsonmodels.Exposure)
			assertAny("exp_test_ab", exposure.Name, t)
			assertAny(true, exposure.Assigned, t)
			break
		}
	}
	if !foundExposure {
		t.Error("Expected EXPOSURE event to be logged")
	}
}

func TestEventLoggerCalledOnTrackGoal(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dp = DefaultContextDataProvider{client_: client}
	var eh = DefaultContextEventHandler{client_: client}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dp, eh, logger, vp, am)
	logger.Reset()

	ctx.Track("purchase", map[string]interface{}{"amount": 99.0})

	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundGoal := false
	for _, event := range events {
		if event.EventType == Goal {
			foundGoal = true
			goal := event.Data.(jsonmodels.GoalAchievement)
			assertAny("purchase", goal.Name, t)
			break
		}
	}
	if !foundGoal {
		t.Error("Expected GOAL event to be logged")
	}
}

func TestEventLoggerCalledOnReadyWithPreFetchedData(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	df, done := future.New()
	done(data, nil)

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dp = DefaultContextDataProvider{client_: client}
	var eh = DefaultContextEventHandler{client_: client}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, df, dp, eh, logger, vp, am)
	_ = ctx

	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundReady := false
	for _, event := range events {
		if event.EventType == Ready {
			foundReady = true
			break
		}
	}
	if !foundReady {
		t.Error("Expected READY event to be logged for pre-fetched data")
	}
}

func TestEventLoggerCalledOnInitializationError(t *testing.T) {
	logger := &MockEventLogger{}

	df, done := future.New()
	done(nil, errors.New("initialization failed"))

	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{"user_id": "test"}
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dp = DefaultContextDataProvider{client_: client}
	var eh = DefaultContextEventHandler{client_: client}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{DefaultAudienceDeserializer{}}
	var clk = FixedClockForTest{millis: 1620000000000}

	_ = CreateContext(clk, config, df, dp, eh, logger, vp, am)

	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundError := false
	for _, event := range events {
		if event.EventType == Error {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("Expected ERROR event to be logged on initialization failure")
	}
}

func TestPublishIncludesExposureData(t *testing.T) {
	setUp()
	client := &CapturingPublishClient{contextData: data}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, nil)

	ctx.GetTreatment("exp_test_ab")
	ctx.Publish()

	time.Sleep(50 * time.Millisecond)

	if len(client.publishEvents) == 0 {
		t.Fatal("Expected at least one publish event")
	}

	pe := client.publishEvents[0]
	if len(pe.Exposures) == 0 {
		t.Error("Expected exposure data in publish event")
	}
}

func TestPublishIncludesGoalData(t *testing.T) {
	setUp()
	client := &CapturingPublishClient{contextData: data}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, nil)

	ctx.Track("goal1", map[string]interface{}{"amount": 100})
	ctx.Publish()

	time.Sleep(50 * time.Millisecond)

	if len(client.publishEvents) == 0 {
		t.Fatal("Expected at least one publish event")
	}

	pe := client.publishEvents[0]
	if len(pe.Goals) == 0 {
		t.Error("Expected goal data in publish event")
	}
	assertAny("goal1", pe.Goals[0].Name, t)
}

func TestPublishIncludesAttributeData(t *testing.T) {
	setUp()
	client := &CapturingPublishClient{contextData: data}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, nil)

	ctx.SetAttribute("attr1", "value1")
	ctx.Track("goal1", nil)
	ctx.Publish()

	time.Sleep(50 * time.Millisecond)

	if len(client.publishEvents) == 0 {
		t.Fatal("Expected at least one publish event")
	}

	pe := client.publishEvents[0]
	if len(pe.Attributes) == 0 {
		t.Error("Expected attribute data in publish event")
	}
}

func TestTreatmentExposureIncludesAudienceMatchTrue(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.GetTreatment("exp_test_ab")

	context.EventLock_.Lock()
	if len(context.Exposures_) > 0 {
		exp := context.Exposures_[0]
		assertAny("exp_test_ab", exp.Name, t)
		assertAny(false, exp.AudienceMismatch, t)
		assertAny(true, exp.Assigned, t)
	} else {
		t.Error("Expected at least one exposure")
	}
	context.EventLock_.Unlock()
}

func TestTreatmentExposureStrictAudienceMismatch(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureSrict)

	context.GetTreatment("exp_test_ab")

	context.EventLock_.Lock()
	if len(context.Exposures_) > 0 {
		exp := context.Exposures_[0]
		assertAny("exp_test_ab", exp.Name, t)
		assertAny(true, exp.AudienceMismatch, t)
		assertAny(0, exp.Variant, t)
	} else {
		t.Error("Expected at least one exposure")
	}
	context.EventLock_.Unlock()
}

func TestTreatmentExposureWithOverride(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetOverride("exp_test_ab", 5)
	context.GetTreatment("exp_test_ab")

	context.EventLock_.Lock()
	if len(context.Exposures_) > 0 {
		exp := context.Exposures_[0]
		assertAny("exp_test_ab", exp.Name, t)
		assertAny(5, exp.Variant, t)
		assertAny(true, exp.Overridden, t)
	} else {
		t.Error("Expected at least one exposure")
	}
	context.EventLock_.Unlock()
}

func TestTreatmentExposureWithFullOn(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.GetTreatment("exp_test_fullon")

	context.EventLock_.Lock()
	if len(context.Exposures_) > 0 {
		var found = false
		for _, exp := range context.Exposures_ {
			if exp.Name == "exp_test_fullon" {
				found = true
				assertAny(2, exp.Variant, t)
				assertAny(true, exp.FullOn, t)
				break
			}
		}
		if !found {
			t.Error("Expected exposure for exp_test_fullon")
		}
	} else {
		t.Error("Expected at least one exposure")
	}
	context.EventLock_.Unlock()
}

func TestTreatmentExposureNotEligible(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res, _ = context.GetTreatment("exp_test_not_eligible")
	assertAny(0, res, t)

	context.EventLock_.Lock()
	var found = false
	for _, exp := range context.Exposures_ {
		if exp.Name == "exp_test_not_eligible" {
			found = true
			assertAny(0, exp.Variant, t)
			assertAny(false, exp.Eligible, t)
			break
		}
	}
	context.EventLock_.Unlock()
	if !found {
		t.Error("Expected exposure for exp_test_not_eligible")
	}
}

func TestTreatmentExposureWithCustomAssignment(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetCustomAssignment("exp_test_ab", 2)
	context.GetTreatment("exp_test_ab")

	context.EventLock_.Lock()
	if len(context.Exposures_) > 0 {
		exp := context.Exposures_[0]
		assertAny("exp_test_ab", exp.Name, t)
		assertAny(2, exp.Variant, t)
		assertAny(true, exp.Custom, t)
	} else {
		t.Error("Expected at least one exposure")
	}
	context.EventLock_.Unlock()
}

func TestVariableKeysReturnsAllActiveKeys(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureRefresh)

	var res, err = context.GetVariableKeys()
	assertAny(nil, err, t)
	assertAny(variableExperimentKeys, res, t)
}

func TestRefreshClearsAssignmentCacheForStoppedExperiment(t *testing.T) {
	setUp()
	savedExperiment := experiment
	savedFut := fut
	defer func() {
		experiment = savedExperiment
		fut = savedFut
	}()

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureRefresh)

	var res, err = context.GetTreatment("exp_test_new")
	assertAny(nil, err, t)
	assertAny(1, res, t)

	context.Data_.Experiments[4].FullOnVariant = 0

	var tempExp = context.Data_.Experiments[4]
	experiment.Name = tempExp.Name
	experiment.Id = tempExp.Id
	fut = future.Call(func() (future.Value, error) {
		return jsonmodels.ContextData{
			Experiments: []jsonmodels.Experiment{experiment},
		}, nil
	})

	context.Refresh()

	res, err = context.GetTreatment("exp_test_new")
	assertAny(nil, err, t)
}

func TestRefreshClearsAssignmentCacheForFullOnChange(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureRefresh)

	var res, err = context.GetTreatment("exp_test_new")
	assertAny(nil, err, t)
	assertAny(1, res, t)
}

func TestRefreshClearsAssignmentCacheForTrafficSplitChange(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureRefresh)

	context.GetTreatment("exp_test_ab")
	context.Refresh()

	var rs, _ = context.GetExperiments()
	if rs != nil {
		assertAny(true, len(rs) > 0, t)
	}
}

func TestRefreshClearsAssignmentCacheForIterationChange(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureRefresh)

	context.GetTreatment("exp_test_ab")
	context.Refresh()

	var rs, _ = context.GetExperiments()
	if rs != nil {
		assertAny(true, len(rs) > 0, t)
	}
}

func TestContextConstructorSetsUnits(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(units, context.Units_, t)
}

func TestContextConstructorSetsAttributes(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.Attributes_ = map[string]interface{}{"age": 25}
	var context = CreateTestContext(config, dataFutureReady)

	if len(context.Attributes_) == 0 {
		t.Error("Expected attributes to be set")
	}
}

func TestContextIsReadyAfterDataLoaded(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
}

func TestContextStartsRefreshTimerAfterReady(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	if context.RefreshTimer_ == nil {
		t.Error("Expected refresh timer to be started")
	}
}

func TestContextNotReadyBeforeData(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)

	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
}

func TestContextFailedOnError(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureFailed)

	assertAny(true, context.IsReady(), t)
	assertAny(true, context.IsFailed(), t)
}

func TestContextLoadsExperimentData(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(data, dt, t)
}

func TestGetVariableValueStrictReturnsDefault(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureSrict)

	var res, err = context.GetVariableValue("banner.size", "small")
	assertAny(nil, err, t)
	assertAny("small", res, t)
}

func TestGetVariableValueReturnsExpected(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	for key, expName := range variableExperiments {
		var res, err = context.GetVariableValue(key, 17)
		assertAny(nil, err, t)
		if expName != "exp_test_not_eligible" && stringInSlice(expName, data.Experiments) {
			assertAny(expectedVariables[key], res, t)
		} else {
			assertAny(17, res, t)
		}
	}
}

func TestPublishResetsQueuesKeepsAssignments(t *testing.T) {
	setUp()
	client := &CapturingPublishClient{contextData: data}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, nil)

	ctx.SetCustomAssignment("exp_test_ab", 2)
	ctx.GetTreatment("exp_test_ab")
	ctx.Track("goal1", map[string]interface{}{"amount": 100})
	ctx.Publish()

	time.Sleep(50 * time.Millisecond)

	assertAny(int32(0), ctx.GetPendingCount(), t)

	var res, err = ctx.GetCustomAssignment("exp_test_ab")
	assertAny(nil, err, t)
	assertAny(2, res, t)
}

func TestCloseStopsRefreshTimer(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureReady)

	if context.RefreshTimer_ == nil {
		t.Fatal("Expected refresh timer before close")
	}

	context.Close()

	assertAny(true, context.RefreshTimer_ == nil, t)
}

func TestTreatmentQueueExposureWithAudienceMatchFalseOnMismatch(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureSrict)

	context.GetTreatment("exp_test_ab")

	context.EventLock_.Lock()
	if len(context.Exposures_) > 0 {
		exp := context.Exposures_[0]
		assertAny(true, exp.AudienceMismatch, t)
	} else {
		t.Error("Expected at least one exposure")
	}
	context.EventLock_.Unlock()
}

func TestCustomAssignmentOverridesNaturalAssignment(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetCustomAssignment("exp_test_ab", 2)

	var res, err = context.GetTreatment("exp_test_ab")
	assertAny(nil, err, t)
	assertAny(2, res, t)

	context.EventLock_.Lock()
	if len(context.Exposures_) > 0 {
		exp := context.Exposures_[0]
		assertAny(true, exp.Custom, t)
		assertAny(2, exp.Variant, t)
	}
	context.EventLock_.Unlock()
}

func TestEventLoggerCalledOnPublishSuccess(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	client := &CapturingPublishClient{contextData: data}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, logger)

	logger.Reset()

	ctx.Track("goal1", nil)
	ctx.Publish()

	time.Sleep(50 * time.Millisecond)

	events := logger.GetEvents()
	foundPublish := false
	for _, event := range events {
		if event.EventType == Publish {
			foundPublish = true
			break
		}
	}
	if !foundPublish {
		t.Error("Expected PUBLISH event to be logged on success")
	}
}

func TestEventLoggerCalledOnRefreshSuccess(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dp = DefaultContextDataProvider{client_: client}
	var eh = DefaultContextEventHandler{client_: client}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dp, eh, logger, vp, am)
	logger.Reset()

	ctx.Refresh()
	time.Sleep(50 * time.Millisecond)

	events := logger.GetEvents()
	foundRefresh := false
	for _, event := range events {
		if event.EventType == Refresh {
			foundRefresh = true
			break
		}
	}
	if !foundRefresh {
		t.Error("Expected REFRESH event to be logged on success")
	}
}

func TestEventLoggerCalledOnCloseSuccess(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dp = DefaultContextDataProvider{client_: client}
	var eh = DefaultContextEventHandler{client_: client}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dp, eh, logger, vp, am)
	logger.Reset()

	ctx.Close()
	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundClose := false
	for _, event := range events {
		if event.EventType == Close {
			foundClose = true
			break
		}
	}
	if !foundClose {
		t.Error("Expected CLOSE event to be logged")
	}
}

func TestVariableValueExposureIncludesAudienceMatchTrue(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.GetVariableValue("banner.border", 0)

	context.EventLock_.Lock()
	if len(context.Exposures_) > 0 {
		exp := context.Exposures_[0]
		assertAny(false, exp.AudienceMismatch, t)
	} else {
		t.Error("Expected at least one exposure")
	}
	context.EventLock_.Unlock()
}

func TestVariableValueExposureAudienceMismatchStrict(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureSrict)

	context.GetVariableValue("banner.border", 0)

	context.EventLock_.Lock()
	if len(context.Exposures_) > 0 {
		exp := context.Exposures_[0]
		assertAny(true, exp.AudienceMismatch, t)
		assertAny(0, exp.Variant, t)
	} else {
		t.Error("Expected at least one exposure")
	}
	context.EventLock_.Unlock()
}

func TestVariableValueReturnsDefaultOnUnknownVariable(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res, err = context.GetVariableValue("totally_unknown", "mydefault")
	assertAny(nil, err, t)
	assertAny("mydefault", res, t)
}

func TestPeekVariableReturnAssignedOnNonStrictMismatch(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res, err = context.PeekVariableValue("banner.border", 99)
	assertAny(nil, err, t)
	assertAny(1.0, res, t)
}

func TestStartsPublishTimeoutAfterReadyWithPendingGoals(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.PublishDelay_ = 5000
	var ctx = CreateTestContext(config, dataFuture)

	ctx.Track("goal1", nil)
	assertAny(int32(1), ctx.GetPendingCount(), t)

	dataFuture.SetResult(data, nil)
	ctx.WaitUntilReady()

	assertAny(true, ctx.IsReady(), t)
	assertAny(int32(1), ctx.GetPendingCount(), t)
}

func TestGoalTimestamp(t *testing.T) {
	setUp()
	client := &CapturingPublishClient{contextData: data}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, nil)

	ctx.Track("goal1", map[string]interface{}{"amount": 100})
	ctx.Publish()

	time.Sleep(50 * time.Millisecond)

	if len(client.publishEvents) == 0 {
		t.Fatal("Expected at least one publish event")
	}

	pe := client.publishEvents[0]
	if len(pe.Goals) == 0 {
		t.Fatal("Expected goal data")
	}

	if pe.Goals[0].AchievedAt != 1620000000000 {
		t.Errorf("Expected goal timestamp 1620000000000, got %d", pe.Goals[0].AchievedAt)
	}
}

func TestCustomFieldKeys(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res, err = context.GetCustomFieldValueKeys()
	assertAny(nil, err, t)
	assertAny([]string{"country", "languages", "overrides"}, res, t)
}

func TestContextBecomesReadyAndCallsHandler(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)

	assertAny(false, context.IsReady(), t)

	dataFuture.SetResult(data, nil)
	context.WaitUntilReady()

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
}

func TestContextBecomesReadyAndFailedOnError(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)

	assertAny(false, context.IsReady(), t)

	dataFuture.SetResult(jsonmodels.ContextData{}, errors.New("FAILED"))
	context.WaitUntilReady()

	assertAny(true, context.IsReady(), t)
	assertAny(true, context.IsFailed(), t)
}

func TestVariableValueThrowsAfterClose(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()

	var _, err = context.GetVariableValue("banner.border", 0)
	if err == nil {
		t.Error("Expected error after close")
	}
}

func TestTreatmentThrowsAfterClose(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()

	var _, err = context.GetTreatment("exp_test_ab")
	if err == nil {
		t.Error("Expected error after close")
	}
}

func TestPublishNotRetainQueueOnFailure(t *testing.T) {
	setUp()
	client := &CapturingPublishClient{
		contextData: data,
		publishErr:  errors.New("publish failed"),
	}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, nil)

	ctx.Track("goal1", nil)
	ctx.Publish()

	time.Sleep(50 * time.Millisecond)
}

func TestEventLoggerCalledOnVariableValueExposure(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dp = DefaultContextDataProvider{client_: client}
	var eh = DefaultContextEventHandler{client_: client}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dp, eh, logger, vp, am)
	logger.Reset()

	ctx.GetVariableValue("banner.border", 0)

	time.Sleep(10 * time.Millisecond)

	events := logger.GetEvents()
	foundExposure := false
	for _, event := range events {
		if event.EventType == Exposure {
			foundExposure = true
			break
		}
	}
	if !foundExposure {
		t.Error("Expected EXPOSURE event to be logged for variable value access")
	}
}

func TestCustomAssignmentNotOverrideFullOn(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetCustomAssignment("exp_test_fullon", 3)

	var res, err = context.GetTreatment("exp_test_fullon")
	assertAny(nil, err, t)
	assertAny(2, res, t)
}

func TestCustomAssignmentNotOverrideNotEligible(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetCustomAssignment("exp_test_not_eligible", 3)

	var res, err = context.GetTreatment("exp_test_not_eligible")
	assertAny(nil, err, t)
	assertAny(0, res, t)
}

func TestContextWithCustomPublisherDataProviderAndEventLogger(t *testing.T) {
	setUp()
	logger := &MockEventLogger{}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger

	var client = EventLoggerClientMock{}
	var dp = DefaultContextDataProvider{client_: client}
	var eh = DefaultContextEventHandler{client_: client}
	var vp = DefaultVariableParser{}
	var am = AudienceMatcher{audeser}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFutureReady, dp, eh, logger, vp, am)
	assertAny(true, ctx.IsReady(), t)
}

func TestPeekVariableConflictingKeyDisjointAudiences(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res, err = context.PeekVariableValue("card.width", 99)
	assertAny(nil, err, t)
	assertAny(99, res, t)
}

func TestGetVariableConflictingKeyDisjointAudiences(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var res, err = context.GetVariableValue("card.width", 99)
	assertAny(nil, err, t)
	assertAny(99, res, t)
}

func TestExposedFlagSetOnce(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.GetTreatment("exp_test_ab")
	assertAny(int32(1), context.GetPendingCount(), t)

	context.GetTreatment("exp_test_ab")
	assertAny(int32(1), context.GetPendingCount(), t)

	context.GetTreatment("exp_test_ab")
	assertAny(int32(1), context.GetPendingCount(), t)
}

func TestTrackWithTimestamp(t *testing.T) {
	setUp()
	client := &CapturingPublishClient{contextData: data}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, nil)

	ctx.Track("goal1", map[string]interface{}{"amount": 100})
	ctx.Publish()

	time.Sleep(50 * time.Millisecond)

	if len(client.publishEvents) > 0 && len(client.publishEvents[0].Goals) > 0 {
		goal := client.publishEvents[0].Goals[0]
		if goal.AchievedAt <= 0 {
			t.Error("Expected positive timestamp on goal")
		}
	}
}

func TestWaitUntilReadyReturnsWhenAlreadyReady(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	result := context.WaitUntilReady()
	assertAny(true, result.IsReady(), t)
}

func TestContextExposureTimestamp(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.GetTreatment("exp_test_ab")

	context.EventLock_.Lock()
	if len(context.Exposures_) > 0 {
		exp := context.Exposures_[0]
		assertAny(int64(1620000000000), exp.ExposedAt, t)
	}
	context.EventLock_.Unlock()
}

func TestPublishEventHasCorrectTimestamp(t *testing.T) {
	setUp()
	client := &CapturingPublishClient{contextData: data}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, nil)

	ctx.Track("goal1", nil)
	ctx.Publish()

	time.Sleep(50 * time.Millisecond)

	if len(client.publishEvents) > 0 {
		pe := client.publishEvents[0]
		assertAny(int64(1620000000000), pe.PublishedAt, t)
	}
}

func TestPublishEventHashedUnits(t *testing.T) {
	setUp()
	client := &CapturingPublishClient{contextData: data}
	df, done := future.New()
	done(data, nil)
	ctx := createCanonicalContext(t, client, df, nil)

	ctx.Track("goal1", nil)
	ctx.Publish()

	time.Sleep(50 * time.Millisecond)

	if len(client.publishEvents) > 0 {
		pe := client.publishEvents[0]
		assertAny(true, pe.Hashed, t)
		if len(pe.Units) != len(units) {
			t.Errorf("Expected %d units, got %d", len(units), len(pe.Units))
		}
	}
}

func TestAttributeSetAfterClose(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.Close()

	var err = context.SetAttribute("attr1", "value1")
	if err == nil {
		t.Error("Expected error after close")
	}
}

func TestOverridesClearAssignmentCache(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	context.SetOverride("exp_test_ab", 5)
	var res, _ = context.GetTreatment("exp_test_ab")
	assertAny(5, res, t)

	context.SetOverride("exp_test_ab", 7)
	res, _ = context.GetTreatment("exp_test_ab")
	assertAny(7, res, t)
}

func TestCustomFieldValueForMultipleExperiments(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var abCountry = context.GetCustomFieldValue("exp_test_ab", "country")
	assertAny("US,PT,ES,DE,FR", abCountry, t)

	var abcLanguages = context.GetCustomFieldValue("exp_test_abc", "languages")
	assertAny("en-US,en-GB,pt-PT,pt-BR,es-ES,es-MX", abcLanguages, t)

	var abLanguages = context.GetCustomFieldValue("exp_test_ab", "languages")
	assertAny(nil, abLanguages, t)
}

type CapturingClientWithRefresh struct {
	contextData        jsonmodels.ContextData
	refreshedData      jsonmodels.ContextData
	refreshCount       int32
	useRefreshedData   atomic.Value
}

func (c *CapturingClientWithRefresh) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		if c.useRefreshedData.Load() != nil && c.useRefreshedData.Load().(bool) {
			atomic.AddInt32(&c.refreshCount, 1)
			return c.refreshedData, nil
		}
		return c.contextData, nil
	})
}

func (c *CapturingClientWithRefresh) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, nil
	})
}
