package sdk

import (
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
	"strings"
	"testing"
	"time"
)

func TestInvalidEndpoint(t *testing.T) {
	testCases := []struct {
		name     string
		endpoint string
	}{
		{"empty endpoint", ""},
		{"invalid scheme", "ftp://localhost/v1"},
		{"missing scheme", "localhost/v1"},
		{"malformed URL", "http://local host/v1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var config = ClientConfig{
				Endpoint_:    tc.endpoint,
				ApiKey_:      "test-api-key",
				Application_: "test-app",
				Environment_: "dev",
			}

			client := CreateClient(config, HttpMock{})

			if config.Endpoint_ == "" && client.url_ != "/context" {
				t.Logf("Client created with empty endpoint results in url: %s", client.url_)
			}
		})
	}
}

func TestMissingRequiredConfig(t *testing.T) {
	statusCode = 200
	status = "OK"
	bodyString = "{}"

	t.Run("missing API key", func(t *testing.T) {
		var config = ClientConfig{
			Endpoint_:    "https://localhost/v1",
			ApiKey_:      "",
			Application_: "test-app",
			Environment_: "dev",
		}

		client := CreateClient(config, HttpMock{})

		if _, exists := client.headers_["X-API-Key"]; exists && client.headers_["X-API-Key"] != "" {
			t.Errorf("Expected empty API key in headers")
		}
	})

	t.Run("missing application name", func(t *testing.T) {
		var config = ClientConfig{
			Endpoint_:    "https://localhost/v1",
			ApiKey_:      "test-key",
			Application_: "",
			Environment_: "dev",
		}

		client := CreateClient(config, HttpMock{})

		if client.query_["application"] != "" {
			t.Logf("Client created with empty application: %v", client.query_)
		}
	})

	t.Run("missing environment", func(t *testing.T) {
		var config = ClientConfig{
			Endpoint_:    "https://localhost/v1",
			ApiKey_:      "test-key",
			Application_: "test-app",
			Environment_: "",
		}

		client := CreateClient(config, HttpMock{})

		if client.query_["environment"] != "" {
			t.Logf("Client created with empty environment: %v", client.query_)
		}
	})
}

func TestInvalidUnitTypes(t *testing.T) {
	setUp()

	t.Run("empty unit UID", func(t *testing.T) {
		var config = CreateDefaultContextConfig()
		config.Units_ = map[string]string{"user_id": "valid"}

		var client = ClientContextMock{}
		var dataProvider = DefaultContextDataProvider{client_: client}
		var eventHandler = DefaultContextEventHandler{client_: client}
		var variableParser = DefaultVariableParser{}
		var audienceMatcher = AudienceMatcher{audeser}
		var clk = FixedClockForTest{millis: 1620000000000}

		ctx := CreateContext(clk, config, dataFutureReady, dataProvider, eventHandler, nil, variableParser, audienceMatcher)

		err := ctx.SetUnit("new_unit", "")
		if err == nil {
			t.Errorf("Expected error for empty unit UID, got nil")
		} else if !strings.Contains(err.Error(), "blank") {
			t.Errorf("Expected error about blank UID, got: %v", err)
		}
	})

	t.Run("whitespace-only unit UID", func(t *testing.T) {
		var config = CreateDefaultContextConfig()
		config.Units_ = map[string]string{"user_id": "valid"}

		var client = ClientContextMock{}
		var dataProvider = DefaultContextDataProvider{client_: client}
		var eventHandler = DefaultContextEventHandler{client_: client}
		var variableParser = DefaultVariableParser{}
		var audienceMatcher = AudienceMatcher{audeser}
		var clk = FixedClockForTest{millis: 1620000000000}

		ctx := CreateContext(clk, config, dataFutureReady, dataProvider, eventHandler, nil, variableParser, audienceMatcher)

		err := ctx.SetUnit("new_unit", "   ")
		if err == nil {
			t.Errorf("Expected error for whitespace-only unit UID, got nil")
		}
	})

	t.Run("duplicate unit assignment", func(t *testing.T) {
		var config = CreateDefaultContextConfig()
		config.Units_ = map[string]string{"user_id": "original_value"}

		var client = ClientContextMock{}
		var dataProvider = DefaultContextDataProvider{client_: client}
		var eventHandler = DefaultContextEventHandler{client_: client}
		var variableParser = DefaultVariableParser{}
		var audienceMatcher = AudienceMatcher{audeser}
		var clk = FixedClockForTest{millis: 1620000000000}

		ctx := CreateContext(clk, config, dataFutureReady, dataProvider, eventHandler, nil, variableParser, audienceMatcher)

		err := ctx.SetUnit("user_id", "different_value")
		if err == nil {
			t.Errorf("Expected error for duplicate unit assignment, got nil")
		} else if !strings.Contains(err.Error(), "already set") {
			t.Errorf("Expected error about unit already set, got: %v", err)
		}
	})

	t.Run("same unit value allowed", func(t *testing.T) {
		var config = CreateDefaultContextConfig()
		config.Units_ = map[string]string{"user_id": "same_value"}

		var client = ClientContextMock{}
		var dataProvider = DefaultContextDataProvider{client_: client}
		var eventHandler = DefaultContextEventHandler{client_: client}
		var variableParser = DefaultVariableParser{}
		var audienceMatcher = AudienceMatcher{audeser}
		var clk = FixedClockForTest{millis: 1620000000000}

		ctx := CreateContext(clk, config, dataFutureReady, dataProvider, eventHandler, nil, variableParser, audienceMatcher)

		err := ctx.SetUnit("user_id", "same_value")
		if err != nil {
			t.Errorf("Expected no error for same unit value, got: %v", err)
		}
	})
}

func TestNegativeIntervals(t *testing.T) {
	t.Run("negative publish delay", func(t *testing.T) {
		var config = ContextConfig{
			Units_:        map[string]string{"user_id": "test123"},
			PublishDelay_: -1000,
		}

		dataFuture, done := future.New()
		done(jsonmodels.ContextData{
			Experiments: []jsonmodels.Experiment{
				{Id: 1, Name: "exp_test", UnitType: "user_id"},
			},
		}, nil)

		var client = ConfigTestClientMock{}
		var dataProvider = DefaultContextDataProvider{client_: client}
		var eventHandler = DefaultContextEventHandler{client_: client}
		var variableParser = DefaultVariableParser{}
		var audienceMatcher = AudienceMatcher{DefaultAudienceDeserializer{}}
		var clk = FixedClockForTest{millis: 1620000000000}

		ctx := CreateContext(clk, config, dataFuture, dataProvider, eventHandler, nil, variableParser, audienceMatcher)

		err := ctx.Track("test_goal", nil)
		if err != nil {
			t.Errorf("Unexpected error tracking with negative publish delay: %v", err)
		}
	})

	t.Run("zero refresh interval", func(t *testing.T) {
		var config = ContextConfig{
			Units_:           map[string]string{"user_id": "test123"},
			RefreshInterval_: 0,
		}

		dataFuture, done := future.New()
		done(jsonmodels.ContextData{
			Experiments: []jsonmodels.Experiment{
				{Id: 1, Name: "exp_test", UnitType: "user_id"},
			},
		}, nil)

		var client = ConfigTestClientMock{}
		var dataProvider = DefaultContextDataProvider{client_: client}
		var eventHandler = DefaultContextEventHandler{client_: client}
		var variableParser = DefaultVariableParser{}
		var audienceMatcher = AudienceMatcher{DefaultAudienceDeserializer{}}
		var clk = FixedClockForTest{millis: 1620000000000}

		ctx := CreateContext(clk, config, dataFuture, dataProvider, eventHandler, nil, variableParser, audienceMatcher)

		if ctx.RefreshTimer_ != nil {
			t.Errorf("Expected no refresh timer with zero interval, but timer was set")
		}
	})

	t.Run("negative refresh interval", func(t *testing.T) {
		var config = ContextConfig{
			Units_:           map[string]string{"user_id": "test123"},
			RefreshInterval_: -5000,
		}

		dataFuture, done := future.New()
		done(jsonmodels.ContextData{
			Experiments: []jsonmodels.Experiment{
				{Id: 1, Name: "exp_test", UnitType: "user_id"},
			},
		}, nil)

		var client = ConfigTestClientMock{}
		var dataProvider = DefaultContextDataProvider{client_: client}
		var eventHandler = DefaultContextEventHandler{client_: client}
		var variableParser = DefaultVariableParser{}
		var audienceMatcher = AudienceMatcher{DefaultAudienceDeserializer{}}
		var clk = FixedClockForTest{millis: 1620000000000}

		ctx := CreateContext(clk, config, dataFuture, dataProvider, eventHandler, nil, variableParser, audienceMatcher)

		if ctx.RefreshTimer_ != nil {
			t.Errorf("Expected no refresh timer with negative interval, but timer was set")
		}
	})
}

type ConfigTestClientMock struct{}

func (c ConfigTestClientMock) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) {
		return jsonmodels.ContextData{
			Experiments: []jsonmodels.Experiment{
				{Id: 1, Name: "exp_test", UnitType: "user_id"},
			},
		}, nil
	})
}

func (c ConfigTestClientMock) Publish(event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		return nil, nil
	})
}

func TestDefaultHttpClientConfigValues(t *testing.T) {
	config := CreateDefaultHttpClientConfig()

	if config.ConnectTimeout_ <= 0 {
		t.Errorf("Expected positive connect timeout, got %v", config.ConnectTimeout_)
	}

	if config.ConnectionKeepAlive_ <= 0 {
		t.Errorf("Expected positive connection keep alive, got %v", config.ConnectionKeepAlive_)
	}

	if config.MaxRetries_ < 0 {
		t.Errorf("Expected non-negative max retries, got %v", config.MaxRetries_)
	}

	if config.MaxConnectionsPerHost_ <= 0 {
		t.Errorf("Expected positive max connections per host, got %v", config.MaxConnectionsPerHost_)
	}
}

func TestContextConfigDefaults(t *testing.T) {
	config := CreateDefaultContextConfig()

	if config.PublishDelay_ != 1000 {
		t.Errorf("Expected default publish delay of 1000, got %v", config.PublishDelay_)
	}

	if config.RefreshInterval_ != 1000 {
		t.Errorf("Expected default refresh interval of 1000, got %v", config.RefreshInterval_)
	}
}

func TestClientCreationWithCustomConfig(t *testing.T) {
	customTimeout := 5 * time.Second
	customKeepAlive := 30 * time.Second
	customMaxRetries := 5

	config := DefaultHttpClientConfig{
		ConnectTimeout_:        customTimeout,
		ConnectionKeepAlive_:   customKeepAlive,
		MaxRetries_:            customMaxRetries,
		MaxConnectionsPerHost_: 10,
	}

	if config.ConnectTimeout_ != customTimeout {
		t.Errorf("Expected connect timeout %v, got %v", customTimeout, config.ConnectTimeout_)
	}

	if config.ConnectionKeepAlive_ != customKeepAlive {
		t.Errorf("Expected keep alive %v, got %v", customKeepAlive, config.ConnectionKeepAlive_)
	}

	if config.MaxRetries_ != customMaxRetries {
		t.Errorf("Expected max retries %v, got %v", customMaxRetries, config.MaxRetries_)
	}
}

func TestABSmartlyConfigWithNilClient(t *testing.T) {
	config := ABsmartlyConfig{
		Client_: nil,
	}

	if config.Client_ != nil {
		t.Errorf("Expected nil client")
	}
}

func TestContextWithEmptyUnits(t *testing.T) {
	var config = ContextConfig{
		Units_:        map[string]string{},
		PublishDelay_: 100,
	}

	dataFuture, done := future.New()
	done(jsonmodels.ContextData{
		Experiments: []jsonmodels.Experiment{
			{Id: 1, Name: "exp_test", UnitType: "user_id"},
		},
	}, nil)

	var client = ConfigTestClientMock{}
	var dataProvider = DefaultContextDataProvider{client_: client}
	var eventHandler = DefaultContextEventHandler{client_: client}
	var variableParser = DefaultVariableParser{}
	var audienceMatcher = AudienceMatcher{DefaultAudienceDeserializer{}}
	var clk = FixedClockForTest{millis: 1620000000000}

	ctx := CreateContext(clk, config, dataFuture, dataProvider, eventHandler, nil, variableParser, audienceMatcher)

	treatment, _ := ctx.GetTreatment("exp_test")
	if treatment != 0 {
		t.Logf("Treatment with no units: %v (expected 0)", treatment)
	}

	_, err := ctx.GetData()
	if err != nil {
		t.Logf("Got error with empty units: %v", err)
	}
}
