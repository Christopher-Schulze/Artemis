package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/telemetry"
)

func TestDoctorJSONExitCode(t *testing.T) {
	code := cmdDoctor([]string{"--format", "json"})
	if code != 0 {
		t.Fatalf("doctor exit code = %d, want 0", code)
	}
}

func TestDoctorTextExitCode(t *testing.T) {
	code := cmdDoctor([]string{"--format", "text"})
	if code != 0 {
		t.Fatalf("doctor exit code = %d, want 0", code)
	}
}

func TestTraceEmitsJSON(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>TraceTest</title></head><body></body></html>`)
	}))
	defer page.Close()
	traceDir := t.TempDir()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	oldStdout := os.Stdout
	os.Stdout = w
	code := cmdTrace([]string{
		"--url", page.URL,
		"--allow-private-networks",
		"--allow-port", portOf(page.URL),
		"--trace-dir", traceDir,
		"--format", "json",
	})
	w.Close()
	os.Stdout = oldStdout

	if code != 0 {
		t.Fatalf("trace exit code = %d, want 0", code)
	}

	var result TraceResult
	if err := json.NewDecoder(r).Decode(&result); err != nil {
		t.Fatalf("decode trace result: %v", err)
	}
	if result.URL != page.URL {
		t.Errorf("trace URL = %q, want %q", result.URL, page.URL)
	}
	if len(result.Events) == 0 {
		t.Errorf("expected trace events, got none")
	}
	if result.TargetID == "" || result.BrowserContextID == "" || result.TracePath == "" {
		t.Fatalf("trace identity/archive missing: %+v", result)
	}
	entries, err := telemetry.ReadTraceZip(result.TracePath)
	if err != nil {
		t.Fatalf("read trace archive: %v", err)
	}
	for _, name := range []string{"trace.meta.json", "debug/console.json", "debug/page-errors.json", "debug/network.json", "screenshots/screenshot-0001.png", "snapshots/snapshot-0001.html"} {
		if _, ok := entries[name]; !ok {
			t.Fatalf("trace archive missing %s", name)
		}
	}
}

func TestDownloadEmitsVerifiedOwnedMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="cli-proof.txt"`)
		_, _ = w.Write([]byte("cli-download"))
	}))
	defer server.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	oldStdout := os.Stdout
	os.Stdout = w
	code := cmdDownload([]string{"--session-id", "cli-test", "--allow-private-networks", "--allow-port", portOf(server.URL), server.URL})
	_ = w.Close()
	os.Stdout = oldStdout
	if code != 0 {
		t.Fatalf("download exit code = %d, want 0", code)
	}
	var result struct {
		Path   string `json:"path"`
		MIME   string `json:"mime"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	}
	if err := json.NewDecoder(r).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.MIME != "text/plain" || result.Size != int64(len("cli-download")) || len(result.SHA256) != 64 {
		t.Fatalf("result=%#v", result)
	}
	if !strings.HasSuffix(result.Path, filepath.Join("cli-test", "downloads", "cli-proof.txt")) {
		t.Fatalf("path=%s", result.Path)
	}
}

func TestProfileCRUD(t *testing.T) {
	root := t.TempDir()
	name := "testprofile"

	code := cmdProfile([]string{"--root", root, "--owner", "alice", "--op", "create", "--name", name})
	if code != 0 {
		t.Fatalf("profile create exit code = %d, want 0", code)
	}

	code = cmdProfile([]string{"--root", root, "--owner", "alice", "--op", "get", "--name", name})
	if code != 0 {
		t.Fatalf("profile get exit code = %d, want 0", code)
	}

	code = cmdProfile([]string{"--root", root, "--owner", "alice", "--op", "list"})
	if code != 0 {
		t.Fatalf("profile list exit code = %d, want 0", code)
	}

	code = cmdProfile([]string{"--root", root, "--owner", "alice", "--op", "delete", "--name", name})
	if code != 0 {
		t.Fatalf("profile delete exit code = %d, want 0", code)
	}
}

func TestProfileRequiresOwner(t *testing.T) {
	code := cmdProfile([]string{"--op", "list"})
	if code != 2 {
		t.Fatalf("profile exit code = %d, want 2 for missing owner", code)
	}
}

func TestBenchmarkArtemisOnly(t *testing.T) {
	outDir := t.TempDir()
	code := cmdBenchmark([]string{
		"--skip-competitor",
		"--iterations", "1",
		"--output", outDir,
		"--format", "json",
	})
	if code != 0 {
		t.Fatalf("benchmark exit code = %d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(outDir, "scorecard.json")); err != nil {
		t.Fatalf("scorecard not written: %v", err)
	}
}

func portOf(raw string) string {
	parts := strings.Split(raw, ":")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		last = strings.TrimSuffix(last, "/")
		return last
	}
	return ""
}
