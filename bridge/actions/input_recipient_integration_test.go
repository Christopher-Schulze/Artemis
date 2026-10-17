package actions

import (
	"testing"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

func TestTypingStopsWhenPageMovesFocusToAnotherRecipient(t *testing.T) {
	f := newActionFixture(t)
	defer f.close()
	snapshot, err := f.observer.Capture(t.Context(), bridgeobserve.ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{}
	for _, node := range snapshot.Nodes {
		if node.Ref != "" && refs[node.Name] == "" {
			refs[node.Name] = node.Ref
		}
	}
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindEvaluate, Expression: `(()=>{const name=document.querySelector('[aria-label="Name"]');let keys=0;
		name.addEventListener('keydown',()=>{if(++keys===3)document.querySelector('[aria-label="First"]').focus()});return true})()`}))
	out := f.runtime.Execute(t.Context(), Request{Kind: KindType, Ref: refs["Name"], Text: "confidential-value"})
	if out.Success || out.Failure != FailurePostcondition {
		t.Fatalf("typing continued after focus moved: %+v", out)
	}
	assertValue(t, f.runtime, `document.querySelector('[aria-label="First"]').value===""`)
}
