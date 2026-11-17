package sdk

type ABSmartlyConfig struct {
	ContextDataProvider_  ContextDataProvider
	ContextEventHandler_  ContextEventHandler
	ContextEventLogger_   ContextEventLogger
	VariableParser_       VariableParser
	AudienceDeserializer_ AudienceDeserializer
	Client_               ClientI
}

// ABsmartlyConfig is an alias of ABSmartlyConfig, spelled as the product is.
type ABsmartlyConfig = ABSmartlyConfig
