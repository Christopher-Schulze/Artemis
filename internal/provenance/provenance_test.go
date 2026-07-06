// Package provenance guards the artemis module against reintroduction
// of derivation-attributing references to other projects. The first
// guard (TASK-2331) forbids the string "lightpanda" anywhere in the
// tracked artemis tree except under benchmark/, where Lightpanda is
// treated as a competing binary that Artemis benchmarks against.
package provenance

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// moduleRoot returns the artemis module root by walking up from the
// test's working directory (which is the package directory) until it
// finds go.mod. The module root is the directory containing go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find go.mod above %s", cwd)
	return ""
}

// trackedFiles returns the git-tracked files under root, relative to
// root. It uses `git ls-files` so untracked/generated artifacts are
// excluded and the guard reflects what will actually be committed.
func trackedFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", ".")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// isExcluded returns true for paths that are allowed to contain the
// guarded name. benchmark/ is where competing binaries are referenced
// (TASK-2338). third_party/ is vendored external code we don't own.
// The provenance test file itself is excluded so the guard's own
// source (which mentions the name in comments and the negative
// subtest) doesn't trip the scan.
func isExcluded(rel string) bool {
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "benchmark/") {
		return true
	}
	if strings.HasPrefix(rel, "third_party/") {
		return true
	}
	// The guard test itself must reference the name to test for it;
	// exclude its own source from the scan.
	if strings.HasSuffix(rel, "internal/provenance/provenance_test.go") {
		return true
	}
	return false
}

// scanForName reads the file at absPath and returns the first
// case-insensitive match line/offset, or empty if none.
func scanForName(t *testing.T, absPath, name string) string {
	t.Helper()
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read %s: %v", absPath, err)
	}
	idx := bytes.Index(bytes.ToLower(data), bytes.ToLower([]byte(name)))
	if idx < 0 {
		return ""
	}
	// Find the line containing the match for a helpful error.
	lineStart := bytes.LastIndexByte(data[:idx], '\n')
	if lineStart < 0 {
		lineStart = 0
	} else {
		lineStart++
	}
	lineEnd := bytes.IndexByte(data[idx:], '\n')
	if lineEnd < 0 {
		lineEnd = len(data) - idx
	}
	return strings.TrimSpace(string(data[lineStart : idx+lineEnd]))
}

// TestNoLightpandaOutsideBenchmark is a durable provenance guard. It
// fails the build if any case-insensitive "lightpanda" string appears
// anywhere in the tracked artemis module tree except under benchmark/
// or third_party/. This includes binary files like js/snapshot.bin,
// where the name was previously baked in by the V8 snapshot tool.
//
// The name is reintroduced ONLY under benchmark/ by TASK-2338 as a
// competitor binary reference; this guard excludes benchmark/ for
// exactly that reason.
func TestNoLightpandaOutsideBenchmark(t *testing.T) {
	root := moduleRoot(t)
	files := trackedFiles(t, root)
	if len(files) == 0 {
		t.Fatal("git ls-files returned no files; cannot run provenance guard")
	}

	const forbidden = "lightpanda"
	var hits []string
	for _, rel := range files {
		if isExcluded(rel) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if line := scanForName(t, abs, forbidden); line != "" {
			hits = append(hits, rel+": "+line)
		}
	}
	if len(hits) > 0 {
		t.Errorf("case-insensitive %q found outside benchmark/ in %d tracked file(s):\n%s",
			forbidden, len(hits), strings.Join(hits, "\n"))
	}
}

// TestNoLightpandaOutsideBenchmarkCanFail proves the guard can
// actually detect a violation. It writes a temporary file containing
// the forbidden name into a non-excluded location and confirms the
// scanner catches it. The file is removed in t.Cleanup so the repo
// is never left dirty.
//
// This is the negative check required by the TASK-2331 acceptance
// criteria: "provably fails when a 'lightpanda' string is present
// outside benchmark/".
func TestNoLightpandaOutsideBenchmarkCanFail(t *testing.T) {
	root := moduleRoot(t)
	// Use a temp file in the module root (non-excluded) so the
	// scanner sees it. We do NOT git add it, so it won't be in
	// `git ls-files` — instead we call scanForName directly on the
	// temp path to prove the byte scanner works.
	tmp, err := os.CreateTemp(root, ".provenance-negative-*.txt")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	t.Cleanup(func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	})
	const forbidden = "lightpanda"
	_, _ = tmp.WriteString("this file mentions " + forbidden + " to prove the scanner catches it")
	_ = tmp.Close()

	abs := filepath.Join(root, filepath.Base(tmp.Name()))
	// Verify the file is not in an excluded path.
	if isExcluded(filepath.Base(tmp.Name())) {
		t.Fatal("temp file landed in an excluded path; test setup is wrong")
	}
	// The scanner must find the forbidden name in the temp file.
	if line := scanForName(t, abs, forbidden); line == "" {
		t.Errorf("scanner failed to detect %q in temp file %s — guard is not effective",
			forbidden, abs)
	} else {
		t.Logf("negative check passed: scanner detected %q in %s: %q",
			forbidden, filepath.Base(tmp.Name()), line)
	}
}
