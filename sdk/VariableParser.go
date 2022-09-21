package sdk

type VariableParser interface {
	Parse(context Context, experimentName string, variantName string, variableValue string) map[string]interface{}
}
