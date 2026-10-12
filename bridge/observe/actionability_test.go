package observe

import "testing"

func TestClassifyActionabilityUsesLayoutAccessibilityAndHitEvidence(t *testing.T) {
	box := &Rect{Width: 100, Height: 40}
	tests := []struct {
		name string
		node *Node
		want ActionabilityState
	}{
		{name: "detached", node: nil, want: ActionabilityDetached},
		{name: "no layout", node: &Node{BackendNodeID: 1, Interactive: true, Hit: HitClear}, want: ActionabilityNoLayout},
		{name: "hidden", node: &Node{BackendNodeID: 1, Box: box, Interactive: true, Hit: HitClear}, want: ActionabilityHidden},
		{name: "disabled", node: &Node{BackendNodeID: 1, Box: box, Visible: true, Interactive: true, Disabled: true, Hit: HitClear}, want: ActionabilityDisabled},
		{name: "non interactive", node: &Node{BackendNodeID: 1, Box: box, Visible: true, Hit: HitClear}, want: ActionabilityNonInteractive},
		{name: "covered", node: &Node{BackendNodeID: 1, Box: box, Visible: true, Interactive: true, Hit: HitCovered}, want: ActionabilityCovered},
		{name: "hit unknown", node: &Node{BackendNodeID: 1, Box: box, Visible: true, Interactive: true, Hit: HitUnknown}, want: ActionabilityHitUnknown},
		{name: "actionable", node: &Node{BackendNodeID: 1, Box: box, Visible: true, Interactive: true, Hit: HitClear}, want: ActionabilityReady},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyActionability(test.node); got != test.want {
				t.Fatalf("actionability = %s, want %s", got, test.want)
			}
		})
	}
}

func TestClassifyMissingActionabilityHonorsCaptureCompleteness(t *testing.T) {
	if got := ClassifyMissingActionability(Snapshot{}); got != ActionabilityDetached {
		t.Fatalf("complete missing node = %s", got)
	}
	if got := ClassifyMissingActionability(Snapshot{Truncated: true}); got != ActionabilityUnavailable {
		t.Fatalf("truncated missing node = %s", got)
	}
	if got := ClassifyMissingActionability(Snapshot{Warnings: []string{"OOPIF unavailable"}}); got != ActionabilityUnavailable {
		t.Fatalf("warned missing node = %s", got)
	}
}
