package fixture

import (
	"bytes"
	"context"
	"encoding/base64"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/engine"
)

func newTestEngine(t *testing.T, srv *Server) *engine.Engine {
	t.Helper()
	eng, err := engine.New(engine.Config{
		PolicyConfig: srv.PolicyConfig(),
		Timeout:      30 * time.Second,
	})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	return eng
}

func TestDefaultScenariosEngine(t *testing.T) {
	srv := NewServerWithDefaults()
	defer srv.Close()

	for _, sc := range DefaultScenarios() {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			eng := newTestEngine(t, srv)
			defer eng.Close()

			page, err := eng.Fetch(context.Background(), srv.URL(sc.Path), engine.FetchOpts{
				RunScripts: sc.RunScripts,
				AsyncFetch: sc.AsyncFetch,
			})
			if err != nil {
				t.Fatalf("Fetch %s: %v", sc.Path, err)
			}
			defer page.Close()

			if sc.RunScripts && sc.WaitForIdle {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := page.WaitIdle(ctx); err != nil {
					t.Fatalf("WaitIdle: %v", err)
				}
			}

			assertExpect(t, page, sc.Expect, srv.BaseURL())
		})
	}
}

func assertExpect(t *testing.T, page *engine.Page, exp Expect, baseURL string) {
	t.Helper()

	wantStatus := exp.Status
	if wantStatus == 0 {
		wantStatus = 200
	}
	if page.StatusCode() != wantStatus {
		t.Errorf("status = %d, want %d", page.StatusCode(), wantStatus)
	}

	if exp.Title != "" && page.Title() != exp.Title {
		t.Errorf("title = %q, want %q", page.Title(), exp.Title)
	}

	text := page.Text()
	for _, s := range exp.Contains {
		if !strings.Contains(text, s) {
			t.Errorf("text does not contain %q\nbody:\n%s", s, text[:min(len(text), 500)])
		}
	}
	for _, s := range exp.NotContains {
		if strings.Contains(text, s) {
			t.Errorf("text contains %q", s)
		}
	}

	if exp.URL != "" {
		final := page.URL()
		if !strings.HasSuffix(final, exp.URL) {
			t.Errorf("final URL = %q, want suffix %q", final, exp.URL)
		}
	}

	if exp.Eval != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		v, err := page.Eval(ctx, exp.Eval)
		if err != nil {
			t.Fatalf("Eval %q: %v", exp.Eval, err)
		}
		got := v.String()
		if !strings.Contains(got, exp.EvalContains) {
			t.Errorf("Eval %q = %q, want containing %q", exp.Eval, got, exp.EvalContains)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestFixtureCookiePersistence(t *testing.T) {
	srv := NewServerWithDefaults()
	defer srv.Close()

	eng := newTestEngine(t, srv)
	defer eng.Close()

	page, err := eng.Fetch(context.Background(), srv.URL("/cookie-001"), engine.FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch cookie-001: %v", err)
	}
	page.Close()

	page, err = eng.Fetch(context.Background(), srv.URL("/cookie-002"), engine.FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch cookie-002: %v", err)
	}
	defer page.Close()
	if !strings.Contains(page.Text(), "fixture-test=1") {
		t.Errorf("cookie not persisted: %q", page.Text())
	}
}

func TestFixtureBasicAuth(t *testing.T) {
	srv := NewServerWithDefaults()
	defer srv.Close()

	eng := newTestEngine(t, srv)
	defer eng.Close()

	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("fixture:secret"))
	page, err := eng.Fetch(context.Background(), srv.URL("/auth-basic-001"), engine.FetchOpts{
		Headers: map[string][]string{
			"Authorization": {auth},
		},
	})
	if err != nil {
		t.Fatalf("Fetch auth: %v", err)
	}
	defer page.Close()
	if page.StatusCode() != 200 {
		t.Errorf("status = %d, want 200", page.StatusCode())
	}
	if !strings.Contains(page.Text(), "Authorized") {
		t.Errorf("body missing Authorized: %q", page.Text())
	}
}

func TestFixtureChallenge(t *testing.T) {
	srv := NewServerWithDefaults()
	defer srv.Close()

	eng := newTestEngine(t, srv)
	defer eng.Close()

	page, err := eng.Fetch(context.Background(), srv.URL("/challenge-001"), engine.FetchOpts{
		Headers: map[string][]string{
			"X-Answer": {"2"},
		},
	})
	if err != nil {
		t.Fatalf("Fetch challenge: %v", err)
	}
	defer page.Close()
	if page.StatusCode() != 200 {
		t.Errorf("status = %d, want 200", page.StatusCode())
	}
	if !strings.Contains(page.Text(), "Challenge passed") {
		t.Errorf("body missing success: %q", page.Text())
	}
}

func TestFixtureFormPOST(t *testing.T) {
	srv := NewServerWithDefaults()
	defer srv.Close()

	eng := newTestEngine(t, srv)
	defer eng.Close()

	page, err := eng.Fetch(context.Background(), srv.URL("/form-001"), engine.FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch form-001: %v", err)
	}
	defer page.Close()

	f := agent.FindForm(page.Document(), "#contact")
	if f == nil {
		t.Fatal("form not found")
	}
	if setErr := f.Set("name", "Alice"); setErr != nil {
		t.Fatalf("Set name: %v", setErr)
	}
	if setErr := f.Set("email", "alice@fixture.test"); setErr != nil {
		t.Fatalf("Set email: %v", setErr)
	}
	sub, err := f.Submit()
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	next, err := eng.Submit(context.Background(), sub, engine.FetchOpts{})
	if err != nil {
		t.Fatalf("engine.Submit: %v", err)
	}
	defer next.Close()
	if !strings.Contains(next.Text(), "Alice") || !strings.Contains(next.Text(), "alice@fixture.test") {
		t.Errorf("next page missing form values: %q", next.Text())
	}
}

func TestFixtureFormGET(t *testing.T) {
	srv := NewServerWithDefaults()
	defer srv.Close()

	eng := newTestEngine(t, srv)
	defer eng.Close()

	page, err := eng.Fetch(context.Background(), srv.URL("/form-002"), engine.FetchOpts{})
	if err != nil {
		t.Fatalf("Fetch form-002: %v", err)
	}
	defer page.Close()

	f := agent.FindForm(page.Document(), "#search")
	if f == nil {
		t.Fatal("form not found")
	}
	if setErr := f.Set("q", "fixture"); setErr != nil {
		t.Fatalf("Set q: %v", setErr)
	}
	sub, err := f.Submit()
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	next, err := eng.Submit(context.Background(), sub, engine.FetchOpts{})
	if err != nil {
		t.Fatalf("engine.Submit: %v", err)
	}
	defer next.Close()
	if !strings.Contains(next.Text(), "fixture") {
		t.Errorf("next page missing query: %q", next.Text())
	}
}

func TestFixtureFileUpload(t *testing.T) {
	srv := NewServerWithDefaults()
	defer srv.Close()

	eng := newTestEngine(t, srv)
	defer eng.Close()

	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, err := mw.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	fw.Write([]byte("hello fixture"))
	mw.Close()

	page, err := eng.Fetch(context.Background(), srv.URL("/file-upload"), engine.FetchOpts{
		Method:      "POST",
		ContentType: mw.FormDataContentType(),
		Body:        b.Bytes(),
	})
	if err != nil {
		t.Fatalf("Fetch file-upload: %v", err)
	}
	defer page.Close()
	if page.StatusCode() != 200 {
		t.Errorf("status = %d, want 200", page.StatusCode())
	}
	if !strings.Contains(page.Text(), "13") || !strings.Contains(page.Text(), "hello fixture") {
		t.Errorf("body missing upload result: %q", page.Text())
	}
}

func TestFixtureSPA(t *testing.T) {
	srv := NewServerWithDefaults()
	defer srv.Close()

	eng := newTestEngine(t, srv)
	defer eng.Close()

	page, err := eng.Fetch(context.Background(), srv.URL("/spa-001"), engine.FetchOpts{
		RunScripts: true,
		AsyncFetch: true,
	})
	if err != nil {
		t.Fatalf("Fetch spa: %v", err)
	}
	defer page.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := page.WaitIdle(ctx); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}
	if !strings.Contains(page.Text(), "Hello SPA") {
		t.Errorf("SPA body missing result: %q", page.Text())
	}
}

func TestFixtureWebsocketExists(t *testing.T) {
	srv := NewServerWithDefaults()
	defer srv.Close()

	eng := newTestEngine(t, srv)
	defer eng.Close()

	page, err := eng.Fetch(context.Background(), srv.URL("/websocket-001"), engine.FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatalf("Fetch websocket: %v", err)
	}
	defer page.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := page.Eval(ctx, "window.wsOK")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if v.String() != "ok" {
		t.Errorf("wsOK = %q, want ok", v.String())
	}
}
