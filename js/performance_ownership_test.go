package js

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestPerformanceNowCallbacksRetireNativeHandles(t *testing.T) {
	c := newCtxFromHTML(t, `<div></div>`, nil)
	const script = `(() => { let previous=performance.now();for(let index=0;index<500;index++){const next=performance.now();if(!Number.isFinite(next)||next<previous)throw Error('clock regressed');previous=next;} })()`
	if err := c.EvalDiscard(t.Context(), script); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	for range 3 {
		if err := c.EvalDiscard(t.Context(), script); err != nil {
			t.Fatal(err)
		}
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("performance callbacks retained handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}

func TestPerformanceClockOriginUsesCurrentRealmInBothSnapshotModes(t *testing.T) {
	_, matchingToolchain := currentToolchainMatches(t)
	for _, useSnapshot := range []bool{true, false} {
		t.Run(map[bool]string{true: "snapshot", false: "fresh"}[useSnapshot], func(t *testing.T) {
			originalBlob := snapshotBlob
			if !useSnapshot {
				snapshotBlob = nil
			}
			t.Cleanup(func() { snapshotBlob = originalBlob })
			before := time.Now().UnixMilli()
			c := newCtxFromHTML(t, `<div></div>`, nil)
			wantSnapshot := useSnapshot && matchingToolchain
			if c.rt.hasSnapshot != wantSnapshot {
				t.Fatalf("wrong actual snapshot mode: active=%v want %v reason=%s", c.rt.hasSnapshot, wantSnapshot, c.rt.snapshotReason)
			}
			after := time.Now().UnixMilli()
			origin, err := c.EvalString(t.Context(), `String(performance.timeOrigin)`)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := strconv.ParseInt(origin, 10, 64)
			if err != nil || actual < before || actual > after {
				t.Fatalf("actual time origin outside construction: %q bounds=%d..%d err=%v", origin, before, after, err)
			}
			if err := c.EvalDiscard(t.Context(), `performance.mark('actual-start');performance.mark('actual-end');performance.measure('actual-span','actual-start','actual-end');if(performance.getEntriesByType('mark').length!==2||performance.getEntriesByType('measure').length!==1)throw Error('actual entry owner lost');`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPristineContextReuseRebindsActualPerformanceClock(t *testing.T) {
	doc, err := parser.ParseHTML(strings.NewReader(`<div></div>`), "https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	owner := NewRuntimeWithPool(1)
	t.Cleanup(owner.Close)
	first, err := owner.NewContext(doc, ContextOpts{})
	if err != nil {
		t.Fatal(err)
	}
	previousRealm, previousOrigin := first.v8ctx, first.performanceOrigin.UnixMilli()
	first.Close()
	time.Sleep(2 * time.Millisecond)
	second, err := owner.NewContext(doc, ContextOpts{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	if second.v8ctx != previousRealm {
		t.Fatal("control did not reuse an actual pristine realm")
	}
	want := second.performanceOrigin.UnixMilli()
	if want <= previousOrigin {
		t.Fatal("control did not advance the actual clock epoch")
	}
	value, err := second.EvalString(t.Context(), `String(performance.timeOrigin)`)
	if err != nil || value != strconv.FormatInt(want, 10) {
		t.Fatalf("reused realm kept previous origin: got=%q want=%d err=%v", value, want, err)
	}
}
