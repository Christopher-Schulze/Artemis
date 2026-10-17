package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type shortCLIWriter struct{}

func (shortCLIWriter) Write([]byte) (int, error) { return 0, nil }

func TestCLIOutputBoundsPreserveBytesAndRejectShortWrites(t *testing.T) {
	var golden bytes.Buffer
	if err := printJSON(&golden, struct{ Name string }{"actual"}); err != nil || golden.String() != "{\n  \"Name\": \"actual\"\n}\n" {
		t.Fatalf("JSON output changed: %q %v", golden.String(), err)
	}
	for _, test := range []struct {
		name  string
		write func(io.Writer) error
	}{
		{"text", func(w io.Writer) error { return printText(w, "actual") }},
		{"json", func(w io.Writer) error { return printJSON(w, struct{ Name string }{"actual"}) }},
		{"bytes", func(w io.Writer) error { return writeCLIOutput(w, []byte("actual\n")) }},
		{"observe_json", func(w io.Writer) error {
			return writeObserveOutput(w, formatEvidence, observeFixture(), "https://example.test/")
		}},
		{"observe_har", func(w io.Writer) error {
			return writeObserveOutput(w, formatHAR, observeFixture(), "https://example.test/")
		}},
		{"observe_ndjson", func(w io.Writer) error {
			return writeObserveOutput(w, formatNDJSON, observeFixture(), "https://example.test/")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.write(shortCLIWriter{}); !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("short stdout write accepted: %v", err)
			}
		})
	}
	text := strings.Repeat("x", maxCLIOutputBytes-1)
	if err := printText(io.Discard, text); err != nil {
		t.Fatalf("exact text ceiling rejected: %v", err)
	}
	var rejected bytes.Buffer
	if err := printText(&rejected, text+"x"); err == nil || rejected.Len() != 0 {
		t.Fatal("oversized text emitted bytes or succeeded")
	}
	if err := printJSON(io.Discard, text[:len(text)-2]); err != nil {
		t.Fatalf("exact JSON ceiling rejected: %v", err)
	}
	if err := printJSON(&rejected, text[:len(text)-1]); err == nil || rejected.Len() != 0 {
		t.Fatal("oversized JSON emitted bytes or succeeded")
	}
	if err := printJSON(&rejected, strings.Repeat("\n", maxCLIOutputBytes/2)); err == nil || rejected.Len() != 0 {
		t.Fatal("JSON escaping bypassed byte budget")
	}
	if err := writeCLIOutput(io.Discard, []byte(text+"x")); err != nil {
		t.Fatalf("exact byte ceiling rejected: %v", err)
	}
	if err := writeCLIOutput(&rejected, []byte(text+"xx")); err == nil || rejected.Len() != 0 {
		t.Fatal("oversized byte output succeeded")
	}
}

func TestCLIHeadersUseCanonicalValidationAndRawBounds(t *testing.T) {
	headers, err := parseHeaderFlags([]string{"X-Test: equal=inside", "Accept=text/html", "X-Limit=" + strings.Repeat("x", 4096)})
	if err != nil || headers.Get("X-Test") != "equal=inside" || len(headers.Get("X-Limit")) != 4096 {
		t.Fatalf("valid bounded headers rejected: %#v %v", headers, err)
	}
	for _, values := range [][]string{
		{"Bad Name=value"}, {"X-Test=value\r\nInjected: true"}, {"X-Test=value\x00"},
		{"X-Test=" + strings.Repeat("x", 4097)},
		{strings.Repeat("x", 257) + "=value"},
		{"Authorization=one", "authorization=two"},
		strings.Split(strings.Repeat("X-Test=x,", maxCLIFlagValues)+"X-Test=x", ","),
		{strings.Repeat("x", maxCLIFlagBytes+1)},
	} {
		if _, err := parseHeaderFlags(values); err == nil {
			t.Fatal("invalid or oversized headers admitted")
		}
	}
	var flags stringSliceFlag
	for index := 0; index < maxCLIFlagValues; index++ {
		if err := flags.Set("x"); err != nil {
			t.Fatal(err)
		}
	}
	if err := flags.Set("x"); err == nil || len(flags) != maxCLIFlagValues {
		t.Fatal("repeatable flag count overflow admitted")
	}
	flags = nil
	if err := flags.Set(strings.Repeat("x", maxCLIFlagBytes)); err != nil {
		t.Fatal(err)
	}
	if err := flags.Set("x"); err == nil || len(flags) != 1 {
		t.Fatal("repeatable flag byte overflow admitted")
	}
}

func TestFetchRejectsArgumentsBeforeDispatchAndPropagatesEveryOutputFailure(t *testing.T) {
	t.Setenv("AGENT_BROWSER_ACTION_POLICY", writeCLIActionPolicy(t, `{"default":"allow"}`))
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writeTestBody(t, w, `<!doctype html><title>Fetch proof</title><body><a href="/link">Link</a><script type="application/ld+json">{"@type":"Thing","name":"proof"}</script></body>`)
	}))
	defer server.Close()
	for _, args := range [][]string{
		{"--dump", "invalid", server.URL},
		{"--max-body-bytes", "-1", server.URL},
		{"--allow-port", "65536", server.URL},
		{"--header", "X-Test=" + strings.Repeat("x", 4097), server.URL},
		{"--server", "ws" + strings.TrimPrefix(server.URL, "http"), "--page", "page", "--dump", "invalid"},
	} {
		if code := cmdFetch(args); code != 2 || hits.Load() != 0 {
			t.Fatalf("invalid fetch reached peer: code=%d hits=%d", code, hits.Load())
		}
	}
	output, err := os.Create(filepath.Join(t.TempDir(), "closed-output"))
	if err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = original }()
	for _, dump := range []string{"html", "markdown", "md", "text", "title", "links", "structured", "semantic", "eval"} {
		t.Run(dump, func(t *testing.T) {
			args := []string{"--allow-private-networks", "--allow-port", portOf(server.URL)}
			if dump == "eval" {
				args = append(args, "--eval", "document.title")
			} else {
				args = append(args, "--dump", dump)
			}
			before := hits.Load()
			if code := cmdFetch(append(args, server.URL)); code != 1 || hits.Load() != before+1 {
				t.Fatalf("actual fetch output failure hidden: code=%d hits=%d before=%d", code, hits.Load(), before)
			}
		})
	}
}

func TestActionAndControlJSONBoundsRejectBeforeConnection(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	oversized := `{"kind":"screenshot","ignored":"` + strings.Repeat("x", maxCLIRequestBytes) + `"}`
	if code := cmdAct([]string{"--server", endpoint, "--page", "p", "--request", oversized}); code != 2 || hits.Load() != 0 {
		t.Fatalf("oversized action reached peer: %d %d", code, hits.Load())
	}
	if code := cmdControl([]string{"--server", endpoint, "--params", oversized}); code != 2 || hits.Load() != 0 {
		t.Fatalf("oversized control reached peer: %d %d", code, hits.Load())
	}
}

func TestTopLevelOutputFailureExitsNonzero(t *testing.T) {
	if command := os.Getenv("ARTEMIS_TEST_CLOSED_STDOUT_COMMAND"); command != "" {
		if err := os.Stdout.Close(); err != nil {
			t.Fatal(err)
		}
		os.Args = []string{"artemis", command}
		main()
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"version", "help", "capabilities"} {
		child := exec.CommandContext(t.Context(), executable, "-test.run=^TestTopLevelOutputFailureExitsNonzero$")
		child.Env = append(os.Environ(), "ARTEMIS_TEST_CLOSED_STDOUT_COMMAND="+command)
		var exitErr *exec.ExitError
		if err := child.Run(); !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("%s ignored output failure: %v", command, err)
		}
	}
}

func TestCLIResourceAndIdentityArgumentsFailBeforeFilesystemWork(t *testing.T) {
	root := filepath.Join(t.TempDir(), "untouched")
	t.Setenv("ARTEMIS_DIAGNOSTICS_FILE", filepath.Join(root, "diagnostics.jsonl"))
	for _, test := range []struct {
		name string
		run  func([]string) int
		args []string
	}{
		{"act", cmdAct, []string{"--request", `{"kind":"screenshot"}`, "https://example.test"}},
		{"observe", cmdObserve, []string{"https://example.test"}},
		{"trace", cmdTrace, []string{"--url", "https://example.test"}},
		{"serve", cmdServe, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, invalid := range [][]string{{"--max-cpu-percent", "NaN"}, {"--max-cpu-percent", "+Inf"}, {"--max-cpu-percent", "0"}, {"--max-memory-bytes", "-1"}, {"--max-profile-bytes", "0"}, {"--session-timeout", "1ns"}} {
				args := append(append([]string(nil), invalid...), test.args...)
				if code := test.run(args); code != 2 {
					t.Fatalf("%v returned %d instead of argument failure", invalid, code)
				}
			}
		})
	}
	for _, args := range [][]string{{"--port", "65536"}, {"--allowed-port", "-1"}, {"extra"}} {
		if code := cmdServe(args); code != 2 {
			t.Fatalf("invalid serve arguments returned %d", code)
		}
	}
	if code := cmdDiagnostics([]string{"extra"}); code != 2 {
		t.Fatalf("diagnostics accepted positional arguments: %d", code)
	}
	for _, args := range [][]string{{"--op", "invalid"}, {"--class", "invalid"}, {"--op", "close"}, {"extra"}} {
		if code := cmdSession(append([]string{"--server", "ws://127.0.0.1:1/", "--client-id", "client"}, args...)); code != 2 {
			t.Fatalf("invalid session arguments returned %d", code)
		}
	}
	for _, args := range [][]string{{"--op", "invalid"}, {"--scope", "operator"}, {"--op", "get"}, {"extra"}} {
		if code := cmdProfile(append([]string{"--root", root, "--owner", "alice"}, args...)); code != 2 {
			t.Fatalf("invalid profile arguments returned %d", code)
		}
	}
	for _, args := range [][]string{{"--mode", "invalid"}, {"--mode", "subtree"}, {"--roles", "button,"}, {"--text", strings.Repeat("x", 257)}, {"--max-bytes", "16777217"}} {
		if code := cmdQuery(append([]string{"--server", "ws://127.0.0.1:1", "--page", "p"}, args...)); code != 2 {
			t.Fatalf("invalid query arguments reached control connection: %d", code)
		}
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected arguments initialized diagnostics/profiles: %v", err)
	}
}

func TestCLICleanupPreservesPrimaryAndAllCloseErrors(t *testing.T) {
	for _, initial := range []int{0, 7} {
		output, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
		if err != nil {
			t.Fatal(err)
		}
		closed, err := os.Create(filepath.Join(t.TempDir(), "closed-resource"))
		if err != nil {
			t.Fatal(err)
		}
		if err := closed.Close(); err != nil {
			t.Fatal(err)
		}
		func() {
			original := os.Stderr
			os.Stderr = output
			defer func() { os.Stderr = original }()
			code := func() (code int) {
				code = initial
				defer cleanupOnReturn(&code, "first close", closed.Close)()
				defer cleanupOnReturn(&code, "second close", closed.Close)()
				return code
			}()
			if code != max(1, initial) {
				t.Fatalf("cleanup exit=%d initial=%d", code, initial)
			}
		}()
		closeTestResource(t, "stderr capture close", output.Close)
		raw, err := os.ReadFile(output.Name())
		if err != nil || !strings.Contains(string(raw), "first close:") || !strings.Contains(string(raw), "second close:") || strings.Count(string(raw), "file already closed") != 2 {
			t.Fatalf("cleanup causes lost: %q %v", raw, err)
		}
	}
}
