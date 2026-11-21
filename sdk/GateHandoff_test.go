package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// The tests in this file cover lending the custom audience gate to a
// Refresh or initial load its holder waits for. A and B are strict custom
// audiences: A's filter is true (variant 1), B's false (variant 0, mismatch).

const handoffTrue = `{"filter":[{"value":true}]}`
const handoffFalse = `{"filter":[{"value":false}]}`

const handoffTimeout = 5 * time.Second

func handoffExperiment(name string, id int, audience string) jsonmodels.Experiment {
	return jsonmodels.Experiment{Id: id, Name: name, UnitType: "session_id", Iteration: 1, SeedHi: 1, SeedLo: 2,
		Split: []float64{0, 1}, TrafficSeedHi: 3, TrafficSeedLo: 4, TrafficSplit: []float64{0, 1},
		AudienceStrict: true, Audience: audience,
		Variants: []jsonmodels.ExperimentVariant{{Name: "A"}, {Name: "B", Config: `{"k":1}`}}}
}

// handoffProvider settles each GetContextData on a new goroutine once release
// (if set) is closed.
type handoffProvider struct {
	data    jsonmodels.ContextData
	release chan struct{}
}

func (p handoffProvider) GetContextData() *future.Future {
	var f, done = future.New()
	go func() {
		if p.release != nil {
			<-p.release
		}
		done(p.data, nil)
	}()
	return f
}

// handoffDeserializer returns one reused result map from every call. For A's
// text it runs hook; afterHook selects whether it fills the map after the
// hook (safe across its own nested calls) or before it (a nested call made
// by the hook overwrites the map, like a deserializer racing another call).
type handoffDeserializer struct {
	mu        sync.Mutex
	scratch   map[string]interface{}
	hook      func()
	afterHook bool
}

func (d *handoffDeserializer) Deserialize(b []byte, _ int, _ int) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	d.mu.Lock()
	var hook, after = d.hook, d.afterHook
	if string(b) != handoffTrue {
		hook = nil
	}
	d.mu.Unlock()
	var fill = func() {
		d.mu.Lock()
		if d.scratch == nil {
			d.scratch = map[string]interface{}{}
		}
		d.scratch["filter"] = m["filter"]
		d.mu.Unlock()
	}
	if !after {
		fill()
	}
	if hook != nil {
		hook()
	}
	if after {
		fill()
	}
	return d.scratch, nil
}

func (d *handoffDeserializer) setHook(after bool, hook func()) {
	d.mu.Lock()
	d.hook, d.afterHook = hook, after
	d.mu.Unlock()
}

type handoffParser func(Context, string, string, string) map[string]interface{}

func (f handoffParser) Parse(c Context, e, v, val string) map[string]interface{} {
	return f(c, e, v, val)
}

type handoffSetup struct {
	ctx    *Context
	d      *handoffDeserializer
	mu     sync.Mutex
	logger func(Context, EventType)
	parse  func(Context)
}

func newHandoff(t *testing.T, release chan struct{}) *handoffSetup {
	setUp()
	var h = &handoffSetup{d: &handoffDeserializer{}}
	var data = jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		handoffExperiment("A", 1, handoffTrue), handoffExperiment("B", 2, handoffFalse)}}
	var ready, done = future.New()
	done(data, nil)
	var cfg = ContextConfig{Units_: map[string]string{"session_id": "u1"}, PublishDelay_: -1}
	h.ctx = CreateContext(clock, cfg, ready, handoffProvider{data, release}, eventHandler,
		callbackEventLogger(func(c Context, event EventType, _ interface{}) {
			h.mu.Lock()
			var l = h.logger
			h.mu.Unlock()
			if l != nil {
				l(c, event)
			}
		}),
		handoffParser(func(c Context, e, v, val string) map[string]interface{} {
			h.mu.Lock()
			var f = h.parse
			h.mu.Unlock()
			if f != nil {
				f(c)
			}
			return DefaultVariableParser{}.Parse(c, e, v, val)
		}), AudienceMatcher{h.d})
	h.ctx.WaitUntilReady()
	return h
}

func (h *handoffSetup) setLogger(l func(Context, EventType)) {
	h.mu.Lock()
	h.logger = l
	h.mu.Unlock()
}

func (h *handoffSetup) setParse(p func(Context)) {
	h.mu.Lock()
	h.parse = p
	h.mu.Unlock()
}

func handoffWithin(t *testing.T, what string, fn func()) {
	t.Helper()
	var done = make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(handoffTimeout):
		t.Fatal(what + " did not finish (deadlock)")
	}
}

func handoffExposure(t *testing.T, ctx *Context, name string) jsonmodels.Exposure {
	t.Helper()
	ctx.EventLock_.Lock()
	defer ctx.EventLock_.Unlock()
	for _, e := range ctx.Exposures_ {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no exposure for %s", name)
	return jsonmodels.Exposure{}
}

func assertHandoffB(t *testing.T, ctx *Context, peeked int) {
	t.Helper()
	assertAny(0, peeked, t)
	var b = ctx.GetAssignment("B")
	assertAny(0, b.Variant, t)
	assertAny(true, b.AudienceMismatch, t)
}

// (a) A's deserializer calls Refresh; the async provider settles on another
// goroutine, whose Refresh event logger reads cold custom B. The holder lent
// the gate to the refresh, so the logger's read runs nested and both finish.
// The deserializer fills its result after the nested call, so A is right.
// The Refresh event is logged before Refresh returns, so B's decode always
// happens inside A's deserializer call: one that fills its reused map before
// calling Refresh gets B's filter back for A (v1.1.1 logged after Refresh
// returned, so B decoded after A was evaluated there). Removing the lend
// deadlocks this test; it does not distinguish the lend from the former
// logger bypass, which TestHandoffIndependentLoggerWaitsForParse and
// TestHandoffUnrelatedRefreshLoggerWaitsForParse do.
func TestHandoffRefreshLoggerReadsColdAudience(t *testing.T) {
	var h = newHandoff(t, nil)
	var peekB = make(chan int, 1)
	h.setLogger(func(c Context, event EventType) {
		if event == Refresh {
			var v, _ = c.PeekTreatment("B")
			peekB <- v
		}
	})
	var once sync.Once
	h.d.setHook(true, func() { once.Do(func() { h.ctx.Refresh() }) })
	var a int
	handoffWithin(t, "deserializer Refresh with a logger reading B", func() { a, _ = h.ctx.GetTreatment("A") })
	assertAny(1, a, t)
	var exposure = handoffExposure(t, h.ctx, "A")
	assertAny(1, exposure.Variant, t)
	assertAny(false, exposure.AudienceMismatch, t)
	assertAny(true, exposure.Assigned, t)
	assertHandoffB(t, h.ctx, <-peekB)
}

// (b) as (a), but the variable parser run while the refresh installs its
// data reads cold custom B. v1.1.1 deadlocked here.
func TestHandoffRefreshParserReadsColdAudience(t *testing.T) {
	var h = newHandoff(t, nil)
	var peekB = make(chan int, 4)
	var once sync.Once
	h.d.setHook(true, func() {
		once.Do(func() {
			h.setParse(func(c Context) { var v, _ = c.PeekTreatment("B"); peekB <- v })
			h.ctx.Refresh()
			h.setParse(nil)
		})
	})
	var a int
	handoffWithin(t, "deserializer Refresh with a parser reading B", func() { a, _ = h.ctx.GetTreatment("A") })
	assertAny(1, a, t)
	assertAny(false, handoffExposure(t, h.ctx, "A").AudienceMismatch, t)
	assertHandoffB(t, h.ctx, <-peekB)
}

// (c) A's deserializer reads cold custom B on its own goroutine. v1.1.1
// deadlocked here; the read now runs nested. B is right; A's result depends
// on the deserializer's own storage reuse across its nested call (this one
// fills before the call, so it is overwritten), so only liveness is checked.
func TestHandoffDeserializerReadsColdAudienceOnItsGoroutine(t *testing.T) {
	var h = newHandoff(t, nil)
	var once sync.Once
	var b = -9
	h.d.setHook(false, func() { once.Do(func() { b, _ = h.ctx.PeekTreatment("B") }) })
	handoffWithin(t, "deserializer reading B on its goroutine", func() { _, _ = h.ctx.PeekTreatment("A") })
	assertHandoffB(t, h.ctx, b)
}

// (d) An independent goroutine's Track, whose Goal logger reads cold custom
// B, while another goroutine is in A's deserializer: the logger waits for
// A's evaluation, as in v1.1.1, so the reused result map is not overwritten.
func TestHandoffIndependentLoggerWaitsForParse(t *testing.T) {
	var h = newHandoff(t, nil)
	var entered, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	var peekB = make(chan int, 1)
	h.setLogger(func(c Context, event EventType) {
		if event == Goal {
			var v, _ = c.PeekTreatment("B")
			peekB <- v
		}
	})
	h.d.setHook(false, func() { once.Do(func() { close(entered); <-release }) })
	var a = make(chan *Assignment, 1)
	go func() { a <- h.ctx.GetAssignment("A") }()
	select {
	case <-entered:
	case <-time.After(handoffTimeout):
		t.Fatal("A's deserializer was not called")
	}
	var tracked = make(chan struct{})
	go func() { _ = h.ctx.Track("g", nil); close(tracked) }()
	select {
	case <-peekB:
		t.Fatal("an independent logger read B while A's parse was in progress")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	var as *Assignment
	select {
	case as = <-a:
	case <-time.After(handoffTimeout):
		t.Fatal("A's read did not finish")
	}
	assertAny(1, as.Variant, t)
	assertAny(false, as.AudienceMismatch, t)
	select {
	case <-tracked:
	case <-time.After(handoffTimeout):
		t.Fatal("Track did not finish")
	}
	assertHandoffB(t, h.ctx, <-peekB)
}

// A refresh that A's deserializer is not waiting for (started on another
// goroutine) completes while A's parse is in progress: its Refresh logger's
// read of cold custom B waits for A's evaluation, as in v1.1.1, so the
// deserializer's reused result map is not overwritten before it is used.
func TestHandoffUnrelatedRefreshLoggerWaitsForParse(t *testing.T) {
	var h = newHandoff(t, nil)
	var entered, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	var peekB = make(chan int, 1)
	h.setLogger(func(c Context, event EventType) {
		if event == Refresh {
			var v, _ = c.PeekTreatment("B")
			peekB <- v
		}
	})
	h.d.setHook(false, func() { once.Do(func() { close(entered); <-release }) })
	var a = make(chan *Assignment, 1)
	go func() { a <- h.ctx.GetAssignment("A") }()
	select {
	case <-entered:
	case <-time.After(handoffTimeout):
		t.Fatal("A's deserializer was not called")
	}
	var refreshed = h.ctx.RefreshAsync()
	select {
	case <-peekB:
		t.Fatal("an unrelated refresh's logger read B while A's parse was in progress")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	var as *Assignment
	select {
	case as = <-a:
	case <-time.After(handoffTimeout):
		t.Fatal("A's read did not finish")
	}
	assertAny(1, as.Variant, t)
	assertAny(false, as.AudienceMismatch, t)
	handoffWithin(t, "the unrelated refresh", func() { refreshed.Join(context.Background()) })
	assertHandoffB(t, h.ctx, <-peekB)
}

// While A's deserializer waits in Refresh, a read from a goroutine that is
// not that refresh's executor still waits for A's evaluation.
func TestHandoffLendAdmitsOnlyTheOperationExecutor(t *testing.T) {
	var release = make(chan struct{})
	var h = newHandoff(t, nil)
	h.ctx.DataProvider_ = handoffProvider{h.ctx.Data_, release}
	var refreshing = make(chan struct{})
	var once sync.Once
	h.d.setHook(false, func() {
		once.Do(func() {
			close(refreshing)
			h.ctx.Refresh()
		})
	})
	var a = make(chan int, 1)
	go func() { v, _ := h.ctx.PeekTreatment("A"); a <- v }()
	select {
	case <-refreshing:
	case <-time.After(handoffTimeout):
		t.Fatal("A's deserializer was not called")
	}
	var b = make(chan int, 1)
	go func() { v, _ := h.ctx.PeekTreatment("B"); b <- v }()
	select {
	case <-b:
		t.Fatal("an unrelated goroutine read B while A's parse was in progress")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case v := <-a:
		assertAny(1, v, t)
	case <-time.After(handoffTimeout):
		t.Fatal("A's read did not finish")
	}
	select {
	case v := <-b:
		assertHandoffB(t, h.ctx, v)
	case <-time.After(handoffTimeout):
		t.Fatal("B's read did not finish")
	}
}

// A deserializer waiting in WaitUntilReady lends the gate to the initial
// load, whose Ready logger reads cold custom B.
func TestHandoffReadyLoggerReadsColdAudience(t *testing.T) {
	setUp()
	var d = &handoffDeserializer{}
	var data = jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{
		handoffExperiment("A", 1, handoffTrue), handoffExperiment("B", 2, handoffFalse)}}
	var ready, settle = future.New()
	var peekB = make(chan int, 1)
	var cfg = ContextConfig{Units_: map[string]string{"session_id": "u1"}, PublishDelay_: -1}
	var ctx = CreateContext(clock, cfg, ready, handoffProvider{data, nil}, eventHandler,
		callbackEventLogger(func(c Context, event EventType, _ interface{}) {
			if event == Ready {
				var v, _ = c.PeekTreatment("B")
				peekB <- v
			}
		}), DefaultVariableParser{}, AudienceMatcher{d})
	// Install data so A can be read before the pending load settles.
	ctx.SetData(data)
	var once sync.Once
	d.setHook(true, func() {
		once.Do(func() {
			go settle(data, nil)
			ctx.WaitUntilReady()
		})
	})
	var a int
	handoffWithin(t, "deserializer WaitUntilReady with a logger reading B", func() { a, _ = ctx.PeekTreatment("A") })
	assertAny(1, a, t)
	assertHandoffB(t, ctx, <-peekB)
}

// Unit tests of the gate: an executor is admitted when the lend is recorded
// whether it registered and blocked before the lend or arrives after it;
// another goroutine is not; depth is accounted per goroutine.
func TestReentrantGateLend(t *testing.T) {
	var blocked = func(ch chan struct{}) bool {
		select {
		case <-ch:
			return false
		case <-time.After(100 * time.Millisecond):
			return true
		}
	}
	for _, executorFirst := range []bool{true, false} {
		var g reentrantGate
		var op = &gateOperation{}
		g.lock()
		g.lock() // nested reentry of the holder
		var registered, admitted, finish, released = make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var other = make(chan struct{})
		go func() {
			var end = g.execute(op)
			close(registered)
			g.lock()
			close(admitted)
			g.lock() // the executor's own nested reentry
			g.unlock()
			<-finish
			g.unlock()
			end()
			close(released)
		}()
		<-registered
		var endLend func()
		if executorFirst {
			if !blocked(admitted) {
				t.Fatal("executor admitted before the lend")
			}
			endLend = g.lend(op)
		} else {
			endLend = g.lend(op)
		}
		select {
		case <-admitted:
		case <-time.After(handoffTimeout):
			t.Fatal("executor not admitted after the lend")
		}
		go func() { g.lock(); close(other); g.unlock() }()
		if !blocked(other) {
			t.Fatal("a non-executor was admitted through the lend")
		}
		// The executor of an operation the holder is not waiting for (say an
		// unrelated refresh completing) is not admitted either.
		var unrelated = make(chan struct{})
		go func() {
			defer g.execute(&gateOperation{})()
			g.lock()
			close(unrelated)
			g.unlock()
		}()
		if !blocked(unrelated) {
			t.Fatal("the executor of an operation the holder is not waiting for was admitted")
		}
		close(finish)
		<-released
		endLend()
		g.unlock()
		if !blocked(other) {
			t.Fatal("gate released while the holder still held it")
		}
		g.unlock()
		for _, waiter := range []chan struct{}{other, unrelated} {
			select {
			case <-waiter:
			case <-time.After(handoffTimeout):
				t.Fatal("waiter not admitted after the holder released the gate")
			}
		}
	}
}

// A's deserializer calls Refresh; that refresh's logger calls Refresh again,
// and the nested refresh's logger reads cold custom B. The first refresh's
// executor lends the gate on to the nested refresh, so every read finishes.
func TestHandoffNestedRefreshLoggerReadsColdAudience(t *testing.T) {
	var h = newHandoff(t, nil)
	var refreshes, peekB = make(chan struct{}, 2), make(chan int, 1)
	var depth int32
	h.setLogger(func(c Context, event EventType) {
		if event != Refresh {
			return
		}
		refreshes <- struct{}{}
		h.mu.Lock()
		depth++
		var d = depth
		h.mu.Unlock()
		if d == 1 {
			h.ctx.Refresh()
		} else if d == 2 {
			var v, _ = c.PeekTreatment("B")
			peekB <- v
		}
	})
	var once sync.Once
	h.d.setHook(true, func() { once.Do(func() { h.ctx.Refresh() }) })
	var a int
	handoffWithin(t, "Refresh nested in a Refresh logger reading B", func() { a, _ = h.ctx.GetTreatment("A") })
	assertAny(1, a, t)
	assertAny(2, len(refreshes), t)
	assertHandoffB(t, h.ctx, <-peekB)
}

// A's deserializer calls Publish; the publish's event logger reads cold
// custom B. Publish lends the gate to its logger, so both finish.
func TestHandoffPublishLoggerReadsColdAudience(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var h = newHandoff(t, nil)
		var publisher = &CapturingPublishClient{}
		if fail {
			publisher.publishErr = errors.New("collector down")
		}
		h.ctx.EventHandler_ = DefaultContextEventHandler{client_: publisher}
		assertAny(nil, h.ctx.Track("goal", nil), t)
		var peekB = make(chan int, 1)
		h.setLogger(func(c Context, event EventType) {
			if event == Publish || event == Error {
				var v, _ = c.PeekTreatment("B")
				peekB <- v
			}
		})
		var once sync.Once
		h.d.setHook(true, func() { once.Do(func() { _ = h.ctx.Publish() }) })
		var a int
		handoffWithin(t, "deserializer Publish with a logger reading B", func() { a, _ = h.ctx.GetTreatment("A") })
		assertAny(1, a, t)
		assertHandoffB(t, h.ctx, <-peekB)
	}
}

// A's deserializer calls Close while the final publish fails and its Error
// logger reads cold custom B: Close lends the gate to that publish, so the
// failed close returns, the context stays open, and the reads finish.
func TestHandoffCloseWithFailedPublishLoggerReadsColdAudience(t *testing.T) {
	var h = newHandoff(t, nil)
	h.ctx.EventHandler_ = DefaultContextEventHandler{client_: &CapturingPublishClient{publishErr: errors.New("collector down")}}
	assertAny(nil, h.ctx.Track("goal", nil), t)
	var peekB = make(chan int, 1)
	h.setLogger(func(c Context, event EventType) {
		if event == Error {
			select {
			case peekB <- func() int { v, _ := c.PeekTreatment("B"); return v }():
			default:
			}
		}
	})
	var once sync.Once
	h.d.setHook(true, func() { once.Do(func() { h.ctx.Close() }) })
	var a int
	handoffWithin(t, "deserializer Close with a failing publish whose logger reads B", func() { a, _ = h.ctx.GetTreatment("A") })
	assertAny(1, a, t)
	assertAny(false, h.ctx.IsClosed(), t)
	assertHandoffB(t, h.ctx, <-peekB)
}
