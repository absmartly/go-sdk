package sdk

import (
	context2 "context"
	"errors"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"io/ioutil"
	"testing"
)

type ClientContextMock struct {
}

var experiment = jsonmodels.Experiment{Name: "test"}

var fut = future.Call(func() (future.Value, error) {
	return &jsonmodels.ContextData{
		Experiments: []jsonmodels.Experiment{experiment},
	}, nil
})

func (c ClientContextMock) GetContextData() *future.Future {
	return fut
}

func (c ClientContextMock) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, nil
	})
}

var expectedVariants = map[string]int{
	"exp_test_ab":           1,
	"exp_test_abc":          2,
	"exp_test_not_eligible": 0,
	"exp_test_fullon":       2,
	"exp_test_new":          1,
}

var expectedVariables = map[string]interface{}{
	"banner.border": 1.0,
	"banner.size":   "large",
	"button.color":  "red",
	"submit.color":  "blue",
	"submit.shape":  "rect",
	"show-modal":    true,
}

var variableExperiments = map[string]string{
	"banner.border": "exp_test_ab",
	"banner.size":   "exp_test_ab",
	"button.color":  "exp_test_abc",
	"card.width":    "exp_test_not_eligible",
	"submit.color":  "exp_test_fullon",
	"submit.shape":  "exp_test_fullon",
	"show-modal":    "exp_test_new",
}

var units = map[string]string{
	"session_id": "e791e240fcd3df7d238cfc285f475e8152fcc0ec",
	"user_id":    "123456789",
	"email":      "bleh@absmartly.com"}

var deser = DefaultContextDataDeserializer{}
var audeser = DefaultAudienceDeserializer{}

var data jsonmodels.ContextData
var refreshData jsonmodels.ContextData
var audienceStrictData jsonmodels.ContextData

var dataFuture *future.Future
var dataFutureSrict *future.Future
var dataFutureRefresh *future.Future
var dataFutureReady *future.Future
var dataFutureFailed *future.Future
var clock internal.Clock
var dataProvider ContextDataProvider
var eventHandler ContextEventHandler
var eventLogger ContextEventLogger
var variableParser DefaultVariableParser
var audienceMatcher AudienceMatcher

var buff = make([]byte, 512)     // should be 512 bytes
var block = make([]int32, 16)    // should be 16 bytes
var st = make([]int32, 4)        // should be 4 bytes
var assignBuf = make([]int8, 12) // should be 12 bytes

func setUp() {
	content, _ := ioutil.ReadFile("../context.json")
	contentstrict, _ := ioutil.ReadFile("../context-strict.json")
	refreshed, _ := ioutil.ReadFile("../refreshed.json")
	data, _ = deser.Deserialize(content)
	audienceStrictData, _ = deser.Deserialize(contentstrict)
	refreshData, _ = deser.Deserialize(refreshed)
	var tempdataFutureReady, donefunc = future.New()
	dataFutureReady = tempdataFutureReady
	donefunc(&data, nil)
	dataFuture, _ = future.New()
	var tempdataFutureFailed, donefuncfailed = future.New()
	dataFutureFailed = tempdataFutureFailed
	donefuncfailed(nil, errors.New("FAILED"))
	var tempdataFutureReadys, donefuncs = future.New()
	dataFutureSrict = tempdataFutureReadys
	donefuncs(&audienceStrictData, nil)
	var tempdataFutureReadyr, donefuncr = future.New()
	dataFutureRefresh = tempdataFutureReadyr
	donefuncr(&refreshData, nil)

	clock = internal.FixedClock{Millis_: 1_620_000_000_000}
	var client = ClientContextMock{}
	dataProvider = DefaultContextDataProvider{client_: client}
	eventHandler = DefaultContextEventHandler{client_: client}
	variableParser = DefaultVariableParser{}
	audienceMatcher = AudienceMatcher{audeser}

}

func CreateTestContext(config ContextConfig, dataFuture *future.Future) *Context {
	return CreateContext(clock, config, dataFuture, dataProvider, eventHandler, eventLogger, variableParser, audienceMatcher,
		buff, block, st)
}

func TestConstructorSetsOverrides(t *testing.T) {
	setUp()
	var overrides = map[string]int{"exp_test": 2, "exp_test_1": 1}
	var config = ContextConfig{}
	config.Units_ = units
	config.Overrides_ = overrides

	var context = CreateTestContext(config, dataFuture)
	for key, value := range overrides {
		var res, _ = context.GetOverride(key)
		assertAny(value, res, t)
	}
}

func TestConstructorSetsCustomAssignments(t *testing.T) {
	setUp()
	var cassignments = map[string]int{"exp_test": 2, "exp_test_1": 1}
	var config = ContextConfig{}
	config.Units_ = units
	config.Cassigmnents_ = cassignments

	var context = CreateTestContext(config, dataFuture)
	for key, value := range cassignments {
		var res, _ = context.GetCustomAssignment(key)
		assertAny(value, res, t)
	}
}

func TestBecomesReadyWithCompletedFuture(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units

	var context = CreateTestContext(config, dataFutureReady)
	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&data, dt, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

}

func TestBecomesReadyExceptionallyWithCompletedFuture(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units

	var context = CreateTestContext(config, dataFutureFailed)
	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&jsonmodels.ContextData{}, dt, t)
	assertAny(true, context.IsReady(), t)
	assertAny(true, context.IsFailed(), t)

}

func TestBecomesReadyWithException(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units

	var context = CreateTestContext(config, dataFuture)
	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	dataFuture.SetResult(&jsonmodels.ContextData{}, errors.New("FAILED"))
	context.WaitUntilReady()
	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&jsonmodels.ContextData{}, dt, t)
	assertAny(true, context.IsReady(), t)
	assertAny(true, context.IsFailed(), t)

}

func TestBecomesReadyWithoutException(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units

	var context = CreateTestContext(config, dataFuture)
	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	dataFuture.SetResult(&data, nil)
	context.WaitUntilReady()
	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&data, dt, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

}

func TestWaitUntilReady(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)
	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	go func() {
		dataFuture.SetResult(&data, nil)
	}()

	context.WaitUntilReady()
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&data, dt, t)

}

func TestWaitUntilReadyCompleted(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	context.WaitUntilReady()
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&data, dt, t)

}

func TestWaitUntilReadyAsync(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)
	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var future = context.WaitUntilReadyAsync()
	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	go func() {
		dataFuture.SetResult(&data, nil)
	}()
	future.Join(context2.Background())
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&data, dt, t)

}

func TestWaitUntilReadyAsynCompleted(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&data, dt, t)
	var future = context.WaitUntilReadyAsync()
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	assertAny(nil, err, t)
	assertAny(&data, dt, t)
	future.Join(context2.Background())
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	assertAny(nil, err, t)
	assertAny(&data, dt, t)
}

func TestErrorWhenClosing(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&data, dt, t)

	var trerr = context.Track("goal1", map[string]interface{}{"amount": 125, "hours": 245}, buff, block, st)
	assertAny(nil, trerr, t)

	var _, er = context.CloseAsync(buff, block, st)
	assertAny(nil, er, t)

	var resErr = context.SetAttribute("attr1", "value1")
	assertAny("ABSmartly Context is closing", resErr.Error(), t)
	assertAny(true, context.IsClosing(), t)
	assertAny(false, context.IsClosed(), t)

	_, resErr = context.GetTreatment("attr1", buff, block, st, assignBuf)
	assertAny("ABSmartly Context is closing", resErr.Error(), t)
	assertAny(true, context.IsClosing(), t)
	assertAny(false, context.IsClosed(), t)

	_, resErr = context.PeekVariableValue("attr1", jsonmodels.ContextData{}, buff, block, st, assignBuf)
	assertAny("ABSmartly Context is closing", resErr.Error(), t)
	assertAny(true, context.IsClosing(), t)
	assertAny(false, context.IsClosed(), t)
}

func TestErrorWhenClosed(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&data, dt, t)

	var trerr = context.Track("goal1", map[string]interface{}{"amount": 125, "hours": 245}, buff, block, st)
	assertAny(nil, trerr, t)

	context.Close(buff, block, st)

	var resErr = context.SetAttribute("attr1", "value1")
	assertAny("ABSmartly Context is closed", resErr.Error(), t)
	assertAny(false, context.IsClosing(), t)
	assertAny(true, context.IsClosed(), t)

	_, resErr = context.GetTreatment("attr1", buff, block, st, assignBuf)
	assertAny("ABSmartly Context is closed", resErr.Error(), t)
	assertAny(false, context.IsClosing(), t)
	assertAny(true, context.IsClosed(), t)

	_, resErr = context.PeekVariableValue("attr1", jsonmodels.ContextData{}, buff, block, st, assignBuf)
	assertAny("ABSmartly Context is closed", resErr.Error(), t)
	assertAny(false, context.IsClosing(), t)
	assertAny(true, context.IsClosed(), t)
}

func TestGetExperiments(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var dt, err = context.GetData()
	assertAny(nil, err, t)
	assertAny(&data, dt, t)

	assertAny(data.Experiments, dt.Experiments, t)
}

func TestRefreshTimerWhenReady(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)
	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	dataFuture.SetResult(&data, nil)
	var futu = context.RefreshAsync()
	context.WaitUntilReady()
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	futu.Join(context2.Background())
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.Refreshing_.Load(), t)
	assertAny(false, context.IsFailed(), t)

}

func TestUnitEmpty(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.SetUnit("db_user_id", "")

	assertAny("unit  UID must not be blank.", err.Error(), t)

	err = context.SetUnit("session_id", "1")

	assertAny("unit already set", err.Error(), t)
}

func TestSetAttributes(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)

	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.SetAttribute("db_user_id", "test")
	assertAny(nil, err, t)
	err = context.SetAttributes(map[string]interface{}{"db_user_id2": "test2"})
	assertAny(nil, err, t)
}

func TestSetOverrides(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)

	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.SetOverride("db_user_id", 1)
	assertAny(nil, err, t)
	var res, er = context.GetOverride("db_user_id")
	assertAny(nil, er, t)
	assertAny(1, res, t)
	err = context.SetOverrides(map[string]int{"db_user_id2": 1})
	assertAny(nil, err, t)
	res, er = context.GetOverride("db_user_id2")
	assertAny(nil, er, t)
	assertAny(1, res, t)

	res, er = context.GetOverride("db_user_id3")
	assertAny("override not found", er.Error(), t)
	assertAny(-1, res, t)

	dataFuture.SetResult(&data, nil)
	context.WaitUntilReady()
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	res, er = context.GetOverride("db_user_id")
	assertAny(nil, er, t)
	assertAny(1, res, t)
	res, er = context.GetOverride("db_user_id2")
	assertAny(nil, er, t)
	assertAny(1, res, t)
}

func TestSetOverridesReady(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.SetOverride("db_user_id", 1)
	assertAny(nil, err, t)
	var res, er = context.GetOverride("db_user_id")
	assertAny(nil, er, t)
	assertAny(1, res, t)
	err = context.SetOverrides(map[string]int{"db_user_id2": 1})
	assertAny(nil, err, t)
	res, er = context.GetOverride("db_user_id2")
	assertAny(nil, er, t)
	assertAny(1, res, t)

	res, er = context.GetOverride("db_user_id3")
	assertAny("override not found", er.Error(), t)
	assertAny(-1, res, t)
}

func TestSetOverridesClearAssignmentCache(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.SetOverride("db_user_id", 1)
	assertAny(nil, err, t)
	var res, er = context.GetOverride("db_user_id")
	assertAny(nil, er, t)
	assertAny(1, res, t)
	res, er = context.GetTreatment("db_user_id", buff, block, st, assignBuf)
	assertAny(nil, er, t)
	var dbover, _ = context.GetOverride("db_user_id")
	assertAny(dbover, res, t)

	err = context.SetOverride("db_user_id", 2)
	assertAny(nil, err, t)
	res, er = context.GetOverride("db_user_id")
	assertAny(nil, er, t)
	assertAny(2, res, t)

	res, er = context.GetTreatment("db_user_id", buff, block, st, assignBuf)
	assertAny(nil, er, t)
	dbover, _ = context.GetOverride("db_user_id")
	assertAny(dbover, res, t)
	assertAny(2, res, t)

	res, er = context.GetOverride("db_user_id3")
	assertAny("override not found", er.Error(), t)
	assertAny(-1, res, t)
}

func TestSetCustomAssignmentsReady(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.SetCustomAssignment("db_user_id", 1)
	assertAny(nil, err, t)
	var res, er = context.GetCustomAssignment("db_user_id")
	assertAny(nil, er, t)
	assertAny(1, res, t)
	err = context.SetCustomAssignments(map[string]int{"db_user_id2": 1})
	assertAny(nil, err, t)
	res, er = context.GetCustomAssignment("db_user_id2")
	assertAny(nil, er, t)
	assertAny(1, res, t)

	res, er = context.GetCustomAssignment("db_user_id3")
	assertAny("customAssignment not found", er.Error(), t)
	assertAny(-1, res, t)
}

func TestSetCustomAssignments(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)

	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.SetCustomAssignment("db_user_id", 1)
	assertAny(nil, err, t)
	var res, er = context.GetCustomAssignment("db_user_id")
	assertAny(nil, er, t)
	assertAny(1, res, t)
	err = context.SetCustomAssignments(map[string]int{"db_user_id2": 1})
	assertAny(nil, err, t)
	res, er = context.GetCustomAssignment("db_user_id2")
	assertAny(nil, er, t)
	assertAny(1, res, t)

	res, er = context.GetCustomAssignment("db_user_id3")
	assertAny("customAssignment not found", er.Error(), t)
	assertAny(-1, res, t)

	dataFuture.SetResult(&data, nil)
	context.WaitUntilReady()
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	res, er = context.GetCustomAssignment("db_user_id")
	assertAny(nil, er, t)
	assertAny(1, res, t)
	res, er = context.GetCustomAssignment("db_user_id2")
	assertAny(nil, er, t)
	assertAny(1, res, t)
}

func TestCustomAssignmentDoesNotOverrideFullOnOrNotEligibleAssignments(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.SetCustomAssignment("exp_test_not_eligible", 3)
	assertAny(nil, err, t)

	err = context.SetCustomAssignments(map[string]int{"exp_test_fullon": 3})
	assertAny(nil, err, t)

	var rs, er = context.GetTreatment("exp_test_not_eligible", buff, block, st, assignBuf)
	assertAny(nil, er, t)
	assertAny(0, rs, t)

	rs, er = context.GetTreatment("exp_test_fullon", buff, block, st, assignBuf)
	assertAny(nil, er, t)
	assertAny(2, rs, t)
}

func TestCustomAssignmentPendingAssignmentCache(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.SetCustomAssignment("exp_test_ab", 2)
	assertAny(nil, err, t)

	err = context.SetCustomAssignments(map[string]int{"exp_test_abc": 3})
	assertAny(nil, err, t)

	assertAny(int32(0), context.GetPendingCount(), t)
	var rs, er = context.GetTreatment("exp_test_ab", buff, block, st, assignBuf)
	assertAny(nil, er, t)
	assertAny(2, rs, t)
	assertAny(int32(1), context.GetPendingCount(), t)

	rs, er = context.GetTreatment("exp_test_abc", buff, block, st, assignBuf)
	assertAny(nil, er, t)
	assertAny(3, rs, t)

	_ = context.SetCustomAssignment("exp_test_ab", 4)
	rs, _ = context.GetTreatment("exp_test_ab", buff, block, st, assignBuf)
	assertAny(4, rs, t)
}

func TestPeekTreatment(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	for _, experiment := range data.Experiments {
		var res, err = context.PeekTreatment(experiment.Name, buff, block, st, assignBuf)
		assertAny(nil, err, t)
		assertAny(expectedVariants[experiment.Name], res, t)
	}

	var res, err = context.PeekTreatment("not_found", buff, block, st, assignBuf)
	assertAny(nil, err, t)
	assertAny(0, res, t)
}

func stringInSlice(a string, list []jsonmodels.Experiment) bool {
	for _, b := range list {
		if b.Name == a {
			return true
		}
	}
	return false
}

func TestPeekVariable(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	for key, value := range variableExperiments {
		var res, err = context.PeekVariableValue(key, 17, buff, block, st, assignBuf)
		assertAny(nil, err, t)
		if value != "exp_test_not_eligible" && stringInSlice(value, data.Experiments) {
			assertAny(expectedVariables[key], res, t)
		} else {
			assertAny(17, res, t)
		}
	}

	assertAny(int32(0), context.GetPendingCount(), t)
}

func TestPeekVariableStrict(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureSrict)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	var res, err = context.PeekVariableValue("banner.size", "small", buff, block, st, assignBuf)
	assertAny(nil, err, t)
	assertAny("small", res, t)

	assertAny(int32(0), context.GetPendingCount(), t)
}

func TestGetVariable(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	for key, value := range variableExperiments {
		var res, err = context.GetVariableValue(key, 17, buff, block, st, assignBuf)
		assertAny(nil, err, t)
		if value != "exp_test_not_eligible" && stringInSlice(value, data.Experiments) {
			assertAny(expectedVariables[key], res, t)
		} else {
			assertAny(17, res, t)
		}
	}

}

func TestGetVariableStrict(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureSrict)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	var res, err = context.GetVariableValue("banner.size", "small", buff, block, st, assignBuf)
	assertAny(nil, err, t)
	assertAny("small", res, t)

}

func TestGetVariableKeys(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureRefresh)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var res, _ = context.GetVariableKeys()
	assertAny(variableExperiments, res, t)

	assertAny(int32(0), context.GetPendingCount(), t)
}

func TestPeekTreatmentOverrideVariant(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	for _, experiment := range data.Experiments {
		var _ = context.SetOverride(experiment.Name, 11+expectedVariants[experiment.Name])
	}
	var _ = context.SetOverride("not_found", 3)

	for _, experiment := range data.Experiments {
		var res, _ = context.PeekTreatment(experiment.Name, buff, block, st, assignBuf)
		assertAny(expectedVariants[experiment.Name]+11, res, t)
	}
	var res, _ = context.PeekTreatment("not_found", buff, block, st, assignBuf)
	assertAny(3, res, t)

	assertAny(int32(0), context.GetPendingCount(), t)
}

func TestGetTreatmentOverrideVariant(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)
	for _, experiment := range data.Experiments {
		var _ = context.SetOverride(experiment.Name, 11+expectedVariants[experiment.Name])
	}
	var _ = context.SetOverride("not_found", 3)

	for _, experiment := range data.Experiments {
		var res, _ = context.GetTreatment(experiment.Name, buff, block, st, assignBuf)
		assertAny(expectedVariants[experiment.Name]+11, res, t)
	}
	var res, _ = context.GetTreatment("not_found", buff, block, st, assignBuf)
	assertAny(3, res, t)

	var err = context.Publish(buff, block, st)
	assertAny(int32(0), context.GetPendingCount(), t)
	assertAny(nil, err, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
	context.Close(buff, block, st)
	assertAny(true, context.IsReady(), t)
	assertAny(true, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
}

func TestTrack(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.Track("goal1", map[string]interface{}{
		"amount": 125, "hours": 245}, buff, block, st)
	assertAny(int32(1), context.GetPendingCount(), t)
	assertAny(nil, err, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
	err = context.Track("goal2", map[string]interface{}{
		"tries": 7}, buff, block, st)
	assertAny(nil, err, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)

	err = context.Publish(buff, block, st)
	assertAny(int32(0), context.GetPendingCount(), t)
	assertAny(nil, err, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)

	context.Close(buff, block, st)
	assertAny(true, context.IsReady(), t)
	assertAny(true, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
}

func TestTrackNotReady(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFuture)
	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.Track("goal1", map[string]interface{}{
		"amount": 125, "hours": 245}, buff, block, st)
	assertAny(int32(1), context.GetPendingCount(), t)
	assertAny(nil, err, t)
	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
	err = context.Track("goal2", map[string]interface{}{
		"tries": 7}, buff, block, st)
	assertAny(int32(2), context.GetPendingCount(), t)
	assertAny(nil, err, t)
	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)

	err = context.Publish(buff, block, st)
	assertAny(int32(0), context.GetPendingCount(), t)
	assertAny(nil, err, t)
	assertAny(false, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)

	context.Close(buff, block, st)
	assertAny(false, context.IsReady(), t)
	assertAny(true, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
}

func TestPublishResetsInternalQueuesAndKeepsAttributesOverridesAndCustomAssignments(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.SetCustomAssignment("exp_test_ab", 2)
	assertAny(nil, err, t)

	err = context.SetCustomAssignments(map[string]int{"exp_test_abc": 3})
	assertAny(nil, err, t)

	assertAny(int32(0), context.GetPendingCount(), t)
	var rs, er = context.GetTreatment("exp_test_ab", buff, block, st, assignBuf)
	assertAny(nil, er, t)
	assertAny(2, rs, t)

	rs, er = context.GetTreatment("exp_test_abc", buff, block, st, assignBuf)
	assertAny(nil, er, t)
	assertAny(3, rs, t)

	_ = context.SetCustomAssignment("exp_test_ab", 4)
	rs, _ = context.GetTreatment("exp_test_ab", buff, block, st, assignBuf)
	assertAny(4, rs, t)

	err = context.Track("goal1", map[string]interface{}{
		"amount": 125, "hours": 245}, buff, block, st)
	assertAny(nil, err, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)

	err = context.Publish(buff, block, st)
	assertAny(nil, err, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)

	rs, er = context.GetCustomAssignment("exp_test_ab")
	assertAny(nil, er, t)
	assertAny(4, rs, t)
}

func TestClose(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var err = context.Track("goal1", map[string]interface{}{
		"amount": 125, "hours": 245}, buff, block, st)
	assertAny(int32(1), context.GetPendingCount(), t)
	assertAny(nil, err, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)

	context.Close(buff, block, st)
	assertAny(true, context.IsReady(), t)
	assertAny(true, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)

	context.Close(buff, block, st)
	assertAny(true, context.IsReady(), t)
	assertAny(true, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
}

func TestCloseStopRefreshTimer(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units

	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	assertAny(false, context.RefreshTimer_ == nil, t)
	context.Close(buff, block, st)
	assertAny(true, context.RefreshTimer_ == nil, t)
	assertAny(true, context.IsReady(), t)
	assertAny(true, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)

}

func TestCloseStopRefreshTimerAsync(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units

	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	assertAny(false, context.RefreshTimer_ == nil, t)
	var _, err = context.CloseAsync(buff, block, st)
	assertAny(nil, err, t)
	assertAny(true, context.RefreshTimer_ == nil, t)
	assertAny(true, context.IsReady(), t)
	assertAny(true, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)

}

func TestRefresh(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units

	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureRefresh)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	assertAny(false, context.RefreshTimer_ == nil, t)
	context.Refresh()
	assertAny(false, context.RefreshTimer_ == nil, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
	var res, err = context.GetExperiments()
	assertAny(nil, err, t)
	assertAny([]string{"test"}, res, t)
}

func TestRefreshAsync(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units

	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureRefresh)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	assertAny(false, context.RefreshTimer_ == nil, t)
	var fut = context.RefreshAsync()
	fut.Join(context2.Background())
	assertAny(false, context.RefreshTimer_ == nil, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
	var res, err = context.GetExperiments()
	assertAny(nil, err, t)
	assertAny([]string{"test"}, res, t)

}

func TestRefreshClearAssignmentCacheForStartedExperiment(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units

	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureReady)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var res, err = context.GetTreatment("exp_test_new", buff, block, st, assignBuf)
	assertAny(0, res, t)
	assertAny(nil, err, t)

	res, err = context.GetTreatment("not_found", buff, block, st, assignBuf)
	assertAny(0, res, t)
	assertAny(nil, err, t)

	fut = dataFutureRefresh
	experiment.Name = "exp_test_new"
	experiment.Id = 2
	assertAny(false, context.RefreshTimer_ == nil, t)
	context.Refresh()
	assertAny(false, context.RefreshTimer_ == nil, t)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
	var rs, er = context.GetExperiments()
	assertAny(nil, er, t)
	assertAny([]string{"exp_test_ab", "exp_test_abc", "exp_test_not_eligible", "exp_test_fullon", "exp_test_new"}, rs, t)

	res, err = context.GetTreatment("exp_test_new", buff, block, st, assignBuf)
	assertAny(1, res, t)
	assertAny(nil, err, t)

	res, err = context.GetTreatment("not_found", buff, block, st, assignBuf)
	assertAny(0, res, t)
	assertAny(nil, err, t)

}

func TestClearAssignmentCacheForExperimentIdChange(t *testing.T) {
	setUp()
	var config = ContextConfig{}
	config.Units_ = units

	config.RefreshInterval_ = 5000
	var context = CreateTestContext(config, dataFutureRefresh)
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsFailed(), t)

	var res, err = context.GetTreatment("exp_test_new", buff, block, st, assignBuf)
	assertAny(1, res, t)
	assertAny(nil, err, t)

	res, err = context.GetTreatment("not_found", buff, block, st, assignBuf)
	assertAny(0, res, t)
	assertAny(nil, err, t)

	assertAny(int32(2), context.GetPendingCount(), t)

	context.Data_.Experiments[4].Name = "exp_test_new"
	context.Data_.Experiments[4].Id = 11
	context.Data_.Experiments[4].TrafficSeedHi = 54870830
	context.Data_.Experiments[4].TrafficSeedLo = 398724581
	context.Data_.Experiments[4].SeedHi = 77498863
	context.Data_.Experiments[4].SeedLo = 34737352
	experiment = context.Data_.Experiments[4]
	fut = future.Call(func() (future.Value, error) {
		return &jsonmodels.ContextData{
			Experiments: []jsonmodels.Experiment{experiment},
		}, nil
	})
	assertAny(false, context.RefreshTimer_ == nil, t)
	var ft = context.RefreshAsync()
	assertAny(false, context.RefreshTimer_ == nil, t)

	ft.Join(context2.Background())
	assertAny(true, context.IsReady(), t)
	assertAny(false, context.IsClosed(), t)
	assertAny(false, context.IsClosing(), t)
	var rs, er = context.GetExperiments()
	assertAny(nil, er, t)
	assertAny([]string{"exp_test_new"}, rs, t)

	res, err = context.GetTreatment("exp_test_new", buff, block, st, assignBuf)
	assertAny(1, res, t)
	assertAny(nil, err, t)

	res, err = context.GetTreatment("not_found", buff, block, st, assignBuf)
	assertAny(0, res, t)
	assertAny(nil, err, t)
}
