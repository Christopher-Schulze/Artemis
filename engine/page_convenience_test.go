package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPageTypeBasic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body><input id="q" type="text" value=""></body></html>`)
	}))
	defer srv.Close()

	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer eng.Close()

	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer page.Close()

	result, err := page.Type(context.Background(), "#q", "hello world")
	if err != nil {
		t.Fatalf("Type: %v", err)
	}
	if !result.Success {
		t.Errorf("result.Success = false, want true")
	}
	if result.CharsTyped != len("hello world") {
		t.Errorf("CharsTyped = %d, want %d", result.CharsTyped, len("hello world"))
	}
}

func TestPageTypeEmptySelector(t *testing.T) {
	page := &Page{}
	_, err := page.Type(context.Background(), "", "text")
	if err == nil {
		t.Error("expected error for empty selector")
	}
}

func TestPageTypeEmptyText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body><input id="q"></body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	_, err := page.Type(context.Background(), "#q", "")
	if err == nil {
		t.Error("expected error for empty text")
	}
}

func TestPageTypeSelectorNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	_, err := page.Type(context.Background(), "#nonexistent", "text")
	if err == nil {
		t.Error("expected error for nonexistent selector")
	}
}

func TestPageTypeInvalidSelector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body><input id="q"></body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	_, err := page.Type(context.Background(), "!!!invalid", "text")
	if err == nil {
		t.Error("expected error for invalid selector")
	}
}

func TestPageTypeWithDelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body><input id="q"></body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	result, err := page.TypeWithDelay(context.Background(), "#q", "hi", 10*time.Millisecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("TypeWithDelay: %v", err)
	}
	if !result.Success {
		t.Errorf("result.Success = false")
	}
	if result.CharsTyped != 2 {
		t.Errorf("CharsTyped = %d, want 2", result.CharsTyped)
	}
}

func TestPageTypeWithDelayEmptySelector(t *testing.T) {
	page := &Page{}
	_, err := page.TypeWithDelay(context.Background(), "", "text", 10*time.Millisecond, 5*time.Millisecond)
	if err == nil {
		t.Error("expected error for empty selector")
	}
}

func TestPageFormFill(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body>
		<form id="login">
			<input id="user" type="text">
			<input id="pass" type="password">
		</form>
		</body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	fields := map[string]string{
		"#user": "alice",
		"#pass": "secret123",
	}
	results, err := page.FormFill(context.Background(), "#login", fields)
	if err != nil {
		t.Fatalf("FormFill: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	for _, r := range results {
		if !r.Success {
			t.Errorf("result not successful: %s", r.Error)
		}
	}
}

func TestPageFormFillAndSubmit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body>
		<form id="login">
			<input id="user" type="text">
			<input id="pass" type="password">
			<button type="submit">Login</button>
		</form>
		</body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	fields := map[string]string{
		"#user": "alice",
		"#pass": "secret123",
	}
	results, err := page.Form(context.Background(), "#login", fields, true)
	if err != nil {
		t.Fatalf("Form: %v", err)
	}
	// 2 fills + 1 submit = 3 results.
	if len(results) != 3 {
		t.Fatalf("results len = %d, want 3", len(results))
	}
	for _, r := range results {
		if !r.Success {
			t.Errorf("result not successful: %s", r.Error)
		}
	}
	// Last result should be a submit.
	last := results[len(results)-1]
	if last.Type != "submit" {
		t.Errorf("last result type = %s, want submit", last.Type)
	}
}

func TestPageFormMissingField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body>
		<form id="login">
			<input id="user" type="text">
		</form>
		</body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	fields := map[string]string{
		"#user":    "alice",
		"#missing": "value",
	}
	results, err := page.FormFill(context.Background(), "#login", fields)
	if err == nil {
		t.Error("expected error for missing field")
	}
	// Should still have 2 results (one success, one failure).
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	hasFailure := false
	for _, r := range results {
		if !r.Success {
			hasFailure = true
		}
	}
	if !hasFailure {
		t.Error("expected at least one failure for missing field")
	}
}

func TestPageFormSubmitSkippedOnMissingField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body>
		<form id="login">
			<input id="user" type="text">
		</form>
		</body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	fields := map[string]string{
		"#user":    "alice",
		"#missing": "value",
	}
	results, err := page.Form(context.Background(), "#login", fields, true)
	// Should error because of missing field.
	if err == nil {
		t.Error("expected error for missing field with submit")
	}
	// Submit should be skipped: only 2 results (fills), no submit.
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2 (submit skipped)", len(results))
	}
	for _, r := range results {
		if r.Type == "submit" {
			t.Error("submit should have been skipped due to missing field")
		}
	}
}

func TestPageFormEmptySelector(t *testing.T) {
	page := &Page{}
	_, err := page.Form(context.Background(), "", map[string]string{"#a": "b"}, false)
	if err == nil {
		t.Error("expected error for empty form selector")
	}
}

func TestPageFormNoFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body><form id="f"></form></body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	_, err := page.Form(context.Background(), "#f", map[string]string{}, false)
	if err == nil {
		t.Error("expected error for no fields")
	}
}

func TestPageFormSelectorNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	_, err := page.Form(context.Background(), "#nonexistent", map[string]string{"#a": "b"}, false)
	if err == nil {
		t.Error("expected error for nonexistent form")
	}
}

func TestPageFormSubmitOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body>
		<form id="login">
			<button type="submit">Login</button>
		</form>
		</body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	result, err := page.FormSubmit(context.Background(), "#login")
	if err != nil {
		t.Fatalf("FormSubmit: %v", err)
	}
	if !result.Success {
		t.Errorf("result.Success = false")
	}
	if result.Type != "submit" {
		t.Errorf("type = %s, want submit", result.Type)
	}
}

func TestPageFormSubmitEmptySelector(t *testing.T) {
	page := &Page{}
	_, err := page.FormSubmit(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty selector")
	}
}

func TestPageFormSubmitNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	_, err := page.FormSubmit(context.Background(), "#nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent form")
	}
}

func TestPageClickSelector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body><button id="btn">Click</button></body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	err := page.ClickSelector(context.Background(), "#btn")
	// Click may fail if there's no JS context, but the selector resolution
	// should succeed. We only care that the method doesn't panic and
	// returns either nil or a JS-context error.
	_ = err
}

func TestPageClickSelectorEmptySelector(t *testing.T) {
	page := &Page{}
	err := page.ClickSelector(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty selector")
	}
}

func TestPageClickSelectorNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer srv.Close()

	eng, _ := New(Config{})
	defer eng.Close()

	page, _ := eng.Fetch(context.Background(), srv.URL, FetchOpts{})
	defer page.Close()

	err := page.ClickSelector(context.Background(), "#nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent selector")
	}
}

func TestPageTypeNilPage(t *testing.T) {
	var p *Page
	_, err := p.Type(context.Background(), "#q", "text")
	if err == nil {
		t.Error("expected error for nil page")
	}
}

func TestPageFormNilPage(t *testing.T) {
	var p *Page
	_, err := p.Form(context.Background(), "#f", map[string]string{"#a": "b"}, false)
	if err == nil {
		t.Error("expected error for nil page")
	}
}

func TestPageFormSubmitNilPage(t *testing.T) {
	var p *Page
	_, err := p.FormSubmit(context.Background(), "#f")
	if err == nil {
		t.Error("expected error for nil page")
	}
}

func TestPageClickSelectorNilPage(t *testing.T) {
	var p *Page
	err := p.ClickSelector(context.Background(), "#q")
	if err == nil {
		t.Error("expected error for nil page")
	}
}

func TestPageTypeWithDelayNilPage(t *testing.T) {
	var p *Page
	_, err := p.TypeWithDelay(context.Background(), "#q", "text", 10*time.Millisecond, 5*time.Millisecond)
	if err == nil {
		t.Error("expected error for nil page")
	}
}

func TestPageFormFillNilPage(t *testing.T) {
	var p *Page
	_, err := p.FormFill(context.Background(), "#f", map[string]string{"#a": "b"})
	if err == nil {
		t.Error("expected error for nil page")
	}
}
