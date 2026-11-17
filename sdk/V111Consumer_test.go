package sdk_test

// This file is written as a v1.1.1 customer would write it, outside the sdk
// package and with v1.1.1 names only: unkeyed struct literals of every
// configuration and model type whose layout v1.1.1 published, extension
// points implemented by customer types, and the v1.1.1 error texts. If it
// stops compiling or passing, v1.2.0 broke a v1.1.1 caller.

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

type v111Publisher struct {
	published []jsonmodels.PublishEvent
	fail      bool
}

func (p *v111Publisher) Publish(context sdk.Context, event jsonmodels.PublishEvent) *future.Future {
	return future.Call(func() (future.Value, error) {
		if p.fail {
			return nil, errors.New("collector down")
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
	// Unkeyed literals of every v1.1.1 configuration struct.
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

	// v1.1.1 exported fields keep their types.
	var ctx sdk.Context
	var _ map[string]sdk.ExperimentVariables = ctx.Index_
	var _ map[interface{}]interface{} = ctx.IndexVariables_
	var _ map[string]sdk.Assignment = ctx.AssignmentCache

	// Customer types still satisfy the v1.1.1 extension interfaces.
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

	// The variants v1.1.1's own tests pin for these units.
	for name, want := range map[string]int{"exp_test_ab": 1, "exp_test_abc": 2, "exp_test_not_eligible": 0, "exp_test_fullon": 2} {
		got, err := ctx.GetTreatment(name)
		if err != nil || got != want {
			t.Errorf("GetTreatment(%q) = %d, %v; want %d", name, got, err, want)
		}
	}

	// GetAssignment returns a copy: changing it does not change the context.
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

	publisher.fail = true
	if err := ctx.Publish(); err == nil {
		t.Error("Publish returned nil on a failed publish")
	}
	publisher.fail = false
	if err := ctx.Publish(); err != nil {
		t.Fatal(err)
	}
	if len(publisher.published) != 1 || len(publisher.published[0].Exposures) != 4 || len(publisher.published[0].Goals) != 1 {
		t.Errorf("published %#v", publisher.published)
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
