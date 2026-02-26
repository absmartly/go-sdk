package sdk

type ClientConfig struct {
	Endpoint     string
	APIKey       string
	Environment  string
	Application  string
	Deserializer ContextDataDeserializer
	Serializer   ContextEventSerializer

	// Deprecated: Use Endpoint instead.
	Endpoint_ string
	// Deprecated: Use APIKey instead.
	ApiKey_ string
	// Deprecated: Use Environment instead.
	Environment_ string
	// Deprecated: Use Application instead.
	Application_ string
	// Deprecated: Use Deserializer instead.
	Deserializer_ ContextDataDeserializer
	// Deprecated: Use Serializer instead.
	Serializer_ ContextEventSerializer
}

func (c ClientConfig) endpoint() string {
	if c.Endpoint != "" {
		return c.Endpoint
	}
	return c.Endpoint_
}

func (c ClientConfig) apiKey() string {
	if c.APIKey != "" {
		return c.APIKey
	}
	return c.ApiKey_
}

func (c ClientConfig) environment() string {
	if c.Environment != "" {
		return c.Environment
	}
	return c.Environment_
}

func (c ClientConfig) application() string {
	if c.Application != "" {
		return c.Application
	}
	return c.Application_
}

func (c ClientConfig) deserializer() ContextDataDeserializer {
	if c.Deserializer != nil {
		return c.Deserializer
	}
	return c.Deserializer_
}

func (c ClientConfig) serializer() ContextEventSerializer {
	if c.Serializer != nil {
		return c.Serializer
	}
	return c.Serializer_
}
