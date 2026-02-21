package sdk

import (
	"errors"
	"time"
)

type ABsmartlyBuilder struct {
	endpoint_              string
	apiKey_                string
	application_           string
	environment_           string
	timeout_               *time.Duration
	retries_               *int
	contextDataProvider_   ContextDataProvider
	contextEventHandler_   ContextEventHandler
	contextEventLogger_    ContextEventLogger
	variableParser_        VariableParser
	audienceDeserializer_  AudienceDeserializer
	deserializer_          ContextDataDeserializer
	serializer_            ContextEventSerializer
	httpClient_            HTTPClient
}

type ABSmartlyBuilder = ABsmartlyBuilder

func NewBuilder() *ABsmartlyBuilder {
	return &ABsmartlyBuilder{}
}

func (b *ABsmartlyBuilder) Endpoint(endpoint string) *ABsmartlyBuilder {
	b.endpoint_ = endpoint
	return b
}

func (b *ABsmartlyBuilder) ApiKey(apiKey string) *ABsmartlyBuilder {
	b.apiKey_ = apiKey
	return b
}

func (b *ABsmartlyBuilder) Application(application string) *ABsmartlyBuilder {
	b.application_ = application
	return b
}

func (b *ABsmartlyBuilder) Environment(environment string) *ABsmartlyBuilder {
	b.environment_ = environment
	return b
}

func (b *ABsmartlyBuilder) Timeout(timeout time.Duration) *ABsmartlyBuilder {
	b.timeout_ = &timeout
	return b
}

func (b *ABsmartlyBuilder) Retries(retries int) *ABsmartlyBuilder {
	b.retries_ = &retries
	return b
}

func (b *ABsmartlyBuilder) ContextDataProvider(provider ContextDataProvider) *ABsmartlyBuilder {
	b.contextDataProvider_ = provider
	return b
}

func (b *ABsmartlyBuilder) ContextEventHandler(handler ContextEventHandler) *ABsmartlyBuilder {
	b.contextEventHandler_ = handler
	return b
}

func (b *ABsmartlyBuilder) ContextEventLogger(logger ContextEventLogger) *ABsmartlyBuilder {
	b.contextEventLogger_ = logger
	return b
}

func (b *ABsmartlyBuilder) VariableParser(parser VariableParser) *ABsmartlyBuilder {
	b.variableParser_ = parser
	return b
}

func (b *ABsmartlyBuilder) AudienceDeserializer(deserializer AudienceDeserializer) *ABsmartlyBuilder {
	b.audienceDeserializer_ = deserializer
	return b
}

func (b *ABsmartlyBuilder) Deserializer(deserializer ContextDataDeserializer) *ABsmartlyBuilder {
	b.deserializer_ = deserializer
	return b
}

func (b *ABsmartlyBuilder) Serializer(serializer ContextEventSerializer) *ABsmartlyBuilder {
	b.serializer_ = serializer
	return b
}

func (b *ABsmartlyBuilder) HTTPClient(httpClient HTTPClient) *ABsmartlyBuilder {
	b.httpClient_ = httpClient
	return b
}

func (b *ABsmartlyBuilder) Build() (ABsmartly, error) {
	if err := b.validate(); err != nil {
		return ABsmartly{}, err
	}

	clientConfig := ClientConfig{
		Endpoint_:     b.endpoint_,
		ApiKey_:       b.apiKey_,
		Environment_:  b.environment_,
		Application_:  b.application_,
		Deserializer_: b.deserializer_,
		Serializer_:   b.serializer_,
	}

	var client ClientI
	if b.httpClient_ != nil {
		client = CreateClient(clientConfig, b.httpClient_)
	} else {
		var httpClient = CreateDefaultHttpClient()

		httpClientConfig := CreateDefaultHttpClientConfig()
		if b.timeout_ != nil {
			httpClientConfig.ConnectTimeout_ = *b.timeout_
		}
		if b.retries_ != nil {
			httpClientConfig.MaxRetries_ = *b.retries_
		}

		httpClient.DefaultHttpClientConfig(httpClientConfig)
		client = CreateClient(clientConfig, httpClient)
	}

	sdkConfig := ABsmartlyConfig{
		Client_:               client,
		ContextDataProvider_:  b.contextDataProvider_,
		ContextEventHandler_:  b.contextEventHandler_,
		ContextEventLogger_:   b.contextEventLogger_,
		VariableParser_:       b.variableParser_,
		AudienceDeserializer_: b.audienceDeserializer_,
	}

	return Create(sdkConfig), nil
}

func (b *ABsmartlyBuilder) validate() error {
	if b.endpoint_ == "" {
		return errors.New("endpoint is required")
	}
	if b.apiKey_ == "" {
		return errors.New("apiKey is required")
	}
	if b.application_ == "" {
		return errors.New("application is required")
	}
	if b.environment_ == "" {
		return errors.New("environment is required")
	}
	if b.timeout_ != nil && *b.timeout_ <= 0 {
		return errors.New("timeout must be positive")
	}
	if b.retries_ != nil && *b.retries_ < 0 {
		return errors.New("retries must be non-negative")
	}
	return nil
}
