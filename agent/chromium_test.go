package agent

import "testing"

func TestNewChromiumAgentRequiresOwnedSurfaces(t *testing.T) {
	if _, err := NewChromiumAgent(nil, nil); err == nil {
		t.Fatal("nil runtime accepted")
	}
}
