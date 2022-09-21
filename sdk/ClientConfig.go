package sdk

type ClientConfig struct {
	Endpoint_     string
	ApiKey_       string
	Environment_  string
	Application_  string
	Deserializer_ ContextDataDeserializer
	Serializer_   ContextEventSerializer
}
