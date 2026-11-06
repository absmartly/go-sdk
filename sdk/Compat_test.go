package sdk

import (
	"testing"
)

// The exported field types below were released in v1.1.1; changing them is a
// compile break for callers.
func TestV111ExportedFieldTypes(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var indexVariables map[interface{}]interface{} = context.IndexVariables_
	var assignmentCache map[string]Assignment = context.AssignmentCache
	var mapper = FlushMapper{Context: *context}
	var _ Context = mapper.Context

	keys, err := context.GetVariableKeys()
	assertAny(nil, err, t)
	assertAny(len(keys), len(indexVariables), t)
	for key, name := range keys {
		assertAny(name, indexVariables[key].(ExperimentVariables).Data.Name, t)
	}

	treatment, _ := context.GetTreatment("exp_test_ab")
	cached, found := assignmentCache["exp_test_ab"]
	assertAny(true, found, t)
	assertAny(treatment, cached.Variant, t)
}

func TestV111FlushMapperValueContext(t *testing.T) {
	setUp()
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	var context = CreateTestContext(config, dataFutureReady)

	var byValue = FlushMapper{Context: *context}.Apply(Pair{a: "session_id", b: units["session_id"]})
	var byPointer = FlushMapper{ctx: context}.Apply(Pair{a: "session_id", b: units["session_id"]})
	assertAny(byPointer, byValue, t)
}
