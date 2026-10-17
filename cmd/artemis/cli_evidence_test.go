package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/telemetry"
)

func captureCLIOutput(t *testing.T, run func() int) (int, []byte) {
	t.Helper()
	output, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = output
	defer func() {
		os.Stdout = original
		closeTestResource(t, "captured output close", output.Close)
	}()
	code := run()
	raw, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, raw
}

func TestDoctorMeasuresRuntimeAndRejectsNonBrowserExecutable(t *testing.T) {
	for _, browser := range []bool{true, false} {
		t.Run(map[bool]string{true: "real_chrome", false: "real_non_browser"}[browser], func(t *testing.T) {
			args := []string{"--format", "json"}
			if !browser {
				binary, err := exec.LookPath("true")
				if err != nil {
					t.Fatal(err)
				}
				args = append(args, "--binary", binary)
			}
			code, raw := captureCLIOutput(t, func() int { return cmdDoctor(args) })
			var result DoctorResult
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatalf("doctor JSON: %v: %s", err, raw)
			}
			wantCode := 1
			if browser {
				wantCode = 0
			}
			if code != wantCode || result.OK != browser || len(result.Checks) != 4 {
				t.Fatalf("doctor status code=%d: %+v", code, result)
			}
			for index, name := range []string{"chromium_binary", "chromium", "agent_start", "capabilities"} {
				check := result.Checks[index]
				wantOK := name != "chromium" || browser
				if check.Name != name || check.OK != wantOK || check.Message == "" {
					t.Fatalf("wrong measured check: %+v", check)
				}
				if browser && name == "chromium" && (!strings.Contains(check.Message, "CDP ") || !strings.Contains(check.Message, "shutdown verified")) {
					t.Fatalf("runtime identity/shutdown proof missing: %+v", check)
				}
			}
		})
	}
}

func TestTraceMeasuresRedirected404DespiteChildAndPageSpoof(t *testing.T) {
	var childHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/missing?private=secret", http.StatusFound)
		case "/missing":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			writeTestBody(t, w, `<!doctype html><title>Measured404</title><script>performance.getEntriesByType=()=>[{responseStatus:200}];</script><iframe src="/child"></iframe>`)
		case "/child":
			childHits.Add(1)
			writeTestBody(t, w, `<!doctype html><title>Child200</title>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	code, raw := captureCLIOutput(t, func() int {
		return cmdTrace([]string{"--url", server.URL + "/start", "--allow-private-networks", "--allow-port", portOf(server.URL), "--trace-dir", t.TempDir()})
	})
	var result TraceResult
	if err := json.Unmarshal(raw, &result); err != nil || code != 0 {
		t.Fatalf("trace code=%d JSON=%s err=%v", code, raw, err)
	}
	loaded, title := false, false
	for _, event := range result.Events {
		if event.Stage == "page_loaded" {
			loaded = true
			if event.Status != http.StatusNotFound || event.URL != server.URL+"/missing" {
				t.Fatalf("trace reported unobserved response: %+v", event)
			}
		}
		if event.Stage == "title" && event.Value == "Measured404" {
			title = true
		}
	}
	if !loaded || !title || childHits.Load() == 0 {
		t.Fatalf("actual main/child document proof missing: %+v hits=%d", result, childHits.Load())
	}
	entries, err := telemetry.ReadTraceZip(result.TracePath)
	if err != nil {
		t.Fatal(err)
	}
	var network []telemetry.TraceNetworkEntry
	if err := json.Unmarshal(entries["debug/network.json"], &network); err != nil {
		t.Fatal(err)
	}
	mainFrame, childFrame := "", ""
	for _, entry := range network {
		if entry.URL == server.URL+"/missing" && entry.Status == http.StatusNotFound {
			mainFrame = entry.FrameID
		}
		if entry.URL == server.URL+"/child" && entry.Status == http.StatusOK {
			childFrame = entry.FrameID
		}
	}
	if mainFrame == "" || childFrame == "" || mainFrame == childFrame {
		t.Fatalf("archive lacks independent frame response proof: %+v", network)
	}
}

func TestDoctorTextOutputFailureReturnsNonzero(t *testing.T) {
	output, err := os.Create(filepath.Join(t.TempDir(), "closed-stdout"))
	if err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = original }()
	diagnostics, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	originalErrors := os.Stderr
	os.Stderr = diagnostics
	defer func() {
		os.Stderr = originalErrors
		closeTestResource(t, "doctor error capture close", diagnostics.Close)
	}()
	if code := cmdDoctor([]string{"--format", "text"}); code != 1 {
		t.Fatalf("doctor ignored required output failure: %d", code)
	}
	raw, err := os.ReadFile(diagnostics.Name())
	if err != nil || !strings.Contains(string(raw), "doctor output:") {
		t.Fatalf("failure did not reach actual stdout write: %q %v", raw, err)
	}
}

func TestTraceOutputFailureRetainsRealArchive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestBody(t, w, "<!doctype html><title>Trace output proof</title>")
	}))
	defer server.Close()
	output, err := os.Create(filepath.Join(t.TempDir(), "closed-stdout"))
	if err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = original }()
	for _, format := range []string{"json", "text"} {
		dir := t.TempDir()
		code := cmdTrace([]string{"--url", server.URL, "--allow-private-networks", "--allow-port", portOf(server.URL), "--trace-dir", dir, "--format", format})
		if code != 1 {
			t.Fatalf("%s trace ignored closed stdout: %d", format, code)
		}
		paths, err := filepath.Glob(filepath.Join(dir, "*.zip"))
		if err != nil || len(paths) != 1 {
			t.Fatalf("output failure hid or skipped actual trace capture: paths=%v err=%v", paths, err)
		}
		entries, err := telemetry.ReadTraceZip(paths[0])
		if err != nil || len(entries["debug/network.json"]) == 0 {
			t.Fatalf("output failure lost actual trace evidence: %v", err)
		}
	}
}
