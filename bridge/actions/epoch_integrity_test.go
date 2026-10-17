package actions

import (
	"strings"
	"testing"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

func TestRuntimeRejectsEvictedNavigationReference(t *testing.T) {
	fixture := newActionFixture(t)
	snapshot, err := fixture.observer.Capture(t.Context(), bridgeobserve.ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	ref := oopifButton(t, snapshot, "Counter").Ref
	requireAction(t, fixture.runtime.Execute(t.Context(), Request{Kind: KindNavigate, URL: fixture.server.URL + "/b"}))
	retained, err := fixture.observer.Resolve(t.Context(), ref)
	if err != nil || retained.Status != bridgeobserve.ResolutionReResolved || retained.Node == nil || retained.Node.Ref == ref {
		t.Fatalf("retained epoch reference lost safe re-resolution: %+v error=%v", retained, err)
	}
	requireAction(t, fixture.runtime.Execute(t.Context(), Request{Kind: KindNavigate, URL: fixture.server.URL + "/c"}))
	result := fixture.runtime.Execute(t.Context(), Request{Kind: KindClick, Ref: ref, Idempotent: true, RetryMax: 3})
	if result.Success || result.Failure != FailureTarget || !strings.Contains(result.Error, "stale") || result.Evidence.ReplayAdmitted {
		t.Fatalf("evicted epoch reference admitted/replayed: %+v", result)
	}
	assertValue(t, fixture.runtime, "document.querySelector('[aria-label=Counter]').dataset.count===undefined")
}
