package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
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
