package fixture

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func captureIntegrityReplay(t *testing.T) *Replay {
	t.Helper()
	scenario := Scenario{ID: "integrity", Path: "/integrity", Kind: KindHTML, HTML: "<html><title>Captured</title><body>Actual body</body></html>"}
	result := CrossResult{StatusCode: 200, URL: "https://example.invalid/integrity", Title: "Captured", HTML: scenario.HTML, Text: "Actual body"}
	replay, err := CaptureReplay(scenario, "renderless", result, time.Millisecond, SystemMetadata(), DefaultRedactionConfig(), "")
	if err != nil {
		t.Fatal(err)
	}
	return replay
}

func TestReplayPublicationIsUniqueExclusiveAndJoined(t *testing.T) {
	first, second := captureIntegrityReplay(t), captureIntegrityReplay(t)
	if first.ID == second.ID || first.filename() == second.filename() {
		t.Fatal("separate captures collided")
	}
	dir := t.TempDir()
	const writers = 16
	var group sync.WaitGroup
	results := make(chan error, writers)
	for i := 0; i < writers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := first.Save(dir)
			results <- err
		}()
	}
	group.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, os.ErrExist) {
			t.Fatalf("unexpected publication cause: %v", err)
		}
	}
	if accepted != 1 {
		t.Fatalf("exclusive owner count = %d", accepted)
	}
	path := filepath.Join(dir, first.filename())
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first.Result.Text = "mutated"
	if path, err := first.Save(dir); err == nil || path != "" {
		t.Fatalf("mutation acknowledged: %q %v", path, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("existing evidence changed: %v", err)
	}
	if _, err := LoadReplay(path); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Save(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("owned stages leaked or captures lost: %v %v", entries, err)
	}
}

func TestReplayLoadRejectsCorruptionAndNoncanonicalRecords(t *testing.T) {
	replay := captureIntegrityReplay(t)
	data, err := replay.validatedBytes()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		data []byte
	}{
		{"unsupported schema", bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":99`), 1)},
		{"duplicate field", bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1)},
		{"unknown field", bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":1,"extra":true`), 1)},
		{"changed body", bytes.Replace(data, []byte("Actual body"), []byte("Altered body"), 1)},
		{"trailing data", append(append([]byte{}, data...), []byte(`{}`)...)},
		{"legacy incomplete record", []byte(`{"id":"legacy"}`)},
		{"invalid UTF-8", []byte{0xff}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), replay.filename())
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			if loaded, err := LoadReplay(path); err == nil || loaded != nil {
				t.Fatalf("invalid record admitted: %+v %v", loaded, err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "wrong-identity.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadReplay(path); err == nil || loaded != nil {
		t.Fatalf("filename identity alias admitted: %+v %v", loaded, err)
	}
}

func TestReplayFilesystemAdmissionRejectsAliasesPermissionsAndExcess(t *testing.T) {
	replay := captureIntegrityReplay(t)
	dir := t.TempDir()
	path, err := replay.Save(dir)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.json")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadReplay(alias); err == nil || loaded != nil {
		t.Fatalf("symlink admitted: %+v %v", loaded, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadReplay(path); err == nil || loaded != nil {
		t.Fatalf("nonprivate file admitted: %+v %v", loaded, err)
	}
	oversized := filepath.Join(t.TempDir(), replay.filename())
	file, err := os.OpenFile(oversized, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(maxReplayBytes+1), file.Close()); err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadReplay(oversized); err == nil || loaded != nil || !strings.Contains(err.Error(), "bounded") {
		t.Fatalf("excess file bypassed bounded admission: %+v %v", loaded, err)
	}
	unsafeDir := t.TempDir()
	if err := os.Chmod(unsafeDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if path, err := replay.Save(unsafeDir); err == nil || path != "" {
		t.Fatalf("writable foreign directory admitted: %q %v", path, err)
	}
}

func TestReplaySnapshotsDetachCallerStateAndBoundInputs(t *testing.T) {
	scenario := Scenario{ID: "detached", Path: "/detached", Kind: KindHTML, Expect: Expect{Contains: []string{"original"}}}
	result := CrossResult{StatusCode: 200, Text: "original", Links: []string{"https://example.invalid/original"}}
	cfg := DefaultRedactionConfig()
	cfg.Enabled = false
	replay, err := CaptureReplay(scenario, "renderless", result, 0, SystemMetadata(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	scenario.Expect.Contains[0], result.Links[0] = "changed", "changed"
	if replay.Scenario.Expect.Contains[0] != "original" || replay.Result.Links[0] != "https://example.invalid/original" {
		t.Fatal("caller mutation crossed capture boundary")
	}
	if _, err := replay.Save(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, result := range []CrossResult{{StatusCode: 0}, {StatusCode: 200, Text: strings.Repeat("x", maxReplayTextBytes+1)}, {StatusCode: 200, Text: string([]byte{0xff})}} {
		if got, err := CaptureReplay(scenario, "renderless", result, 0, SystemMetadata(), cfg, ""); err == nil || got != nil {
			t.Fatalf("invalid capture admitted: %+v %v", got, err)
		}
	}
	if got, err := CaptureReplay(scenario, "renderless", CrossResult{StatusCode: 200}, -time.Nanosecond, SystemMetadata(), cfg, ""); err == nil || got != nil {
		t.Fatalf("negative duration admitted: %+v %v", got, err)
	}
}

func TestReplayRegressionServesCapturedBytesWithoutOriginalHandler(t *testing.T) {
	server := NewServer()
	defer server.Close()
	var calls atomic.Int32
	const captured = "<html><title>Captured</title><body>Actual snapshot</body></html>"
	scenario := Scenario{ID: "handler", Path: "/handler", Kind: KindHTML, RunScripts: true, AsyncFetch: true, WaitForIdle: true}
	scenario.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			if _, err := io.WriteString(w, captured); err != nil {
				t.Error(err)
			}
			return
		}
		w.WriteHeader(http.StatusTeapot)
	})
	if err := server.Register(scenario); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(server.URL(scenario.Path))
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		t.Fatal(err)
	}
	replay, err := CaptureReplay(scenario, "renderless", CrossResult{URL: server.URL(scenario.Path), StatusCode: response.StatusCode, HTML: string(body), Title: "Captured"}, 0, SystemMetadata(), DefaultRedactionConfig(), "")
	if err != nil {
		t.Fatal(err)
	}
	regression, err := replay.AsRegression()
	if err != nil {
		t.Fatal(err)
	}
	if regression.Handler != nil || regression.RunScripts || regression.AsyncFetch || regression.WaitForIdle || regression.Expect.URL != regression.Path {
		t.Fatal("regression retained source execution or URL")
	}
	if err := server.Register(regression); err != nil {
		t.Fatal(err)
	}
	actual, err := http.Get(server.URL(regression.Path))
	if err != nil {
		t.Fatal(err)
	}
	regressionBytes, readErr := io.ReadAll(actual.Body)
	if err := errors.Join(readErr, actual.Body.Close()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || actual.StatusCode != http.StatusOK || string(regressionBytes) != captured {
		t.Fatalf("source handler leaked into replay: calls=%d status=%d body=%q", calls.Load(), actual.StatusCode, regressionBytes)
	}
}
