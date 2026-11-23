package sdk_test

import (
	"reflect"
	"testing"

	"github.com/absmartly/go-sdk/sdk"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
)

// v111Methods is every exported method of v1.1.1's exported types, with its
// signature, as reflect reports them for the published v1.1.1 sources
// (tag dde76a2). Each must still exist with the same signature.
var v111Methods = map[string]string{
	"ABSmartly.CreateContext":                    "func(*sdk.ABSmartly, sdk.ContextConfig) *sdk.Context",
	"ABSmartly.CreateContextWith":                "func(*sdk.ABSmartly, sdk.ContextConfig, jsonmodels.ContextData) *sdk.Context",
	"ABSmartly.GetContextData":                   "func(*sdk.ABSmartly) *future.Future",
	"AudienceMatcher.Evaluate":                   "func(*sdk.AudienceMatcher, string, map[string]interface {}) (sdk.Result, error)",
	"Client.GetContextData":                      "func(*sdk.Client) *future.Future",
	"Client.Publish":                             "func(*sdk.Client, jsonmodels.PublishEvent) *future.Future",
	"Context.CheckNotClosed":                     "func(*sdk.Context) error",
	"Context.CheckReady":                         "func(*sdk.Context, bool) error",
	"Context.ClearRefreshTimer":                  "func(*sdk.Context)",
	"Context.ClearTimeout":                       "func(*sdk.Context)",
	"Context.Close":                              "func(*sdk.Context)",
	"Context.CloseAsync":                         "func(*sdk.Context) (*future.Future, error)",
	"Context.ExperimentMatches":                  "func(*sdk.Context, jsonmodels.Experiment, sdk.Assignment) bool",
	"Context.Flush":                              "func(*sdk.Context) *future.Future",
	"Context.GetAssignment":                      "func(*sdk.Context, string) *sdk.Assignment",
	"Context.GetCustomAssignment":                "func(*sdk.Context, string) (int, error)",
	"Context.GetCustomFieldValue":                "func(*sdk.Context, string, string) interface {}",
	"Context.GetCustomFieldValueKeys":            "func(*sdk.Context) ([]string, error)",
	"Context.GetCustomFieldValueType":            "func(*sdk.Context, string, string) string",
	"Context.GetData":                            "func(*sdk.Context) (jsonmodels.ContextData, error)",
	"Context.GetExperiment":                      "func(*sdk.Context, string) (sdk.ExperimentVariables, bool)",
	"Context.GetExperiments":                     "func(*sdk.Context) ([]string, error)",
	"Context.GetOverride":                        "func(*sdk.Context, string) (int, error)",
	"Context.GetPendingCount":                    "func(*sdk.Context) int32",
	"Context.GetTreatment":                       "func(*sdk.Context, string) (int, error)",
	"Context.GetUnitHash":                        "func(*sdk.Context, string, string, []uint8, bool) []uint8",
	"Context.GetVariableAssignment":              "func(*sdk.Context, string) (*sdk.Assignment, error)",
	"Context.GetVariableExperiment":              "func(*sdk.Context, string) (sdk.ExperimentVariables, error)",
	"Context.GetVariableKeys":                    "func(*sdk.Context) (map[string]string, error)",
	"Context.GetVariableValue":                   "func(*sdk.Context, string, interface {}) (interface {}, error)",
	"Context.GetVariantAssigner":                 "func(*sdk.Context, string, []uint8, bool) *sdk.VariantAssigner",
	"Context.IsClosed":                           "func(*sdk.Context) bool",
	"Context.IsClosing":                          "func(*sdk.Context) bool",
	"Context.IsFailed":                           "func(*sdk.Context) bool",
	"Context.IsReady":                            "func(*sdk.Context) bool",
	"Context.LogError":                           "func(*sdk.Context, error)",
	"Context.LogEvent":                           "func(*sdk.Context, sdk.EventType, interface {})",
	"Context.PeekTreatment":                      "func(*sdk.Context, string) (int, error)",
	"Context.PeekVariableValue":                  "func(*sdk.Context, string, interface {}) (interface {}, error)",
	"Context.Publish":                            "func(*sdk.Context) error",
	"Context.PublishAsync":                       "func(*sdk.Context) (*future.Future, error)",
	"Context.QueueExposure":                      "func(*sdk.Context, *sdk.Assignment)",
	"Context.Refresh":                            "func(*sdk.Context)",
	"Context.RefreshAsync":                       "func(*sdk.Context) *future.Future",
	"Context.SetAttribute":                       "func(*sdk.Context, string, interface {}) error",
	"Context.SetAttributes":                      "func(*sdk.Context, map[string]interface {}) error",
	"Context.SetCustomAssignment":                "func(*sdk.Context, string, int) error",
	"Context.SetCustomAssignments":               "func(*sdk.Context, map[string]int) error",
	"Context.SetData":                            "func(*sdk.Context, jsonmodels.ContextData)",
	"Context.SetDataFailed":                      "func(*sdk.Context, error)",
	"Context.SetOverride":                        "func(*sdk.Context, string, int) error",
	"Context.SetOverrides":                       "func(*sdk.Context, map[string]int) error",
	"Context.SetRefreshTimer":                    "func(*sdk.Context)",
	"Context.SetTimeout":                         "func(*sdk.Context)",
	"Context.SetUnit":                            "func(*sdk.Context, string, string) error",
	"Context.SetUnits":                           "func(*sdk.Context, map[string]string) error",
	"Context.Track":                              "func(*sdk.Context, string, map[string]interface {}) error",
	"Context.WaitUntilReady":                     "func(*sdk.Context) sdk.Context",
	"Context.WaitUntilReadyAsync":                "func(*sdk.Context) *future.Future",
	"DefaultAudienceDeserializer.Deserialize":    "func(*sdk.DefaultAudienceDeserializer, []uint8, int, int) (map[string]interface {}, error)",
	"DefaultContextDataDeserializer.Deserialize": "func(*sdk.DefaultContextDataDeserializer, []uint8) (jsonmodels.ContextData, error)",
	"DefaultContextDataProvider.GetContextData":  "func(*sdk.DefaultContextDataProvider) *future.Future",
	"DefaultContextEventHandler.Publish":         "func(*sdk.DefaultContextEventHandler, sdk.Context, jsonmodels.PublishEvent) *future.Future",
	"DefaultContextEventSerializer.Serialize":    "func(*sdk.DefaultContextEventSerializer, jsonmodels.PublishEvent) ([]uint8, error)",
	"DefaultHttpClient.DefaultHttpClientConfig":  "func(*sdk.DefaultHttpClient, sdk.DefaultHttpClientConfig)",
	"DefaultHttpClient.Get":                      "func(*sdk.DefaultHttpClient, string, map[string]string, map[string]string) *future.Future",
	"DefaultHttpClient.Post":                     "func(*sdk.DefaultHttpClient, string, map[string]string, map[string]string, []uint8) *future.Future",
	"DefaultHttpClient.Put":                      "func(*sdk.DefaultHttpClient, string, map[string]string, map[string]string, []uint8) *future.Future",
	"DefaultVariableParser.Parse":                "func(*sdk.DefaultVariableParser, sdk.Context, string, string, string) map[string]interface {}",
	"Evaluator.BooleanConvert":                   "func(*eval.Evaluator, reflect.Value) bool",
	"Evaluator.Compare":                          "func(*eval.Evaluator, reflect.Value, reflect.Value) interface {}",
	"Evaluator.Evaluate":                         "func(*eval.Evaluator, reflect.Value) interface {}",
	"Evaluator.ExtractVar":                       "func(*eval.Evaluator, string) interface {}",
	"Evaluator.NumberConvert":                    "func(*eval.Evaluator, reflect.Value) (float64, error)",
	"Evaluator.StringConvert":                    "func(*eval.Evaluator, reflect.Value) (string, error)",
	"FlushMapper.Apply":                          "func(*sdk.FlushMapper, interface {}) interface {}",
	"Future.Get":                                 "func(*future.Future, context.Context) (future.Value, error)",
	"Future.Join":                                "func(*future.Future, context.Context)",
	"Future.Listen":                              "func(*future.Future, future.SetResultFunc)",
	"Future.NotifyCallbacks":                     "func(*future.Future)",
	"Future.Ready":                               "func(*future.Future) bool",
	"Future.SetResult":                           "func(*future.Future, future.Value, error)",
	"VariantAssigner.Assign":                     "func(*sdk.VariantAssigner, []float64, int, int, []int8) int",
	"VariantAssigner.ChooseVariant":              "func(*sdk.VariantAssigner, []float64, float64) int",
}

func TestV111MethodSetKept(t *testing.T) {
	var types = []reflect.Type{
		reflect.TypeOf(&sdk.Context{}), reflect.TypeOf(&sdk.ABSmartly{}), reflect.TypeOf(&sdk.AudienceMatcher{}),
		reflect.TypeOf(&sdk.Client{}), reflect.TypeOf(&sdk.DefaultAudienceDeserializer{}),
		reflect.TypeOf(&sdk.DefaultContextDataDeserializer{}), reflect.TypeOf(&sdk.DefaultContextDataProvider{}),
		reflect.TypeOf(&sdk.DefaultContextEventHandler{}), reflect.TypeOf(&sdk.DefaultContextEventSerializer{}),
		reflect.TypeOf(&sdk.DefaultHttpClient{}), reflect.TypeOf(&sdk.DefaultVariableParser{}),
		reflect.TypeOf(&sdk.FlushMapper{}), reflect.TypeOf(&sdk.VariantAssigner{}),
		reflect.TypeOf(&future.Future{}), reflect.TypeOf(&eval.Evaluator{}),
	}
	var methods = map[string]string{}
	for _, typ := range types {
		for i := 0; i < typ.NumMethod(); i++ {
			var m = typ.Method(i)
			methods[typ.Elem().Name()+"."+m.Name] = m.Type.String()
		}
	}
	for name, signature := range v111Methods {
		if got, found := methods[name]; !found {
			t.Errorf("%s was removed; v1.1.1 callers of it no longer compile", name)
		} else if got != signature {
			t.Errorf("%s is %s, v1.1.1 had %s", name, got, signature)
		}
	}
}
