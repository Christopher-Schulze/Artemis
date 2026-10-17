package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestRunScriptInPageSimple(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><head><title>Test</title></head><body><h1 id="h">Hello</h1></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	result, err := runScriptInPage(context.Background(), eng, srv.URL, `document.title`, engine.FetchOpts{})
	if err != nil {
		t.Fatalf("runScriptInPage: %v", err)
	}
	if result != "Test" {
		t.Errorf("result = %q, want Test", result)
	}
}

func TestRunScriptInPageQuerySelector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body><h1 id="h">Hello World</h1></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	result, err := runScriptInPage(context.Background(), eng, srv.URL, `document.querySelector('#h').textContent`, engine.FetchOpts{})
	if err != nil {
		t.Fatalf("runScriptInPage: %v", err)
	}
	if result != "Hello World" {
		t.Errorf("result = %q, want Hello World", result)
	}
}

func TestRunScriptInPageMultiLineScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body><div id="a">1</div><div id="b">2</div></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	script := `
		var a = document.querySelector('#a').textContent;
		var b = document.querySelector('#b').textContent;
		a + b
	`
	result, err := runScriptInPage(context.Background(), eng, srv.URL, script, engine.FetchOpts{})
	if err != nil {
		t.Fatalf("runScriptInPage: %v", err)
	}
	if result != "12" {
		t.Errorf("result = %q, want 12", result)
	}
}

func TestRunScriptInPageDOMMutation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body><div id="target">old</div></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	script := `
		document.querySelector('#target').textContent = 'new';
		document.querySelector('#target').textContent
	`
	result, err := runScriptInPage(context.Background(), eng, srv.URL, script, engine.FetchOpts{})
	if err != nil {
		t.Fatalf("runScriptInPage: %v", err)
	}
	if result != "new" {
		t.Errorf("result = %q, want new", result)
	}
}

func TestRunScriptInPageFetchError(t *testing.T) {
	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	_, err = runScriptInPage(context.Background(), eng, "http://nonexistent.invalid.localhost", `1+1`, engine.FetchOpts{})
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestRunScriptInPageScriptError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	_, err = runScriptInPage(context.Background(), eng, srv.URL, `throw new Error("boom")`, engine.FetchOpts{})
	if err == nil {
		t.Error("expected error for script that throws")
	}
}

func TestRunScriptInPageUndefinedResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	result, err := runScriptInPage(context.Background(), eng, srv.URL, `undefined`, engine.FetchOpts{})
	if err != nil {
		t.Fatalf("runScriptInPage: %v", err)
	}
	// undefined should return empty or "undefined" string.
	if result != "" && result != "undefined" {
		t.Errorf("result = %q, want empty or undefined", result)
	}
}

func TestRunScriptInPageArithmetic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	result, err := runScriptInPage(context.Background(), eng, srv.URL, `2 + 3 * 4`, engine.FetchOpts{})
	if err != nil {
		t.Fatalf("runScriptInPage: %v", err)
	}
	if result != "14" {
		t.Errorf("result = %q, want 14", result)
	}
}

func TestRunScriptInPageWithInlineScripts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body><script>globalThis.injected = "from-inline";</script></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	result, err := runScriptInPage(context.Background(), eng, srv.URL, `globalThis.injected`, engine.FetchOpts{RunInlineScripts: true})
	if err != nil {
		t.Fatalf("runScriptInPage: %v", err)
	}
	if !strings.Contains(result, "from-inline") {
		t.Errorf("result = %q, want to contain from-inline", result)
	}
}

func TestRunScriptInPageWithoutInlineScripts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><body><script>globalThis.injected = "from-inline";</script></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	result, err := runScriptInPage(context.Background(), eng, srv.URL, `globalThis.injected`, engine.FetchOpts{RunInlineScripts: false})
	if err != nil {
		t.Fatalf("runScriptInPage: %v", err)
	}
	// Without running inline scripts, the global should not be set.
	if strings.Contains(result, "from-inline") {
		t.Errorf("result = %q, should not contain from-inline when inline scripts disabled", result)
	}
}

func TestCmdRunMissingScriptFlag(t *testing.T) {
	exitCode := cmdRun([]string{"https://example.com"})
	if exitCode != 2 {
		t.Errorf("exit code = %d, want 2 for missing --script", exitCode)
	}
}

func TestCmdRunMissingURL(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.js")
	if err := os.WriteFile(scriptPath, []byte("1+1"), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	exitCode := cmdRun([]string{"--script", scriptPath})
	if exitCode != 2 {
		t.Errorf("exit code = %d, want 2 for missing URL", exitCode)
	}
}

func TestCmdRunEmptyScriptFile(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "empty.js")
	if err := os.WriteFile(scriptPath, []byte("   "), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	exitCode := cmdRun([]string{"--script", scriptPath, "https://example.com"})
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 for empty script", exitCode)
	}
}

func TestCmdRunNonexistentScriptFile(t *testing.T) {
	exitCode := cmdRun([]string{"--script", "/nonexistent/path/script.js", "https://example.com"})
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 for nonexistent script", exitCode)
	}
}

func TestCmdRunScriptFileNotReadable(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.js")
	if err := os.WriteFile(scriptPath, []byte("1+1"), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := os.Chmod(scriptPath, 0o000); err != nil {
		t.Fatalf("make script unreadable: %v", err)
	}
	defer func() {
		if err := os.Chmod(scriptPath, 0o600); err != nil {
			t.Errorf("restore script permissions: %v", err)
		}
	}()
	exitCode := cmdRun([]string{"--script", scriptPath, "https://example.com"})
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 for unreadable script", exitCode)
	}
}

func TestCmdRunValidScript(t *testing.T) {
	t.Setenv("AGENT_BROWSER_ACTION_POLICY", writeCLIActionPolicy(t, `{"default":"allow"}`))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestBody(t, w, `<!doctype html><html><head><title>RunTest</title></head><body></body></html>`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.js")
	if err := os.WriteFile(scriptPath, []byte(`document.title`), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	exitCode := cmdRun([]string{"--script", scriptPath, "--allow-private-networks", "--allow-port", srvPort(srv), srv.URL})
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 for valid script", exitCode)
	}
}

func TestCmdRunWithHeaders(t *testing.T) {
	t.Setenv("AGENT_BROWSER_ACTION_POLICY", writeCLIActionPolicy(t, `{"default":"allow"}`))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "test" {
			t.Errorf("missing custom header")
		}
		writeTestBody(t, w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.js")
	if err := os.WriteFile(scriptPath, []byte(`1+1`), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	exitCode := cmdRun([]string{"--script", scriptPath, "--header", "X-Custom=test", "--allow-private-networks", "--allow-port", srvPort(srv), srv.URL})
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
}

func TestPrintResultText(t *testing.T) {
	verifyRunPrinterOutput(t, func() error { return printResultText(nil, false, "https://example.test/") }, "\n")
}

func TestPrintResultJSON(t *testing.T) {
	verifyRunPrinterOutput(t, func() error { return printResultJSON(nil, "https://example.test/") },
		`{"trust":{"origin":"https://example.test","source":"run","trust":"untrusted","threat":"untrusted_content"},"value":null}`+"\n")
}

func verifyRunPrinterOutput(t *testing.T, print func() error, want string) {
	t.Helper()
	output, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = output
	defer func() {
		os.Stdout = original
		if err := output.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	}()
	if err := print(); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(output)
	if err != nil || string(raw) != want {
		t.Fatalf("printer output = %q, want %q: %v", raw, want, err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if err := print(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed stdout did not fail: %v", err)
	}
}

func TestRunScriptFileBoundsAndType(t *testing.T) {
	root := t.TempDir()
	for _, size := range []int64{maxRunScriptBytes, maxRunScriptBytes + 1} {
		file, err := os.CreateTemp(root, "bounded-script-")
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(size); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := readRunScript(file.Name())
		if size == maxRunScriptBytes && (err != nil || int64(len(data)) != size) {
			t.Fatalf("exact script bound rejected: length=%d err=%v", len(data), err)
		}
		if size > maxRunScriptBytes && (err == nil || data != nil) {
			t.Fatal("oversized script was read or accepted")
		}
	}
	if data, err := readRunScript(root); err == nil || data != nil {
		t.Fatal("directory admitted as a script")
	}
}

func TestCmdRunInputAndOutputFailuresAtRealHTTPBoundary(t *testing.T) {
	t.Setenv("AGENT_BROWSER_ACTION_POLICY", writeCLIActionPolicy(t, `{"default":"allow"}`))
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writeTestBody(t, w, "<!doctype html><title>Output proof</title>")
	}))
	defer server.Close()
	root := t.TempDir()
	oversized, err := os.Create(filepath.Join(root, "oversized.js"))
	if err != nil {
		t.Fatal(err)
	}
	if err := oversized.Truncate(maxRunScriptBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := oversized.Close(); err != nil {
		t.Fatal(err)
	}
	if code := cmdRun([]string{"--script", oversized.Name(), "--allow-private-networks", "--allow-port", srvPort(server), server.URL}); code != 1 || hits.Load() != 0 {
		t.Fatalf("oversized input reached network or succeeded: code=%d hits=%d", code, hits.Load())
	}
	closed, err := os.Create(filepath.Join(root, "closed-stdout"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = closed
	defer func() { os.Stdout = original }()
	for _, test := range []struct {
		name   string
		script string
		json   bool
	}{
		{"text", `document.title`, false},
		{"undefined", `undefined`, false},
		{"json_object", `JSON.stringify({answer:42})`, true},
		{"json_string", `document.title`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.name+".js")
			if err := os.WriteFile(path, []byte(test.script), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--script", path, "--allow-private-networks", "--allow-port", srvPort(server)}
			if test.json {
				args = append(args, "--json")
			}
			before := hits.Load()
			if code := cmdRun(append(args, server.URL)); code != 1 || hits.Load() != before+1 {
				t.Fatalf("real script output failure hidden: code=%d before=%d hits=%d", code, before, hits.Load())
			}
		})
	}
}
