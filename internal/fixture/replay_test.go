package fixture

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestReplayCaptureAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	srv := NewServerWithDefaults()
	defer srv.Close()

	sc := srv.ScenarioByID("html-001")
	if sc == nil {
		t.Fatal("html-001 scenario not found")
	}

	eng, err := engine.New(engine.Config{
		PolicyConfig:      srv.PolicyConfig(),
		Timeout:           30 * time.Second,
		MaxBodyBytes:      10 * 1024 * 1024,
		JSContextPoolSize: 4,
		JSContextPoolWarm: false,
	})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	defer eng.Close()

	page, err := eng.Fetch(ctx, srv.URL(sc.Path), engine.FetchOpts{RunScripts: sc.RunScripts})
	if err != nil {
		t.Fatalf("engine.Fetch: %v", err)
	}
	defer page.Close()

	result := resultFromEnginePage(ctx, t, page, *sc)
	dir := t.TempDir()
	meta := SystemMetadata()

	replay, err := CaptureReplay(*sc, "renderless", result, 1*time.Millisecond, meta, DefaultRedactionConfig(), dir)
	if err != nil {
		t.Fatalf("CaptureReplay: %v", err)
	}
	if replay.ID == "" {
		t.Error("replay.ID is empty")
	}
	if replay.ScenarioID != sc.ID {
		t.Errorf("ScenarioID = %q, want %q", replay.ScenarioID, sc.ID)
	}
	if replay.Redacted != true {
		t.Error("Redacted should be true")
	}

	path := filepath.Join(dir, replay.filename())
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("replay file not written: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 0600", info.Mode().Perm())
	}

	loaded, err := LoadReplay(path)
	if err != nil {
		t.Fatalf("LoadReplay: %v", err)
	}
	if loaded.Runner != "renderless" {
		t.Errorf("Runner = %q, want renderless", loaded.Runner)
	}
	if loaded.Result.StatusCode != result.StatusCode {
		t.Errorf("Result.StatusCode = %d, want %d", loaded.Result.StatusCode, result.StatusCode)
	}

	reg := loaded.AsRegression()
	if reg.Status != result.StatusCode {
		t.Errorf("regression Status = %d, want %d", reg.Status, result.StatusCode)
	}
	if reg.Expect.Title != result.Title {
		t.Errorf("regression Expect.Title = %q, want %q", reg.Expect.Title, result.Title)
	}
	if reg.Path != "/regression/html-001" {
		t.Errorf("regression Path = %q, want /regression/html-001", reg.Path)
	}

	// The regression scenario should be servable and reproduce the title.
	srv.Register(reg)
	res, err := http.Get(srv.URL(reg.Path))
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != reg.Status {
		t.Errorf("served regression status = %d, want %d", res.StatusCode, reg.Status)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), result.Title) {
		t.Errorf("regression body missing title %q", result.Title)
	}
}

func TestReplayRedaction(t *testing.T) {
	result := CrossResult{
		URL:        "https://example.com?api_key=secret123&token=abc",
		Title:      "Contact page",
		Text:       "Email us at user@example.com or call 555-123-4567",
		HTML:       `<input name="password" value="hunter2"/>`,
		Markdown:   "SSN 123-45-6789",
		EvalResult: `{"password": "hunter2"}`,
		Links:      []string{"https://example.com?token=abc"},
	}

	dir := t.TempDir()
	sc := Scenario{
		ID:          "redaction-test",
		Path:        "/redaction-test",
		Kind:        KindHTML,
		Status:      200,
		Description: "Contact user@example.com",
		HTML:        `<input name="password" value="scenario-secret"/>`,
		Expect: Expect{
			Title:       "Token token=scenario-secret",
			Contains:    []string{"user@example.com"},
			NotContains: []string{"123-45-6789"},
		},
	}
	replay, err := CaptureReplay(sc, "renderless", result, 0, SystemMetadata(), DefaultRedactionConfig(), dir)
	if err != nil {
		t.Fatalf("CaptureReplay: %v", err)
	}

	if !replay.Redacted {
		t.Error("Redacted should be true")
	}
	if strings.Contains(replay.Result.Text, "user@example.com") {
		t.Errorf("email not redacted: %q", replay.Result.Text)
	}
	if strings.Contains(replay.Result.URL, "secret123") {
		t.Errorf("api_key not redacted: %q", replay.Result.URL)
	}
	if strings.Contains(replay.Result.Markdown, "123-45-6789") {
		t.Errorf("SSN not redacted: %q", replay.Result.Markdown)
	}
	if strings.Contains(replay.Result.EvalResult, "hunter2") {
		t.Errorf("json secret not redacted: %q", replay.Result.EvalResult)
	}
	if strings.Contains(replay.Result.Links[0], "token=abc") {
		t.Errorf("link token not redacted: %q", replay.Result.Links[0])
	}
	if !strings.Contains(replay.Result.HTML, `name="password"`) {
		t.Errorf("benign HTML attribute name=\"password\" was incorrectly redacted: %q", replay.Result.HTML)
	}
	if !strings.Contains(replay.Result.HTML, "[REDACTED]") {
		t.Errorf("password value not redacted: %q", replay.Result.HTML)
	}
	if len(replay.RedactionLog) == 0 {
		t.Error("RedactionLog should contain matched patterns")
	}
	scenarioJSON, err := json.Marshal(replay.Scenario)
	if err != nil {
		t.Fatalf("marshal replay scenario: %v", err)
	}
	for _, secret := range []string{"scenario-secret", "user@example.com", "123-45-6789"} {
		if strings.Contains(string(scenarioJSON), secret) {
			t.Errorf("scenario secret %q was persisted: %s", secret, scenarioJSON)
		}
	}
	if replay.Metadata.Hostname != "" {
		t.Errorf("redacted replay retained hostname %q", replay.Metadata.Hostname)
	}
}

func TestReplayRejectsInvalidRedactionPattern(t *testing.T) {
	cfg := DefaultRedactionConfig()
	cfg.Patterns = []string{"["}

	if _, err := CaptureReplay(Scenario{ID: "invalid-pattern"}, "renderless", CrossResult{}, 0, SystemMetadata(), cfg, ""); err == nil {
		t.Fatal("CaptureReplay accepted an invalid redaction pattern")
	}
}

func TestReplayDisabledRedaction(t *testing.T) {
	result := CrossResult{URL: "https://example.com?token=abc", Text: "user@example.com"}
	cfg := DefaultRedactionConfig()
	cfg.Enabled = false

	dir := t.TempDir()
	sc := Scenario{ID: "no-redaction", Path: "/no-redaction"}
	replay, err := CaptureReplay(sc, "renderless", result, 0, SystemMetadata(), cfg, dir)
	if err != nil {
		t.Fatalf("CaptureReplay: %v", err)
	}
	if replay.Redacted {
		t.Error("Redacted should be false")
	}
	if !strings.Contains(replay.Result.Text, "user@example.com") {
		t.Error("email should not be redacted when disabled")
	}
}

func TestReplayRedactUserPattern(t *testing.T) {
	cfg := DefaultRedactionConfig()
	cfg.Patterns = []string{`user-\d+`}
	got := Redact("hello user-123 world user-999", cfg)
	if strings.Contains(got, "user-123") || strings.Contains(got, "user-999") {
		t.Errorf("user pattern not redacted: %q", got)
	}
}

func TestReplayAsRegressionFromText(t *testing.T) {
	result := CrossResult{
		StatusCode: 200,
		Title:      "Plain text",
		Text:       "Hello, regression",
		URL:        "http://127.0.0.1:1234/text",
	}
	sc := Scenario{ID: "text-001", Path: "/text"}
	replay := &Replay{ScenarioID: sc.ID, Scenario: sc, Result: result}
	reg := replay.AsRegression()
	if reg.ContentType != "text/plain; charset=utf-8" {
		t.Errorf("ContentType = %q, want text/plain", reg.ContentType)
	}
	if reg.HTML != "Hello, regression" {
		t.Errorf("HTML = %q, want text", reg.HTML)
	}
	if !strings.Contains(reg.Expect.URL, "/text") {
		t.Errorf("Expect.URL = %q, want /text", reg.Expect.URL)
	}
}

func TestReplayAsRegressionURLPath(t *testing.T) {
	result := CrossResult{
		StatusCode: 404,
		URL:        "http://[::1]:8080/missing?x=1",
		Title:      "Not found",
	}
	sc := Scenario{ID: "missing"}
	replay := &Replay{ScenarioID: sc.ID, Scenario: sc, Result: result}
	reg := replay.AsRegression()
	if reg.Expect.URL != "/missing" {
		t.Errorf("Expect.URL = %q, want /missing", reg.Expect.URL)
	}
}

func TestReplaySaveAndLoad(t *testing.T) {
	sc := Scenario{ID: "roundtrip", Path: "/roundtrip"}
	result := CrossResult{StatusCode: 200, Title: "Roundtrip"}
	replay := &Replay{ScenarioID: sc.ID, Scenario: sc, Result: result}
	dir := t.TempDir()
	path, err := replay.Save(dir)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadReplay(path)
	if err != nil {
		t.Fatalf("LoadReplay: %v", err)
	}
	if loaded.ScenarioID != sc.ID {
		t.Errorf("ScenarioID = %q, want %q", loaded.ScenarioID, sc.ID)
	}
	if loaded.Result.Title != "Roundtrip" {
		t.Errorf("Result.Title = %q, want Roundtrip", loaded.Result.Title)
	}
}

func TestReplaySanitizeFilename(t *testing.T) {
	sc := Scenario{ID: "path/with spaces"}
	replay := &Replay{ScenarioID: sc.ID, Runner: "omnimus runner"}
	want := "replay-omnimus-runner-path-with-spaces.json"
	if got := replay.filename(); got != want {
		t.Errorf("filename = %q, want %q", got, want)
	}
}

func TestRedactAuthorization(t *testing.T) {
	cfg := DefaultRedactionConfig()
	got := Redact("Authorization: Bearer eyJ0eXAiOiJKV1Q", cfg)
	if strings.Contains(got, "eyJ0eXAiOiJKV1Q") {
		t.Errorf("bearer token not redacted: %q", got)
	}
}

func TestReplayCaptureNoOutputDir(t *testing.T) {
	sc := Scenario{ID: "no-dir"}
	result := CrossResult{StatusCode: 200}
	replay, err := CaptureReplay(sc, "renderless", result, 0, SystemMetadata(), DefaultRedactionConfig(), "")
	if err != nil {
		t.Fatalf("CaptureReplay: %v", err)
	}
	if replay.ID == "" {
		t.Error("ID should be set even when not saved to disk")
	}
}
