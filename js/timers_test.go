package js

import (
	"container/heap"
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

func awaitTimerValue(t *testing.T, c *Context, expression, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	for {
		value, err := c.Eval(ctx, expression)
		if err != nil {
			value.Release()
			t.Fatal(err)
		}
		got := value.String()
		value.Release()
		if got == want {
			return
		}
		select {
		case <-time.After(time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("timer value %s = %q, want %q", expression, got, want)
		}
	}
}

func TestTimersHonorDueOrderArgumentsIntervalsAndCancellation(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	started := time.Now()
	if _, err := c.Eval(t.Context(), `
globalThis.timerTrace = [];
setTimeout(function(a,b) { timerTrace.push(this === globalThis ? a+b : 'bad-this'); }, 0, 'args', '-ok');
let cancelled;
setTimeout(() => clearInterval(cancelled), 0);
cancelled = setInterval(() => timerTrace.push('cancelled'), 0);
let count = 0;
const interval = setInterval(() => { if (++count === 3) { timerTrace.push('interval'); clearTimeout(interval); } }, 2);
setTimeout("timerTrace.push('code')", 10);
setTimeout(() => timerTrace.push('later'), 80);
`); err != nil {
		t.Fatal(err)
	}
	awaitTimerValue(t, c, "String(timerTrace.includes('interval') && timerTrace.includes('code') && timerTrace.includes('later'))", "true")
	value, err := c.Eval(t.Context(), "timerTrace.length === 4 && timerTrace[0] === 'args-ok' && timerTrace.indexOf('code') < timerTrace.indexOf('later') && !timerTrace.includes('cancelled')")
	if err != nil || !value.Bool() {
		t.Fatalf("actual argument/order/cancellation contract failed: %v", err)
	}
	if time.Since(started) < 80*time.Millisecond {
		t.Fatal("future timer ran before its monotonic deadline")
	}
	if len(c.timers.pending) != 0 || len(c.timers.ready) != 0 {
		t.Fatal("completed or cancelled timers retained callbacks")
	}
}

func TestTimerCallbacksAndScriptFailuresRemainObservable(t *testing.T) {
	for _, script := range []string{
		`setTimeout(() => { throw new Error('timer-failure'); }, 0);`,
		`setTimeout(() => { throw new Error('timer-failure'); }, 0); throw new Error('script-failure');`,
	} {
		c := newCtxFromHTML(t, "<html></html>", nil)
		_, err := c.Eval(t.Context(), script)
		if err == nil || !strings.Contains(err.Error(), "timer-failure") || strings.Contains(script, "script-failure") && !strings.Contains(err.Error(), "script-failure") {
			t.Fatalf("actual callback or script error was discarded: %v", err)
		}
		if len(c.timers.pending) != 0 {
			t.Fatal("failing timer retained its callback")
		}
	}
}

func TestTimerFailuresDoNotDropDueWorkOrRepeatingTimers(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	err := c.EvalDiscard(t.Context(), `
globalThis.intervalCalls = 0;
globalThis.laterTimerRan = false;
globalThis.intervalID = setInterval(() => {
  intervalCalls++;
  throw Error('actual-interval-failure');
}, 0);
setTimeout(() => { laterTimerRan = true; }, 0);
`)
	if err == nil || !strings.Contains(err.Error(), "actual-interval-failure") {
		t.Fatalf("interval callback failure was not returned: %v", err)
	}
	if len(c.timers.pending) != 1 {
		t.Fatalf("failed interval was not retained and later one-shot retired: %d pending", len(c.timers.pending))
	}
	if err := c.EvalDiscard(t.Context(), "globalThis.firstIntervalCount = intervalCalls"); err == nil || !strings.Contains(err.Error(), "actual-interval-failure") {
		t.Fatalf("repeating interval did not report its next callback failure: %v", err)
	}
	result, err := c.EvalString(t.Context(), `
clearInterval(intervalID);
String(firstIntervalCount === 1 && intervalCalls === 2 && laterTimerRan)
`)
	if err != nil || result != "true" {
		t.Fatalf("timer pump dropped unrelated due work or interval repetition: %q %v", result, err)
	}
	if len(c.timers.pending) != 0 {
		t.Fatalf("cleared interval retained timer state: %d", len(c.timers.pending))
	}
}

func TestTimerCallbackFailureOverflowRemainsObservable(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	err := c.EvalDiscard(t.Context(), `
for (let index = 0; index < 20; index++) {
  const marker = 'timer-overflow-' + index;
  setTimeout(() => { throw Error(marker); }, 0);
}
`)
	if err == nil || !strings.Contains(err.Error(), "timer-overflow-0") || !strings.Contains(err.Error(), "4 additional timer callback failures omitted") {
		t.Fatalf("bounded timer callback failures lost detail or overflow count: %v", err)
	}
	if strings.Contains(err.Error(), "timer-overflow-16") || len(c.timers.pending) != 0 {
		t.Fatalf("timer error bound or one-shot retirement failed: %v", err)
	}
	if got, err := c.EvalString(t.Context(), `String(6*7)`); err != nil || got != "42" {
		t.Fatalf("timer failure drain prevented recovery: %q %v", got, err)
	}
}

func TestTimerCanCancelARearmedIntervalBeforeItsNextFire(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if err := c.EvalDiscard(t.Context(), `
globalThis.deferredIntervalCalls = 0;
globalThis.deferredIntervalID = setInterval(() => { deferredIntervalCalls++; }, 0);
setTimeout(() => clearInterval(deferredIntervalID), 0);
`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), "String(deferredIntervalCalls)"); err != nil || got != "1" {
		t.Fatalf("due interval or cancellation callback was lost: %q %v", got, err)
	}
	if len(c.timers.pending) != 0 || len(c.timers.ready) != 0 {
		t.Fatalf("cancelled deferred interval retained callback state: pending=%d ready=%d", len(c.timers.pending), len(c.timers.ready))
	}
}

func TestTimerPumpPreservesIntervalDeadlineBeforeNestedTimeout(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if err := c.EvalDiscard(t.Context(), `
globalThis.intervalCalls = 0;
globalThis.nestedTimeoutRan = false;
globalThis.intervalObservedNestedTimeout = true;
let intervalID;
intervalID = setInterval(() => {
  intervalCalls++;
  if (intervalCalls === 2) {
    intervalObservedNestedTimeout = nestedTimeoutRan;
    clearInterval(intervalID);
  }
}, 0);
setTimeout(() => setTimeout(() => { nestedTimeoutRan = true; }, 0), 0);
`); err != nil {
		t.Fatal(err)
	}
	if err := c.EvalDiscard(t.Context(), "globalThis.secondTimerPump = true"); err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalString(t.Context(), `String(intervalCalls === 2 && !intervalObservedNestedTimeout && nestedTimeoutRan)`)
	if err != nil || got != "true" {
		t.Fatalf("nested timeout ran ahead of the earlier interval deadline: %q %v", got, err)
	}
}

func TestTimersBoundAdmissionReleaseCancelledSlotsAndRetireOnClose(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	if _, err := c.Eval(t.Context(), `
globalThis.timerIDs = [];
globalThis.limitRejected = false;
try { while (true) timerIDs.push(setTimeout(() => {}, 3600000)); }
catch (error) { limitRejected = error instanceof RangeError; }
`); err != nil {
		t.Fatal(err)
	}
	value, err := c.Eval(t.Context(), "limitRejected && timerIDs.length === 4096")
	if err != nil || !value.Bool() || len(c.timers.pending) != maxPendingTimers {
		t.Fatalf("timer admission was not bounded: %v", err)
	}
	if _, err := c.Eval(t.Context(), "timerIDs.forEach(clearTimeout); globalThis.replacement = setTimeout(() => {}, 3600000)"); err != nil {
		t.Fatal(err)
	}
	if len(c.timers.pending) != 1 || len(c.timers.ready) != 1 {
		t.Fatal("cleared timers retained admission slots")
	}
	c.Close()
	if len(c.timers.pending) != 0 || len(c.timers.ready) != 0 {
		t.Fatal("closed Context retained timer callbacks")
	}
}

func TestTimersClampNestedZeroDelay(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	if _, err := c.Eval(t.Context(), "globalThis.nested = 0; function next(){ if (++nested < 10) setTimeout(next, 0); } setTimeout(next, 0)"); err != nil {
		t.Fatal(err)
	}
	if len(c.timers.ready) != 1 || c.timers.ready[0].delay < 4*time.Millisecond {
		t.Fatal("nested zero-delay timers bypassed the minimum delay")
	}
	awaitTimerValue(t, c, "String(nested)", "10")
	c.Close()
}

func TestTimerPumpBudgetRejectsActualRunawayDueWork(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	_, err := c.Eval(t.Context(), `
globalThis.budgetCallbacks = 0;
function repeatDue(){ budgetCallbacks++; setTimeout(repeatDue,0); }
for (let index=0; index<4095; index++) setTimeout(repeatDue,0);
`)
	if err == nil || !strings.Contains(err.Error(), "timer callback budget exceeded") {
		t.Fatalf("actual runaway due work did not hit its pump bound: %v", err)
	}
	count := func() int64 {
		c.rt.ctxMu.Lock()
		defer c.rt.ctxMu.Unlock()
		value, err := c.v8ctx.RunScript("budgetCallbacks", "<test-timer-count>")
		if err != nil {
			t.Fatal(err)
		}
		defer value.Release()
		return value.Integer()
	}()
	if count != maxTimerCallbacksPerPump {
		t.Fatalf("timer pump ran %d callbacks, want %d", count, maxTimerCallbacksPerPump)
	}
	c.Close()
	if len(c.timers.pending) != 0 || len(c.timers.ready) != 0 {
		t.Fatal("runaway work retained callbacks after owner close")
	}
}

func TestTimerPumpBudgetAllowsAnIntervalYieldBoundary(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if err := c.EvalDiscard(t.Context(), `
for (let index = 0; index < 4095; index++) setTimeout(() => {}, 0);
globalThis.boundaryIntervalCalls = 0;
globalThis.boundaryIntervalID = setInterval(() => { boundaryIntervalCalls++; }, 0);
`); err != nil {
		t.Fatalf("due interval yield boundary was reported as runaway work: %v", err)
	}
	if len(c.timers.pending) != 1 {
		t.Fatalf("one repeating interval should remain after the bounded pump: %d", len(c.timers.pending))
	}
	got, err := c.EvalString(t.Context(), `clearInterval(boundaryIntervalID); String(boundaryIntervalCalls)`)
	if err != nil || got != "1" {
		t.Fatalf("interval was not yielded exactly once at the pump boundary: %q %v", got, err)
	}
}

func TestTimerConversionsAndInvocationDoNotUseReplacedPageIntrinsics(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	if _, err := c.Eval(t.Context(), `
globalThis.timerInvocation = '';
function original(value) { timerInvocation = this === window ? value : 'bad-owner'; }
original.apply = () => { throw new Error('page apply replacement'); };
Number = () => { throw new Error('page number replacement'); };
String = () => { throw new Error('page string replacement'); };
Math.max = () => { throw new Error('page math replacement'); };
setTimeout(original, 0, 'actual-handler');
`); err != nil {
		t.Fatal(err)
	}
	value, err := c.Eval(t.Context(), "timerInvocation")
	if err != nil || value.String() != "actual-handler" {
		t.Fatalf("timer invocation used page replacements: %v", err)
	}
	for _, script := range []string{"setTimeout()", "setInterval()", "setTimeout(()=>{},Symbol())", "clearTimeout(Symbol())"} {
		if _, err := c.Eval(t.Context(), script); err == nil {
			t.Fatalf("invalid timer conversion silently accepted: %s", script)
		}
	}
	if len(c.timers.pending) != 0 {
		t.Fatal("invalid timer conversion retained a callback")
	}
}

func TestTimerHeapOrdersEqualDeadlinesByIdentity(t *testing.T) {
	now := time.Now()
	queue := timerHeap{{id: 3, due: now}, {id: 2, due: now.Add(time.Millisecond)}, {id: 1, due: now}}
	heap.Init(&queue)
	for _, want := range []int32{1, 3, 2} {
		entry := heap.Pop(&queue).(*timerEntry)
		if entry.id != want || entry.index != -1 {
			t.Fatalf("timer ordering/index: id=%d index=%d want=%d", entry.id, entry.index, want)
		}
	}
}

func TestTimerIdentityExhaustionCannotWrapOrReuseAnID(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	c.timers.nextID = math.MaxInt32
	if _, err := c.Eval(t.Context(), "setTimeout(()=>{},0)"); err == nil || !strings.Contains(err.Error(), "timer admission limit exceeded") {
		t.Fatalf("exhausted timer identity was silently reused: %v", err)
	}
	if len(c.timers.pending) != 0 || len(c.timers.ready) != 0 || c.timers.nextID != math.MaxInt32 {
		t.Fatal("identity rejection changed queue state")
	}
}

func TestTimerQueueRetiresNativeCallbackHandles(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	c.rt.ctxMu.Lock()
	defer c.rt.ctxMu.Unlock()
	baseline := c.v8ctx.RetainedValueCount()
	for _, action := range []string{"cancel", "fire", "close"} {
		value, err := c.v8ctx.RunScript("(()=>7)", "<test-owned-timer>")
		if err != nil {
			t.Fatal(err)
		}
		callback, err := value.AsFunction()
		if err != nil {
			t.Fatal(err)
		}
		delay := time.Hour
		if action == "fire" {
			delay = 0
		}
		id := c.timers.schedule(callback, delay, false)
		if id == 0 || c.v8ctx.RetainedValueCount() != baseline+1 {
			t.Fatal("queue did not retain exactly its real callback handle")
		}
		switch action {
		case "cancel":
			c.timers.cancel(id)
		case "fire":
			if err := c.fireTimers(); err != nil {
				t.Fatal(err)
			}
		case "close":
			c.timers.close()
		}
		if count := c.v8ctx.RetainedValueCount(); count != baseline {
			t.Fatalf("%s retained native timer handles: got=%d baseline=%d", action, count, baseline)
		}
	}
}

func TestTimersRetireReturnedNativeIdentityHandles(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	baseline := c.rt.iso.RetainedValueCount()
	for range 3 {
		if err := c.EvalDiscard(t.Context(), `for(let index=0;index<100;index++){const id=setTimeout(()=>{},3600000);clearTimeout(id)}`); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.rt.iso.RetainedValueCount(); got != baseline {
		t.Fatalf("completed timer identity returns leaked internal-isolate handles: got=%d baseline=%d", got, baseline)
	}
	if len(c.timers.pending) != 0 || len(c.timers.ready) != 0 {
		t.Fatal("cancelled timers retained callback ownership")
	}
}
