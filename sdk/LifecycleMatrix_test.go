package sdk

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// This file checks the close / flush / refresh / logger-callback path against
// its invariants for every combination of
//
//	publisher behaviour x publish outcome x logger behaviour x caller action
//
// Invariants, checked for every combination:
//  1. Every future a caller gets settles, and a listener on it runs exactly once.
//  2. A failed batch stays queued and is delivered exactly once by a later retry.
//  3. A logger panic never reaches the host process (an escaped panic kills
//     the test binary) and never changes what was delivered.
//  4. Nothing deadlocks, including loggers that call Close, Publish or Refresh
//     from inside a callback.
//  5. A close that succeeds leaves nothing queued, nothing in flight, no
//     closing state and no refresh timer.

// matrixPublisher is the event handler and, through matrixProvider, the data
// provider. It decides when and how each call completes.
type matrixPublisher struct {
	mode  string // "sync": already complete, "async": own goroutine, "held": test settles
	fault string // "", "panic": panics when called, "nilfuture": returns nil

	failing atomic.Bool

	mu        sync.Mutex
	heldPub   []func()
	heldOther []func()
	delivered map[string]int
}

func (p *matrixPublisher) schedule(settle func(), refresh bool) {
	switch p.mode {
	case "sync":
		settle()
	case "async":
		go settle()
	default:
		p.mu.Lock()
		if refresh {
			p.heldOther = append(p.heldOther, settle)
		} else {
			p.heldPub = append(p.heldPub, settle)
		}
		p.mu.Unlock()
	}
}

func (p *matrixPublisher) Publish(_ Context, event jsonmodels.PublishEvent) *future.Future {
	switch p.fault {
	case "panic":
		panic("publisher panic")
	case "nilfuture":
		return nil
	}
	var fail = p.failing.Load()
	var result, done = future.New()
	p.schedule(func() {
		if fail {
			done(nil, errors.New("publish failed"))
			return
		}
		p.mu.Lock()
		for _, g := range event.Goals {
			p.delivered[g.Name]++
		}
		p.mu.Unlock()
		done(nil, nil)
	}, false)
	return result
}

// drain settles the held calls: publishes first, then refreshes, so a refresh
// can finish after the close it raced with.
func (p *matrixPublisher) drain() bool {
	p.mu.Lock()
	var batch = p.heldPub
	p.heldPub = nil
	if len(batch) == 0 {
		batch = p.heldOther
		p.heldOther = nil
	}
	p.mu.Unlock()
	for _, settle := range batch {
		settle()
	}
	return len(batch) > 0
}

func (p *matrixPublisher) deliveredCopy() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out = map[string]int{}
	for k, v := range p.delivered {
		out[k] = v
	}
	return out
}

type matrixProvider struct {
	p    *matrixPublisher
	data jsonmodels.ContextData
}

func (m matrixProvider) GetContextData() *future.Future {
	var result, done = future.New()
	m.p.schedule(func() { done(m.data, nil) }, true)
	return result
}

// matrixLogger misbehaves on one event type.
type matrixLogger struct {
	behavior string
	ctx      atomic.Pointer[Context]
	used     atomic.Bool
	inflight atomic.Int32

	// fired counts how often the misbehaviour ran; seen counts the events the
	// logger received. The cases assert on both, so a case cannot pass while its
	// logger never ran.
	fired atomic.Int32
	mu    sync.Mutex
	seen  map[EventType]int
}

func (l *matrixLogger) seenCount(event EventType) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seen[event]
}

func (l *matrixLogger) HandleEvent(supplied Context, event EventType, _ interface{}) {
	l.inflight.Add(1)
	defer l.inflight.Add(-1)
	l.mu.Lock()
	if l.seen == nil {
		l.seen = map[EventType]int{}
	}
	l.seen[event]++
	l.mu.Unlock()
	var c = l.ctx.Load()
	// The "...ViaCopy" behaviors act through the Context value the SDK passed
	// to the logger, not through the original pointer. Whether that copy still
	// lists the finished publish as in flight depends on scheduling the matrix
	// cannot control, so that ordering is pinned deterministically by
	// TestCloseThroughStaleCopyActsOnLiveContext instead.
	var copied = supplied
	var first = func() bool { return l.used.CompareAndSwap(false, true) }
	switch {
	case l.behavior == "panicPublish" && event == Publish,
		l.behavior == "panicError" && event == Error,
		l.behavior == "panicClose" && event == Close,
		l.behavior == "panicRefresh" && event == Refresh:
		l.fired.Add(1)
		panic("logger panic")
	case l.behavior == "closeOnPublish" && event == Publish && c != nil && first():
		l.fired.Add(1)
		c.Close()
	case l.behavior == "closeOnError" && event == Error && c != nil && first():
		l.fired.Add(1)
		c.Close()
	case l.behavior == "publishOnPublish" && event == Publish && c != nil && first():
		l.fired.Add(1)
		_, _ = c.PublishAsync()
	case l.behavior == "refreshOnPublish" && event == Publish && c != nil && first():
		l.fired.Add(1)
		c.Refresh()
	case l.behavior == "closeViaCopy" && event == Publish && first():
		l.fired.Add(1)
		copied.Close()
	case l.behavior == "closeViaCopyOnError" && event == Error && first():
		l.fired.Add(1)
		copied.Close()
	case l.behavior == "publishViaCopy" && event == Publish && first():
		l.fired.Add(1)
		_, _ = copied.PublishAsync()
	case l.behavior == "refreshOnError" && event == Error && c != nil && first():
		l.fired.Add(1)
		c.Refresh()
	case l.behavior == "refreshViaCopy" && event == Publish && first():
		l.fired.Add(1)
		copied.Refresh()
	}
}

type observedFuture struct {
	kind  string
	f     *future.Future
	calls *int32
}

type matrixAction struct {
	name    string
	closes  bool
	refresh bool
	run     func(c *Context, collect func(kind string, f *future.Future))
}

func inParallel(n int, fn func()) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}
	wg.Wait()
}

func matrixActions() []matrixAction {
	var publish = func(c *Context, collect func(string, *future.Future)) {
		if f, err := c.PublishAsync(); err == nil {
			collect("publish", f)
		}
	}
	var closeIt = func(c *Context, collect func(string, *future.Future)) {
		f, _ := c.CloseAsync()
		collect("close", f)
	}
	return []matrixAction{
		{name: "publish", run: publish},
		{name: "close", closes: true, run: closeIt},
		{name: "close6", closes: true, run: func(c *Context, collect func(string, *future.Future)) {
			inParallel(6, func() { closeIt(c, collect) })
		}},
		{name: "publishXclose", closes: true, run: func(c *Context, collect func(string, *future.Future)) {
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); publish(c, collect) }()
			go func() { defer wg.Done(); closeIt(c, collect) }()
			wg.Wait()
		}},
		{name: "publish3Xclose3", closes: true, run: func(c *Context, collect func(string, *future.Future)) {
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); inParallel(3, func() { publish(c, collect) }) }()
			go func() { defer wg.Done(); inParallel(3, func() { closeIt(c, collect) }) }()
			wg.Wait()
		}},
		{name: "refreshXclose", closes: true, refresh: true, run: func(c *Context, collect func(string, *future.Future)) {
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); collect("refresh", c.RefreshAsync()) }()
			go func() { defer wg.Done(); closeIt(c, collect) }()
			wg.Wait()
		}},
	}
}

func TestLifecycleMatrix(t *testing.T) {
	type outcome struct {
		name  string
		fault string
		fail  bool
	}
	var outcomes = []outcome{
		{"ok", "", false},
		{"fail", "", true},
		{"publisherPanics", "panic", true},
		{"publisherNil", "nilfuture", true},
	}
	var loggers = []string{"none", "panicPublish", "panicError", "panicClose", "panicRefresh",
		"closeOnPublish", "closeOnError", "publishOnPublish", "refreshOnPublish",
		"closeViaCopy", "closeViaCopyOnError", "publishViaCopy", "refreshViaCopy"}

	for _, mode := range []string{"sync", "async", "held"} {
		for _, oc := range outcomes {
			// A publisher that panics or returns nil does so before any mode applies.
			if oc.fault != "" && mode != "sync" {
				continue
			}
			for _, lg := range loggers {
				for _, act := range matrixActions() {
					var name = strings.Join([]string{mode, oc.name, lg, act.name}, "_")
					t.Run(name, func(t *testing.T) {
						for rep := 0; rep < 2; rep++ {
							runLifecycleCase(t, mode, oc.fault, oc.fail, lg, act)
						}
					})
				}
			}
		}
	}
}

func runLifecycleCase(t *testing.T, mode, fault string, failing bool, loggerBehavior string, act matrixAction) {
	t.Helper()
	setUp()
	var p = &matrixPublisher{mode: mode, fault: fault, delivered: map[string]int{}}
	p.failing.Store(failing)
	var logger = &matrixLogger{behavior: loggerBehavior}

	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.PublishDelay_ = -1
	config.RefreshInterval_ = 0
	if act.refresh {
		config.RefreshInterval_ = 5000
	}
	config.EventLogger_ = logger
	var ctx = createTrackedContext(clock, config, dataFutureReady, matrixProvider{p: p, data: data}, p, logger, variableParser, audienceMatcher)
	logger.ctx.Store(ctx)
	defer ctx.ClearRefreshTimer()

	var goals = []string{"g1", "g2", "g3"}
	for _, g := range goals {
		assertAny(nil, ctx.Track(g, nil), t)
	}

	var mu sync.Mutex
	var observed []*observedFuture
	var collect = func(kind string, f *future.Future) {
		if f == nil {
			return
		}
		var o = &observedFuture{kind: kind, f: f, calls: new(int32)}
		f.Listen(func(future.Value, error) { atomic.AddInt32(o.calls, 1) })
		mu.Lock()
		observed = append(observed, o)
		mu.Unlock()
	}

	var state = func() string {
		mu.Lock()
		defer mu.Unlock()
		var ready []string
		for _, o := range observed {
			ready = append(ready, fmt.Sprintf("%s:%v/%d", o.kind, o.f.Ready(), atomic.LoadInt32(o.calls)))
		}
		return fmt.Sprintf("closed=%v closing=%v pending=%d inflight=%d loggerBusy=%d delivered=%v futures=%v",
			ctx.IsClosed(), ctx.IsClosing(), ctx.GetPendingCount(), ctx.inflightCount(), logger.inflight.Load(), p.deliveredCopy(), ready)
	}

	// quiesce settles held calls until every future is ready and nothing is
	// running, or fails the case after a deadline (a deadlock or a lost callback).
	var quiesce = func(stage string) {
		t.Helper()
		var deadline = time.Now().Add(10 * time.Second)
		var stable = 0
		for {
			var progressed = p.drain()
			mu.Lock()
			var allReady = true
			for _, o := range observed {
				if !o.f.Ready() {
					allReady = false
				}
			}
			mu.Unlock()
			if !progressed && allReady && logger.inflight.Load() == 0 && ctx.inflightCount() == 0 {
				stable++
			} else {
				stable = 0
			}
			if stable >= 3 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: did not settle (deadlock or lost callback): %s", stage, state())
			}
			if !progressed {
				time.Sleep(time.Millisecond)
				runtime.Gosched()
			}
		}
	}

	act.run(ctx, collect)
	quiesce("after action")

	mu.Lock()
	var futs = append([]*observedFuture(nil), observed...)
	mu.Unlock()
	for _, o := range futs {
		if got := atomic.LoadInt32(o.calls); got != 1 {
			t.Fatalf("a %s future ran its listener %d times, want exactly 1: %s", o.kind, got, state())
		}
	}

	// The logger must really have run. A case whose logger never ran would pass
	// every invariant below without testing anything.
	if failing {
		if logger.seenCount(Error) == 0 {
			t.Fatalf("the logger never received the Error event of the failed publish: %s", state())
		}
	} else if logger.seenCount(Publish) == 0 {
		t.Fatalf("the logger never received the Publish event: %s", state())
	}
	var mustFire = false
	switch loggerBehavior {
	case "panicPublish", "closeOnPublish", "publishOnPublish", "refreshOnPublish", "closeViaCopy", "publishViaCopy", "refreshViaCopy":
		mustFire = !failing
	case "panicError", "closeOnError", "closeViaCopyOnError":
		mustFire = failing
	case "panicClose":
		mustFire = !failing && act.closes
	}
	// A refresh that succeeded must have reported a Refresh event. The event is
	// logged right after the future settles, so give it a moment.
	for _, o := range futs {
		if o.kind != "refresh" {
			continue
		}
		if _, err := o.f.Get(context.Background()); err != nil {
			continue
		}
		var waitUntil = time.Now().Add(5 * time.Second)
		for logger.seenCount(Refresh) == 0 && time.Now().Before(waitUntil) {
			time.Sleep(time.Millisecond)
		}
		if logger.seenCount(Refresh) == 0 {
			t.Fatalf("a refresh succeeded but the logger never received the Refresh event: %s", state())
		}
		if loggerBehavior == "panicRefresh" && logger.fired.Load() == 0 {
			t.Fatalf("logger behavior panicRefresh never ran: %s", state())
		}
	}
	if mustFire && logger.fired.Load() == 0 {
		t.Fatalf("logger behavior %q never ran: %s", loggerBehavior, state())
	}

	var delivered = p.deliveredCopy()
	if !failing {
		if act.closes && !ctx.IsClosed() {
			t.Fatalf("close succeeded but the context is not closed: %s", state())
		}
		if ctx.GetPendingCount() != 0 {
			t.Fatalf("events left queued after a successful run: %s", state())
		}
		for _, g := range goals {
			if delivered[g] != 1 {
				t.Fatalf("goal %s delivered %d times, want exactly 1: %s", g, delivered[g], state())
			}
		}
		for _, o := range futs {
			if o.kind == "close" {
				if _, err := o.f.Get(context.Background()); err != nil {
					t.Fatalf("close future failed after successful publishes: %v: %s", err, state())
				}
			}
			if o.kind == "publish" && loggerBehavior != "panicPublish" {
				if _, err := o.f.Get(context.Background()); err != nil {
					t.Fatalf("publish future failed: %v: %s", err, state())
				}
			}
		}
		if act.refresh && ctx.IsClosed() {
			ctx.StateLock_.Lock()
			var leaked = ctx.RefreshTimer_ != nil
			ctx.StateLock_.Unlock()
			if leaked {
				t.Fatalf("closed context still has a refresh timer: %s", state())
			}
		}
	} else {
		if ctx.IsClosed() || ctx.IsClosing() {
			t.Fatalf("a failed publish closed the context: %s", state())
		}
		if ctx.GetPendingCount() != int32(len(goals)) {
			t.Fatalf("failed batch is not queued for a retry (pending %d, want %d): %s", ctx.GetPendingCount(), len(goals), state())
		}
		if len(delivered) != 0 {
			t.Fatalf("events were delivered although every publish failed: %s", state())
		}
		for _, o := range futs {
			if o.kind == "close" {
				if _, err := o.f.Get(context.Background()); err == nil {
					t.Fatalf("close future succeeded although events are unsent: %s", state())
				}
			}
		}

		// Retry: the same events must be delivered exactly once.
		p.failing.Store(false)
		p.fault = ""
		logger.behavior = "none"
		fut, err := ctx.PublishAsync()
		if err != nil {
			t.Fatalf("retry rejected: %v: %s", err, state())
		}
		collect("retry", fut)
		quiesce("after retry")
		for _, g := range goals {
			if got := p.deliveredCopy()[g]; got != 1 {
				t.Fatalf("after retry goal %s delivered %d times, want exactly 1: %s", g, got, state())
			}
		}
		if ctx.GetPendingCount() != 0 {
			t.Fatalf("events left after retry: %s", state())
		}
		closing, _ := ctx.CloseAsync()
		collect("close", closing)
		quiesce("after final close")
		if !ctx.IsClosed() {
			t.Fatalf("context did not close after the retry: %s", state())
		}
	}

	if ctx.IsClosing() || ctx.inflightCount() != 0 {
		t.Fatalf("lifecycle state left behind: %s", state())
	}
}

// Track racing Close: every Track call either returns an error or its event is
// delivered. An event must never be accepted and then stranded behind a closed
// context.
func TestTrackRacingCloseNeverStrandsAnEvent(t *testing.T) {
	for _, mode := range []string{"sync", "async"} {
		for i := 0; i < 200; i++ {
			setUp()
			var p = &matrixPublisher{mode: mode, delivered: map[string]int{}}
			var config = CreateDefaultContextConfig()
			config.Units_ = units
			config.PublishDelay_ = -1
			config.RefreshInterval_ = 0
			var ctx = createTrackedContext(clock, config, dataFutureReady, matrixProvider{p: p, data: data}, p, nil, variableParser, audienceMatcher)

			var accepted sync.Map
			var wg sync.WaitGroup
			for g := 0; g < 8; g++ {
				wg.Add(1)
				go func(g int) {
					defer wg.Done()
					var name = fmt.Sprintf("t%d", g)
					if ctx.Track(name, nil) == nil {
						accepted.Store(name, true)
					}
				}(g)
			}
			wg.Add(1)
			go func() { defer wg.Done(); _, _ = ctx.CloseAsync() }()
			wg.Wait()

			var deadline = time.Now().Add(5 * time.Second)
			for (!ctx.IsClosed() || ctx.inflightCount() != 0) && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if !ctx.IsClosed() {
				t.Fatalf("%s iteration %d: context did not close", mode, i)
			}
			if ctx.GetPendingCount() != 0 {
				t.Fatalf("%s iteration %d: %d events stranded behind a closed context", mode, i, ctx.GetPendingCount())
			}
			var delivered = p.deliveredCopy()
			accepted.Range(func(k, _ interface{}) bool {
				if delivered[k.(string)] != 1 {
					t.Fatalf("%s iteration %d: accepted event %v delivered %d times", mode, i, k, delivered[k.(string)])
				}
				return true
			})
		}
	}
}

// A Context copy a logger receives is taken while the publish it is told about
// is still registered as in flight. Closing through that copy after the publish
// finished must act on the live context. A copy that kept its own stale
// in-flight list would wait on the finished publish for ever.
func TestCloseThroughStaleCopyActsOnLiveContext(t *testing.T) {
	var publisher = &heldPublisher{}
	var ctx = createHeldPublisherContext(t, publisher, nil)
	assertAny(nil, ctx.Track("goal1", nil), t)
	published, err := ctx.PublishAsync()
	assertAny(nil, err, t)

	var stale = ctx.snapshot() // taken while the publish is in flight
	publisher.settle(t, 0, nil)
	assertAny(nil, getWithTimeout(t, published), t)

	var closing, closeErr = stale.CloseAsync()
	assertAny(nil, closeErr, t)
	assertAny(nil, getWithTimeout(t, closing), t)
	assertAny(true, ctx.IsClosed(), t)
}

// A logger that panics while the SDK reports malformed data during creation
// must not leave the context's readiness pending.
func TestLoggerPanicWhileParsingDoesNotStrandReadiness(t *testing.T) {
	var malformed = jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{{
		Id: 1, Name: "bad_config", UnitType: "session_id", Split: []float64{0.5, 0.5}, TrafficSplit: []float64{0, 1},
		Variants:          []jsonmodels.ExperimentVariant{{Name: "A", Config: "{"}, {Name: "B", Config: `{"a":1}`}},
		CustomFieldValues: []jsonmodels.CustomFieldValue{{Name: "flag", Type: "boolean", Value: "notabool"}, {Name: "n", Type: "number", Value: "x"}},
	}}}
	for _, name := range []string{"async-initial-future", "ready-initial-future"} {
		t.Run(name, func(t *testing.T) {
			setUp()
			var logger = &matrixLogger{behavior: "panicError"}
			var config = CreateDefaultContextConfig()
			config.Units_ = units
			config.PublishDelay_ = -1
			config.RefreshInterval_ = 0
			config.EventLogger_ = logger

			var initial, settle = future.New()
			if name == "ready-initial-future" {
				settle(malformed, nil)
			}
			var ctx = createTrackedContext(clock, config, initial, dataProvider, eventHandler, logger, DefaultVariableParser{}, audienceMatcher)
			if name == "async-initial-future" {
				settle(malformed, nil)
			}
			var waited = make(chan struct{})
			go func() { ctx.WaitUntilReady(); close(waited) }()
			select {
			case <-waited:
			case <-time.After(5 * time.Second):
				t.Fatal("WaitUntilReady never returned after a logger panic during parsing")
			}
			assertAny(true, ctx.IsReady(), t)
			if logger.fired.Load() == 0 {
				t.Fatal("the logger never saw the parse error")
			}
		})
	}
}

// A variable parser that panics fails the data load instead of leaving the
// context permanently not ready.
func TestVariableParserPanicFailsLoadInsteadOfStrandingReadiness(t *testing.T) {
	for _, name := range []string{"async-initial-future", "ready-initial-future"} {
		t.Run(name, func(t *testing.T) {
			setUp()
			var config = CreateDefaultContextConfig()
			config.Units_ = units
			config.PublishDelay_ = -1
			config.RefreshInterval_ = 0
			var initial, settle = future.New()
			if name == "ready-initial-future" {
				settle(data, nil)
			}
			var ctx = createTrackedContext(clock, config, initial, dataProvider, eventHandler, nil, panickingParser{}, audienceMatcher)
			if name == "async-initial-future" {
				settle(data, nil)
			}
			var waited = make(chan struct{})
			go func() { ctx.WaitUntilReady(); close(waited) }()
			select {
			case <-waited:
			case <-time.After(5 * time.Second):
				t.Fatal("WaitUntilReady never returned after the variable parser panicked")
			}
			assertAny(true, ctx.IsReady(), t)
			assertAny(true, ctx.IsFailed(), t)
		})
	}
}

type panickingParser struct{}

func (panickingParser) Parse(Context, string, string, string) map[string]interface{} {
	panic("parser panic")
}

// Rejecting an exposure because the context is closing must not consume the
// assignment's exposure marker: a failed close reopens the context, and the
// next GetTreatment has to queue the exposure.
func TestRejectedExposureIsQueuedAfterFailedClose(t *testing.T) {
	setUp()
	var publisher = &heldPublisher{}
	var ctx = createHeldPublisherContext(t, publisher, nil)

	var assignment = ctx.GetAssignment("exp_test_ab")
	// Close has begun by the time the exposure is queued.
	ctx.Closing_.Store(true)
	ctx.QueueExposure(assignment)
	assertAny(false, assignment.Exposed.Load().(bool), t)
	assertAny(int32(0), ctx.GetPendingCount(), t)

	// The close failed and left the context open.
	ctx.Closing_.Store(false)
	_, err := ctx.GetTreatment("exp_test_ab")
	assertAny(nil, err, t)
	assertAny(int32(1), ctx.GetPendingCount(), t)
	assertAny(1, len(ctx.Exposures_), t)
}

// A parse diagnostic must not wait on the load that reports it.
func TestParseDiagnosticLoggerCallingRefreshDoesNotSelfWait(t *testing.T) {
	var malformed = jsonmodels.ContextData{Experiments: []jsonmodels.Experiment{{
		Id: 1, Name: "bad_config", UnitType: "session_id", Split: []float64{0.5, 0.5}, TrafficSplit: []float64{0, 1},
		Variants:          []jsonmodels.ExperimentVariant{{Name: "A", Config: "{"}, {Name: "B", Config: `{"a":1}`}},
		CustomFieldValues: []jsonmodels.CustomFieldValue{{Name: "flag", Type: "boolean", Value: "notabool"}},
	}}}
	setUp()
	var p = &matrixPublisher{mode: "async", delivered: map[string]int{}}
	var logger = &matrixLogger{behavior: "refreshOnError"}
	var config = CreateDefaultContextConfig()
	config.Units_ = units
	config.PublishDelay_ = -1
	config.RefreshInterval_ = 0
	config.EventLogger_ = logger
	var ctx = createTrackedContext(clock, config, dataFutureReady, matrixProvider{p: p, data: malformed}, p, logger, DefaultVariableParser{}, audienceMatcher)
	logger.ctx.Store(ctx)

	var refreshed = ctx.RefreshAsync()
	assertAny(nil, getWithTimeout(t, refreshed), t)
	var deadline = time.Now().Add(5 * time.Second)
	for logger.inflight.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if logger.fired.Load() == 0 {
		t.Fatal("the logger never saw a parse diagnostic")
	}
	if logger.seenCount(Error) < 2 {
		t.Fatalf("expected both the config and the custom field diagnostics, saw %d Error events", logger.seenCount(Error))
	}
	assertAny(nil, getWithTimeout(t, ctx.RefreshAsync()), t)
}
