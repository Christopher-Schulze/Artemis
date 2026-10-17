package sourcepin

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"os"
	"testing"
)

//go:embed hash.go
var testSources embed.FS

func TestHashBindsActualSourceNamesBytesAndIdentityOrder(t *testing.T) {
	data, err := os.ReadFile("hash.go")
	if err != nil {
		t.Fatal(err)
	}
	expected := fmt.Sprintf("%x", sha256.Sum256(append([]byte("identity:1:a\x00identity:1:b\x00hash.go\x00"), append(data, 0)...)))
	actual, err := Hash(testSources, "a", "b")
	if err != nil || actual != expected {
		t.Fatalf("source pin=%q expected=%q error=%v", actual, expected, err)
	}
	changed, err := Hash(testSources, "b", "a")
	if err != nil || changed == actual {
		t.Fatalf("identity ordering disappeared: %q %v", changed, err)
	}
}
