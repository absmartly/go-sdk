package sdk

type ABSmartlyConfig struct {
	ContextDataProvider_  ContextDataProvider
	ContextEventHandler_  ContextEventHandler
	ContextEventLogger_   ContextEventLogger
	VariableParser_       VariableParser
	AudienceDeserializer_ AudienceDeserializer
	Client_               ClientI
}
