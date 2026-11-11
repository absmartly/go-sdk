package sdk

import (
	"encoding/json"
	"testing"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// systemAttrClient is a ClientI that also exposes the application and
// environment names, like the default Client. The context reads them through
// the optional clientSystemAttributes interface and injects them into the
// attribute map audiences are evaluated against.
type systemAttrClient struct {
	application string
	environment string
	published   []jsonmodels.PublishEvent
}

func (c *systemAttrClient) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) { return jsonmodels.ContextData{}, nil })
}

func (c *systemAttrClient) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		c.published = append(c.published, event)
		return nil, nil
	})
}

func (c *systemAttrClient) GetApplication() string { return c.application }
func (c *systemAttrClient) GetEnvironment() string { return c.environment }

// audienceExperiment builds an experiment whose variant is fixed to
// fullOnVariant when the audience matches, so audience evaluation is the only
// thing under test. With audienceStrict true, an audience mismatch forces
// variant 0 before the full-on variant is applied.
func audienceExperiment(name, audience string, strict bool) jsonmodels.Experiment {
	return jsonmodels.Experiment{
		Id:             1,
		Name:           name,
		UnitType:       "session_id",
		Iteration:      1,
		Split:          []float64{0.5, 0.5},
		TrafficSplit:   []float64{0.0, 1.0},
		FullOnVariant:  1,
		Applications:   []jsonmodels.ExperimentApplication{{Name: "website"}},
		Variants:       []jsonmodels.ExperimentVariant{{Name: "A"}, {Name: "B"}},
		AudienceStrict: strict,
		Audience:       audience,
	}
}

// newSystemAttrContext creates a ready context through the full ABsmartly path,
// so application/environment are plumbed from the client exactly as in
// production.
func newSystemAttrContext(application, environment string, experiments ...jsonmodels.Experiment) (*Context, *systemAttrClient) {
	var client = &systemAttrClient{application: application, environment: environment}
	var sdkConfig = ABsmartlyConfig{
		Client_:              client,
		ContextDataProvider_: DefaultContextDataProvider{client_: client},
		ContextEventHandler_: DefaultContextEventHandler{client_: client},
	}
	var abs = Create(sdkConfig)
	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{"session_id": "e791e240fcd3df7d238cfc285f475e8152fcc0ec"}
	config.PublishDelay_ = -1
	// No refresh timer: these contexts are not registered with the shared
	// stopLeakedTimers helper, and a timer that fires later would hit the
	// documented pre-existing Index_/GetExperiment race under -race.
	config.RefreshInterval_ = -1
	var ctx = abs.CreateContextWith(config, jsonmodels.ContextData{Experiments: experiments})
	ctx.WaitUntilReady()
	return ctx, client
}

const (
	envFilter    = `{"filter":[{"eq":[{"var":"environment"},{"value":"production"}]}]}`
	envFilterStg = `{"filter":[{"eq":[{"var":"environment"},{"value":"staging"}]}]}`
	appFilter    = `{"filter":[{"eq":[{"var":"application"},{"value":"website"}]}]}`
)

// §4.1: a strict audience on environment matches the configured environment.
func TestSystemAttributes_StrictEnvironmentMatches(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_env", envFilter, true))

	var variant, err = ctx.GetTreatment("exp_env")
	assertAny(nil, err, t)
	assertAny(1, variant, t)

	var assignment = ctx.GetAssignment("exp_env")
	assertAny(false, assignment.AudienceMismatch, t)
}

// §4.2: a strict audience on application matches the configured application.
func TestSystemAttributes_StrictApplicationMatches(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", appFilter, true))

	var variant, err = ctx.GetTreatment("exp_app")
	assertAny(nil, err, t)
	assertAny(1, variant, t)

	var assignment = ctx.GetAssignment("exp_app")
	assertAny(false, assignment.AudienceMismatch, t)
}

// §4.3: a strict audience that does not match the configured environment
// serves variant 0 with an audience mismatch.
func TestSystemAttributes_NoMatch(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_env", envFilterStg, true))

	var variant, err = ctx.GetTreatment("exp_env")
	assertAny(nil, err, t)
	assertAny(0, variant, t)

	var assignment = ctx.GetAssignment("exp_env")
	assertAny(true, assignment.AudienceMismatch, t)
}

// §4.4: a user-set attribute overrides the system value, even when set after
// the context was created, and invalidates an assignment cached before it.
func TestSystemAttributes_UserValueOverrides(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_env", envFilterStg, true))

	var before = ctx.GetAssignment("exp_env")
	assertAny(0, before.Variant, t)
	assertAny(true, before.AudienceMismatch, t)

	if err := ctx.SetAttribute("environment", "staging"); err != nil {
		t.Fatalf("SetAttribute failed: %v", err)
	}

	var after = ctx.GetAssignment("exp_env")
	assertAny(1, after.Variant, t)
	assertAny(false, after.AudienceMismatch, t)
}

// §4.4 (second half): a production filter no longer matches once the user has
// set environment to staging.
func TestSystemAttributes_UserValueBreaksProductionFilter(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_env", envFilter, true))

	var before = ctx.GetAssignment("exp_env")
	assertAny(1, before.Variant, t)

	if err := ctx.SetAttribute("environment", "staging"); err != nil {
		t.Fatalf("SetAttribute failed: %v", err)
	}

	var after = ctx.GetAssignment("exp_env")
	assertAny(0, after.Variant, t)
	assertAny(true, after.AudienceMismatch, t)
}

// §4.6: when no environment is configured, the key is absent from the map, a
// filter on environment does not match, and nothing crashes.
func TestSystemAttributes_MissingEnvironmentConfig(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "",
		audienceExperiment("exp_env", envFilter, true))

	var variant, err = ctx.GetTreatment("exp_env")
	assertAny(nil, err, t)
	assertAny(0, variant, t)

	var assignment = ctx.GetAssignment("exp_env")
	assertAny(true, assignment.AudienceMismatch, t)

	ctx.ContextLock_.RLock()
	var attrs = ctx.evaluationAttributes()
	ctx.ContextLock_.RUnlock()
	if _, present := attrs["environment"]; present {
		t.Errorf("evaluation map has an environment key when none is configured: %v", attrs)
	}
	if got := attrs["application"]; got != "website" {
		t.Errorf("evaluation map application = %v, want website", got)
	}
}

// §4.7: nothing extra is published. The serialized payload carries no
// application/environment/app_version attribute entries; the collector adds
// those server-side.
func TestSystemAttributes_NothingExtraPublished(t *testing.T) {
	var ctx, client = newSystemAttrContext("website", "production",
		audienceExperiment("exp_env", envFilter, true))

	if _, err := ctx.GetTreatment("exp_env"); err != nil {
		t.Fatalf("GetTreatment failed: %v", err)
	}
	if err := ctx.Publish(); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	if len(client.published) != 1 {
		t.Fatalf("expected exactly one publish, got %d", len(client.published))
	}

	var serializer = DefaultContextEventSerializer{}
	var body, err = serializer.Serialize(client.published[0])
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}
	var payload struct {
		Attributes []struct {
			Name string `json:"name"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	for _, attr := range payload.Attributes {
		switch attr.Name {
		case "application", "environment", "app_version", "application_version":
			t.Errorf("publish payload must not include system attribute %q: %s", attr.Name, string(body))
		}
	}
}

// §4.7 (user-set still published): a user-set attribute named like a system
// one is still published, because the user set it explicitly.
func TestSystemAttributes_UserSetAttributeStillPublished(t *testing.T) {
	var ctx, client = newSystemAttrContext("website", "production",
		audienceExperiment("exp_env", envFilter, true))

	if err := ctx.SetAttribute("environment", "staging"); err != nil {
		t.Fatalf("SetAttribute failed: %v", err)
	}
	if _, err := ctx.GetTreatment("exp_env"); err != nil {
		t.Fatalf("GetTreatment failed: %v", err)
	}
	if err := ctx.Publish(); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	var serializer = DefaultContextEventSerializer{}
	var body, _ = serializer.Serialize(client.published[0])
	var payload struct {
		Attributes []struct {
			Name  string      `json:"name"`
			Value interface{} `json:"value"`
		} `json:"attributes"`
	}
	_ = json.Unmarshal(body, &payload)
	var found bool
	for _, attr := range payload.Attributes {
		if attr.Name == "environment" {
			found = true
			assertAny("staging", attr.Value, t)
		}
	}
	if !found {
		t.Errorf("a user-set environment attribute must be published: %s", string(body))
	}
}

// §4.8: app_version is never injected, so a filter on app_version does not
// match even though an application version is configured on the client (the Go
// SDK sends X-Application-Version: 0 but never evaluates against it).
func TestSystemAttributes_AppVersionGone(t *testing.T) {
	var appVersionFilter = `{"filter":[{"eq":[{"var":"app_version"},{"value":"0"}]}]}`
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_ver", appVersionFilter, true))

	var variant, err = ctx.GetTreatment("exp_ver")
	assertAny(nil, err, t)
	assertAny(0, variant, t)

	var assignment = ctx.GetAssignment("exp_ver")
	assertAny(true, assignment.AudienceMismatch, t)

	ctx.ContextLock_.RLock()
	var attrs = ctx.evaluationAttributes()
	ctx.ContextLock_.RUnlock()
	for _, name := range []string{"app_version", "application_version"} {
		if _, present := attrs[name]; present {
			t.Errorf("evaluation map must not contain %q: %v", name, attrs)
		}
	}
}

// A custom ClientI that does not expose application/environment contributes no
// system attributes, keeping the public ClientI interface backward compatible.
func TestSystemAttributes_LegacyClientNoSystemAttributes(t *testing.T) {
	var config = ABsmartlyConfig{Client_: ClientABSMock{}}
	var abs = Create(config)
	var contextConfig = ContextConfig{Units_: map[string]string{"user_id": "1234567"}}
	var ctx = abs.CreateContextWith(contextConfig, contextData)

	ctx.ContextLock_.RLock()
	var attrs = ctx.evaluationAttributes()
	ctx.ContextLock_.RUnlock()
	if len(attrs) != 0 {
		t.Errorf("a legacy client must contribute no system attributes, got %v", attrs)
	}
}

// readyAssignmentLogger evaluates an experiment's assignment from inside the
// Ready callback, which CreateContextWith delivers synchronously during
// construction. It records the variant it saw.
type readyAssignmentLogger struct {
	experiment string
	variant    int
	seen       bool
}

func (l *readyAssignmentLogger) HandleEvent(ctx Context, event EventType, data interface{}) {
	if event == Ready {
		l.variant = ctx.GetAssignment(l.experiment).Variant
		l.seen = true
	}
}

// A Ready logger that evaluates a strict environment audience during
// construction must already see the system attributes, so it caches the correct
// variant. CreateContextWith settles the data future before CreateContext runs,
// so the Ready callback fires synchronously; the system values have to be set
// on the context before the listener is attached, not afterwards.
func TestSystemAttributes_ReadyLoggerSeesSystemAttributes(t *testing.T) {
	var logger = &readyAssignmentLogger{experiment: "exp_env"}
	var client = &systemAttrClient{application: "website", environment: "production"}
	var sdkConfig = ABsmartlyConfig{
		Client_:              client,
		ContextDataProvider_: DefaultContextDataProvider{client_: client},
		ContextEventHandler_: DefaultContextEventHandler{client_: client},
	}
	var abs = Create(sdkConfig)

	var config = CreateDefaultContextConfig()
	config.Units_ = map[string]string{"session_id": "e791e240fcd3df7d238cfc285f475e8152fcc0ec"}
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	config.EventLogger_ = logger

	var data = jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		audienceExperiment("exp_env", envFilter, true),
	}}
	var ctx = abs.CreateContextWith(config, data)
	ctx.WaitUntilReady()

	if !logger.seen {
		t.Fatal("Ready logger did not run")
	}
	// The logger evaluates its audience synchronously during construction.
	assertAny(1, logger.variant, t)

	assertAny(1, ctx.GetAssignment("exp_env").Variant, t)
}
