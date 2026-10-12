package artemis

import (
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestSessionDumpSemanticHandlesAbsentDocument(t *testing.T) {
	session := &Session{pages: map[string]*engine.Page{"absent": {}}}
	result, taskErr := session.Dump("absent", "semantic")
	if taskErr != nil {
		t.Fatalf("semantic dump error = %v", taskErr)
	}
	rendered, ok := result.(string)
	if !ok || rendered != "" {
		t.Fatalf("semantic dump = %#v", result)
	}
}
