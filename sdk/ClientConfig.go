package sdk

type ClientConfig struct {
	Endpoint_     string
	ApiKey_       string
	Environment_  string
	Application_  string
	Deserializer_ ContextDataDeserializer
	Serializer_   ContextEventSerializer

	// ApplicationVersion_ is the version of the application using the SDK.
	// It is sent as the X-Application-Version header when it is a whole
	// number, as the collector requires; any other value, or none, is sent
	// as "0".
	ApplicationVersion_ string
}
