package sdk

import (
	"testing"
	"time"
)

func TestBuilderMinimalConfiguration(t *testing.T) {
	sdk, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Application("website").
		Environment("development").
		Build()

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if sdk.Client_ == nil {
		t.Error("Expected client to be initialized")
	}

	if sdk.ContextDataProvider_ == nil {
		t.Error("Expected ContextDataProvider to be initialized with default")
	}

	if sdk.ContextEventHandler_ == nil {
		t.Error("Expected ContextEventHandler to be initialized with default")
	}

	if sdk.VariableParser_ == nil {
		t.Error("Expected VariableParser to be initialized with default")
	}

	if sdk.AudienceDeserializer_ == nil {
		t.Error("Expected AudienceDeserializer to be initialized with default")
	}
}

func TestBuilderWithOptionalParameters(t *testing.T) {
	timeout := 5 * time.Second
	retries := 3

	sdk, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Application("website").
		Environment("development").
		Timeout(timeout).
		Retries(retries).
		Build()

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if sdk.Client_ == nil {
		t.Error("Expected client to be initialized")
	}
}

func TestBuilderWithCustomLogger(t *testing.T) {
	logger := &CustomEventLogger{}

	sdk, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Application("website").
		Environment("development").
		ContextEventLogger(logger).
		Build()

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if sdk.ContextEventLogger_ != logger {
		t.Error("Expected custom logger to be set")
	}
}

func TestBuilderValidationMissingEndpoint(t *testing.T) {
	_, err := NewBuilder().
		ApiKey("test-api-key").
		Application("website").
		Environment("development").
		Build()

	if err == nil {
		t.Error("Expected error for missing endpoint")
	}

	if err.Error() != "endpoint is required" {
		t.Errorf("Expected 'endpoint is required' error, got: %v", err)
	}
}

func TestBuilderValidationMissingApiKey(t *testing.T) {
	_, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		Application("website").
		Environment("development").
		Build()

	if err == nil {
		t.Error("Expected error for missing apiKey")
	}

	if err.Error() != "apiKey is required" {
		t.Errorf("Expected 'apiKey is required' error, got: %v", err)
	}
}

func TestBuilderValidationMissingApplication(t *testing.T) {
	_, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Environment("development").
		Build()

	if err == nil {
		t.Error("Expected error for missing application")
	}

	if err.Error() != "application is required" {
		t.Errorf("Expected 'application is required' error, got: %v", err)
	}
}

func TestBuilderValidationMissingEnvironment(t *testing.T) {
	_, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Application("website").
		Build()

	if err == nil {
		t.Error("Expected error for missing environment")
	}

	if err.Error() != "environment is required" {
		t.Errorf("Expected 'environment is required' error, got: %v", err)
	}
}

func TestBuilderValidationInvalidTimeout(t *testing.T) {
	_, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Application("website").
		Environment("development").
		Timeout(-1 * time.Second).
		Build()

	if err == nil {
		t.Error("Expected error for invalid timeout")
	}

	if err.Error() != "timeout must be positive" {
		t.Errorf("Expected 'timeout must be positive' error, got: %v", err)
	}
}

func TestBuilderValidationInvalidRetries(t *testing.T) {
	_, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Application("website").
		Environment("development").
		Retries(-1).
		Build()

	if err == nil {
		t.Error("Expected error for invalid retries")
	}

	if err.Error() != "retries must be non-negative" {
		t.Errorf("Expected 'retries must be non-negative' error, got: %v", err)
	}
}

func TestBuilderFluentInterface(t *testing.T) {
	builder := NewBuilder()

	builder2 := builder.Endpoint("https://sandbox.absmartly.io/v1")
	if builder != builder2 {
		t.Error("Expected fluent interface to return same builder instance")
	}

	builder3 := builder2.ApiKey("test-api-key")
	if builder2 != builder3 {
		t.Error("Expected fluent interface to return same builder instance")
	}

	builder4 := builder3.Application("website")
	if builder3 != builder4 {
		t.Error("Expected fluent interface to return same builder instance")
	}

	builder5 := builder4.Environment("development")
	if builder4 != builder5 {
		t.Error("Expected fluent interface to return same builder instance")
	}
}

func TestBuilderWithAllCustomComponents(t *testing.T) {
	logger := &CustomEventLogger{}
	parser := DefaultVariableParser{}
	deserializer := DefaultAudienceDeserializer{}

	sdk, err := NewBuilder().
		Endpoint("https://sandbox.absmartly.io/v1").
		ApiKey("test-api-key").
		Application("website").
		Environment("development").
		ContextEventLogger(logger).
		VariableParser(parser).
		AudienceDeserializer(deserializer).
		Build()

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if sdk.ContextEventLogger_ != logger {
		t.Error("Expected custom logger to be set")
	}

	if sdk.VariableParser_ == nil {
		t.Error("Expected custom variable parser to be set")
	}

	if sdk.AudienceDeserializer_ == nil {
		t.Error("Expected custom audience deserializer to be set")
	}
}

type CustomEventLogger struct{}

func (l *CustomEventLogger) HandleEvent(context Context, eventType EventType, data interface{}) {
}
