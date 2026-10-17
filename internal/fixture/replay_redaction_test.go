package fixture

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestReplayRedactionCoversMetadataSourceAndStructuredURLs(t *testing.T) {
	cfg := DefaultRedactionConfig()
	cfg.Patterns = []string{"private-guid-1337"}
	scenario := Scenario{ID: "privacy", Path: "/contact/user@example.org", Kind: KindHTML,
		Description: "private-guid-1337", ContentType: `text/html; name="user@example.org"; token=hidden`,
		HTML:   `<html><a href="https://user:password@host.invalid/path">user@example.org</a></html>`,
		Expect: Expect{Title: "user@example.org", Contains: []string{"private-guid-1337"}, NotContains: []string{"user@example.org"}, URL: "/contact/user@example.org"},
	}
	result := CrossResult{StatusCode: 200, URL: "https://user:password@host.invalid/contact/user@example.org?unknown=hidden#hidden",
		Title: "user@example.org", Text: "private-guid-1337", HTML: scenario.HTML, Markdown: "user@example.org", EvalResult: "private-guid-1337",
		EvalErr: "private-guid-1337", Links: []string{"https://user:password@host.invalid/contact/user@example.org?encoded=%68idden#hidden", "mailto:user%40example.org"}}
	metadata := SystemMetadata()
	metadata.Browser, metadata.BrowserVersion, metadata.UserAgent, metadata.Viewport = "user@example.org", "private-guid-1337", "private-guid-1337", "user@example.org"
	replay, err := CaptureReplay(scenario, "renderless", result, 0, metadata, cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if replay.Scenario.ID != scenario.ID || replay.ScenarioID != scenario.ID || replay.Runner != "renderless" {
		t.Fatal("opaque identities changed")
	}
	if replay.Metadata.Hostname != "" || strings.ContainsAny(replay.Result.URL, "?#") || strings.Contains(replay.Result.URL, "user:password@") {
		t.Fatal("private URL components or hostname retained")
	}
	data, err := json.Marshal(replay)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"user@example.org", "user%40example.org", "private-guid-1337", "user:password", "hidden"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("sensitive field survived capture: %q", secret)
		}
	}
	if len(replay.RedactionProfileHash) != 64 {
		t.Fatalf("profile identity absent: %q", replay.RedactionProfileHash)
	}
	path, err := replay.Save(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadReplay(path); err == nil || loaded != nil {
		t.Fatalf("custom profile silently replaced with default: %+v %v", loaded, err)
	}
	loaded, err := LoadReplay(path, cfg)
	if err != nil || loaded.ContentHash != replay.ContentHash {
		t.Fatalf("matching verified profile: %+v %v", loaded, err)
	}
	loaded.Metadata.UserAgent = "private-guid-1337"
	if err := loaded.seal(); err == nil {
		t.Fatal("unsafe metadata could be resealed under a redacted profile")
	}
	result.URL = "https://private-guid-1337.invalid/path"
	if got, err := CaptureReplay(scenario, "renderless", result, 0, metadata, cfg, ""); err == nil || got != nil {
		t.Fatal("sensitive URL authority bypassed its verified profile")
	}
}

func TestRedactFailsClosedAndUsesLiteralBoundedCustomMasks(t *testing.T) {
	valid := DefaultRedactionConfig()
	valid.Patterns, valid.Mask = []string{`secret-(\d+)`}, "$1"
	value, err := Redact("secret-123", valid)
	if err != nil || value != "$1" {
		t.Fatalf("mask expanded source capture: %q %v", value, err)
	}
	cases := []struct {
		name  string
		cfg   RedactionConfig
		input string
	}{
		{"invalid regex", RedactionConfig{Enabled: true, Patterns: []string{"["}}, "private"},
		{"empty match", RedactionConfig{Enabled: true, Patterns: []string{"x*"}}, "private"},
		{"sensitive mask", RedactionConfig{Enabled: true, Mask: "user@example.org"}, "private"},
		{"matching mask", RedactionConfig{Enabled: true, Mask: "secret", Patterns: []string{"secret"}}, "private"},
		{"oversized pattern", RedactionConfig{Enabled: true, Patterns: []string{strings.Repeat("x", 1025)}}, "private"},
		{"invalid mask UTF-8", RedactionConfig{Enabled: true, Mask: string([]byte{0xff})}, "private"},
		{"oversized input", DefaultRedactionConfig(), strings.Repeat("x", maxReplayTextBytes+1)},
		{"expansion", RedactionConfig{Enabled: true, Mask: strings.Repeat("m", 128), Patterns: []string{"x"}}, strings.Repeat("x", 1<<20)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if value, err := Redact(test.input, test.cfg); err == nil || value != "" {
				t.Fatalf("unsafe or partial redaction returned: bytes=%d %v", len(value), err)
			}
		})
	}
	if _, err := compileDefaultRedactionPatterns(); err != nil {
		t.Fatal(err)
	}
}

func TestReplayPrivacyProfileIdentityAndExplicitRawLoading(t *testing.T) {
	scenario := Scenario{ID: "profile", Path: "/profile", Kind: KindHTML}
	result := CrossResult{StatusCode: 200, Text: "user@example.org"}
	cfg := DefaultRedactionConfig()
	cfg.Enabled = false
	replay, err := CaptureReplay(scenario, "renderless", result, 0, SystemMetadata(), cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	path, err := replay.Save(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if loaded, err := LoadReplay(path); err == nil || loaded != nil {
		t.Fatal("default loading admitted an explicitly raw record")
	}
	loaded, err := LoadReplay(path, cfg)
	if err != nil || loaded.Result.Text != result.Text {
		t.Fatalf("explicit raw roundtrip changed data: %+v %v", loaded, err)
	}
	if _, err := LoadReplay(path, cfg, cfg); err == nil {
		t.Fatal("multiple profiles accepted")
	}
	first, err := newReplayRedactor(DefaultRedactionConfig())
	if err != nil {
		t.Fatal(err)
	}
	equivalent := DefaultRedactionConfig()
	equivalent.Mask, equivalent.Patterns = "", []string{}
	second, err := newReplayRedactor(equivalent)
	if err != nil || first.hash != second.hash {
		t.Fatalf("equivalent default profile identity diverged: %v", err)
	}
	scenario.ID = "user@example.org"
	if got, err := CaptureReplay(scenario, "renderless", result, 0, SystemMetadata(), DefaultRedactionConfig(), ""); err == nil || got != nil {
		t.Fatal("sensitive immutable fixture identity admitted")
	}
}

func TestReplayRegressionUsesActualPlaintextAndRejectsImpossibleProjection(t *testing.T) {
	server := NewServer()
	defer server.Close()
	source := Scenario{ID: "plain", Path: "/plain", Kind: KindUnknown, ContentType: "text/plain; charset=utf-8", HTML: "less < than"}
	if err := server.Register(source); err != nil {
		t.Fatal(err)
	}
	engineOwner := newTestEngine(t, server)
	defer closeTestResource(t, "engine", engineOwner.Close)
	page, err := engineOwner.Fetch(context.Background(), server.URL(source.Path), engine.FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "source page", page.Close)
	observed := CrossResult{URL: page.URL(), StatusCode: page.StatusCode(), Text: page.Text(), Title: page.Title()}
	replay, err := CaptureReplay(source, "renderless", observed, 0, SystemMetadata(), DefaultRedactionConfig(), "")
	if err != nil {
		t.Fatal(err)
	}
	regression, err := replay.AsRegression()
	if err != nil {
		t.Fatal(err)
	}
	if regression.ContentType != source.ContentType || regression.Expect.Title != observed.Title || regression.Expect.URL != regression.Path {
		t.Fatal("text snapshot changed its observed projection")
	}
	if err := server.Register(regression); err != nil {
		t.Fatal(err)
	}
	actual, err := engineOwner.Fetch(context.Background(), server.URL(regression.Path), engine.FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "regression page", actual.Close)
	if err := ValidateCrossResult(regression, resultFromEnginePage(context.Background(), t, actual, regression)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name              string
		observed          CrossResult
		captureFailure    bool
		projectionFailure bool
	}{
		{"impossible title", CrossResult{StatusCode: 200, Text: "text", Title: "invented title"}, false, true},
		{"complete snapshot text", CrossResult{StatusCode: 200, Text: strings.Repeat("x", (16<<10)+1)}, false, false},
		{"excess snapshot text", CrossResult{StatusCode: 200, Text: strings.Repeat("x", maxReplayTextBytes+1)}, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			replay, err := CaptureReplay(source, "renderless", test.observed, 0, SystemMetadata(), DefaultRedactionConfig(), "")
			if test.captureFailure {
				if err == nil || replay != nil {
					t.Fatal("excess snapshot bypassed capture admission")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			scenario, err := replay.AsRegression()
			if test.projectionFailure {
				if err == nil || scenario.ID != "" {
					t.Fatal("impossible projection returned a passing regression")
				}
				return
			}
			if err != nil || len(scenario.Expect.Contains) != 1 || scenario.Expect.Contains[0] != test.observed.Text || scenario.HTML != test.observed.Text {
				t.Fatalf("bounded complete text projection changed captured bytes: %v", err)
			}
			scenario.inertReplay = false
			if _, _, err := prepareScenario(scenario); err == nil {
				t.Fatal("snapshot exception widened ordinary fixture metadata")
			}
		})
	}
}

func TestReplayBuiltinPatternCompilationReturnsErrorsAndProfileBindsDefinitions(t *testing.T) {
	if patterns, err := compileReplayPatterns([]replayPatternDefinition{{"invalid", "[", "mask"}}); err == nil || patterns != nil {
		t.Fatal("invalid built-in definition was accepted")
	}
	patterns, err := compileReplayPatterns([]replayPatternDefinition{{"actual", "secret", "mask"}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := encodeReplayProfile("mask", patterns)
	if err != nil {
		t.Fatal(err)
	}
	patterns[0].replacement = "different"
	second, err := encodeReplayProfile("mask", patterns)
	if err != nil || string(first) == string(second) {
		t.Fatal("profile omitted its actual replacement definition")
	}
}
