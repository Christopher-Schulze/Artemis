package actions

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/tabs"
)

func waitPendingDialog(t *testing.T, page *bridge.Page) *tabs.PendingDialog {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if dialog, ok := page.PendingDialog(); ok {
			return dialog
		}
		select {
		case <-ctx.Done():
			t.Fatal("real dialog was not registered by the page owner")
		case <-ticker.C:
		}
	}
}

func TestRuntimeDialogUnblocksAdmittedActionAndPreservesDeniedPending(t *testing.T) {
	fixture := newActionFixture(t)
	done := make(chan Outcome, 1)
	go func() {
		done <- fixture.runtime.Execute(t.Context(), Request{Kind: KindEvaluate, Expression: "prompt('owned prompt','initial')"})
	}()
	dialog := waitPendingDialog(t, fixture.page)
	if dialog.Type != tabs.DialogTypePrompt || dialog.Message != "owned prompt" || dialog.SessionID != fixture.page.SessionID() {
		t.Fatalf("wrong real renderer dialog: %+v", dialog)
	}
	denied, err := NewRuntime(fixture.page, fixture.observer, func(context.Context, Request) error { return fmt.Errorf("dialog denied") })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := denied.Close(); err != nil {
			t.Error(err)
		}
	})
	outcome := denied.Execute(t.Context(), Request{Kind: KindDialog, Accept: true, DialogGeneration: dialog.Generation})
	if outcome.Success || outcome.Failure != FailurePolicy || outcome.Evidence.Effect != EffectNotAttempted {
		t.Fatalf("denied dialog dispatched: %+v", outcome)
	}
	current, ok := fixture.page.PendingDialog()
	if !ok || current.Generation != dialog.Generation {
		t.Fatal("denied reply consumed pending dialog")
	}
	invalid := fixture.runtime.Execute(t.Context(), Request{Kind: KindDialog, Ref: "e1", Accept: true})
	if invalid.Success || invalid.Failure != FailureValidation || invalid.Evidence.Effect != EffectNotAttempted {
		t.Fatalf("dialog target ref escaped validation: %+v", invalid)
	}
	stale := fixture.runtime.Execute(t.Context(), Request{Kind: KindDialog, Accept: true, DialogGeneration: dialog.Generation + 1})
	if stale.Success || stale.Failure != FailureTarget || stale.Evidence.Effect != EffectNotAttempted {
		t.Fatalf("stale dialog response dispatched: %+v", stale)
	}
	requireAction(t, fixture.runtime.Execute(t.Context(), Request{Kind: KindDialog, Accept: true, PromptText: "approved", DialogGeneration: dialog.Generation}))
	select {
	case result := <-done:
		if !result.Success || result.Value != "approved" {
			t.Fatalf("modal action did not resume: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dialog response was queued behind its blocked action")
	}
	if _, ok := fixture.page.PendingDialog(); ok {
		t.Fatal("handled dialog retained pending state")
	}
}
