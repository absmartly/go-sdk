package sdk

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func buildAgainst(t *testing.T, server *httptest.Server, configure func(*ABsmartlyBuilder) *ABsmartlyBuilder) ABsmartly {
	t.Helper()
	var builder = NewBuilder().
		Endpoint(server.URL + "/v1").
		ApiKey("test-api-key").
		Application("website").
		Environment("development")
	abs, err := configure(builder).Build()
	if err != nil {
		t.Fatalf("Failed to build SDK: %v", err)
	}
	return abs
}

func contextConfigForTest(session string) ContextConfig {
	return ContextConfig{Units_: map[string]string{"session_id": session}}
}

func TestBuilderIntegrationContextCreation(t *testing.T) {
	var requests int32
	var server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"experiments":[{"id":1,"name":"exp_a","unitType":"session_id","iteration":1,"seedHi":1,"seedLo":2,"split":[0.5,0.5],"trafficSeedHi":3,"trafficSeedLo":4,"trafficSplit":[0,1],"fullOnVariant":0,"variants":[{"name":"A","config":null},{"name":"B","config":null}]}]}`))
	}))
	defer server.Close()

	var abs = buildAgainst(t, server, func(b *ABsmartlyBuilder) *ABsmartlyBuilder { return b })
	var ctx = abs.CreateContext(contextConfigForTest("test-session-123"))
	defer ctx.Close()
	ctx.WaitUntilReady()

	if !ctx.IsReady() || ctx.IsFailed() {
		t.Fatalf("context not ready: ready=%v failed=%v", ctx.IsReady(), ctx.IsFailed())
	}
	experiments, err := ctx.GetExperiments()
	assertAny(nil, err, t)
	assertAny([]string{"exp_a"}, experiments, t)
	assertAny(int32(1), atomic.LoadInt32(&requests), t)
}

func TestBuilderIntegrationRetries(t *testing.T) {
	var requests int32
	var server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var abs = buildAgainst(t, server, func(b *ABsmartlyBuilder) *ABsmartlyBuilder { return b.Retries(2) })
	var ctx = abs.CreateContext(contextConfigForTest("test-session-456"))
	defer ctx.Close()
	ctx.WaitUntilReady()

	assertAny(true, ctx.IsFailed(), t)
	assertAny(int32(3), atomic.LoadInt32(&requests), t)
}

func TestBuilderIntegrationTimeout(t *testing.T) {
	var server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"experiments":[]}`))
	}))
	defer server.Close()

	t.Run("raised timeout lets a slow response through", func(t *testing.T) {
		var abs = buildAgainst(t, server, func(b *ABsmartlyBuilder) *ABsmartlyBuilder {
			return b.Timeout(5 * time.Second).Retries(0)
		})
		var ctx = abs.CreateContext(contextConfigForTest("slow-ok"))
		defer ctx.Close()
		ctx.WaitUntilReady()
		assertAny(false, ctx.IsFailed(), t)
	})

	t.Run("lowered timeout fails a slow response", func(t *testing.T) {
		var abs = buildAgainst(t, server, func(b *ABsmartlyBuilder) *ABsmartlyBuilder {
			return b.Timeout(200 * time.Millisecond).Retries(0)
		})
		var ctx = abs.CreateContext(contextConfigForTest("slow-fail"))
		defer ctx.Close()
		ctx.WaitUntilReady()
		assertAny(true, ctx.IsFailed(), t)
	})
}

func TestBuilderBackwardsCompatibility(t *testing.T) {
	clientConfig := ClientConfig{
		Endpoint_:    "https://sandbox.absmartly.io/v1",
		ApiKey_:      "test-api-key",
		Application_: "website",
		Environment_: "development",
	}

	sdkConfig := ABsmartlyConfig{
		Client_: CreateDefaultClient(clientConfig),
	}

	sdkOldWay := Create(sdkConfig)

	sdkNewWay, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Application("website").
		Environment("development").
		Build()

	if err != nil {
		t.Fatalf("Failed to build SDK: %v", err)
	}

	if sdkOldWay.Client_ == nil || sdkNewWay.Client_ == nil {
		t.Error("Both initialization methods should create valid SDK instances")
	}

	if sdkOldWay.ContextDataProvider_ == nil || sdkNewWay.ContextDataProvider_ == nil {
		t.Error("Both initialization methods should initialize ContextDataProvider")
	}

	if sdkOldWay.ContextEventHandler_ == nil || sdkNewWay.ContextEventHandler_ == nil {
		t.Error("Both initialization methods should initialize ContextEventHandler")
	}
}
