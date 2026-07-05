package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestRunScriptInPageSimple(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>Test</title></head><body><h1 id="h">Hello</h1></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

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
		fmt.Fprint(w, `<!doctype html><html><body><h1 id="h">Hello World</h1></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

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
		fmt.Fprint(w, `<!doctype html><html><body><div id="a">1</div><div id="b">2</div></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

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
		fmt.Fprint(w, `<!doctype html><html><body><div id="target">old</div></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

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
	defer eng.Close()

	_, err = runScriptInPage(context.Background(), eng, "http://nonexistent.invalid.localhost", `1+1`, engine.FetchOpts{})
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestRunScriptInPageScriptError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	_, err = runScriptInPage(context.Background(), eng, srv.URL, `throw new Error("boom")`, engine.FetchOpts{})
	if err == nil {
		t.Error("expected error for script that throws")
	}
}

func TestRunScriptInPageUndefinedResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

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
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

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
		fmt.Fprint(w, `<!doctype html><html><body><script>globalThis.injected = "from-inline";</script></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

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
		fmt.Fprint(w, `<!doctype html><html><body><script>globalThis.injected = "from-inline";</script></body></html>`)
	}))
	defer srv.Close()

	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

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
	os.WriteFile(scriptPath, []byte("1+1"), 0o600)
	exitCode := cmdRun([]string{"--script", scriptPath})
	if exitCode != 2 {
		t.Errorf("exit code = %d, want 2 for missing URL", exitCode)
	}
}

func TestCmdRunEmptyScriptFile(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "empty.js")
	os.WriteFile(scriptPath, []byte("   "), 0o600)
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
	os.WriteFile(scriptPath, []byte("1+1"), 0o600)
	os.Chmod(scriptPath, 0o000)
	defer os.Chmod(scriptPath, 0o600)
	exitCode := cmdRun([]string{"--script", scriptPath, "https://example.com"})
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 for unreadable script", exitCode)
	}
}

func TestCmdRunValidScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>RunTest</title></head><body></body></html>`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.js")
	os.WriteFile(scriptPath, []byte(`document.title`), 0o600)

	exitCode := cmdRun([]string{"--script", scriptPath, srv.URL})
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 for valid script", exitCode)
	}
}

func TestCmdRunWithHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "test" {
			t.Errorf("missing custom header")
		}
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.js")
	os.WriteFile(scriptPath, []byte(`1+1`), 0o600)

	exitCode := cmdRun([]string{"--script", scriptPath, "--header", "X-Custom=test", srv.URL})
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
}

func TestPrintResultText(t *testing.T) {
	// Test with a nil value (simulated).
	printResultText(nil) // should print empty line, not panic
}

func TestPrintResultJSON(t *testing.T) {
	printResultJSON(nil) // should print "null", not panic
}
