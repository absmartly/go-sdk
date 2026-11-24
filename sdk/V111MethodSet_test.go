package sdk_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/absmartly/go-sdk/sdk"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/operators"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// v111Types is every exported struct type of v1.1.1's importable packages
// (tag dde76a2; sdk/internal cannot be imported outside the module).
var v111Types = []reflect.Type{
	reflect.TypeOf(eval.Evaluator{}),
	reflect.TypeOf(future.Future{}),
	reflect.TypeOf(jsonmodels.Attribute{}),
	reflect.TypeOf(jsonmodels.ContextData{}),
	reflect.TypeOf(jsonmodels.CustomFieldValue{}),
	reflect.TypeOf(jsonmodels.Experiment{}),
	reflect.TypeOf(jsonmodels.ExperimentApplication{}),
	reflect.TypeOf(jsonmodels.ExperimentVariant{}),
	reflect.TypeOf(jsonmodels.Exposure{}),
	reflect.TypeOf(jsonmodels.GoalAchievement{}),
	reflect.TypeOf(jsonmodels.PublishEvent{}),
	reflect.TypeOf(jsonmodels.Unit{}),
	reflect.TypeOf(operators.AndCombinator{}),
	reflect.TypeOf(operators.BinaryOperator{}),
	reflect.TypeOf(operators.BooleanCombinator{}),
	reflect.TypeOf(operators.EqualsOperator{}),
	reflect.TypeOf(operators.GreaterThanOperator{}),
	reflect.TypeOf(operators.GreaterThanOrEqualOperator{}),
	reflect.TypeOf(operators.InOperator{}),
	reflect.TypeOf(operators.LessThanOperator{}),
	reflect.TypeOf(operators.LessThanOrEqualOperator{}),
	reflect.TypeOf(operators.MatchOperator{}),
	reflect.TypeOf(operators.NotOperator{}),
	reflect.TypeOf(operators.NullOperator{}),
	reflect.TypeOf(operators.OrCombinator{}),
	reflect.TypeOf(operators.UnaryOperator{}),
	reflect.TypeOf(operators.ValueOperator{}),
	reflect.TypeOf(operators.VarOperator{}),
	reflect.TypeOf(sdk.ABSmartly{}),
	reflect.TypeOf(sdk.ABSmartlyConfig{}),
	reflect.TypeOf(sdk.Assignment{}),
	reflect.TypeOf(sdk.AudienceMatcher{}),
	reflect.TypeOf(sdk.Client{}),
	reflect.TypeOf(sdk.ClientConfig{}),
	reflect.TypeOf(sdk.ComputerUnitHash{}),
	reflect.TypeOf(sdk.ComputerVariantAssigner{}),
	reflect.TypeOf(sdk.Context{}),
	reflect.TypeOf(sdk.ContextConfig{}),
	reflect.TypeOf(sdk.ContextCustomFieldValue{}),
	reflect.TypeOf(sdk.DefaultAudienceDeserializer{}),
	reflect.TypeOf(sdk.DefaultContextDataDeserializer{}),
	reflect.TypeOf(sdk.DefaultContextDataProvider{}),
	reflect.TypeOf(sdk.DefaultContextEventHandler{}),
	reflect.TypeOf(sdk.DefaultContextEventSerializer{}),
	reflect.TypeOf(sdk.DefaultHttpClient{}),
	reflect.TypeOf(sdk.DefaultHttpClientConfig{}),
	reflect.TypeOf(sdk.DefaultVariableParser{}),
	reflect.TypeOf(sdk.ExperimentVariables{}),
	reflect.TypeOf(sdk.FlushMapper{}),
	reflect.TypeOf(sdk.Logger{}),
	reflect.TypeOf(sdk.Pair{}),
	reflect.TypeOf(sdk.Result{}),
	reflect.TypeOf(sdk.VariantAssigner{}),
}

// v111ValueMethods are the methods v1.1.1 declared on value receivers, with
// signatures, as reflect reports them for the published v1.1.1 sources. A
// v1.1.1 caller can call each on both a value and a pointer, so each must
// still be in both method sets: moving one to a pointer receiver drops it
// from the value set. v111PointerMethods are the methods v1.1.1 declared on
// pointer receivers, callable through a pointer.
var v111ValueMethods = map[string]string{
	"eval.Evaluator.BooleanConvert":                  "func(eval.Evaluator, reflect.Value) bool",
	"eval.Evaluator.Compare":                         "func(eval.Evaluator, reflect.Value, reflect.Value) interface {}",
	"eval.Evaluator.Evaluate":                        "func(eval.Evaluator, reflect.Value) interface {}",
	"eval.Evaluator.ExtractVar":                      "func(eval.Evaluator, string) interface {}",
	"eval.Evaluator.NumberConvert":                   "func(eval.Evaluator, reflect.Value) (float64, error)",
	"eval.Evaluator.StringConvert":                   "func(eval.Evaluator, reflect.Value) (string, error)",
	"operators.AndCombinator.Combine":                "func(operators.AndCombinator, eval.Evaluator, reflect.Value) bool",
	"operators.AndCombinator.Evaluate":               "func(operators.AndCombinator, eval.Evaluator, interface {}) interface {}",
	"operators.BinaryOperator.Evaluate":              "func(operators.BinaryOperator, eval.Evaluator, interface {}) interface {}",
	"operators.BooleanCombinator.Evaluate":           "func(operators.BooleanCombinator, eval.Evaluator, interface {}) interface {}",
	"operators.EqualsOperator.Binary":                "func(operators.EqualsOperator, eval.Evaluator, interface {}, interface {}) interface {}",
	"operators.EqualsOperator.Evaluate":              "func(operators.EqualsOperator, eval.Evaluator, interface {}) interface {}",
	"operators.GreaterThanOperator.Binary":           "func(operators.GreaterThanOperator, eval.Evaluator, interface {}, interface {}) interface {}",
	"operators.GreaterThanOperator.Evaluate":         "func(operators.GreaterThanOperator, eval.Evaluator, interface {}) interface {}",
	"operators.GreaterThanOrEqualOperator.Binary":    "func(operators.GreaterThanOrEqualOperator, eval.Evaluator, interface {}, interface {}) interface {}",
	"operators.GreaterThanOrEqualOperator.Evaluate":  "func(operators.GreaterThanOrEqualOperator, eval.Evaluator, interface {}) interface {}",
	"operators.InOperator.Binary":                    "func(operators.InOperator, eval.Evaluator, interface {}, interface {}) interface {}",
	"operators.InOperator.Evaluate":                  "func(operators.InOperator, eval.Evaluator, interface {}) interface {}",
	"operators.LessThanOperator.Binary":              "func(operators.LessThanOperator, eval.Evaluator, interface {}, interface {}) interface {}",
	"operators.LessThanOperator.Evaluate":            "func(operators.LessThanOperator, eval.Evaluator, interface {}) interface {}",
	"operators.LessThanOrEqualOperator.Binary":       "func(operators.LessThanOrEqualOperator, eval.Evaluator, interface {}, interface {}) interface {}",
	"operators.LessThanOrEqualOperator.Evaluate":     "func(operators.LessThanOrEqualOperator, eval.Evaluator, interface {}) interface {}",
	"operators.MatchOperator.Binary":                 "func(operators.MatchOperator, eval.Evaluator, interface {}, interface {}) interface {}",
	"operators.MatchOperator.Evaluate":               "func(operators.MatchOperator, eval.Evaluator, interface {}) interface {}",
	"operators.NotOperator.Evaluate":                 "func(operators.NotOperator, eval.Evaluator, interface {}) interface {}",
	"operators.NotOperator.Unary":                    "func(operators.NotOperator, eval.Evaluator, interface {}) interface {}",
	"operators.NullOperator.Evaluate":                "func(operators.NullOperator, eval.Evaluator, interface {}) interface {}",
	"operators.NullOperator.Unary":                   "func(operators.NullOperator, eval.Evaluator, interface {}) interface {}",
	"operators.OrCombinator.Combine":                 "func(operators.OrCombinator, eval.Evaluator, reflect.Value) bool",
	"operators.OrCombinator.Evaluate":                "func(operators.OrCombinator, eval.Evaluator, interface {}) interface {}",
	"operators.UnaryOperator.Evaluate":               "func(operators.UnaryOperator, eval.Evaluator, interface {}) interface {}",
	"operators.ValueOperator.Evaluate":               "func(operators.ValueOperator, eval.Evaluator, interface {}) interface {}",
	"operators.VarOperator.Evaluate":                 "func(operators.VarOperator, eval.Evaluator, interface {}) interface {}",
	"sdk.ABSmartly.CreateContext":                    "func(sdk.ABSmartly, sdk.ContextConfig) *sdk.Context",
	"sdk.ABSmartly.CreateContextWith":                "func(sdk.ABSmartly, sdk.ContextConfig, jsonmodels.ContextData) *sdk.Context",
	"sdk.ABSmartly.GetContextData":                   "func(sdk.ABSmartly) *future.Future",
	"sdk.AudienceMatcher.Evaluate":                   "func(sdk.AudienceMatcher, string, map[string]interface {}) (sdk.Result, error)",
	"sdk.Client.GetContextData":                      "func(sdk.Client) *future.Future",
	"sdk.Client.Publish":                             "func(sdk.Client, jsonmodels.PublishEvent) *future.Future",
	"sdk.ComputerUnitHash.Apply":                     "func(sdk.ComputerUnitHash, interface {}) interface {}",
	"sdk.ComputerVariantAssigner.Apply":              "func(sdk.ComputerVariantAssigner, interface {}) interface {}",
	"sdk.DefaultAudienceDeserializer.Deserialize":    "func(sdk.DefaultAudienceDeserializer, []uint8, int, int) (map[string]interface {}, error)",
	"sdk.DefaultContextDataDeserializer.Deserialize": "func(sdk.DefaultContextDataDeserializer, []uint8) (jsonmodels.ContextData, error)",
	"sdk.DefaultContextDataProvider.GetContextData":  "func(sdk.DefaultContextDataProvider) *future.Future",
	"sdk.DefaultContextEventHandler.Publish":         "func(sdk.DefaultContextEventHandler, sdk.Context, jsonmodels.PublishEvent) *future.Future",
	"sdk.DefaultContextEventSerializer.Serialize":    "func(sdk.DefaultContextEventSerializer, jsonmodels.PublishEvent) ([]uint8, error)",
	"sdk.DefaultHttpClient.DefaultHttpClientConfig":  "func(sdk.DefaultHttpClient, sdk.DefaultHttpClientConfig)",
	"sdk.DefaultHttpClient.Get":                      "func(sdk.DefaultHttpClient, string, map[string]string, map[string]string) *future.Future",
	"sdk.DefaultHttpClient.Post":                     "func(sdk.DefaultHttpClient, string, map[string]string, map[string]string, []uint8) *future.Future",
	"sdk.DefaultHttpClient.Put":                      "func(sdk.DefaultHttpClient, string, map[string]string, map[string]string, []uint8) *future.Future",
	"sdk.DefaultVariableParser.Parse":                "func(sdk.DefaultVariableParser, sdk.Context, string, string, string) map[string]interface {}",
	"sdk.FlushMapper.Apply":                          "func(sdk.FlushMapper, interface {}) interface {}",
	"sdk.Logger.Debugf":                              "func(sdk.Logger, string, ...interface {})",
	"sdk.Logger.Errorf":                              "func(sdk.Logger, string, ...interface {})",
	"sdk.Logger.Warnf":                               "func(sdk.Logger, string, ...interface {})",
	"sdk.Result.Get":                                 "func(sdk.Result) bool",
	"sdk.VariantAssigner.Assign":                     "func(sdk.VariantAssigner, []float64, int, int, []int8) int",
	"sdk.VariantAssigner.ChooseVariant":              "func(sdk.VariantAssigner, []float64, float64) int",
}

var v111PointerMethods = map[string]string{
	"*future.Future.Get":                   "func(*future.Future, context.Context) (future.Value, error)",
	"*future.Future.Join":                  "func(*future.Future, context.Context)",
	"*future.Future.Listen":                "func(*future.Future, future.SetResultFunc)",
	"*future.Future.NotifyCallbacks":       "func(*future.Future)",
	"*future.Future.Ready":                 "func(*future.Future) bool",
	"*future.Future.SetResult":             "func(*future.Future, future.Value, error)",
	"*sdk.Context.CheckNotClosed":          "func(*sdk.Context) error",
	"*sdk.Context.CheckReady":              "func(*sdk.Context, bool) error",
	"*sdk.Context.ClearRefreshTimer":       "func(*sdk.Context)",
	"*sdk.Context.ClearTimeout":            "func(*sdk.Context)",
	"*sdk.Context.Close":                   "func(*sdk.Context)",
	"*sdk.Context.CloseAsync":              "func(*sdk.Context) (*future.Future, error)",
	"*sdk.Context.ExperimentMatches":       "func(*sdk.Context, jsonmodels.Experiment, sdk.Assignment) bool",
	"*sdk.Context.Flush":                   "func(*sdk.Context) *future.Future",
	"*sdk.Context.GetAssignment":           "func(*sdk.Context, string) *sdk.Assignment",
	"*sdk.Context.GetCustomAssignment":     "func(*sdk.Context, string) (int, error)",
	"*sdk.Context.GetCustomFieldValue":     "func(*sdk.Context, string, string) interface {}",
	"*sdk.Context.GetCustomFieldValueKeys": "func(*sdk.Context) ([]string, error)",
	"*sdk.Context.GetCustomFieldValueType": "func(*sdk.Context, string, string) string",
	"*sdk.Context.GetData":                 "func(*sdk.Context) (jsonmodels.ContextData, error)",
	"*sdk.Context.GetExperiment":           "func(*sdk.Context, string) (sdk.ExperimentVariables, bool)",
	"*sdk.Context.GetExperiments":          "func(*sdk.Context) ([]string, error)",
	"*sdk.Context.GetOverride":             "func(*sdk.Context, string) (int, error)",
	"*sdk.Context.GetPendingCount":         "func(*sdk.Context) int32",
	"*sdk.Context.GetTreatment":            "func(*sdk.Context, string) (int, error)",
	"*sdk.Context.GetUnitHash":             "func(*sdk.Context, string, string, []uint8, bool) []uint8",
	"*sdk.Context.GetVariableAssignment":   "func(*sdk.Context, string) (*sdk.Assignment, error)",
	"*sdk.Context.GetVariableExperiment":   "func(*sdk.Context, string) (sdk.ExperimentVariables, error)",
	"*sdk.Context.GetVariableKeys":         "func(*sdk.Context) (map[string]string, error)",
	"*sdk.Context.GetVariableValue":        "func(*sdk.Context, string, interface {}) (interface {}, error)",
	"*sdk.Context.GetVariantAssigner":      "func(*sdk.Context, string, []uint8, bool) *sdk.VariantAssigner",
	"*sdk.Context.IsClosed":                "func(*sdk.Context) bool",
	"*sdk.Context.IsClosing":               "func(*sdk.Context) bool",
	"*sdk.Context.IsFailed":                "func(*sdk.Context) bool",
	"*sdk.Context.IsReady":                 "func(*sdk.Context) bool",
	"*sdk.Context.LogError":                "func(*sdk.Context, error)",
	"*sdk.Context.LogEvent":                "func(*sdk.Context, sdk.EventType, interface {})",
	"*sdk.Context.PeekTreatment":           "func(*sdk.Context, string) (int, error)",
	"*sdk.Context.PeekVariableValue":       "func(*sdk.Context, string, interface {}) (interface {}, error)",
	"*sdk.Context.Publish":                 "func(*sdk.Context) error",
	"*sdk.Context.PublishAsync":            "func(*sdk.Context) (*future.Future, error)",
	"*sdk.Context.QueueExposure":           "func(*sdk.Context, *sdk.Assignment)",
	"*sdk.Context.Refresh":                 "func(*sdk.Context)",
	"*sdk.Context.RefreshAsync":            "func(*sdk.Context) *future.Future",
	"*sdk.Context.SetAttribute":            "func(*sdk.Context, string, interface {}) error",
	"*sdk.Context.SetAttributes":           "func(*sdk.Context, map[string]interface {}) error",
	"*sdk.Context.SetCustomAssignment":     "func(*sdk.Context, string, int) error",
	"*sdk.Context.SetCustomAssignments":    "func(*sdk.Context, map[string]int) error",
	"*sdk.Context.SetData":                 "func(*sdk.Context, jsonmodels.ContextData)",
	"*sdk.Context.SetDataFailed":           "func(*sdk.Context, error)",
	"*sdk.Context.SetOverride":             "func(*sdk.Context, string, int) error",
	"*sdk.Context.SetOverrides":            "func(*sdk.Context, map[string]int) error",
	"*sdk.Context.SetRefreshTimer":         "func(*sdk.Context)",
	"*sdk.Context.SetTimeout":              "func(*sdk.Context)",
	"*sdk.Context.SetUnit":                 "func(*sdk.Context, string, string) error",
	"*sdk.Context.SetUnits":                "func(*sdk.Context, map[string]string) error",
	"*sdk.Context.Track":                   "func(*sdk.Context, string, map[string]interface {}) error",
	"*sdk.Context.WaitUntilReady":          "func(*sdk.Context) sdk.Context",
	"*sdk.Context.WaitUntilReadyAsync":     "func(*sdk.Context) *future.Future",
}

func TestV111MethodSetKept(t *testing.T) {
	var methods = map[string]string{}
	for _, typ := range v111Types {
		for _, rt := range []reflect.Type{typ, reflect.PtrTo(typ)} {
			for i := 0; i < rt.NumMethod(); i++ {
				var m = rt.Method(i)
				methods[rt.String()+"."+m.Name] = m.Type.String()
			}
		}
	}
	var expected = map[string]string{}
	for name, signature := range v111PointerMethods {
		expected[name] = signature
	}
	for name, signature := range v111ValueMethods {
		var receiver = name[:strings.LastIndex(name, ".")]
		expected[name] = signature
		expected["*"+name] = strings.Replace(signature, "func("+receiver, "func(*"+receiver, 1)
	}
	for name, signature := range expected {
		if got, found := methods[name]; !found {
			t.Errorf("%s was removed; v1.1.1 callers of it no longer compile", name)
		} else if got != signature {
			t.Errorf("%s is %s, v1.1.1 had %s", name, got, signature)
		}
	}
}

// v111Interfaces is the exact method set of each exported v1.1.1 interface.
// A customer type implementing one must keep satisfying it, so the set must
// not gain a method; a caller using one must keep compiling, so it must not
// lose one or change a signature.
var v111Interfaces = map[reflect.Type][]string{
	reflect.TypeOf((*eval.Operator)(nil)).Elem():                        []string{"Evaluate func(eval.Evaluator, interface {}) interface {}"},
	reflect.TypeOf((*operators.BinaryOperatorInterface)(nil)).Elem():    []string{"Binary func(eval.Evaluator, interface {}, interface {}) interface {}"},
	reflect.TypeOf((*operators.BooleanCombinatorInterface)(nil)).Elem(): []string{"Combine func(eval.Evaluator, reflect.Value) bool"},
	reflect.TypeOf((*operators.UnaryOperatorInterface)(nil)).Elem():     []string{"Unary func(eval.Evaluator, interface {}) interface {}"},
	reflect.TypeOf((*sdk.AudienceDeserializer)(nil)).Elem():             []string{"Deserialize func([]uint8, int, int) (map[string]interface {}, error)"},
	reflect.TypeOf((*sdk.ClientI)(nil)).Elem():                          []string{"GetContextData func() *future.Future", "Publish func(jsonmodels.PublishEvent) *future.Future"},
	reflect.TypeOf((*sdk.ContextDataDeserializer)(nil)).Elem():          []string{"Deserialize func([]uint8) (jsonmodels.ContextData, error)"},
	reflect.TypeOf((*sdk.ContextDataProvider)(nil)).Elem():              []string{"GetContextData func() *future.Future"},
	reflect.TypeOf((*sdk.ContextEventHandler)(nil)).Elem():              []string{"Publish func(sdk.Context, jsonmodels.PublishEvent) *future.Future"},
	reflect.TypeOf((*sdk.ContextEventLogger)(nil)).Elem():               []string{"HandleEvent func(sdk.Context, sdk.EventType, interface {})"},
	reflect.TypeOf((*sdk.ContextEventSerializer)(nil)).Elem():           []string{"Serialize func(jsonmodels.PublishEvent) ([]uint8, error)"},
	reflect.TypeOf((*sdk.HTTPClient)(nil)).Elem():                       []string{"Get func(string, map[string]string, map[string]string) *future.Future", "Post func(string, map[string]string, map[string]string, []uint8) *future.Future", "Put func(string, map[string]string, map[string]string, []uint8) *future.Future"},
	reflect.TypeOf((*sdk.MapperInt)(nil)).Elem():                        []string{"Apply func(interface {}) interface {}"},
	reflect.TypeOf((*sdk.VariableParser)(nil)).Elem():                   []string{"Parse func(sdk.Context, string, string, string) map[string]interface {}"},
}

func TestV111InterfacesUnchanged(t *testing.T) {
	for it, want := range v111Interfaces {
		var got []string
		for i := 0; i < it.NumMethod(); i++ {
			var m = it.Method(i)
			got = append(got, m.Name+" "+m.Type.String())
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s has methods %v, v1.1.1 had %v", it, got, want)
		}
	}
}
