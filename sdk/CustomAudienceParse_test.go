package sdk

import (
	"encoding/json"
	"sync"
	"testing"
)

// pointerOperandDeserializer decodes {"n": k} into a fresh filter
// eq(value(shared), value(&one)), where shared is one *int it reuses for
// every audience and sets to k: the result is correct only until the next
// call.
type pointerOperandDeserializer struct {
	mu     sync.Mutex
	shared *int
}

func (d *pointerOperandDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	var doc struct{ N int }
	if err := json.Unmarshal(b[offset:offset+length], &doc); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.shared == nil {
		d.shared = new(int)
	}
	*d.shared = doc.N
	var one = 1
	return map[string]interface{}{"filter": []interface{}{
		map[string]interface{}{"eq": []interface{}{
			map[string]interface{}{"value": d.shared},
			map[string]interface{}{"value": &one},
		}},
	}}, nil
}

// A custom deserializer's result is used at once and never kept, so one that
// reuses a pointer operand for the next audience cannot change an earlier
// audience's outcome: after another audience is decoded and an attribute
// change revalidates, the excluded experiment still gives control and
// exposes it with AudienceMismatch, as in v1.1.1.
func TestReusedPointerOperandDoesNotChangeAnEarlierAudience(t *testing.T) {
	var excluded = audienceExperiment("excluded", `{"n":0}`, true)
	var included = audienceExperiment("included", `{"n":1}`, true)
	included.Id = 2
	var ctx, _ = newSystemAttrContext("website", "production", excluded, included)
	ctx.AudienceMatcher_ = AudienceMatcher{&pointerOperandDeserializer{}}
	assertAny(0, treatmentPeek(t, ctx, "excluded"), t)
	assertAny(1, treatmentPeek(t, ctx, "included"), t)
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	var variant, err = ctx.GetTreatment("excluded")
	assertAny(nil, err, t)
	assertAny(0, variant, t)
	ctx.EventLock_.Lock()
	defer ctx.EventLock_.Unlock()
	assertAny(1, len(ctx.Exposures_), t)
	assertAny(true, ctx.Exposures_[0].AudienceMismatch, t)
}

// shortCircuitDeserializer returns a fresh outer filter
// or(value(true), value(scratch)), rewriting the reused scratch slice under
// its own mutex on every call. The evaluator never reads scratch (or stops
// at true), so evaluating its result is race-free.
type shortCircuitDeserializer struct {
	mu      sync.Mutex
	scratch []interface{}
	calls   int
}

func (d *shortCircuitDeserializer) Deserialize([]byte, int, int) (map[string]interface{}, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.scratch == nil {
		d.scratch = make([]interface{}, 8)
	}
	d.calls++
	for i := range d.scratch {
		d.scratch[i] = d.calls
	}
	return map[string]interface{}{"filter": map[string]interface{}{"or": []interface{}{
		map[string]interface{}{"value": true},
		map[string]interface{}{"value": d.scratch},
	}}}, nil
}

// Concurrent cold reads of two audiences only evaluate the deserializer's
// result, which reads what v1.1.1 read: the SDK never walks the parts of a
// result the evaluator does not reach, so this is race-free under -race.
func TestConcurrentColdReadsOfShortCircuitAudiencesAreRaceFree(t *testing.T) {
	var d = &shortCircuitDeserializer{}
	for round := 0; round < 50; round++ {
		var first = audienceExperiment("first", `{"a":1}`, true)
		var second = audienceExperiment("second", `{"a":2}`, true)
		second.Id = 2
		var ctx, _ = newSystemAttrContext("website", "production", first, second)
		ctx.AudienceMatcher_ = AudienceMatcher{d}
		var start = make(chan struct{})
		var wg sync.WaitGroup
		var variants [2]int
		var errs [2]error
		for i, name := range []string{"first", "second"} {
			wg.Add(1)
			go func(i int, name string) {
				defer wg.Done()
				<-start
				variants[i], errs[i] = ctx.PeekTreatment(name)
			}(i, name)
		}
		close(start)
		wg.Wait()
		assertAny([2]error{}, errs, t)
		assertAny([2]int{1, 1}, variants, t)
	}
}

// cyclicLiteralDeserializer returns not(value(m)) where m contains itself:
// a literal operand the evaluator never walks into.
type cyclicLiteralDeserializer struct{}

func (cyclicLiteralDeserializer) Deserialize([]byte, int, int) (map[string]interface{}, error) {
	var m = map[string]interface{}{}
	m["self"] = m
	return map[string]interface{}{"filter": map[string]interface{}{"not": map[string]interface{}{"value": m}}}, nil
}

// An audience with a cyclic literal operand is evaluable, as in v1.1.1: it is
// false, so the strict experiment gives control with AudienceMismatch.
func TestCyclicLiteralOperandIsEvaluated(t *testing.T) {
	var result, err = AudienceMatcher{cyclicLiteralDeserializer{}}.Evaluate(`{}`, nil)
	assertAny(nil, err, t)
	assertAny(false, result.Get(), t)

	var ctx, _ = newSystemAttrContext("website", "production", audienceExperiment("exp_app", appFilter, true))
	ctx.AudienceMatcher_ = AudienceMatcher{cyclicLiteralDeserializer{}}
	var variant, gerr = ctx.GetTreatment("exp_app")
	assertAny(nil, gerr, t)
	assertAny(0, variant, t)
	ctx.EventLock_.Lock()
	defer ctx.EventLock_.Unlock()
	assertAny(1, len(ctx.Exposures_), t)
	assertAny(true, ctx.Exposures_[0].AudienceMismatch, t)
}

// The built-in deserializer's parse is cached once per audience text and
// reused by revalidation; a custom deserializer's parse is never stored.
func TestOnlyTheBuiltInDeserializerParseIsCached(t *testing.T) {
	var cached = func(ctx *Context) map[string]interface{} {
		var entries = map[string]interface{}{}
		ctx.state().audiences.Range(func(text, entry interface{}) bool {
			entries[text.(string)] = entry
			return true
		})
		return entries
	}
	for _, builtin := range []AudienceDeserializer{nil, DefaultAudienceDeserializer{}, &DefaultAudienceDeserializer{}} {
		var first = audienceExperiment("first", appFilter, true)
		var second = audienceExperiment("second", appFilter, true)
		second.Id = 2
		var ctx, _ = newSystemAttrContext("website", "production", first, second)
		ctx.AudienceMatcher_ = AudienceMatcher{builtin}
		assertAny(1, treatmentPeek(t, ctx, "first"), t)
		var entries = cached(ctx)
		assertAny(1, len(entries), t)
		var entry = entries[appFilter]
		if entry == nil {
			t.Fatalf("%T: the parse was not cached", builtin)
		}
		assertAny(1, treatmentPeek(t, ctx, "second"), t)
		assertAny(nil, ctx.SetAttribute("x", 1), t)
		assertAny(1, treatmentPeek(t, ctx, "first"), t)
		entries = cached(ctx)
		assertAny(1, len(entries), t)
		if entries[appFilter] != entry {
			t.Errorf("%T: the audience was parsed again", builtin)
		}
	}

	var counter = &countingDeserializer{}
	var ctx, _ = newSystemAttrContext("website", "production", audienceExperiment("exp_app", appFilter, true))
	ctx.AudienceMatcher_ = AudienceMatcher{counter}
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	assertAny(0, len(cached(ctx)), t)
	var exposed = ctx.GetAssignment("exp_app").Exposed
	assertAny(nil, ctx.SetAttribute("x", 1), t)
	assertAny(1, treatmentPeek(t, ctx, "exp_app"), t)
	assertAny(0, len(cached(ctx)), t)
	assertAny([]string{appFilter, appFilter}, counter.texts(), t)
	if ctx.GetAssignment("exp_app").Exposed != exposed {
		t.Error("revalidating with the same outcome rebuilt the assignment")
	}
}
