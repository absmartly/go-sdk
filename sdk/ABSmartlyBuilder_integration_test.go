package sdk

import (
	"testing"
	"time"
)

func TestBuilderIntegrationContextCreation(t *testing.T) {
	sdk, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Application("website").
		Environment("development").
		Build()

	if err != nil {
		t.Fatalf("Failed to build SDK: %v", err)
	}

	contextConfig := ContextConfig{
		Units_: map[string]string{
			"session_id": "test-session-123",
		},
	}

	ctx := sdk.CreateContext(contextConfig)

	if ctx == nil {
		t.Fatal("Expected context to be created")
	}
}

func TestBuilderIntegrationWithTimeout(t *testing.T) {
	sdk, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Application("website").
		Environment("development").
		Timeout(2 * time.Second).
		Retries(2).
		Build()

	if err != nil {
		t.Fatalf("Failed to build SDK: %v", err)
	}

	contextConfig := ContextConfig{
		Units_: map[string]string{
			"session_id": "test-session-456",
		},
	}

	ctx := sdk.CreateContext(contextConfig)

	if ctx == nil {
		t.Fatal("Expected context to be created")
	}
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
