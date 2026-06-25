package sdk

type ABsmartlyConfig struct {
	ContextDataProvider_  ContextDataProvider
	ContextPublisher_     ContextEventHandler
	// Deprecated: Use ContextPublisher_ instead.
	ContextEventHandler_  ContextEventHandler
	ContextEventLogger_   ContextEventLogger
	VariableParser_       VariableParser
	AudienceDeserializer_ AudienceDeserializer
	Client_               ClientI
}

type ABSmartlyConfig = ABsmartlyConfig
