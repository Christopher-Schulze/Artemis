package fixture

import "testing"

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
