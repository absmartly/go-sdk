package sdk

type ABSmartlyConfig struct {
	ContextDataProvider  ContextDataProvider
	ContextEventHandler  ContextEventHandler
	ContextEventLogger   ContextEventLogger
	VariableParser       VariableParser
	AudienceDeserializer AudienceDeserializer
	Client               ClientI

	// Deprecated: Use ContextDataProvider instead.
	ContextDataProvider_ ContextDataProvider
	// Deprecated: Use ContextEventHandler instead.
	ContextEventHandler_ ContextEventHandler
	// Deprecated: Use ContextEventLogger instead.
	ContextEventLogger_ ContextEventLogger
	// Deprecated: Use VariableParser instead.
	VariableParser_ VariableParser
	// Deprecated: Use AudienceDeserializer instead.
	AudienceDeserializer_ AudienceDeserializer
	// Deprecated: Use Client instead.
	Client_ ClientI
}

func (c ABSmartlyConfig) contextDataProvider() ContextDataProvider {
	if c.ContextDataProvider != nil {
		return c.ContextDataProvider
	}
	return c.ContextDataProvider_
}

func (c ABSmartlyConfig) contextEventHandler() ContextEventHandler {
	if c.ContextEventHandler != nil {
		return c.ContextEventHandler
	}
	return c.ContextEventHandler_
}

func (c ABSmartlyConfig) contextEventLogger() ContextEventLogger {
	if c.ContextEventLogger != nil {
		return c.ContextEventLogger
	}
	return c.ContextEventLogger_
}

func (c ABSmartlyConfig) variableParser() VariableParser {
	if c.VariableParser != nil {
		return c.VariableParser
	}
	return c.VariableParser_
}

func (c ABSmartlyConfig) audienceDeserializer() AudienceDeserializer {
	if c.AudienceDeserializer != nil {
		return c.AudienceDeserializer
	}
	return c.AudienceDeserializer_
}

func (c ABSmartlyConfig) client() ClientI {
	if c.Client != nil {
		return c.Client
	}
	return c.Client_
}
