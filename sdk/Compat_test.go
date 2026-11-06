package sdk

import (
	"context"
	"testing"
	"time"
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

// v1.1.1 logged the Close event before an empty-queue CloseAsync settled.
func TestEmptyCloseAsyncLogsCloseBeforeSettling(t *testing.T) {
	for i := 0; i < 50; i++ {
		setUp()
		var logger = &MockEventLogger{}
		var config = CreateDefaultContextConfig()
		config.Units_ = units
		config.EventLogger_ = logger
		var context = CreateTestContext(config, dataFutureReady)
		logger.Reset()

		closing, err := context.CloseAsync()
		assertAny(nil, err, t)
		if !closing.Ready() {
			t.Fatal("empty-queue CloseAsync returned an unsettled future")
		}
		var events = logger.GetEvents()
		if len(events) != 1 || events[0].EventType != Close {
			t.Fatalf("Close event not logged before CloseAsync settled: %v", events)
		}
	}
}

func TestEmptyCloseAsyncFromCloseLoggerDoesNotHang(t *testing.T) {
	setUp()
	var reentered = make(chan bool, 1)
	var logger = &reentrantReadyLogger{onEvent: Close, call: func(ctx *Context) {
		closing, _ := ctx.CloseAsync()
		closing.Join(context.Background())
		ctx.Close()
		reentered <- true
	}}
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.EventLogger_ = logger
	var ctx = CreateTestContext(config, dataFutureReady)

	var finished = make(chan struct{})
	go func() {
		closing, _ := ctx.CloseAsync()
		closing.Join(context.Background())
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("CloseAsync re-entered from the Close logger hung")
	}
	assertAny(true, <-reentered, t)
	assertAny(true, ctx.IsClosed(), t)
}
