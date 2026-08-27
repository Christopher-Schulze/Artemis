package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/diagnostics"
	"github.com/Christopher-Schulze/Artemis/network"
)

func TestEngineEmitsRedactedPolicyAndResourceDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		writeTestBody(t, w, "diagnostic-body")
	}))
	defer server.Close()
	cfg := testConfig(server)
	cfg.SessionID = "raw-session-id"
	cfg.DownloadRoot = t.TempDir()
	engine, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine", engine.Close)
	page, err := engine.Fetch(context.Background(), server.URL+"/private?token=secret", FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if closeErr := page.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	download, err := engine.Download(context.Background(), server.URL+"/download?credential=secret", "proof.txt")
	if err != nil {
		t.Fatal(err)
	}
	if download.Size == 0 {
		t.Fatal("download size missing")
	}
	records, err := engine.Diagnostics()
	if err != nil {
		t.Fatal(err)
	}
	var policySeen, resourceSeen, diskSeen bool
	for _, record := range records {
		if record.Policy != nil {
			policySeen = true
			if record.Policy.SessionRef != diagnostics.HashSession(cfg.SessionID) {
				t.Fatalf("session ref=%q", record.Policy.SessionRef)
			}
		}
		if record.Resource != nil {
			resourceSeen = true
			diskSeen = diskSeen || record.Resource.DiskBytes >= download.Size
		}
	}
	if !policySeen || !resourceSeen || !diskSeen {
		t.Fatalf("diagnostic coverage policy=%v resource=%v disk=%v records=%+v", policySeen, resourceSeen, diskSeen, records)
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"raw-session-id", "/private", "/download", "token=secret", "credential=secret", "diagnostic-body"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("diagnostics leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestEngineFailsClosedWhenConfiguredDiagnosticsDisappear(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeTestBody(t, w, "ok") }))
	defer server.Close()
	root := t.TempDir()
	path := filepath.Join(root, "audit", "artemis.jsonl")
	cfg := testConfig(server)
	cfg.Diagnostics = diagnostics.Config{Path: path}
	engine, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := engine.Close(); closeErr != nil && !strings.Contains(closeErr.Error(), "record resource diagnostics") {
			t.Errorf("engine close: %v", closeErr)
		}
	}()
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Fetch(context.Background(), server.URL, FetchOpts{}); !errors.Is(err, network.ErrDecisionAudit) {
		t.Fatalf("fetch error=%v, want decision audit failure", err)
	}
}

func TestPageCloseFailsClosedWhenConfiguredDiagnosticsDisappear(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeTestBody(t, w, "ok") }))
	defer server.Close()
	root := t.TempDir()
	path := filepath.Join(root, "audit", "artemis.jsonl")
	cfg := testConfig(server)
	cfg.Diagnostics = diagnostics.Config{Path: path}
	engine, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := engine.Close(); closeErr != nil && !strings.Contains(closeErr.Error(), "record resource diagnostics") {
			t.Errorf("engine close: %v", closeErr)
		}
	}()
	page, err := engine.Fetch(context.Background(), server.URL, FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err == nil || !strings.Contains(err.Error(), "record resource diagnostics") {
		t.Fatalf("close error=%v, want diagnostics failure", err)
	}
}
