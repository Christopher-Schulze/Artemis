package bridge

import (
	"context"
	"testing"
)

func TestRootTargetStatesCancelPageLifetime(t *testing.T) {
	for _, state := range []TargetState{TargetStateDetached, TargetStateCrashed, TargetStateClosed} {
		t.Run(string(state), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			page := &Page{state: TargetStateAttached, frameCancel: cancel}
			page.setState(state, "native retirement")
			select {
			case <-ctx.Done():
			default:
				t.Fatalf("%s left owned work active", state)
			}
			if page.State() != state {
				t.Fatalf("lifetime cancellation changed target state: %s", page.State())
			}
		})
	}
}

func TestClosedRootTargetCannotRegressUnderLateEvents(t *testing.T) {
	page := &Page{state: TargetStateAttached}
	page.setState(TargetStateClosed, "target destroyed")
	for _, state := range []TargetState{TargetStateAttached, TargetStateDetached, TargetStateCrashed} {
		page.setState(state, "late target event")
		if page.State() != TargetStateClosed {
			t.Fatalf("late %s event changed terminal state to %s", state, page.State())
		}
	}
}
