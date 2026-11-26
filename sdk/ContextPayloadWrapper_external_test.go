package sdk_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/absmartly/go-sdk/sdk"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// A customer's client wrapper, outside the package, that decorates the SDK
// client: overriding only GetContextData keeps v1.2.0 behaviour (no
// holdouts), and declaring GetContextPayload that delegates through
// sdk.ContextPayloadFuture serves the payload's holdouts.
type decoratingClient struct {
	sdk.Client
	fetches *int
}

func (c decoratingClient) GetContextData() *future.Future {
	*c.fetches++
	return c.Client.GetContextData()
}

type payloadDecoratingClient struct{ decoratingClient }

func (c payloadDecoratingClient) GetContextPayload() *future.Future {
	*c.fetches++
	return sdk.ContextPayloadFuture(c.Client)
}

func TestExternalClientWrapperOptsInThroughContextPayloadFuture(t *testing.T) {
	var payload = jsonmodels.ContextPayload{
		Experiments: []jsonmodels.ExperimentData{{Experiment: jsonmodels.Experiment{
			Id: 1, Name: "exp", UnitType: "session_id", Iteration: 1, SeedHi: 100, SeedLo: 200,
			Split: []float64{0.5, 0.5}, TrafficSeedHi: 1, TrafficSeedLo: 2, TrafficSplit: []float64{0, 1},
			Applications: []jsonmodels.ExperimentApplication{{Name: "website"}},
			Variants:     []jsonmodels.ExperimentVariant{{Name: "A"}, {Name: "B"}},
		}, HoldoutIds: []int{11}}},
		Holdouts: []jsonmodels.Holdout{{Id: 11, Name: "holdout_a", UnitType: "session_id", Iteration: 1,
			SeedHi: 0, SeedLo: 0, Split: []float64{1, 0}, HoldoutType: "full"}},
	}
	var body, _ = json.Marshal(payload)
	var server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	var client = sdk.CreateDefaultClient(sdk.ClientConfig{Endpoint_: server.URL, ApiKey_: "key",
		Application_: "website", Environment_: "dev"})

	for name, tc := range map[string]struct {
		wrap        func(*int) sdk.ClientI
		wantHoldout bool
	}{
		"legacy override":    {func(n *int) sdk.ClientI { return decoratingClient{client, n} }, false},
		"payload delegation": {func(n *int) sdk.ClientI { return payloadDecoratingClient{decoratingClient{client, n}} }, true},
	} {
		t.Run(name, func(t *testing.T) {
			var fetches int
			var config = sdk.CreateDefaultContextConfig()
			config.Units_["session_id"] = "uid"
			config.PublishDelay_, config.RefreshInterval_ = -1, -1
			var ctx = sdk.Create(sdk.ABSmartlyConfig{Client_: tc.wrap(&fetches)}).CreateContext(config)
			ctx.WaitUntilReady()
			if fetches != 1 || ctx.IsFailed() {
				t.Fatalf("fetches %d, failed %v; want the wrapper called once", fetches, ctx.IsFailed())
			}
			var got, _ = ctx.GetPayload()
			if (len(got.Holdouts) == 1) != tc.wantHoldout {
				t.Fatalf("installed %d holdouts, want holdout %v", len(got.Holdouts), tc.wantHoldout)
			}
		})
	}
}
