package artemis

import (
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestOwnedAgentFactoryPreservesSessionAndSharedMemoryCeilings(t *testing.T) {
	for _, tc := range []struct {
		name            string
		session, shared int64
	}{
		{name: "session limit", session: 1, shared: 4},
		{name: "shared limit", session: 4, shared: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shared, closeShared, err := engine.NewMemoryAdmission(engine.SessionBudget{MaxMemoryBytes: tc.shared})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := closeShared(); err != nil {
					t.Error(err)
				}
			}()
			owned, err := (engineRuntimeFactory{}).Start(t.Context(), AgentConfig{SessionBudget: engine.SessionBudget{MaxMemoryBytes: tc.session}, MemoryAdmission: shared})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := owned.Close(); err != nil {
					t.Error(err)
				}
			}()
			runtime, ok := owned.(*ownedEngineRuntime)
			if !ok || runtime.Config().SessionBudget.MaxMemoryBytes != tc.session {
				t.Fatal("factory dropped configured session capacity")
			}
			if _, err := runtime.InputMemoryAdmission().ReserveMemory(t.Context(), 2); !engine.IsBudgetExceeded(err) || runtime.SessionUsage().MemoryBytes != 0 {
				t.Fatalf("owned Agent bypassed or leaked a memory ceiling: %v", err)
			}
		})
	}
}
