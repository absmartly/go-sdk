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

// TestHermeticWireContract drives the PUBLIC SDK API against a local
// httptest.Server so the SDK's REAL HTTP client makes real GET /context
// (refresh -> ready) and PUT /context (publish) requests. It asserts the
// wire contract documented in /tmp/absmartly-wire-contract.md.
//
// Per the contract, Go sends NO auth headers on GET (auth is via query
// params), so API-key headers are only asserted on the PUT.
func TestHermeticWireContract(t *testing.T) {
	const (
		appName = "website"
		envName = "development"
		apiKey  = "test-api-key"
	)

	var (
		mu sync.Mutex

		getSeen   bool
		getPath   string
		getQuery  url.Values
		getHasKey bool // X-API-Key present on GET (should be false for Go)

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
			getHasKey = r.Header.Get("X-API-Key") != ""
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

	// Configure the SDK's public client with endpoint = local server base.
	sdk, err := NewBuilder().
		Endpoint(server.URL).
		ApiKey(apiKey).
		Application(appName).
		Environment(envName).
		Build()
	if err != nil {
		t.Fatalf("Failed to build SDK: %v", err)
	}

	ctx := sdk.CreateContext(ContextConfig{
		Units_: map[string]string{
			"session_id": "test-session-123",
		},
	})
	if ctx == nil {
		t.Fatal("Expected context to be created")
	}

	// Drives the real GET /context and blocks until the context is ready.
	ctx.WaitUntilReady()
	if !ctx.IsReady() {
		t.Fatal("Expected context to be ready after WaitUntilReady")
	}

	// --- Assert GET /context ---
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
	if getHasKey {
		t.Error("GET must NOT send X-API-Key header (Go auths via query params)")
	}
	mu.Unlock()

	// Queue an exposure (treatment) and a goal (track) so publish has a payload.
	if _, err := ctx.GetTreatment("exp_test"); err != nil {
		t.Fatalf("GetTreatment failed: %v", err)
	}
	if err := ctx.Track("payment", map[string]interface{}{"amount": 100}); err != nil {
		t.Fatalf("Track failed: %v", err)
	}

	// Drives the real PUT /context.
	if err := ctx.Publish(); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	// --- Assert PUT /context ---
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
	if got := putHeaders.Get("X-Agent"); got == "" {
		t.Error("PUT header X-Agent must be present and non-empty")
	}

	// Body fields per contract.
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
