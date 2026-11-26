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
// ContextPayload (SetPayload, CreateContextWithPayload, DeserializeContextPayload
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
// holdouts, even with the default deserializer; the client's payload path
// resolves to the full payload.
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
	value, err = client.getContextData(true).Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var payload, ok = value.(jsonmodels.ContextPayload)
	if !ok || len(payload.Holdouts) != 1 || len(payload.Experiments) != 1 || payload.Experiments[0].HoldoutIds[0] != 11 {
		t.Fatalf("the payload path resolved to %#v", value)
	}
}

// A deserializer without DeserializePayload still works: the payload path
// falls back to its ContextData.
func TestClientGetContextPayloadFallsBackToDeserialize(t *testing.T) {
	var client = CreateClient(ClientConfig{Deserializer_: DeserMock{}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key", Application_: "website", Environment_: "dev"}, HttpMock{bodyString: "{}"})
	var value, err = client.getContextData(true).Get(context.Background())
	assertAny(nil, err, t)
	assertAny(jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{{}}}, value, t)
}

// The default provider asks its client for the payload when it can, and for
// the ContextData when the client has only GetContextData.
func TestDefaultContextDataProviderGetContextPayload(t *testing.T) {
	var client = CreateClient(ClientConfig{Deserializer_: DefaultContextDataDeserializer{}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key", Application_: "website", Environment_: "dev"}, cpHTTP{HttpMock{bodyString: cpBody(t)}})
	var value, err = contextPayloadFuture(CreateDefaultContextDataProvider(client)).Get(context.Background())
	if _, ok := value.(jsonmodels.ContextPayload); !ok || err != nil {
		t.Fatalf("payload provider resolved to %T, %v", value, err)
	}
	value, err = contextPayloadFuture(CreateDefaultContextDataProvider(ClientABSMock{})).Get(context.Background())
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

// GetPayload keeps an installed empty list empty, so a handed-off payload
// encodes as it was received ([] rather than null), and nil stays nil.
func TestGetPayloadKeepsEmptyListsEmpty(t *testing.T) {
	var h = hoCreateFor(t, hoUID, hoData(nil))
	h.ctx.SetPayload(jsonmodels.ContextPayload{Experiments: []jsonmodels.ExperimentData{}, Holdouts: []jsonmodels.Holdout{}})
	var payload, _ = h.ctx.GetPayload()
	var raw, _ = json.Marshal(payload)
	expectValue(t, `{"experiments":[]}`, string(raw))
	if payload.Holdouts == nil {
		t.Fatal("an installed empty holdouts list became nil")
	}
	h.ctx.SetPayload(jsonmodels.ContextPayload{})
	payload, _ = h.ctx.GetPayload()
	if payload.Experiments != nil || payload.Holdouts != nil {
		t.Fatalf("nil lists became non-nil: %#v", payload)
	}
}

func TestEmptyFullOnSwitchbackDoesNotDuplicateExposure(t *testing.T) {
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
	var exposures = queuedExposures(h.ctx)
	if len(exposures) != 1 {
		t.Fatalf("queued %d exposures, want 1: %+v", len(exposures), exposures)
	}
}

// A deserializer that embeds the default one and overrides only Deserialize
// keeps its override in use for every embedding shape: by value, by pointer,
// nested inside another wrapper, and with a pointer receiver. None of them
// opts into the payload, so a context loads what the override returns.
type cpValueEmbed struct {
	DefaultContextDataDeserializer
	calls *int
}

func (d cpValueEmbed) Deserialize([]byte) (jsonmodels.ContextData, error) {
	*d.calls++
	return jsonmodels.ContextData{}, errors.New("rejected by the override")
}

type cpPointerEmbed struct {
	*DefaultContextDataDeserializer
	calls *int
}

func (d cpPointerEmbed) Deserialize([]byte) (jsonmodels.ContextData, error) {
	*d.calls++
	return jsonmodels.ContextData{}, errors.New("rejected by the override")
}

type cpNestedEmbed struct{ cpValueEmbed }

type cpPointerReceiver struct {
	DefaultContextDataDeserializer
	calls int
}

func (d *cpPointerReceiver) Deserialize([]byte) (jsonmodels.ContextData, error) {
	d.calls++
	return jsonmodels.ContextData{}, errors.New("rejected by the override")
}

func TestEmbeddedDeserializerOverridesKeepRunning(t *testing.T) {
	var value, pointer, nested int
	var receiver = &cpPointerReceiver{}
	for name, tc := range map[string]struct {
		deserializer ContextDataDeserializer
		calls        func() int
	}{
		"value embed":      {cpValueEmbed{calls: &value}, func() int { return value }},
		"pointer embed":    {cpPointerEmbed{&DefaultContextDataDeserializer{}, &pointer}, func() int { return pointer }},
		"nested embed":     {cpNestedEmbed{cpValueEmbed{calls: &nested}}, func() int { return nested }},
		"pointer receiver": {receiver, func() int { return receiver.calls }},
	} {
		t.Run(name, func(t *testing.T) {
			var client = CreateClient(ClientConfig{Deserializer_: tc.deserializer, Endpoint_: "https://localhost/v1",
				ApiKey_: "key"}, cpHTTP{HttpMock{bodyString: cpBody(t)}})
			var ctx = Create(ABSmartlyConfig{Client_: client}).CreateContext(cpContextConfig())
			ctx.WaitUntilReady()
			if !ctx.IsFailed() || tc.calls() != 1 {
				t.Fatalf("failed %v after %d override calls; want the override's error after 1", ctx.IsFailed(), tc.calls())
			}
			ctx.Refresh()
			if tc.calls() != 2 {
				t.Fatalf("refresh made %d override calls in all, want 2", tc.calls())
			}
		})
	}
}

// A deserializer that embeds the default one and declares DeserializePayload
// opts in: its payload method, not the promoted Deserialize, loads the
// context, so its holdouts are served.
type cpExplicitPayload struct {
	DefaultContextDataDeserializer
	calls *int
}

func (d cpExplicitPayload) DeserializePayload(bytes []byte) (jsonmodels.ContextPayload, error) {
	*d.calls++
	return DeserializeContextPayload(bytes)
}

func TestEmbeddedDeserializerOptsInByDeclaringDeserializePayload(t *testing.T) {
	var calls int
	var client = CreateClient(ClientConfig{Deserializer_: cpExplicitPayload{calls: &calls}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key"}, cpHTTP{HttpMock{bodyString: cpBody(t)}})
	var ctx = Create(ABSmartlyConfig{Client_: client}).CreateContext(cpContextConfig())
	ctx.WaitUntilReady()
	expectInt(t, 1, calls)
	cpExpectHeldOut(t, ctx)
}

// A provider or client that embeds the default one and overrides only
// GetContextData keeps that override in use on creation and refresh, by
// value or pointer: the default's payload path is not promoted.
type cpProviderEmbed struct {
	DefaultContextDataProvider
	calls *int
}

func (p cpProviderEmbed) GetContextData() *future.Future {
	*p.calls++
	var ft, done = future.New()
	done(cpHeldOut().ContextData(), nil)
	return ft
}

type cpProviderPointerEmbed struct {
	*DefaultContextDataProvider
	calls *int
}

func (p cpProviderPointerEmbed) GetContextData() *future.Future {
	*p.calls++
	var ft, done = future.New()
	done(cpHeldOut().ContextData(), nil)
	return ft
}

type cpClientEmbed struct {
	Client
	calls *int
}

func (c cpClientEmbed) GetContextData() *future.Future {
	*c.calls++
	var ft, done = future.New()
	done(cpHeldOut().ContextData(), nil)
	return ft
}

type cpClientPointerEmbed struct {
	*Client
	calls *int
}

func (c cpClientPointerEmbed) GetContextData() *future.Future {
	*c.calls++
	var ft, done = future.New()
	done(cpHeldOut().ContextData(), nil)
	return ft
}

func TestEmbeddedProviderOverridesKeepRunning(t *testing.T) {
	var client = CreateClient(ClientConfig{Deserializer_: DefaultContextDataDeserializer{}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key"}, cpHTTP{HttpMock{bodyString: cpBody(t)}})
	var provider, providerPointer, embeddedClient, clientPointer int
	for name, tc := range map[string]struct {
		config ABSmartlyConfig
		calls  *int
	}{
		"provider value embed":   {ABSmartlyConfig{Client_: client, ContextDataProvider_: cpProviderEmbed{CreateDefaultContextDataProvider(client), &provider}}, &provider},
		"provider pointer embed": {ABSmartlyConfig{Client_: client, ContextDataProvider_: cpProviderPointerEmbed{&DefaultContextDataProvider{client_: client}, &providerPointer}}, &providerPointer},
		"client value embed":     {ABSmartlyConfig{Client_: cpClientEmbed{client, &embeddedClient}}, &embeddedClient},
		"client pointer embed":   {ABSmartlyConfig{Client_: cpClientPointerEmbed{&client, &clientPointer}}, &clientPointer},
	} {
		t.Run(name, func(t *testing.T) {
			var ctx = Create(tc.config).CreateContext(cpContextConfig())
			ctx.WaitUntilReady()
			if *tc.calls != 1 {
				t.Fatalf("creation made %d override calls, want 1", *tc.calls)
			}
			// The override's plain ContextData has no holdouts to serve.
			cpExpectServedWithoutHoldout(t, ctx)
			ctx.Refresh()
			if *tc.calls != 2 {
				t.Fatalf("refresh made %d override calls in all, want 2", *tc.calls)
			}
		})
	}
}

// The default provider and client, by value or pointer, give the payload.
func TestDefaultProviderAndClientGiveThePayload(t *testing.T) {
	var client = CreateClient(ClientConfig{Deserializer_: DefaultContextDataDeserializer{}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key"}, cpHTTP{HttpMock{bodyString: cpBody(t)}})
	var provider = CreateDefaultContextDataProvider(client)
	for name, source := range map[string]ContextDataProvider{
		"provider": provider, "provider pointer": &provider, "client": client, "client pointer": &client,
	} {
		var value, err = contextPayloadFuture(source).Get(context.Background())
		if _, ok := value.(jsonmodels.ContextPayload); !ok || err != nil {
			t.Errorf("%s: resolved to %T, %v; want a ContextPayload", name, value, err)
		}
	}
}

// An initial data future that has already settled with a value that is not
// context data fails the context with a TypeError, as a pending one does.
func TestSettledInvalidDataFutureFailsTheContext(t *testing.T) {
	var logger = &hoLogger{}
	var errorsLogged []interface{}
	logger.onEvent = func(event EventType, data interface{}) {
		if event == Error {
			errorsLogged = append(errorsLogged, data)
		}
	}
	var settled, done = future.New()
	done(nil, nil)
	var ctx = CreateContext(internal.FixedClock{Millis_: hoMillis}, cpContextConfig(), settled, &hoProvider{}, &hoPublisher{}, logger,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	if !ctx.IsReady() || !ctx.IsFailed() {
		t.Fatalf("ready %v, failed %v; want a failed context", ctx.IsReady(), ctx.IsFailed())
	}
	if len(errorsLogged) != 1 {
		t.Fatalf("logged %d errors, want 1: %v", len(errorsLogged), errorsLogged)
	}
	var err, ok = errorsLogged[0].(*TypeError)
	if !ok || err.Error() != "type assertion failed: expected jsonmodels.ContextData or jsonmodels.ContextPayload, got <nil>" {
		t.Fatalf("logged %#v", errorsLogged[0])
	}
}
