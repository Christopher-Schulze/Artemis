package v8go

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tommie/v8go/bindingdigest"
)

// TestBindingArchiveMatchesSources fails when a .cc or .h file changed
// without rebuilding the prebuilt darwin archives.
func TestBindingArchiveMatchesSources(t *testing.T) {
	digest, err := bindingdigest.SourceDigest(os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join("lib", "BINDING.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if recorded := bindingdigest.Recorded(manifest); recorded != digest {
		t.Fatalf("lib archives were built from sources %s, current sources are %s; run go run ./cmd/artemis-v8go-binding in the Artemis module", recorded, digest)
	}
	for _, arch := range []string{"darwin_arm64", "darwin_amd64"} {
		if info, err := os.Stat(filepath.Join("lib", arch, "libv8go_artemis.a")); err != nil || info.Size() == 0 {
			t.Fatalf("%s archive missing: %v", arch, err)
		}
	}
}

func TestBindingArchiveDigestDetectsSourceChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.cc"), []byte("int a;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.go"), []byte("package x"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := bindingdigest.SourceDigest(os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.go"), []byte("package y"), 0o600); err != nil {
		t.Fatal(err)
	}
	unchanged, err := bindingdigest.SourceDigest(os.DirFS(dir))
	if err != nil || unchanged != before {
		t.Fatalf("a Go file changed the native digest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.cc"), []byte("int b;"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := bindingdigest.SourceDigest(os.DirFS(dir))
	if err != nil || after == before {
		t.Fatalf("a source change kept the digest: %v", err)
	}
	if bindingdigest.Recorded([]byte(after+"  sources\n")) != after {
		t.Fatal("manifest digest not parsed")
	}
}
