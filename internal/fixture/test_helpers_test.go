package fixture

import (
	"testing"
	"time"
)

// nativeRunnerCleanupBudget bounds runner.stop for owned native browsers. It
// covers the bridge's own bounded shutdown (Browser.close request, graceful
// exit wait, then owned process-group cleanup), so a slow but correct exit
// under host load passes while a hung close still fails.
const nativeRunnerCleanupBudget = 30 * time.Second

func newDefaultTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := NewServerWithDefaults()
	if err != nil {
		t.Fatalf("initialize complete fixture corpus: %v", err)
	}
	return server
}

func closeTestResource(t *testing.T, label string, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Errorf("%s: %v", label, err)
	}
}
