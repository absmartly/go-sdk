package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// Switchbacks load through the plain ContextData path, as holdouts do: the
// default client decodes the switchback fields into jsonmodels.Experiment,
// and CreateContext, CreateContextWith and SetData serve the schedule from
// them. GetData returns them as installed.

// sbHTTP is HttpMock with the body readable through resty's Body.
type sbHTTP struct{ HttpMock }

func (h sbHTTP) Get(string, map[string]string, map[string]string) *future.Future {
	return future.Call(func() (future.Value, error) {
		return h.response().SetBody([]byte(h.bodyString)), nil
	})
}

func sbBody(t *testing.T, data jsonmodels.ContextData) string {
	t.Helper()
	var raw, err = json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func sbLoadConfig() ContextConfig {
	var config = CreateDefaultContextConfig()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	config.Units_[sbUnitType] = sbUID
	config.Attributes_ = map[string]interface{}{sbClusterPath: sbClusterKey}
	return config
}

// sbExpectScheduled checks the context served the switchback's scheduled arm
// for the current period: a unit experiment on the same definition would
// draw by the unit hash instead.
func sbExpectScheduled(t *testing.T, ctx *Context, want int) {
	t.Helper()
	var variant, err = ctx.PeekTreatment("exp_sb")
	if err != nil {
		t.Fatal(err)
	}
	expectInt(t, want, variant)
	var assignment = ctx.GetAssignment("exp_sb")
	if assignment.UnitType != sbUnitType {
		t.Fatalf("served under %q, want the serving unit %q", assignment.UnitType, sbUnitType)
	}
}

// The default client decodes the switchback fields and CreateContext serves
// the schedule; the fields round-trip through GetData.
func TestSwitchbackLoad_CreateContextServesFromTheClient(t *testing.T) {
	var data = sbData(sbDefault(2, sbHour))
	var client = CreateClient(ClientConfig{Deserializer_: DefaultContextDataDeserializer{}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key", Application_: "website", Environment_: "dev"}, sbHTTP{HttpMock{bodyString: sbBody(t, data)}})
	var value, err = client.GetContextData().Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var decoded = value.(jsonmodels.ContextData)
	if e := decoded.Experiments[0]; e.RandomizationType != "switchback" || len(e.SwitchbackClusters) != 1 ||
		e.SwitchbackStartAt == nil || *e.SwitchbackStartAt != sbAnchor || e.SwitchbackPeriodMs == nil || *e.SwitchbackPeriodMs != sbHour {
		t.Fatalf("decoded switchback fields %+v", e)
	}
	var abs = Create(ABSmartlyConfig{Client_: client})
	var ctx = createContext(&sbClock{now: sbAnchor + 5}, sbLoadConfig(), abs.ContextDataProvider_.GetContextData(),
		abs.ContextDataProvider_, abs.ContextEventHandler_, nil, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_}, "", "")
	ctx.WaitUntilReady()
	sbExpectScheduled(t, ctx, sbExpected2Arms[0])
	installed, err := ctx.GetData()
	if err != nil {
		t.Fatal(err)
	}
	assertAny(decoded, installed, t)
}

// SetData installs a switchback from plain ContextData, and a later SetData
// without one turns it back into nothing served.
func TestSwitchbackLoad_SetDataInstallsAndRemoves(t *testing.T) {
	var h = sbCreate(t, sbData())
	h.ctx.SetData(sbData(sbDefault(2, sbHour)))
	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour+5).peek("exp_sb"))
	h.ctx.SetData(sbData(sbUnit(sbDefault(2, sbHour))))
	if h.ctx.state().indexedExperiment("exp_sb").switchback != nil {
		t.Fatal("a SetData without the switchback fields kept the switchback")
	}
}

// A context keeps its own copy of the switchback fields: editing the
// caller's slice after SetData changes nothing served.
func TestSwitchbackLoad_SetDataKeepsItsOwnCopy(t *testing.T) {
	var data = sbData(sbDefault(2, sbHour))
	var h = sbCreate(t, sbData())
	h.ctx.SetData(data)
	data.Experiments[0].SwitchbackPeriodMs = nil
	data.Experiments[0].RandomizationType = ""
	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour+5).peek("exp_sb"))
}

// An unchanged full-on switchback with an empty split reuses its slot: three
// reads in one period queue one exposure.
func TestSwitchbackLoad_EmptyFullOnSwitchbackDoesNotDuplicateExposure(t *testing.T) {
	var experiment = sbDefault(1, sbHour)
	experiment.FullOnVariant = 1
	experiment.Split = []float64{}
	var h = sbCreate(t, sbData(experiment))
	for i := 0; i < 3; i++ {
		var got, err = h.ctx.GetTreatment("exp_sb")
		if err != nil || got != 1 {
			t.Fatalf("read %d: got %d, want full-on arm 1", i, got)
		}
	}
	if exposures := queuedExposures(h.ctx); len(exposures) != 1 {
		t.Fatalf("queued %d exposures, want 1: %+v", len(exposures), exposures)
	}
}

// An initial data future that has already settled with a value that is not
// context data fails the context with a TypeError, as a pending one does.
func TestSwitchbackLoad_SettledInvalidDataFutureFailsTheContext(t *testing.T) {
	var logger = &hoLogger{}
	var errorsLogged []interface{}
	logger.onEvent = func(event EventType, data interface{}) {
		if event == Error {
			errorsLogged = append(errorsLogged, data)
		}
	}
	var settled, done = future.New()
	done(nil, nil)
	var ctx = CreateContext(internal.FixedClock{Millis_: hoMillis}, sbLoadConfig(), settled, &hoProvider{}, &hoPublisher{}, logger,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	if !ctx.IsReady() || !ctx.IsFailed() {
		t.Fatalf("ready %v, failed %v; want a failed context", ctx.IsReady(), ctx.IsFailed())
	}
	if len(errorsLogged) != 1 {
		t.Fatalf("logged %d errors, want 1: %v", len(errorsLogged), errorsLogged)
	}
	var err, ok = errorsLogged[0].(*TypeError)
	if !ok || err.Error() != "type assertion failed: expected jsonmodels.ContextData, got <nil>" {
		t.Fatalf("logged %#v", errorsLogged[0])
	}
}
