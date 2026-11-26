package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// The additive payload API: ContextData and Experiment keep their v1.2.0
// layouts, and holdouts and switchbacks reach a context only through
// ContextPayload (SetPayload, CreateContextWithPayload, DeserializePayload
// and the optional payload provider and client methods).

// cpHeldOut is a payload whose holdout holds hoUID out of "exp": served, the
// unit gets control and only the holdout's exposure.
func cpHeldOut() jsonmodels.ContextPayload {
	return hoData([]jsonmodels.Holdout{hoHoldoutA()}, hoExperiment(1, "exp", hoUnitType, 0, 11))
}

// cpHTTP is HttpMock with the body readable through resty's Body.
type cpHTTP struct{ HttpMock }

func (h cpHTTP) Get(string, map[string]string, map[string]string) *future.Future {
	return future.Call(func() (future.Value, error) {
		return h.response().SetBody([]byte(h.bodyString)), nil
	})
}

func cpBody(t *testing.T) string {
	t.Helper()
	var raw, err = json.Marshal(cpHeldOut())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// cpExpectHeldOut checks the context served the holdout: control, with the
// holdout's exposure and not the experiment's.
func cpExpectHeldOut(t *testing.T, ctx *Context) {
	t.Helper()
	var variant, err = ctx.GetTreatment("exp")
	if err != nil {
		t.Fatal(err)
	}
	expectInt(t, 0, variant)
	var exposures = queuedExposures(ctx)
	if len(exposures) != 1 || exposures[0].Id != 11 || exposures[0].Name != "holdout_a" {
		t.Fatalf("exposures %+v, want only holdout_a", exposures)
	}
}

// cpExpectServedWithoutHoldout checks the context served "exp" with no
// holdout: its own exposure only.
func cpExpectServedWithoutHoldout(t *testing.T, ctx *Context) {
	t.Helper()
	if _, err := ctx.GetTreatment("exp"); err != nil {
		t.Fatal(err)
	}
	var exposures = queuedExposures(ctx)
	if len(exposures) != 1 || exposures[0].Id != 1 {
		t.Fatalf("exposures %+v, want only exp", exposures)
	}
}

func cpContextConfig() ContextConfig {
	var config = CreateDefaultContextConfig()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	config.Units_[hoUnitType] = hoUID
	return config
}

// GetContextData keeps resolving to jsonmodels.ContextData, without the
// holdouts, even with the default deserializer; GetContextPayload resolves
// to the full payload.
func TestClientGetContextDataKeepsContextDataAndPayloadCarriesHoldouts(t *testing.T) {
	var client = CreateClient(ClientConfig{Deserializer_: DefaultContextDataDeserializer{}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key", Application_: "website", Environment_: "dev"}, cpHTTP{HttpMock{bodyString: cpBody(t)}})
	var value, err = client.GetContextData().Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := value.(jsonmodels.ContextData); !ok {
		t.Fatalf("GetContextData resolved to %T, want jsonmodels.ContextData", value)
	}
	value, err = client.GetContextPayload().Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload, ok = value.(jsonmodels.ContextPayload)
	if !ok || len(payload.Holdouts) != 1 || len(payload.Experiments) != 1 || payload.Experiments[0].HoldoutIds[0] != 11 {
		t.Fatalf("GetContextPayload resolved to %#v", value)
	}
}

// A deserializer without DeserializePayload still works: GetContextPayload
// falls back to its ContextData.
func TestClientGetContextPayloadFallsBackToDeserialize(t *testing.T) {
	var client = CreateClient(ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key", Application_: "website", Environment_: "dev"}, HttpMock{bodyString: "{}"})
	var value, err = client.GetContextPayload().Get(context.Background())
	assertAny(nil, err, t)
	assertAny(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{{}}}, value, t)
}

// The default provider asks its client for the payload when it can, and for
// the ContextData when the client has only GetContextData.
func TestDefaultContextDataProviderGetContextPayload(t *testing.T) {
	var client = CreateClient(ClientConfig{Deserializer_: DefaultContextDataDeserializer{}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key", Application_: "website", Environment_: "dev"}, cpHTTP{HttpMock{bodyString: cpBody(t)}})
	var value, err = CreateDefaultContextDataProvider(client).GetContextPayload().Get(context.Background())
	if _, ok := value.(jsonmodels.ContextPayload); !ok || err != nil {
		t.Fatalf("payload provider resolved to %T, %v", value, err)
	}
	value, err = CreateDefaultContextDataProvider(ClientABSMock{}).GetContextPayload().Get(context.Background())
	if _, ok := value.(jsonmodels.ContextData); !ok || err != nil {
		t.Fatalf("legacy client resolved to %T, %v", value, err)
	}
}

// ABSmartly.CreateContext with the default client serves the payload's
// holdouts; GetData still returns the v1.2.0 ContextData.
func TestCreateContextServesHoldoutsFromTheClient(t *testing.T) {
	var client = CreateClient(ClientConfig{Deserializer_: DefaultContextDataDeserializer{}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key", Application_: "website", Environment_: "dev"}, cpHTTP{HttpMock{bodyString: cpBody(t)}})
	var abs = Create(ABSmartlyConfig{Client_: client})
	var ctx = abs.CreateContext(cpContextConfig())
	ctx.WaitUntilReady()
	cpExpectHeldOut(t, ctx)
	var data, err = ctx.GetData()
	if err != nil {
		t.Fatal(err)
	}
	assertAny(cpHeldOut().ContextData(), data, t)
}

// CreateContextWithPayload serves holdouts; CreateContextWith, given the
// same experiments as ContextData, has none to serve.
func TestCreateContextWithPayload(t *testing.T) {
	var abs = Create(ABSmartlyConfig{Client_: ClientABSMock{}})
	cpExpectHeldOut(t, abs.CreateContextWithPayload(cpContextConfig(), cpHeldOut()))
	cpExpectServedWithoutHoldout(t, abs.CreateContextWith(cpContextConfig(), cpHeldOut().ContextData()))
}

// SetPayload installs holdouts; SetData installs the experiments without
// them.
func TestSetPayloadAndSetData(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil))
	h.ctx.SetPayload(cpHeldOut())
	cpExpectHeldOut(t, h.ctx)
	h = hoCreateFor(t, hoUID, hoData(nil))
	h.ctx.SetData(cpHeldOut().ContextData())
	cpExpectServedWithoutHoldout(t, h.ctx)
}

type cpDataOnlyProvider struct{ data jsonmodels.ContextData }

func (p cpDataOnlyProvider) GetContextData() *future.Future {
	var ft, done = future.New()
	done(p.data, nil)
	return ft
}

type cpPayloadProvider struct {
	cpDataOnlyProvider
	payload jsonmodels.ContextPayload
}

func (p cpPayloadProvider) GetContextPayload() *future.Future {
	var ft, done = future.New()
	done(p.payload, nil)
	return ft
}

// Refresh asks a ContextPayloadProvider for the payload, and a provider with
// only GetContextData for its ContextData.
func TestRefreshUsesThePayloadProvider(t *testing.T) {
	var payload = cpHeldOut()
	for name, tc := range map[string]struct {
		provider ContextDataProvider
		heldOut  bool
	}{
		"payload":   {cpPayloadProvider{cpDataOnlyProvider{payload.ContextData()}, payload}, true},
		"data only": {cpDataOnlyProvider{payload.ContextData()}, false},
	} {
		t.Run(name, func(t *testing.T) {
			var h = hoCreateFor(t, hoUID, hoData(nil))
			h.ctx.DataProvider_ = tc.provider
			h.ctx.Refresh()
			if tc.heldOut {
				cpExpectHeldOut(t, h.ctx)
			} else {
				cpExpectServedWithoutHoldout(t, h.ctx)
			}
		})
	}
}

// A plain ContextData installed over a payload removes the holdouts and the
// experiments' holdout and switchback fields: SetData installs exactly what
// it is given.
func TestSetDataOverAPayloadDropsTheHoldoutsAndSwitchbacks(t *testing.T) {
	var payload = cpHeldOut()
	payload.Experiments = append(payload.Experiments, sbDefault(2, sbHour))
	var h = hoCreateFor(t, hoUID, payload)
	var index = h.ctx.state().loadIndex()
	if len(index.holdouts) != 1 || index.experiments["exp_sb"].switchback == nil || index.experiments["exp"].payload == nil {
		t.Fatalf("fixture: payload not installed: %+v", index)
	}
	h.ctx.SetData(payload.ContextData())
	index = h.ctx.state().loadIndex()
	if len(index.holdouts) != 0 || index.experiments["exp_sb"].switchback != nil || index.experiments["exp"].payload != nil ||
		len(index.experiments["exp"].holdouts) != 0 {
		t.Fatalf("SetData kept payload fields: %+v", index)
	}
	cpExpectServedWithoutHoldout(t, h.ctx)
}

// The README's server-to-client handoff: GetPayload carries the holdouts, so
// a client context created from it holds the unit out as the server does;
// GetData, the v1.2.0 handoff, drops them.
func TestGetPayloadHandsOffHoldouts(t *testing.T) {
	var abs = Create(ABSmartlyConfig{Client_: ClientABSMock{}})
	var server = abs.CreateContextWithPayload(cpContextConfig(), cpHeldOut())
	var payload, err = server.GetPayload()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(payload, cpHeldOut()) {
		t.Fatalf("GetPayload = %+v, want the installed payload", payload)
	}
	cpExpectHeldOut(t, abs.CreateContextWithPayload(cpContextConfig(), payload))
	data, err := server.GetData()
	if err != nil {
		t.Fatal(err)
	}
	cpExpectServedWithoutHoldout(t, abs.CreateContextWith(cpContextConfig(), data))
}

// GetPayload after a plain ContextData is that data; after a failed load it
// is empty; before ready and after close it fails as GetData does; and the
// caller cannot edit the installed definitions through it.
func TestGetPayloadStates(t *testing.T) {
	var h = hoCreateFor(t, hoUID, cpHeldOut())
	var payload, _ = h.ctx.GetPayload()
	payload.Experiments[0].HoldoutIds[0] = 99
	payload.Experiments[0].Name = "edited"
	payload.Holdouts[0].Id = 99
	payload, _ = h.ctx.GetPayload()
	if payload.Experiments[0].Name != "exp" || payload.Holdouts[0].Id != 11 {
		t.Fatalf("GetPayload exposed the installed slices: %+v", payload)
	}
	h.ctx.SetData(cpHeldOut().ContextData())
	payload, _ = h.ctx.GetPayload()
	assertAny(jsonmodels.PayloadOf(cpHeldOut().ContextData()), payload, t)
	h.ctx.SetPayload(cpHeldOut())
	h.ctx.SetDataFailed(errors.New("load failed"))
	payload, err := h.ctx.GetPayload()
	assertAny(nil, err, t)
	assertAny(jsonmodels.ContextPayload{}, payload, t)

	var pending, _ = future.New()
	var notReady = CreateContext(internal.FixedClock{Millis_: hoMillis}, cpContextConfig(), pending, &hoProvider{}, &hoPublisher{}, nil,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	if _, err := notReady.GetPayload(); err != ErrContextNotReady {
		t.Fatalf("GetPayload before ready: %v", err)
	}
	var closed = hoCreateFor(t, hoUID, cpHeldOut())
	closed.ctx.Close()
	_, dataErr := closed.ctx.GetData()
	if _, err := closed.ctx.GetPayload(); err == nil || err.Error() != dataErr.Error() {
		t.Fatalf("GetPayload after close: %v, GetData: %v", err, dataErr)
	}
}

// setData keeps its own copy of the payload's experiments: editing the
// caller's slice after SetPayload changes nothing served.
func TestSetPayloadCopiesTheCallersExperiments(t *testing.T) {
	var data = sbData(sbDefault(2, sbHour))
	var h = sbCreate(t, sbData())
	h.ctx.SetPayload(data)
	data.Experiments[0].SwitchbackPeriodMs = nil
	data.Experiments[0].RandomizationType = ""
	// Period 1 draws arm 1; a unit experiment would draw otherwise or the
	// unservable schedule would give 0.
	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour+5).peek("exp_sb"))
	var payload, _ = h.ctx.GetPayload()
	if payload.Experiments[0].SwitchbackPeriodMs == nil || payload.Experiments[0].RandomizationType != "switchback" {
		t.Fatalf("the installed definition followed the caller's edit: %+v", payload.Experiments[0])
	}
}

// A copy of the context reads the live payload, as GetData does: data
// installed on the original after the copy is what the copy returns.
func TestGetPayloadThroughACopyReadsTheLiveData(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil))
	var copied = *h.ctx
	h.ctx.SetPayload(cpHeldOut())
	var payload, err = copied.GetPayload()
	if err != nil {
		t.Fatal(err)
	}
	assertAny(cpHeldOut(), payload, t)
	// A plain ContextData is read from the live context's Data_, not the
	// copy's.
	h.ctx.SetData(cpHeldOut().ContextData())
	payload, _ = copied.GetPayload()
	assertAny(jsonmodels.PayloadOf(cpHeldOut().ContextData()), payload, t)
}

// GetPayload returns the holdouts as installed, not the caller's slice, and
// keeps them when the payload has no experiments.
func TestGetPayloadKeepsTheInstalledHoldouts(t *testing.T) {
	var data = cpHeldOut()
	var h = hoCreateFor(t, hoUID, hoData(nil))
	h.ctx.SetPayload(data)
	data.Holdouts[0].Name = "edited"
	var payload, _ = h.ctx.GetPayload()
	expectValue(t, "holdout_a", payload.Holdouts[0].Name)

	h.ctx.SetPayload(hoData([]jsonmodels.Holdout{hoHoldoutA()}))
	payload, _ = h.ctx.GetPayload()
	if len(payload.Holdouts) != 1 || len(payload.Experiments) != 0 {
		t.Fatalf("payload without experiments: %+v", payload)
	}
}
