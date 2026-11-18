package sdk_test

// This file is written as a v1.1.1 customer would write it, outside the sdk
// package and with v1.1.1 names only: unkeyed struct literals of the
// configuration and model types whose v1.1.1 layout v1.2.0 keeps, extension
// points implemented by customer types, and the v1.1.1 error texts. If it
// stops compiling or passing, v1.2.0 broke a v1.1.1 caller. The two layouts
// v1.2.0 changes, jsonmodels.Experiment and jsonmodels.Exposure (one wire
// field each, see CHANGELOG), are deliberately not written unkeyed here.
// The publish-failure steps check v1.2.0's fixed behaviour (v1.1.1 returned
// nil and dropped the batch), so they are not a v1.1.1 compatibility check.
//
// The unkeyed literals are the point of the file, so `go vet ./...` reports
// them by design; run vet with -composites=false.

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/absmartly/go-sdk/sdk"
	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonexpr/eval"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

type v111Logger struct {
	mu     sync.Mutex
	events []sdk.EventType
}

func (l *v111Logger) HandleEvent(context sdk.Context, event sdk.EventType, data interface{}) {
	l.mu.Lock()
	l.events = append(l.events, event)
	l.mu.Unlock()
}

func (l *v111Logger) count(event sdk.EventType) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	var n = 0
	for _, e := range l.events {
		if e == event {
			n++
		}
	}
	return n
}

var errCollectorDown = errors.New("collector down")

type v111Publisher struct {
	attempts  []jsonmodels.PublishEvent
	published []jsonmodels.PublishEvent
	fail      bool
}

func (p *v111Publisher) Publish(context sdk.Context, event jsonmodels.PublishEvent) *future.Future {
	p.attempts = append(p.attempts, event)
	return future.Call(func() (future.Value, error) {
		if p.fail {
			return nil, errCollectorDown
		}
		p.published = append(p.published, event)
		return nil, nil
	})
}

type v111Provider struct {
	data jsonmodels.ContextData
}

func (p v111Provider) GetContextData() *future.Future {
	return future.Call(func() (future.Value, error) { return p.data, nil })
}

// A customer parser that embeds DefaultVariableParser and overrides Parse.
type v111Parser struct {
	sdk.DefaultVariableParser
	calls int32
}

func (p *v111Parser) Parse(context sdk.Context, experimentName string, variantName string, value string) map[string]interface{} {
	atomic.AddInt32(&p.calls, 1)
	return sdk.DefaultVariableParser{}.Parse(context, experimentName, variantName, value)
}

func v111Data(t *testing.T) jsonmodels.ContextData {
	t.Helper()
	content, err := os.ReadFile("testAssets/context.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := sdk.DefaultContextDataDeserializer{}.Deserialize(content)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestV111ConsumerCompiles(t *testing.T) {
	var clientConfig = sdk.ClientConfig{"https://acme.absmartly.io/v1", "key", "prod", "website", nil, nil}
	var client = sdk.CreateDefaultClient(clientConfig)
	var _ = sdk.ABSmartlyConfig{nil, nil, nil, nil, nil, client}
	var _ = sdk.ABSmartly{nil, nil, nil, nil, nil, client}
	var _ = sdk.ContextConfig{map[string]string{"session_id": "1"}, nil, nil, nil, nil, 0, 0}
	var _ = sdk.Assignment{1, 1, 0, "exp", "session_id", nil, 1, true, false, true, false, false, false, nil, nil}
	var _ = sdk.ExperimentVariables{jsonmodels.Experiment{}, nil}
	var _ = sdk.ContextCustomFieldValue{"name", "string", "value"}
	var _ = sdk.FlushMapper{nil, sdk.Context{}}
	var _ = sdk.AudienceMatcher{sdk.DefaultAudienceDeserializer{}}
	var _ = eval.Evaluator{nil, nil}
	var _ = jsonmodels.PublishEvent{true, nil, 0, nil, nil, nil}
	var _ = jsonmodels.Unit{"session_id", "uid"}
	var _ = jsonmodels.GoalAchievement{"goal", 0, nil}
	var _ = jsonmodels.Attribute{"name", "value", 0}
	var _ = jsonmodels.CustomFieldValue{"name", "string", "value"}
	var _ = jsonmodels.ExperimentVariant{"A", "{}"}
	var _ = jsonmodels.ExperimentApplication{"website"}
	var _ = jsonmodels.ContextData{nil}

	var ctx sdk.Context
	var _ map[string]sdk.ExperimentVariables = ctx.Index_
	var _ map[interface{}]interface{} = ctx.IndexVariables_
	var _ map[string]sdk.Assignment = ctx.AssignmentCache

	var _ sdk.ContextEventLogger = &v111Logger{}
	var _ sdk.ContextEventHandler = &v111Publisher{}
	var _ sdk.ContextDataProvider = v111Provider{}
	var _ sdk.VariableParser = &v111Parser{}
	var _ sdk.EventType = sdk.Close
}

func TestV111ConsumerBehaviour(t *testing.T) {
	var data = v111Data(t)
	var logger = &v111Logger{}
	var publisher = &v111Publisher{}
	var parser = &v111Parser{}
	var abs = sdk.Create(sdk.ABSmartlyConfig{v111Provider{data}, publisher, logger, parser, nil, nil})

	var ctx = abs.CreateContext(sdk.ContextConfig{Units_: map[string]string{
		"session_id": "e791e240fcd3df7d238cfc285f475e8152fcc0ec",
		"user_id":    "123456789",
		"email":      "bleh@absmartly.com",
	}, PublishDelay_: -1})
	ctx.WaitUntilReady()
	if !ctx.IsReady() || ctx.IsFailed() {
		t.Fatalf("ready=%v failed=%v", ctx.IsReady(), ctx.IsFailed())
	}
	if atomic.LoadInt32(&parser.calls) == 0 {
		t.Error("the customer's VariableParser was not used")
	}

	for name, want := range map[string]int{"exp_test_ab": 1, "exp_test_abc": 2, "exp_test_not_eligible": 0, "exp_test_fullon": 2} {
		got, err := ctx.GetTreatment(name)
		if err != nil || got != want {
			t.Errorf("GetTreatment(%q) = %d, %v; want %d", name, got, err, want)
		}
	}

	var assignment = ctx.GetAssignment("exp_test_ab")
	assignment.Variant = 7
	if got, _ := ctx.PeekTreatment("exp_test_ab"); got != 1 {
		t.Errorf("mutating GetAssignment's result changed the treatment to %d", got)
	}

	if err := ctx.SetUnit("session_id", "other"); err == nil || err.Error() != "unit already set" {
		t.Errorf("SetUnit error = %v, want %q", err, "unit already set")
	}
	if err := ctx.Track("payment", map[string]interface{}{"amount": 1}); err != nil {
		t.Fatal(err)
	}

	// v1.2.0 behaviour (v1.1.1 returned nil and dropped the batch): a failed
	// publish returns the handler's error and the retry sends the same batch.
	publisher.fail = true
	if err := ctx.Publish(); !errors.Is(err, errCollectorDown) {
		t.Errorf("Publish on a failed publish returned %v, want %v", err, errCollectorDown)
	}
	publisher.fail = false
	if err := ctx.Publish(); err != nil {
		t.Fatal(err)
	}
	if len(publisher.attempts) != 2 {
		t.Fatalf("publisher called %d times, want 2", len(publisher.attempts))
	}
	for i, attempt := range publisher.attempts {
		if len(attempt.Exposures) != 4 || len(attempt.Goals) != 1 {
			t.Errorf("attempt %d sent %d exposures and %d goals, want 4 and 1", i, len(attempt.Exposures), len(attempt.Goals))
		}
	}
	if len(publisher.published) != 1 {
		t.Errorf("published %d batches, want 1", len(publisher.published))
	}

	ctx.Close()
	if !ctx.IsClosed() || logger.count(sdk.Close) != 1 {
		t.Errorf("closed=%v Close events=%d", ctx.IsClosed(), logger.count(sdk.Close))
	}
	if _, err := ctx.GetTreatment("exp_test_ab"); err == nil || err.Error() != "ABSmartly Context is closed" {
		t.Errorf("GetTreatment after close: %v", err)
	}
	if err := ctx.Track("payment", nil); err == nil || err.Error() != "ABSmartly Context is closed" {
		t.Errorf("Track after close: %v", err)
	}
}
