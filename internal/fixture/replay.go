package fixture

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Replay is a captured execution of a fixture scenario through one runner.
// It stores the source scenario descriptor, observed CrossResult, runtime metadata,
// and whether PII/credential redaction was applied. It is used for evidence
// capture and for converting external-smoke or local-regression observations
// into deterministic fixtures.

type Replay struct {
	SchemaVersion        int             `json:"schema_version"`
	ContentHash          string          `json:"content_hash"`
	ID                   string          `json:"id"`
	ScenarioID           string          `json:"scenario_id"`
	Runner               string          `json:"runner"`
	Scenario             Scenario        `json:"scenario"`
	Result               CrossResult     `json:"result"`
	CapturedAt           time.Time       `json:"captured_at"`
	DurationMs           int64           `json:"duration_ms"`
	Metadata             CaptureMetadata `json:"metadata"`
	Redacted             bool            `json:"redacted"`
	RedactionProfileHash string          `json:"redaction_profile_hash"`
	RedactionLog         []string        `json:"redaction_log,omitempty"`
	redactionProfile     *replayRedactor
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
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
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
	profile, err := newReplayRedactor(cfg)
	if err != nil {
		return nil, err
	}
	replay, err := newReplayObservation(sc, runner, result, d, meta)
	if err != nil {
		return nil, err
	}
	replay.Redacted = cfg.Enabled
	replay.RedactionProfileHash = profile.hash
	replay.redactionProfile = profile
	if err := profile.apply(replay); err != nil {
		return nil, err
	}
	if err := replay.seal(); err != nil {
		return nil, err
	}

	if outDir != "" {
		if _, err := replay.Save(outDir); err != nil {
			return replay, err
		}
	}
	return replay, nil
}

func newReplayObservation(sc Scenario, runner string, result CrossResult, duration time.Duration, metadata CaptureMetadata) (*Replay, error) {
	if duration < 0 {
		return nil, fmt.Errorf("replay: negative capture duration")
	}
	scenario, _, err := prepareScenario(sc)
	if err != nil {
		return nil, err
	}
	if result.FixtureManifest != nil {
		result.FixtureManifest, err = bindCorpusCapture(scenario, runner, *result.FixtureManifest)
		if err != nil {
			return nil, err
		}
	}
	scenario.Handler = nil
	if result.Links != nil {
		result.Links = append([]string{}, result.Links...)
	}
	replay := &Replay{
		SchemaVersion: replaySchemaVersion, ID: rand.Text(), ScenarioID: scenario.ID,
		Runner: runner, Scenario: scenario, Result: result, CapturedAt: time.Now().UTC(),
		DurationMs: duration.Milliseconds(), Metadata: completeCaptureMetadata(metadata),
	}
	if err := replay.validateFields(); err != nil {
		return nil, err
	}
	return replay, nil
}

func completeCaptureMetadata(metadata CaptureMetadata) CaptureMetadata {
	system := SystemMetadata()
	if metadata.OS == "" {
		metadata.OS = system.OS
	}
	if metadata.Arch == "" {
		metadata.Arch = system.Arch
	}
	if metadata.GoVersion == "" {
		metadata.GoVersion = system.GoVersion
	}
	if metadata.Hostname == "" {
		metadata.Hostname = system.Hostname
	}
	return metadata
}

// Save publishes a validated replay exclusively with file and directory sync.
// A successful path never replaces an existing replay.
func (r *Replay) Save(dir string) (string, error) {
	data, err := r.validatedBytes()
	if err != nil {
		return "", fmt.Errorf("replay: marshal: %w", err)
	}
	return publishReplay(dir, r.filename(), data)
}

func (r *Replay) filename() string {
	return "replay-" + r.ID + ".json"
}

// LoadReplay requires the same verified redaction profile used for capture.
// No supplied config selects the default profile; raw records require Enabled=false.
func LoadReplay(path string, configurations ...RedactionConfig) (*Replay, error) {
	profile, err := replayProfile(configurations)
	if err != nil {
		return nil, err
	}
	data, err := readReplayBytes(path)
	if err != nil {
		return nil, fmt.Errorf("replay: read %s: %w", path, err)
	}
	r, err := decodeVerifiedReplay(data, profile)
	if err != nil {
		return nil, fmt.Errorf("replay: parse %s: %w", path, err)
	}
	if filepath.Base(path) != r.filename() {
		return nil, fmt.Errorf("replay: filename does not match immutable identity")
	}
	return r, nil
}

// DecodeReplay applies the existing file loader's strict record/profile checks
// to bounded bytes supplied by a governed content owner, without a file copy.
func DecodeReplay(data []byte, configurations ...RedactionConfig) (*Replay, error) {
	profile, err := replayProfile(configurations)
	if err != nil {
		return nil, err
	}
	return decodeVerifiedReplay(data, profile)
}

func replayProfile(configurations []RedactionConfig) (*replayRedactor, error) {
	if len(configurations) > 1 {
		return nil, fmt.Errorf("replay: at most one redaction profile is accepted")
	}
	cfg := DefaultRedactionConfig()
	if len(configurations) == 1 {
		cfg = configurations[0]
	}
	return newReplayRedactor(cfg)
}

func decodeVerifiedReplay(data []byte, profile *replayRedactor) (*Replay, error) {
	var r Replay
	if err := decodeReplay(data, &r); err != nil {
		return nil, err
	}
	r.redactionProfile = profile
	if _, err := r.validatedBytes(); err != nil {
		return nil, err
	}
	return &r, nil
}

// AsRegression converts a replay into a deterministic Scenario that can be
// registered on a fixture server. The new scenario uses the captured HTML
// and status, and an Expect derived from the redacted result so the
// projection retains the captured document semantics. External subresources
// are not captured here. The fixture server enforces an inert HTTP projection;
// native browsers cannot execute its scripts or load its subresources.
func (r *Replay) AsRegression() (Scenario, error) {
	if _, err := r.validatedBytes(); err != nil {
		return Scenario{}, err
	}
	return r.projectRegression()
}

func (r *Replay) projectRegression() (Scenario, error) {
	body := r.Result.HTML
	ct := "text/html; charset=utf-8"
	kind := KindHTML
	if body == "" {
		body = r.Result.Text
		ct = "text/plain; charset=utf-8"
		kind = KindUnknown
	}
	if kind == KindUnknown && r.Result.Title != "" {
		return Scenario{}, fmt.Errorf("replay: plaintext snapshot cannot reproduce captured document title")
	}

	reg := r.Scenario
	reg.inertReplay = true
	reg.Handler = nil
	reg.RunScripts = false
	reg.AsyncFetch = false
	reg.WaitForIdle = false
	reg.ScriptCount = 0
	reg.ID = "regression-" + r.ID
	reg.Path = "/regression/" + r.ID
	reg.Kind = kind
	reg.ContentType = ct
	reg.Status = r.Result.StatusCode
	reg.HTML = body
	reg.BodyBytes = len(body)
	reg.Expect = Expect{
		Status: r.Result.StatusCode,
		Title:  r.Result.Title,
	}
	if r.Result.Text != "" {
		reg.Expect.Contains = []string{r.Result.Text}
	}
	reg.Expect.URL = reg.Path
	prepared, _, err := prepareScenario(reg)
	if err != nil {
		return Scenario{}, fmt.Errorf("replay: snapshot is not representable as a bounded regression: %w", err)
	}
	return prepared, nil
}

// Redact applies the configured redaction patterns to a single string and
// returns the sanitized string or an error without partial output. If cfg.Enabled is false, s is returned
// unchanged. Built-in patterns use their own replacement labels; user-supplied
// patterns use cfg.Mask.
func Redact(s string, cfg RedactionConfig) (string, error) {
	profile, err := newReplayRedactor(cfg)
	if err != nil {
		return "", err
	}
	value, _, err := profile.text(s)
	return value, err
}

// redactionPattern pairs a compiled regex with the literal replacement.
// The replacement is used for built-in patterns; user-supplied patterns use
// cfg.Mask.
type redactionPattern struct {
	name        string
	re          *regexp.Regexp
	replacement string
	literal     bool
}

// defaultRedactionPatterns are compiled once and matched case-insensitively.
// Each pattern is deliberately conservative: it targets literal values, not
// benign HTML form attribute names like name="password".
var defaultRedactionPatterns, defaultRedactionError = compileDefaultRedactionPatterns()

type replayPatternDefinition struct{ name, expr, replacement string }

func compileDefaultRedactionPatterns() ([]redactionPattern, error) {
	defs := []replayPatternDefinition{
		{name: "url_userinfo", expr: `([a-z][a-z0-9+.-]*://)[^/@\s"'<>]+@`, replacement: "$1[REDACTED]@"},
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
	return compileReplayPatterns(defs)
}

func compileReplayPatterns(defs []replayPatternDefinition) ([]redactionPattern, error) {
	var out []redactionPattern
	for _, d := range defs {
		re, err := regexp.Compile("(?i:" + d.expr + ")")
		if err != nil {
			return nil, fmt.Errorf("replay: invalid built-in redaction pattern %s: %w", d.name, err)
		}
		out = append(out, redactionPattern{name: d.name, re: re, replacement: d.replacement})
	}
	return out, nil
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
