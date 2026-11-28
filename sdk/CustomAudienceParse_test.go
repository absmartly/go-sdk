package sdk

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/absmartly/go-sdk/sdk/jsonmodels"
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

// A custom deserializer is called once per evaluation: not for warm reads,
// and once per attribute change that revalidates the cached assignment,
// which keeps its single exposure.
func TestCustomDeserializerCallsOnWarmReadsAndRevalidation(t *testing.T) {
	var counter = &countingDeserializer{}
	var ctx, _ = newSystemAttrContext("website", "production", audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	ctx.AudienceMatcher_ = AudienceMatcher{counter}
	for i := 0; i < 50; i++ {
		var variant, err = ctx.GetTreatment("exp_app")
		assertAny(nil, err, t)
		assertAny(1, variant, t)
	}
	assertAny(1, len(counter.texts()), t)
	for change := 0; change < 5; change++ {
		assertAny(nil, ctx.SetAttribute("round", change), t)
		for i := 0; i < 10; i++ {
			var _, _ = ctx.GetTreatment("exp_app")
		}
	}
	assertAny(6, len(counter.texts()), t)
	assertAny(int32(1), ctx.GetPendingCount(), t)
}

// panicOnRevalidationDeserializer parses normally, except that call number
// panicAt panics.
type panicOnRevalidationDeserializer struct {
	calls   int32
	panicAt int32
}

func (d *panicOnRevalidationDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	if atomic.AddInt32(&d.calls, 1) == d.panicAt {
		panic("temporary")
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// A panic logger that peeks at the experiment whose revalidation panicked
// neither replaces the exposed assignment (no second exposure) nor makes the
// failure persist: the next attribute change asks the recovered deserializer
// again, as it does without a logger.
func TestPanicLoggerPeekDuringRevalidationKeepsTheExposure(t *testing.T) {
	for _, withLogger := range []bool{false, true} {
		var ctx, _ = newSystemAttrContext("website", "production",
			audienceExperiment("exp_app", `{"filter":[{"eq":[{"var":"country"},{"value":"US"}]}]}`, true))
		var d = &panicOnRevalidationDeserializer{panicAt: 2}
		ctx.AudienceMatcher_ = AudienceMatcher{d}
		assertAny(nil, ctx.SetAttribute("country", "US"), t)
		var variant, err = ctx.GetTreatment("exp_app")
		assertAny(nil, err, t)
		assertAny(1, variant, t)
		var peeks int32
		if withLogger {
			ctx.EventLogger_ = callbackEventLogger(func(c Context, event EventType, _ interface{}) {
				if event == Error && atomic.AddInt32(&peeks, 1) == 1 {
					var _, _ = c.PeekTreatment("exp_app")
				}
			})
		}
		assertAny(nil, ctx.SetAttribute("unrelated", 1), t)
		variant, err = ctx.GetTreatment("exp_app")
		assertAny(nil, err, t)
		assertAny(1, variant, t)
		assertAny(int32(1), ctx.GetPendingCount(), t)
		// Leaving the audience is seen by the recovered deserializer.
		assertAny(nil, ctx.SetAttribute("country", "FR"), t)
		variant, _ = ctx.PeekTreatment("exp_app")
		assertAny(0, variant, t)
		if withLogger {
			assertAny(int32(1), atomic.LoadInt32(&peeks), t)
		}
	}
}

// Within one context a custom parse and the evaluation of its result are
// serialised, as v1.1.1 serialised them under ContextLock_: two concurrent
// cold reads of different strict audiences, through a deserializer that
// reuses one *int operand (written under its own mutex) for every result,
// each see their own audience.
func TestConcurrentColdReadsOfReusedPointerOperandAudiences(t *testing.T) {
	var d = &pointerOperandDeserializer{}
	for round := 0; round < 100; round++ {
		var excluded = audienceExperiment("excluded", `{"n":0}`, true)
		var included = audienceExperiment("included", `{"n":1}`, true)
		included.Id = 2
		var ctx, _ = newSystemAttrContext("website", "production", excluded, included)
		ctx.AudienceMatcher_ = AudienceMatcher{d}
		var start = make(chan struct{})
		var wg sync.WaitGroup
		var variants [2]int
		var errs [2]error
		for i, name := range []string{"excluded", "included"} {
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
		if variants != [2]int{0, 1} {
			t.Fatalf("round %d: variants %v, want [0 1]", round, variants)
		}
	}
}

// peekingDeserializer decodes with the built-in deserializer; while parsing
// an audience with a "peek" key, it first reads that experiment of the same
// context, whose audience is custom too.
type peekingDeserializer struct {
	mu    sync.Mutex
	ctx   *Context
	peeks []int
}

func (d *peekingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	var doc struct{ Peek string }
	_ = json.Unmarshal(b[offset:offset+length], &doc)
	d.mu.Lock()
	var ctx = d.ctx
	d.mu.Unlock()
	if ctx != nil && doc.Peek != "" {
		var variant, _ = ctx.PeekTreatment(doc.Peek)
		d.mu.Lock()
		d.peeks = append(d.peeks, variant)
		d.mu.Unlock()
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// A deserializer that, while parsing one experiment, reads another custom
// audience of the same context re-enters the per-context custom-audience
// serialisation on its own goroutine without deadlocking, while other
// goroutines' cold reads wait their turn; every result is correct.
func TestDeserializerReadingAnotherCustomAudienceDoesNotDeadlock(t *testing.T) {
	for round := 0; round < 20; round++ {
		var a = audienceExperiment("a", `{"peek":"b","filter":[{"value":true}]}`, true)
		var b = audienceExperiment("b", `{"filter":[{"value":false}]}`, true)
		var c = audienceExperiment("c", `{"filter":[{"value":true}]}`, true)
		b.Id, c.Id = 2, 3
		var ctx, _ = newSystemAttrContext("website", "production", a, b, c)
		var d = &peekingDeserializer{ctx: ctx}
		ctx.AudienceMatcher_ = AudienceMatcher{d}
		var done = make(chan [3]int, 1)
		go func() {
			var results [3]int
			var wg sync.WaitGroup
			for i, name := range []string{"a", "b", "c"} {
				wg.Add(1)
				go func(i int, name string) {
					defer wg.Done()
					results[i], _ = ctx.PeekTreatment(name)
				}(i, name)
			}
			wg.Wait()
			done <- results
		}()
		select {
		case results := <-done:
			assertAny([3]int{1, 0, 1}, results, t)
		case <-time.After(5 * time.Second):
			t.Fatal("a deserializer reading another custom audience deadlocked")
		}
		d.mu.Lock()
		assertAny([]int{0}, d.peeks, t)
		d.mu.Unlock()
	}
}

// panickingDeserializer panics on audiences with a "panic" key and otherwise
// decodes with the built-in deserializer.
type panickingDeserializer struct{}

func (panickingDeserializer) Deserialize(b []byte, offset, length int) (map[string]interface{}, error) {
	var doc struct{ Panic bool }
	_ = json.Unmarshal(b[offset:offset+length], &doc)
	if doc.Panic {
		panic("broken")
	}
	return DefaultAudienceDeserializer{}.Deserialize(b, offset, length)
}

// A logger reporting a deserializer panic, logged while the custom-audience
// serialisation is held, can read another custom audience of the context.
func TestPanicLoggerReadingAnotherCustomAudienceDoesNotDeadlock(t *testing.T) {
	var a = audienceExperiment("a", `{"panic":true}`, true)
	var b = audienceExperiment("b", `{"filter":[{"value":false}]}`, true)
	b.Id = 2
	var ctx, _ = newSystemAttrContext("website", "production", a, b)
	ctx.AudienceMatcher_ = AudienceMatcher{panickingDeserializer{}}
	var peeked = make(chan int, 1)
	ctx.EventLogger_ = callbackEventLogger(func(c Context, event EventType, _ interface{}) {
		if event == Error {
			var variant, _ = c.PeekTreatment("b")
			peeked <- variant
		}
	})
	var done = make(chan int, 1)
	go func() { v, _ := ctx.PeekTreatment("a"); done <- v }()
	select {
	case v := <-done:
		assertAny(1, v, t) // unparseable, so ignored
	case <-time.After(5 * time.Second):
		t.Fatal("a panic logger reading another custom audience deadlocked")
	}
	assertAny(0, <-peeked, t)
}

// A deserializer that refreshes through an asynchronous provider waits for
// the Refresh event, logged on another goroutine; a logger reading another
// custom audience from that event does not wait for the deserializer's
// parse, so neither deadlocks.
func TestRefreshLoggerReadingCustomAudienceDuringDeserializerRefresh(t *testing.T) {
	var other = audienceExperiment("other", appFilter, true)
	other.Id = 2
	var client = &fetchClient{systemAttrClient: systemAttrClient{application: "website", environment: "production"},
		data: jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{audienceExperiment("exp_app", appFilter, true), other}}}
	var peeked = make(chan int, 4)
	var d = &snapshotCallingDeserializer{}
	var cc = CreateDefaultContextConfig()
	cc.Units_, cc.PublishDelay_, cc.RefreshInterval_ = withUnit(), -1, -1
	var ctx = Create(ABSmartlyConfig{Client_: client, ContextDataProvider_: DefaultContextDataProvider{client_: client},
		ContextEventHandler_: DefaultContextEventHandler{client_: client}, AudienceDeserializer_: d,
		ContextEventLogger_: callbackEventLogger(func(c Context, event EventType, _ interface{}) {
			if event == Refresh {
				var variant, _ = c.PeekTreatment("other")
				peeked <- variant
			}
		})}).CreateContextWith(cc, client.data)
	ctx.WaitUntilReady()
	d.mu.Lock()
	d.ctx = ctx
	d.mu.Unlock()
	var done = make(chan int, 1)
	go func() { v, _ := ctx.PeekTreatment("exp_app"); done <- v }()
	select {
	case v := <-done:
		assertAny(1, v, t)
	case <-time.After(5 * time.Second):
		t.Fatal("a Refresh logger reading a custom audience during a deserializer's Refresh deadlocked")
	}
	assertAny(1, <-peeked, t)
}

// Readers that selected the same cold audience before waiting at the gate
// reuse the assignment installed by the first reader, without decoding again.
func TestQueuedAudienceAttemptsRecheckBeforeDecode(t *testing.T) {
	var ctx, _ = newSystemAttrContext("website", "production",
		audienceExperiment("exp_app", `{"filter":[{"value":true}]}`, true))
	var counter = &countingDeserializer{}
	ctx.AudienceMatcher_ = AudienceMatcher{counter}
	var st = ctx.state()
	var serving *readServing
	var first, requested = ctx.buildAssignmentLocked(st, "exp_app", &clockReading{clock: ctx.Clock_}, false, nil, &serving)
	if first != nil || requested == nil {
		t.Fatal("expected a cold assignment requesting a parse")
	}
	const readers = 32
	var done = make(chan int, readers)
	for i := 0; i < readers; i++ {
		go func() {
			var serving *readServing
			var entry, pending = ctx.buildAudienceAttempt(st, "exp_app", &clockReading{clock: ctx.Clock_}, false, &serving)
			if pending != nil || entry == nil {
				done <- -1
				return
			}
			done <- entry.assignment.Variant
		}()
	}
	var deadline = time.After(5 * time.Second)
	for i := 0; i < readers; i++ {
		select {
		case variant := <-done:
			assertAny(1, variant, t)
		case <-deadline:
			t.Fatal("queued readers did not finish")
		}
	}
	assertAny(1, len(counter.texts()), t)
}
