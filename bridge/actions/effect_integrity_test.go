package actions

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

func TestEffectfulEvaluationCannotBeReplayedByCallerAssertion(t *testing.T) {
	f := newActionFixture(t)
	tests := []struct{ kind Kind }{{KindEvaluate}, {KindAssert}}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			if err := f.runtime.evalInto(t.Context(), "window.effectCount=0", new(int)); err != nil {
				t.Fatal(err)
			}
			outcome := f.runtime.Execute(t.Context(), Request{Kind: test.kind, Expression: "(()=>{window.effectCount++;throw new Error('ambiguous effect')})()", Idempotent: true, RetryMax: 3})
			var count int
			if err := f.runtime.evalInto(t.Context(), "window.effectCount", &count); err != nil {
				t.Fatal(err)
			}
			if outcome.Success || outcome.Evidence.Attempts != 1 || outcome.Evidence.ReplayAdmitted || outcome.Evidence.Effect != EffectPossible || count != 1 || len(outcome.Evidence.ActionID) != 32 {
				t.Fatalf("ambiguous effect replayed or hidden: outcome=%+v effects=%d", outcome, count)
			}
		})
	}
}

type failAfterCaptureCaller struct {
	page     *bridge.Page
	captures int
}

func (c *failAfterCaptureCaller) Call(ctx context.Context, method string, params, result any) error {
	if method == "DOMSnapshot.captureSnapshot" {
		c.captures++
		if c.captures > 1 {
			return fmt.Errorf("after-effect observation unavailable")
		}
	}
	return c.page.Call(ctx, method, params, result)
}

func TestCompletedEffectWithMissingAfterEvidenceIsNotSuccess(t *testing.T) {
	f := newActionFixture(t)
	caller := &failAfterCaptureCaller{page: f.page}
	observer, err := bridgeobserve.NewCollector(caller, bridgeobserve.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(f.page, observer, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	outcome := runtime.Execute(t.Context(), Request{Kind: KindEvaluate, Expression: "document.body.dataset.effect='performed';'performed'", Idempotent: true, RetryMax: 3})
	var actual string
	if err := f.runtime.evalInto(t.Context(), "document.body.dataset.effect", &actual); err != nil {
		t.Fatal(err)
	}
	if outcome.Success || outcome.Failure != FailureEvidence || outcome.Evidence.Effect != EffectDispatched || outcome.Evidence.Attempts != 1 || outcome.Evidence.AfterHash != "" || outcome.Value != "performed" || actual != "performed" {
		t.Fatalf("completed but unverified effect was hidden or repeated: outcome=%+v actual=%q", outcome, actual)
	}
}

func TestNavigationPrefixRedirectIsNotExactSuccess(t *testing.T) {
	f := newActionFixture(t)
	f.server.Config.Handler.(*http.ServeMux).HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirected", http.StatusFound)
	})
	outcome := f.runtime.Execute(t.Context(), Request{Kind: KindNavigate, URL: f.server.URL + "/redirect", Idempotent: true, RetryMax: 3})
	if outcome.Success || outcome.Failure != FailurePostcondition || outcome.Evidence.Attempts != 1 || outcome.Evidence.Postcondition.Actual != f.server.URL+"/redirected" || outcome.Evidence.ReplayAdmitted {
		t.Fatalf("redirect prefix accepted or repeated: %+v", outcome)
	}
}

type cancelCaptureCaller struct {
	page     *bridge.Page
	cancel   context.CancelFunc
	captures int
}

func (c *cancelCaptureCaller) Call(ctx context.Context, method string, params, result any) error {
	if err := c.page.Call(ctx, method, params, result); err != nil {
		return err
	}
	if method == "DOMSnapshot.captureSnapshot" {
		c.captures++
		c.cancel()
		return context.Canceled
	}
	return nil
}

func TestCancelledBeforeCaptureDoesNotRetryOrExecute(t *testing.T) {
	f := newActionFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	caller := &cancelCaptureCaller{page: f.page, cancel: cancel}
	observer, err := bridgeobserve.NewCollector(caller, bridgeobserve.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(f.page, observer, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})
	outcome := runtime.Execute(ctx, Request{Kind: KindEvaluate, Expression: "window.cancelledEffect=1", Idempotent: true, RetryMax: 3})
	var effectAbsent bool
	if err := f.runtime.evalInto(t.Context(), "window.cancelledEffect===undefined", &effectAbsent); err != nil {
		t.Fatal(err)
	}
	if outcome.Success || outcome.Failure != FailureCancelled || outcome.Evidence.Attempts != 0 || outcome.Evidence.CaptureAttempts != 1 || caller.captures != 1 || outcome.Evidence.Effect != EffectNotAttempted || !effectAbsent {
		t.Fatalf("cancelled admission retried or produced an effect: outcome=%+v captures=%d effectAbsent=%v", outcome, caller.captures, effectAbsent)
	}
}
