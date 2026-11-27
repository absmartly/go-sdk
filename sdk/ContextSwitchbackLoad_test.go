package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/absmartly/go-sdk/sdk/future"
	"github.com/absmartly/go-sdk/sdk/internal"
	"github.com/absmartly/go-sdk/sdk/jsonmodels"
)

// Switchbacks load through the plain ContextData path, as holdouts do: the
// default client decodes the switchback fields into jsonmodels.Experiment,
// and CreateContext, CreateContextWith and SetData serve the schedule from
// them. GetData returns them as installed.

// sbHTTP is HttpMock with the body readable through resty's Body.
type sbHTTP struct{ HttpMock }

func (h sbHTTP) Get(string, map[string]string, map[string]string) *future.Future {
	return future.Call(func() (future.Value, error) {
		return h.response().SetBody([]byte(h.bodyString)), nil
	})
}

func sbBody(t *testing.T, data jsonmodels.ContextData) string {
	t.Helper()
	var raw, err = json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func sbLoadConfig() ContextConfig {
	var config = CreateDefaultContextConfig()
	config.PublishDelay_ = -1
	config.RefreshInterval_ = -1
	config.Units_[sbUnitType] = sbUID
	config.Attributes_ = map[string]interface{}{sbClusterPath: sbClusterKey}
	return config
}

// sbExpectScheduled checks the context served the switchback's scheduled arm
// for the current period: a unit experiment on the same definition would
// draw by the unit hash instead.
func sbExpectScheduled(t *testing.T, ctx *Context, want int) {
	t.Helper()
	var variant, err = ctx.PeekTreatment("exp_sb")
	if err != nil {
		t.Fatal(err)
	}
	expectInt(t, want, variant)
	var assignment = ctx.GetAssignment("exp_sb")
	if assignment.UnitType != sbUnitType {
		t.Fatalf("served under %q, want the serving unit %q", assignment.UnitType, sbUnitType)
	}
}

// The default client decodes the switchback fields and CreateContext serves
// the schedule; the fields round-trip through GetData.
func TestSwitchbackLoad_CreateContextServesFromTheClient(t *testing.T) {
	var data = sbData(sbDefault(2, sbHour))
	var client = CreateClient(ClientConfig{Deserializer_: DefaultContextDataDeserializer{}, Endpoint_: "https://localhost/v1",
		ApiKey_: "key", Application_: "website", Environment_: "dev"}, sbHTTP{HttpMock{bodyString: sbBody(t, data)}})
	var value, err = client.GetContextData().Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var decoded = value.(jsonmodels.ContextData)
	if e := decoded.Experiments[0]; e.RandomizationType != "switchback" || len(e.SwitchbackClusters) != 1 ||
		e.SwitchbackStartAt == nil || *e.SwitchbackStartAt != sbAnchor || e.SwitchbackPeriodMs == nil || *e.SwitchbackPeriodMs != sbHour {
		t.Fatalf("decoded switchback fields %+v", e)
	}
	var abs = Create(ABSmartlyConfig{Client_: client})
	var ctx = createContext(&sbClock{now: sbAnchor + 5}, sbLoadConfig(), abs.ContextDataProvider_.GetContextData(),
		abs.ContextDataProvider_, abs.ContextEventHandler_, nil, abs.VariableParser_, AudienceMatcher{abs.AudienceDeserializer_}, "", "")
	ctx.WaitUntilReady()
	sbExpectScheduled(t, ctx, sbExpected2Arms[0])
	installed, err := ctx.GetData()
	if err != nil {
		t.Fatal(err)
	}
	assertAny(decoded, installed, t)
}

// SetData installs a switchback from plain ContextData, and a later SetData
// without one turns it back into nothing served.
func TestSwitchbackLoad_SetDataInstallsAndRemoves(t *testing.T) {
	var h = sbCreate(t, sbData())
	h.ctx.SetData(sbData(sbDefault(2, sbHour)))
	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour+5).peek("exp_sb"))
	h.ctx.SetData(sbData(sbUnit(sbDefault(2, sbHour))))
	if h.ctx.state().indexedExperiment("exp_sb").switchback != nil {
		t.Fatal("a SetData without the switchback fields kept the switchback")
	}
}

// A context keeps its own copy of the switchback fields: editing the
// caller's slice after SetData changes nothing served.
func TestSwitchbackLoad_SetDataKeepsItsOwnCopy(t *testing.T) {
	var data = sbData(sbDefault(2, sbHour))
	var h = sbCreate(t, sbData())
	h.ctx.SetData(data)
	data.Experiments[0].SwitchbackPeriodMs = nil
	data.Experiments[0].RandomizationType = ""
	expectInt(t, sbExpected2Arms[1], h.at(sbAnchor+sbHour+5).peek("exp_sb"))
}

// An unchanged full-on switchback with an empty split reuses its slot: three
// reads in one period queue one exposure.
func TestSwitchbackLoad_EmptyFullOnSwitchbackDoesNotDuplicateExposure(t *testing.T) {
	var experiment = sbDefault(1, sbHour)
	experiment.FullOnVariant = 1
	experiment.Split = []float64{}
	var h = sbCreate(t, sbData(experiment))
	for i := 0; i < 3; i++ {
		var got, err = h.ctx.GetTreatment("exp_sb")
		if err != nil || got != 1 {
			t.Fatalf("read %d: got %d, want full-on arm 1", i, got)
		}
	}
	if exposures := queuedExposures(h.ctx); len(exposures) != 1 {
		t.Fatalf("queued %d exposures, want 1: %+v", len(exposures), exposures)
	}
}

// An initial data future that has already settled with a value that is not
// context data fails the context with a TypeError, as a pending one does.
func TestSwitchbackLoad_SettledInvalidDataFutureFailsTheContext(t *testing.T) {
	var logger = &hoLogger{}
	var errorsLogged []interface{}
	logger.onEvent = func(event EventType, data interface{}) {
		if event == Error {
			errorsLogged = append(errorsLogged, data)
		}
	}
	var settled, done = future.New()
	done(nil, nil)
	var ctx = CreateContext(internal.FixedClock{Millis_: hoMillis}, sbLoadConfig(), settled, &hoProvider{}, &hoPublisher{}, logger,
		DefaultVariableParser{}, AudienceMatcher{DefaultAudienceDeserializer{}})
	if !ctx.IsReady() || !ctx.IsFailed() {
		t.Fatalf("ready %v, failed %v; want a failed context", ctx.IsReady(), ctx.IsFailed())
	}
	if len(errorsLogged) != 1 {
		t.Fatalf("logged %d errors, want 1: %v", len(errorsLogged), errorsLogged)
	}
	var err, ok = errorsLogged[0].(*TypeError)
	if !ok || err.Error() != "type assertion failed: expected jsonmodels.ContextData, got <nil>" {
		t.Fatalf("logged %#v", errorsLogged[0])
	}
}

// A literal whose switchback's Index_ entry is edited keeps serving the
// schedule: periods 0 to 3 draw the same arms before and after an audience
// edit. An edit to its switchback identity (the period) starts a new
// generation, so the cached slot is not reused.
func TestSwitchbackLoad_LiteralIndexEditKeepsServingTheSchedule(t *testing.T) {
	var sb = sbDefault(2, sbHour)
	var literal = audienceLiteral(sb)
	var clock = &sbClock{now: sbAnchor + 5}
	literal.Clock_ = clock
	literal.Units_ = map[string]string{sbUnitType: sbUID}
	literal.Attributes_ = []interface{}{}
	literal.SetData(sbData(sb))
	if err := literal.SetAttribute(sbClusterPath, sbClusterKey); err != nil {
		t.Fatal(err)
	}
	var schedule = func() []int {
		var arms = make([]int, 4)
		for period := range arms {
			clock.set(sbAnchor + int64(period)*sbHour + 5)
			arms[period] = treatmentPeek(t, literal, "exp_sb")
		}
		return arms
	}
	expectValue(t, sbExpected2Arms[:4], schedule())
	var generation = literal.state().indexedExperiment("exp_sb").switchback.generation

	var edited = literal.Index_["exp_sb"]
	edited.Data.Audience = `{"filter":[{"value":true}]}`
	literal.Index_["exp_sb"] = edited
	expectValue(t, sbExpected2Arms[:4], schedule())
	var indexed = literal.state().indexedExperiment("exp_sb")
	if indexed.switchback == nil || indexed.switchback.generation != generation {
		t.Fatalf("an audience edit changed the switchback: %+v, generation %d", indexed.switchback, generation)
	}

	var period = int64(2 * sbHour)
	edited = literal.Index_["exp_sb"]
	edited.Data.SwitchbackPeriodMs = &period
	literal.Index_["exp_sb"] = edited
	clock.set(sbAnchor + 5)
	treatmentPeek(t, literal, "exp_sb")
	if indexed = literal.state().indexedExperiment("exp_sb"); indexed.switchback.generation == generation {
		t.Fatal("a period edit kept the switchback generation")
	}
}

// An unchanged entry synchronizes while another reader holds ContextLock_:
// only a rebuild needs that lock exclusively.
func TestSwitchbackLoad_UnchangedLiteralSyncTakesNoExclusiveContextLock(t *testing.T) {
	var literal = audienceLiteral(sbUnit(sbDefault(2, sbHour)))
	var state = literal.state()
	literal.syncLiteralExperiment(state, "exp_sb")
	literal.ContextLock_.RLock()
	var done = make(chan struct{})
	go func() {
		literal.syncLiteralExperiment(state, "exp_sb")
		close(done)
	}()
	select {
	case <-done:
		literal.ContextLock_.RUnlock()
	case <-time.After(time.Second):
		literal.ContextLock_.RUnlock()
		<-done
		t.Fatal("unchanged literal synchronization waited for an exclusive context lock")
	}
}

func TestSwitchbackLoad_LiteralIdentityEdits(t *testing.T) {
	for name, edit := range map[string]func(*jsonmodels.Experiment){
		"start":      func(e *jsonmodels.Experiment) { e.SwitchbackStartAt = sbInt64(sbAnchor - sbHour) },
		"clusters":   func(e *jsonmodels.Experiment) { e.SwitchbackClusters = sbClusters(sbUnitType, "other") },
		"allow list": func(e *jsonmodels.Experiment) { e.SwitchbackClusterValues = []string{"other"} },
		"design":     func(e *jsonmodels.Experiment) { e.SwitchbackDesignType = "other" },
		"unit":       func(e *jsonmodels.Experiment) { e.RandomizationType = "unit" },
	} {
		t.Run(name, func(t *testing.T) {
			var sb = sbDefault(2, sbHour)
			var literal = audienceLiteral(sb)
			literal.Clock_ = &sbClock{now: sbAnchor + sbHour + 5}
			literal.Units_ = map[string]string{sbUnitType: sbUID}
			literal.SetData(sbData(sb))
			_ = literal.SetAttribute(sbClusterPath, sbClusterKey)
			treatmentPeek(t, literal, "exp_sb")
			var generation = literal.state().indexedExperiment("exp_sb").switchback.generation
			var edited = literal.Index_["exp_sb"]
			edit(&edited.Data)
			literal.Index_["exp_sb"] = edited
			treatmentPeek(t, literal, "exp_sb")
			var next = literal.state().indexedExperiment("exp_sb").switchback
			if name == "unit" {
				if next != nil {
					t.Fatal("unit edit kept the switchback")
				}
			} else if next == nil || next.generation == generation {
				t.Fatal("switchback identity edit kept its generation")
			}
		})
	}
}

// A unit-only literal edited into a switchback enables the rebuild gates,
// so a later audience edit before exposure is observed.
func TestSwitchbackLoad_LiteralTransitionEnablesRebuildGates(t *testing.T) {
	var sb = sbDefault(2, sbHour)
	sb.RandomizationType = "unit"
	var literal = audienceLiteral(sb)
	literal.Clock_ = &sbClock{now: sbAnchor + sbHour + 5}
	literal.Units_ = map[string]string{sbUnitType: sbUID}
	literal.SetData(sbData(sb))
	_ = literal.SetAttribute(sbClusterPath, sbClusterKey)
	var edited = literal.Index_["exp_sb"]
	edited.Data.RandomizationType = "switchback"
	literal.Index_["exp_sb"] = edited
	expectInt(t, 1, treatmentPeek(t, literal, "exp_sb"))
	edited = literal.Index_["exp_sb"]
	edited.Data.AudienceStrict = true
	edited.Data.Audience = `{"filter":[{"value":false}]}`
	literal.Index_["exp_sb"] = edited
	expectInt(t, 0, treatmentPeek(t, literal, "exp_sb"))
}

// Unused schedule metadata on an ordinary experiment does not require
// switchback identity work; a transition into a switchback still does.
func TestSwitchbackLoad_OrdinaryLiteralIgnoresUnusedSchedule(t *testing.T) {
	var a = jsonmodels.Experiment{Name: "unit"}
	var b = a
	b.SwitchbackStartAt = sbInt64(sbAnchor)
	b.SwitchbackPeriodMs = sbInt64(sbHour)
	b.SwitchbackClusters = sbClusters(sbUnitType, sbClusterPath)
	if !sameLiteralSwitchback(&a, &b) {
		t.Fatal("ordinary definition compared unused schedule fields")
	}
	b.RandomizationType = "switchback"
	if sameLiteralSwitchback(&a, &b) {
		t.Fatal("ordinary-to-switchback transition was ignored")
	}
}

type sbReentrantClock struct {
	ctx      *Context
	now      int64
	callback func()
}

func (c *sbReentrantClock) Millis() int64 {
	if c.callback != nil {
		var callback = c.callback
		c.callback = nil
		callback()
	}
	_ = c.ctx.SetOverride("other", 1)
	return c.now
}

// Both cached lookup and a cold build read customer clocks without holding
// ContextLock_. A refresh from that callback must be seen by the read.
func TestSwitchbackLoad_ReentrantClockDoesNotHoldContextLock(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached=%v", cached), func(t *testing.T) {
			var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
			if cached {
				h.peek("exp_sb")
			}
			var clock = &sbReentrantClock{ctx: h.ctx, now: sbAnchor + sbHour + 5}
			clock.callback = func() {
				var fresh = sbDefault(2, sbHour)
				fresh.Iteration = 2
				h.ctx.SetData(sbData(fresh))
			}
			h.ctx.Clock_.(*contextClock).Clock = clock
			var done = make(chan int, 1)
			go func() { v, _ := h.ctx.GetTreatment("exp_sb"); done <- v }()
			select {
			case v := <-done:
				expectInt(t, sbExpected2Arms[1], v)
			case <-time.After(time.Second):
				t.Fatal("clock callback deadlocked against the context lock")
			}
			if a := h.ctx.GetAssignment("exp_sb"); a.Iteration != 2 {
				t.Fatalf("served stale iteration %d", a.Iteration)
			}
		})
	}
}

// A unit lookup can miss its cache just before a refresh installs a
// switchback. The build path must read its clock unlocked too.
func TestSwitchbackLoad_ColdBuildReadsReentrantClockUnlocked(t *testing.T) {
	var h = sbCreate(t, sbData(sbDefault(2, sbHour)))
	var clock = &sbReentrantClock{ctx: h.ctx, now: sbAnchor + sbHour + 5}
	h.ctx.Clock_.(*contextClock).Clock = clock
	var done = make(chan *cachedAssignment, 1)
	go func() {
		var at = clockReading{clock: h.ctx.Clock_}
		done <- h.ctx.buildAssignment(h.ctx.state(), "exp_sb", &at)
	}()
	select {
	case entry := <-done:
		expectInt(t, sbExpected2Arms[1], entry.assignment.Variant)
	case <-time.After(time.Second):
		t.Fatal("cold build read the clock under ContextLock_")
	}
}

// A clock callback that refreshes the definition is seen by the read that
// called the clock: a cached switchback made strict and mismatched serves
// the new definition's control, and a cold build draws on the new grid.
func TestSwitchbackLoad_ReadUsesTheDefinitionInstalledByTheClock(t *testing.T) {
	t.Run("cached", func(t *testing.T) {
		var h = sbCreateAt(t, sbAnchor+sbHour+5, map[string]string{sbUnitType: sbUID}, sbData(sbDefault(2, sbHour)))
		h.peek("exp_sb")
		var fresh = sbDefault(2, sbHour)
		fresh.Audience, fresh.AudienceStrict = `{"filter":[{"value":false}]}`, true
		var clock = &sbReentrantClock{ctx: h.ctx, now: sbAnchor + sbHour + 5}
		clock.callback = func() { h.ctx.SetData(sbData(fresh)) }
		h.ctx.Clock_.(*contextClock).Clock = clock
		var done = make(chan int, 1)
		go func() { v, _ := h.ctx.GetTreatment("exp_sb"); done <- v }()
		select {
		case v := <-done:
			if v != 0 || !h.ctx.GetAssignment("exp_sb").AudienceMismatch {
				t.Fatalf("served the superseded definition: variant %d", v)
			}
		case <-time.After(time.Second):
			t.Fatal("clock callback deadlocked against the context lock")
		}
	})
	t.Run("cold build", func(t *testing.T) {
		var h = sbCreateAt(t, sbAnchor+sbHour+5, map[string]string{sbUnitType: sbUID}, sbData(sbDefault(2, sbHour)))
		var clock = &sbReentrantClock{ctx: h.ctx, now: sbAnchor + sbHour + 5}
		clock.callback = func() { h.ctx.SetData(sbData(sbDefault(2, 2*sbHour))) }
		h.ctx.Clock_.(*contextClock).Clock = clock
		var at = clockReading{clock: h.ctx.Clock_}
		var entry = h.ctx.buildAssignment(h.ctx.state(), "exp_sb", &at)
		if entry.switchback.period != 0 || entry.assignment.Variant != sbExpected2Arms[0] {
			t.Fatalf("built on the superseded grid: period %d variant %d", entry.switchback.period, entry.assignment.Variant)
		}
	})
}

// A literal entry edited while another reader waits for the rebuild locks is
// installed once: the reader that gets the locks second finds the first
// one's index and keeps it.
func TestSwitchbackLoad_LiteralRebuildRechecksAfterTheLocks(t *testing.T) {
	var sb = sbDefault(2, sbHour)
	var literal = audienceLiteral(sb)
	literal.Clock_ = &sbClock{now: sbAnchor + 5}
	literal.Units_ = map[string]string{sbUnitType: sbUID}
	literal.SetData(sbData(sb))
	var state = literal.state()
	var edited = literal.Index_["exp_sb"]
	edited.Data.Audience = `{"filter":[{"value":true}]}`
	literal.Index_["exp_sb"] = edited
	literal.ContextLock_.Lock()
	var installed = make(chan *contextIndex, 2)
	for i := 0; i < 2; i++ {
		go func() {
			literal.syncLiteralExperiment(state, "exp_sb")
			installed <- state.loadIndex()
		}()
	}
	time.Sleep(50 * time.Millisecond)
	literal.ContextLock_.Unlock()
	var first, second = <-installed, <-installed
	if first != second {
		t.Fatal("both waiting readers installed their own index")
	}
}
