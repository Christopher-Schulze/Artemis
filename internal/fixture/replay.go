package fixture

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Replay is a captured execution of a fixture scenario through one runner.
// It stores the original scenario, the observed crossResult, runtime metadata,
// and whether PII/credential redaction was applied. It is used for evidence
// capture and for converting external-smoke or local-regression observations
// into deterministic fixtures.

type Replay struct {
	ID           string          `json:"id"`
	ScenarioID   string          `json:"scenario_id"`
	Runner       string          `json:"runner"`
	Scenario     Scenario        `json:"scenario"`
	Result       CrossResult     `json:"result"`
	CapturedAt   time.Time       `json:"captured_at"`
	DurationMs   int64           `json:"duration_ms"`
	Metadata     CaptureMetadata `json:"metadata"`
	Redacted     bool            `json:"redacted"`
	RedactionLog []string        `json:"redaction_log,omitempty"`
}

// CaptureMetadata records environment and browser context for a replay.
// Browser, BrowserVersion, and UserAgent are best-effort; renderless paths
// leave them empty.
type CaptureMetadata struct {
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	GoVersion      string `json:"go_version"`
	Hostname       string `json:"hostname,omitempty"`
	Browser        string `json:"browser,omitempty"`
	BrowserVersion string `json:"browser_version,omitempty"`
	UserAgent      string `json:"user_agent,omitempty"`
	Viewport       string `json:"viewport,omitempty"`
}

// RedactionConfig controls sensitive-data redaction in replay captures.
// Enabled is true by default. Mask is the replacement string. Patterns may
// contain additional user-supplied regular expressions; they are matched
// case-insensitively and replace the whole match.
type RedactionConfig struct {
	Enabled  bool
	Mask     string
	Patterns []string
}

// DefaultRedactionConfig returns the canonical redaction config.
func DefaultRedactionConfig() RedactionConfig {
	return RedactionConfig{
		Enabled: true,
		Mask:    "[REDACTED]",
	}
}

// SystemMetadata returns capture metadata that is available without a live
// browser. The caller can fill Browser, BrowserVersion, UserAgent, and
// Viewport from the concrete runner or page.
func SystemMetadata() CaptureMetadata {
	host, _ := os.Hostname()
	return CaptureMetadata{
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		Hostname:  host,
	}
}

// CaptureReplay records a CrossResult for a Scenario, optionally redacts
// sensitive literals, writes the replay to outDir, and returns the captured
// Replay. If outDir is empty, the replay is not written to disk.
func CaptureReplay(sc Scenario, runner string, result CrossResult, d time.Duration, meta CaptureMetadata, cfg RedactionConfig, outDir string) (*Replay, error) {
	redacted, log := redactCrossResult(result, cfg)
	if meta.OS == "" {
		sys := SystemMetadata()
		if meta.OS == "" {
			meta.OS = sys.OS
		}
		if meta.Arch == "" {
			meta.Arch = sys.Arch
		}
		if meta.GoVersion == "" {
			meta.GoVersion = sys.GoVersion
		}
		if meta.Hostname == "" {
			meta.Hostname = sys.Hostname
		}
	}

	id := fmt.Sprintf("%s-%s-%d", sanitize(runner), sanitize(sc.ID), time.Now().UTC().UnixMilli())
	replay := &Replay{
		ID:           id,
		ScenarioID:   sc.ID,
		Runner:       runner,
		Scenario:     sc,
		Result:       redacted,
		CapturedAt:   time.Now().UTC(),
		DurationMs:   d.Milliseconds(),
		Metadata:     meta,
		Redacted:     cfg.Enabled,
		RedactionLog: log,
	}

	if outDir != "" {
		if _, err := replay.Save(outDir); err != nil {
			return replay, err
		}
	}
	return replay, nil
}

// Save serializes the replay to JSON under dir with 0600 permissions and
// returns the written path. Parent directories are created with 0700.
func (r *Replay) Save(dir string) (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("replay: marshal: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("replay: mkdir: %w", err)
	}
	path := filepath.Join(dir, r.filename())
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("replay: write: %w", err)
	}
	return path, nil
}

func (r *Replay) filename() string {
	return fmt.Sprintf("replay-%s-%s.json", sanitize(r.Runner), sanitize(r.ScenarioID))
}

// LoadReplay reads a replay from a JSON file.
func LoadReplay(path string) (*Replay, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("replay: read %s: %w", path, err)
	}
	var r Replay
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("replay: parse %s: %w", path, err)
	}
	return &r, nil
}

// AsRegression converts a replay into a deterministic Scenario that can be
// registered on a fixture server. The new scenario uses the captured HTML
// and status, and an Expect derived from the redacted result so the
// regression is self-contained and does not depend on external state.
func (r *Replay) AsRegression() Scenario {
	body := r.Result.HTML
	ct := "text/html; charset=utf-8"
	kind := KindHTML
	if body == "" {
		body = r.Result.Text
		ct = "text/plain; charset=utf-8"
		kind = KindUnknown
	}
	if !strings.Contains(body, "<") {
		ct = "text/plain; charset=utf-8"
		kind = KindUnknown
	}

	reg := r.Scenario
	reg.ID = r.ScenarioID + "-regression"
	reg.Path = "/regression/" + sanitize(r.ScenarioID)
	reg.Kind = kind
	reg.ContentType = ct
	reg.Status = r.Result.StatusCode
	reg.HTML = body
	reg.Expect = Expect{
		Status: r.Result.StatusCode,
		Title:  r.Result.Title,
	}
	if r.Result.Title != "" {
		reg.Expect.Contains = []string{r.Result.Title}
	}
	if u, err := url.Parse(r.Result.URL); err == nil && u.Path != "" {
		reg.Expect.URL = u.Path
	}
	return reg
}

// Redact applies the configured redaction patterns to a single string and
// returns the sanitized string. If cfg.Enabled is false, s is returned
// unchanged. Built-in patterns use their own replacement labels; user-supplied
// patterns use cfg.Mask.
func Redact(s string, cfg RedactionConfig) string {
	if !cfg.Enabled || s == "" {
		return s
	}
	for _, p := range defaultRedactionPatterns {
		s = p.re.ReplaceAllString(s, p.replacement)
	}
	mask := cfg.Mask
	if mask == "" {
		mask = "[REDACTED]"
	}
	for _, expr := range cfg.Patterns {
		re, err := regexp.Compile("(?i:" + expr + ")")
		if err != nil {
			// Ignore invalid user patterns; continue with the rest.
			continue
		}
		s = re.ReplaceAllString(s, mask)
	}
	return s
}

// redactionPattern pairs a compiled regex with the literal replacement.
// The replacement is used for built-in patterns; user-supplied patterns use
// cfg.Mask.
type redactionPattern struct {
	name        string
	re          *regexp.Regexp
	replacement string
}

// defaultRedactionPatterns are compiled once and matched case-insensitively.
// Each pattern is deliberately conservative: it targets literal values, not
// benign HTML form attribute names like name="password".
var defaultRedactionPatterns = func() []redactionPattern {
	defs := []struct{ name, expr, replacement string }{
		{
			name:        "email",
			expr:        `[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}`,
			replacement: "[REDACTED_EMAIL]",
		},
		{
			name:        "credit_card",
			expr:        `\b(?:\d[ -]*?){13,16}\b`,
			replacement: "[REDACTED_CC]",
		},
		{
			name:        "ssn",
			expr:        `\b\d{3}-\d{2}-\d{4}\b`,
			replacement: "[REDACTED_SSN]",
		},
		{
			name:        "phone",
			expr:        `\b(?:\+\d{1,3}[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`,
			replacement: "[REDACTED_PHONE]",
		},
		{
			name:        "url_secret",
			expr:        `(api[_-]?key|token|secret|password|passwd|pwd)=[^&\s"']+`,
			replacement: "[REDACTED]",
		},
		{
			name:        "json_secret",
			expr:        `"(api[_-]?key|token|secret|password|passwd|pwd)"\s*:\s*"[^"]*"`,
			replacement: "[REDACTED]",
		},
		{
			name:        "html_password_value",
			expr:        `(name=["']?password["']?[^>]*\svalue=["']?)([^"'\s>]+)`,
			replacement: "$1[REDACTED]",
		},
		{
			name:        "auth_header",
			expr:        `(Authorization:\s*(?:Bearer|Basic|Token)\s+)[^\s]+`,
			replacement: "[REDACTED]",
		},
	}
	var out []redactionPattern
	for _, d := range defs {
		re, err := regexp.Compile("(?i:" + d.expr + ")")
		if err != nil {
			panic(fmt.Sprintf("replay: invalid built-in redaction pattern %q: %v", d.name, err))
		}
		out = append(out, redactionPattern{name: d.name, re: re, replacement: d.replacement})
	}
	return out
}()

// redactCrossResult returns a copy of result with all string fields redacted.
// It also returns a log of built-in patterns that actually matched.
func redactCrossResult(result CrossResult, cfg RedactionConfig) (CrossResult, []string) {
	if !cfg.Enabled {
		return result, nil
	}
	log := make(map[string]struct{})

	redact := func(s string) string {
		for _, p := range defaultRedactionPatterns {
			if p.re.MatchString(s) {
				log[p.name] = struct{}{}
			}
		}
		return Redact(s, cfg)
	}

	out := result
	out.URL = redact(result.URL)
	out.Title = redact(result.Title)
	out.Text = redact(result.Text)
	out.HTML = redact(result.HTML)
	out.Markdown = redact(result.Markdown)
	out.EvalResult = redact(result.EvalResult)
	out.EvalErr = redact(result.EvalErr)
	if len(result.Links) > 0 {
		out.Links = make([]string, len(result.Links))
		for i, l := range result.Links {
			out.Links[i] = redact(l)
		}
	}

	var names []string
	for n := range log {
		names = append(names, n)
	}
	return out, names
}

// sanitize turns an identifier into a filesystem-safe string.
func sanitize(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	name := b.String()
	if name == "" {
		name = "unknown"
	}
	return name
}
