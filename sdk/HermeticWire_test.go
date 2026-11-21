package sdk

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// The GET carries the same SDK headers as the PUT (X-API-Key, X-Agent), as
// javascript-sdk does on every request (cross-SDK scenario 320).
func TestHermeticWireContract(t *testing.T) {
	const (
		appName = "website"
		envName = "development"
		apiKey  = "test-api-key"
	)

	var (
		mu sync.Mutex

		getSeen  bool
		getPath  string
		getQuery url.Values
		getKey   string
		getAgent string

		putSeen    bool
		putMethod  string
		putPath    string
		putHeaders http.Header
		putBody    map[string]interface{}
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			mu.Lock()
			getSeen = true
			getPath = r.URL.Path
			getQuery = r.URL.Query()
			getKey = r.Header.Get("X-API-Key")
			getAgent = r.Header.Get("X-Agent")
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"experiments":[]}`))

		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			parsed := map[string]interface{}{}
			_ = json.Unmarshal(body, &parsed)

			mu.Lock()
			putSeen = true
			putMethod = r.Method
			putPath = r.URL.Path
			putHeaders = r.Header.Clone()
			putBody = parsed
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	sdk, err := NewBuilder().
		Endpoint(server.URL).
		ApiKey(apiKey).
		Application(appName).
		Environment(envName).
		Build()
	if err != nil {
		t.Fatalf("Failed to build SDK: %v", err)
	}

	// PublishDelay_ -1 turns off automatic publishing, so the explicit Publish
	// below is the only publish and the test waits for its request.
	ctx := sdk.CreateContext(ContextConfig{
		Units_: map[string]string{
			"session_id": "test-session-123",
		},
		PublishDelay_: -1,
	})
	if ctx == nil {
		t.Fatal("Expected context to be created")
	}

	ctx.WaitUntilReady()
	if !ctx.IsReady() {
		t.Fatal("Expected context to be ready after WaitUntilReady")
	}

	mu.Lock()
	if !getSeen {
		mu.Unlock()
		t.Fatal("Expected a GET /context request to have been received")
	}
	if getPath != "/context" {
		t.Errorf("GET path = %q, want %q", getPath, "/context")
	}
	if got := getQuery.Get("application"); got != appName {
		t.Errorf("GET query application = %q, want %q", got, appName)
	}
	if got := getQuery.Get("environment"); got != envName {
		t.Errorf("GET query environment = %q, want %q", got, envName)
	}
	if getKey != apiKey {
		t.Errorf("GET X-API-Key = %q, want %q", getKey, apiKey)
	}
	if getAgent != SDKAgent {
		t.Errorf("GET X-Agent = %q, want %q", getAgent, SDKAgent)
	}
	mu.Unlock()

	if _, err := ctx.GetTreatment("exp_test"); err != nil {
		t.Fatalf("GetTreatment failed: %v", err)
	}
	if err := ctx.Track("payment", map[string]interface{}{"amount": 100}); err != nil {
		t.Fatalf("Track failed: %v", err)
	}

	if err := ctx.Publish(); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !putSeen {
		t.Fatal("Expected a PUT /context request to have been received")
	}
	if putMethod != http.MethodPut {
		t.Errorf("publish method = %q, want PUT", putMethod)
	}
	if putPath != "/context" {
		t.Errorf("PUT path = %q, want %q", putPath, "/context")
	}

	wantHeaders := map[string]string{
		"X-API-Key":             apiKey,
		"X-Application":         appName,
		"X-Environment":         envName,
		"X-Application-Version": "0",
		"Content-Type":          "application/json",
	}
	for name, want := range wantHeaders {
		if got := putHeaders.Get(name); got != want {
			t.Errorf("PUT header %s = %q, want %q", name, got, want)
		}
	}
	if got := putHeaders.Get("X-Agent"); got != "absmartly-go-sdk/"+SDKVersion {
		t.Errorf("PUT header X-Agent = %q, want %q", got, "absmartly-go-sdk/"+SDKVersion)
	}

	for _, field := range []string{"hashed", "units", "publishedAt"} {
		if _, ok := putBody[field]; !ok {
			t.Errorf("PUT body missing required field %q (body=%v)", field, putBody)
		}
	}
	if _, ok := putBody["exposures"]; !ok {
		t.Error("PUT body missing exposures (a treatment was requested)")
	}
	if _, ok := putBody["goals"]; !ok {
		t.Error("PUT body missing goals (a goal was tracked)")
	}
}

func TestHermeticWireContractSuppressedHoldoutExposure(t *testing.T) {
	const payload = `{"experiments":[{"id":1,"name":"covered_experiment","unitType":"session_id","iteration":1,
		"seedHi":100,"seedLo":200,"split":[0,1],"trafficSeedHi":1,"trafficSeedLo":2,"trafficSplit":[0,1],
		"variants":[{"name":"A","config":"{\"v\":\"control\"}"},{"name":"B","config":"{\"v\":\"treatment\"}"}],"holdoutIds":[11]}],
		"holdouts":[{"id":11,"name":"wire_holdout","unitType":"session_id","iteration":1,"seedHi":13,"seedLo":111,"split":[1,0]}]}`
	var mu sync.Mutex
	var published []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/context" {
			t.Errorf("request path = %q, want /context", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = io.WriteString(w, payload)
		case http.MethodPut:
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode published JSON: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			published = append(published, body)
			mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	sdk, err := NewBuilder().Endpoint(server.URL).ApiKey("test-api-key").
		Application("website").Environment("development").Build()
	if err != nil {
		t.Fatal(err)
	}
	ctx := sdk.CreateContext(ContextConfig{
		Units_: map[string]string{"session_id": "wire-user"}, PublishDelay_: -1, RefreshInterval_: -1,
	})
	t.Cleanup(func() { ctx.ClearTimeout(); ctx.ClearRefreshTimer() })
	ctx.WaitUntilReady()
	if !ctx.IsReady() || ctx.IsFailed() {
		t.Fatal("HTTP context did not become ready")
	}
	if variant, err := ctx.GetTreatment("covered_experiment"); err != nil || variant != 0 {
		t.Fatalf("suppressed treatment = %d, %v; want 0", variant, err)
	}
	if value, err := ctx.GetVariableValue("v", "default"); err != nil || value != "control" {
		t.Fatalf("suppressed variable = %v, %v; want control", value, err)
	}
	if err := ctx.Publish(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(published) != 1 {
		t.Fatalf("PUT requests = %d, want 1", len(published))
	}
	exposures, ok := published[0]["exposures"].([]interface{})
	if !ok || len(exposures) != 1 {
		t.Fatalf("wire exposures = %+v, want exactly one holdout record", published[0]["exposures"])
	}
	exposure, ok := exposures[0].(map[string]interface{})
	if !ok {
		t.Fatalf("wire exposure is not an object: %+v", exposures[0])
	}
	for field, want := range map[string]interface{}{
		"id": float64(11), "name": "wire_holdout", "unit": "session_id", "variant": float64(0),
		"assigned": true, "eligible": true, "overridden": false, "ruleOverride": false,
	} {
		if got := exposure[field]; got != want {
			t.Errorf("wire exposure %s = %v, want %v (record=%+v)", field, got, want, exposure)
		}
	}
}
