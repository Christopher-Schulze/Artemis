package fixture

import "testing"

func closeTestResource(t *testing.T, label string, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Errorf("%s: %v", label, err)
	}
}
