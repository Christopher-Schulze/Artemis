package artemis

import (
	"testing"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestSessionDumpSemanticHandlesAbsentDocument(t *testing.T) {
	session := &Session{pages: map[string]*engine.Page{"absent": {}}}
	result, taskErr := session.Dump("absent", "semantic")
	if taskErr == nil || taskErr.Code != TaskErrorInvalidInput || result != nil {
		t.Fatalf("unowned session escaped policy: result=%v error=%v", result, taskErr)
	}
	// Pure extraction remains defined; managed commands require an owner.
	if rendered := agent.SemanticString(session.pages["absent"].SemanticTree()); rendered != "" {
		t.Fatalf("absent-document semantic output = %q", rendered)
	}
}
