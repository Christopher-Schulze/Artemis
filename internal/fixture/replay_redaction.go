package fixture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type replayRedactor struct {
	enabled  bool
	hash     string
	patterns []redactionPattern
}

type replayProfilePattern struct {
	Name        string
	Expression  string
	Replacement string
	Literal     bool
}

func newReplayRedactor(cfg RedactionConfig) (*replayRedactor, error) {
	if len(cfg.Patterns) > 32 || len(cfg.Mask) > 128 || !utf8.ValidString(cfg.Mask) {
		return nil, fmt.Errorf("replay: invalid or oversized redaction configuration")
	}
	profile := &replayRedactor{enabled: cfg.Enabled}
	if !cfg.Enabled {
		return profile, nil
	}
	if defaultRedactionError != nil {
		return nil, defaultRedactionError
	}
	if cfg.Mask == "" {
		cfg.Mask = "[REDACTED]"
	}
	cfg.Patterns = append([]string(nil), cfg.Patterns...)
	profile.patterns = append([]redactionPattern{}, defaultRedactionPatterns...)
	if err := profile.compileCustom(cfg); err != nil {
		return nil, err
	}
	for _, pattern := range profile.patterns {
		if pattern.re.MatchString(cfg.Mask) {
			return nil, fmt.Errorf("replay: replacement matches a sensitive-data pattern")
		}
	}
	data, err := encodeReplayProfile(cfg.Mask, profile.patterns)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(append([]byte("artemis.replay-redaction.v1:"), data...))
	profile.hash = hex.EncodeToString(digest[:])
	return profile, nil
}

func encodeReplayProfile(mask string, patterns []redactionPattern) ([]byte, error) {
	entries := make([]replayProfilePattern, 0, len(patterns))
	for _, pattern := range patterns {
		entries = append(entries, replayProfilePattern{pattern.name, pattern.re.String(), pattern.replacement, pattern.literal})
	}
	return json.Marshal(struct {
		Mask     string
		Patterns []replayProfilePattern
	}{mask, entries})
}

func (p *replayRedactor) compileCustom(cfg RedactionConfig) error {
	for i, expression := range cfg.Patterns {
		if len(expression) > 1024 || !utf8.ValidString(expression) {
			return fmt.Errorf("replay: invalid or oversized custom pattern %d", i+1)
		}
		compiled, err := regexp.Compile("(?i:" + expression + ")")
		if err != nil || compiled.MatchString("") {
			return fmt.Errorf("replay: invalid or empty-matching custom pattern %d", i+1)
		}
		p.patterns = append(p.patterns, redactionPattern{name: fmt.Sprintf("custom_%02d", i+1), re: compiled, replacement: cfg.Mask, literal: true})
	}
	return nil
}

func (p *replayRedactor) text(value string) (string, []string, error) {
	if len(value) > maxReplayTextBytes || !utf8.ValidString(value) {
		return "", nil, fmt.Errorf("replay: invalid or oversized redaction input")
	}
	var matched []string
	for _, pattern := range p.patterns {
		redacted, err := boundedReplayReplacement(value, pattern)
		if err != nil {
			return "", nil, err
		}
		if redacted != value {
			matched = append(matched, pattern.name)
		}
		value = redacted
	}
	return value, matched, nil
}

// Count replacements without building the expanded output. The second pass
// runs only when its exact size fits; no unbounded match-index slice is kept.
func boundedReplayReplacement(value string, pattern redactionPattern) (string, error) {
	if pattern.re.FindStringIndex(value) == nil {
		return value, nil
	}
	replacement := func(match string) string {
		if pattern.literal || !strings.Contains(pattern.replacement, "$") {
			return pattern.replacement
		}
		indices := pattern.re.FindStringSubmatchIndex(match)
		return string(pattern.re.ExpandString(nil, pattern.replacement, match, indices))
	}
	var replacementBytes int64
	unmatched := pattern.re.ReplaceAllStringFunc(value, func(match string) string {
		replacementBytes += int64(len(replacement(match)))
		return ""
	})
	if int64(len(unmatched))+replacementBytes > maxReplayTextBytes {
		return "", fmt.Errorf("replay: redaction expansion exceeds text limit")
	}
	return pattern.re.ReplaceAllStringFunc(value, replacement), nil
}

type replayFieldFormat uint8

const (
	replayTextField replayFieldFormat = iota
	replayURLField
	replayMIMEField
	replayIdentityField
)

type replayField struct {
	value  *string
	format replayFieldFormat
}

func replayFields(r *Replay) []replayField {
	fields := []replayField{
		{&r.ScenarioID, replayIdentityField}, {&r.Scenario.ID, replayIdentityField}, {&r.Runner, replayIdentityField},
		{&r.Scenario.Path, replayURLField}, {&r.Scenario.ContentType, replayMIMEField}, {&r.Scenario.Description, replayTextField}, {&r.Scenario.HTML, replayTextField},
		{&r.Scenario.Expect.Title, replayTextField}, {&r.Scenario.Expect.Eval, replayTextField}, {&r.Scenario.Expect.EvalContains, replayTextField}, {&r.Scenario.Expect.URL, replayURLField},
		{&r.Result.URL, replayURLField}, {&r.Result.Title, replayTextField}, {&r.Result.Text, replayTextField}, {&r.Result.HTML, replayTextField},
		{&r.Result.Markdown, replayTextField}, {&r.Result.EvalResult, replayTextField}, {&r.Result.EvalErr, replayTextField},
		{&r.Metadata.OS, replayTextField}, {&r.Metadata.Arch, replayTextField}, {&r.Metadata.GoVersion, replayTextField}, {&r.Metadata.Hostname, replayTextField},
		{&r.Metadata.Browser, replayTextField}, {&r.Metadata.BrowserVersion, replayTextField}, {&r.Metadata.UserAgent, replayTextField}, {&r.Metadata.Viewport, replayTextField},
	}
	for i := range r.Result.Links {
		fields = append(fields, replayField{&r.Result.Links[i], replayURLField})
	}
	for i := range r.Scenario.Expect.Contains {
		fields = append(fields, replayField{&r.Scenario.Expect.Contains[i], replayTextField})
	}
	for i := range r.Scenario.Expect.NotContains {
		fields = append(fields, replayField{&r.Scenario.Expect.NotContains[i], replayTextField})
	}
	fields = append(fields, corpusReplayFields(r.Result.FixtureManifest)...)
	return fields
}

func (p *replayRedactor) field(field replayField) (string, []string, error) {
	value := *field.value
	if !p.enabled {
		return value, nil, nil
	}
	switch field.format {
	case replayURLField:
		return p.cleanURL(value)
	case replayMIMEField:
		return p.cleanMIME(value)
	case replayIdentityField:
		redacted, names, err := p.text(value)
		if err != nil || redacted != value {
			return "", nil, fmt.Errorf("replay: opaque fixture and runner identities must not contain sensitive values")
		}
		return redacted, names, nil
	default:
		return p.text(value)
	}
}

func (p *replayRedactor) cleanURL(value string) (string, []string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", nil, fmt.Errorf("replay: invalid URL field")
	}
	if err := p.validateURLIdentity(parsed); err != nil {
		return "", nil, err
	}
	components := parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != ""
	parsed.User, parsed.RawQuery, parsed.Fragment, parsed.RawFragment = nil, "", "", ""
	parsed.ForceQuery = false
	path, names, err := p.text(parsed.Path)
	if err != nil {
		return "", nil, err
	}
	opaque, opaqueNames, err := p.cleanOpaqueURL(parsed.Opaque)
	if err != nil {
		return "", nil, err
	}
	names = append(names, opaqueNames...)
	changed := path != parsed.Path || opaque != parsed.Opaque
	if path != parsed.Path {
		parsed.RawPath = ""
	}
	parsed.Path, parsed.Opaque = path, opaque
	if components {
		names = append(names, "url_components")
	}
	if !components && !changed {
		return value, names, nil
	}
	return parsed.String(), names, nil
}

func (p *replayRedactor) cleanOpaqueURL(value string) (string, []string, error) {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return "", nil, fmt.Errorf("replay: invalid opaque URL encoding")
	}
	redacted, names, err := p.text(decoded)
	if err != nil {
		return "", nil, err
	}
	if redacted == decoded {
		return value, names, nil
	}
	return url.PathEscape(redacted), names, nil
}

func (p *replayRedactor) validateURLIdentity(parsed *url.URL) error {
	for _, identity := range []string{parsed.Host, parsed.Scheme} {
		redacted, _, err := p.text(identity)
		if err != nil || redacted != identity {
			return fmt.Errorf("replay: URL authority or scheme violates the redaction profile")
		}
	}
	return nil
}

func (p *replayRedactor) cleanMIME(value string) (string, []string, error) {
	if value == "" {
		return "", nil, nil
	}
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil {
		return "", nil, fmt.Errorf("replay: invalid content-type field")
	}
	var names []string
	for key, parameter := range parameters {
		if replayCredentialMIMEKey(key) {
			parameters[key] = "[REDACTED]"
			if parameter != "[REDACTED]" {
				names = append(names, "mime_credentials")
			}
			continue
		}
		redacted, matched, err := p.text(parameter)
		if err != nil {
			return "", nil, err
		}
		parameters[key] = redacted
		names = append(names, matched...)
	}
	if len(names) == 0 {
		return value, nil, nil
	}
	formatted := mime.FormatMediaType(mediaType, parameters)
	if formatted == "" {
		return "", nil, fmt.Errorf("replay: redacted content type is not representable")
	}
	return formatted, names, nil
}

func replayCredentialMIMEKey(key string) bool {
	switch strings.ToLower(key) {
	case "api_key", "api-key", "token", "secret", "password", "passwd", "pwd":
		return true
	default:
		return false
	}
}

func (p *replayRedactor) apply(r *Replay) error {
	matched := make(map[string]bool)
	if p.enabled && r.Metadata.Hostname != "" {
		r.Metadata.Hostname = ""
		matched["hostname"] = true
	}
	for _, field := range replayFields(r) {
		value, names, err := p.field(field)
		if err != nil {
			return err
		}
		*field.value = value
		for _, name := range names {
			matched[name] = true
		}
	}
	for name := range matched {
		r.RedactionLog = append(r.RedactionLog, name)
	}
	sort.Strings(r.RedactionLog)
	return nil
}

func (r *Replay) validateRedaction() error {
	p := r.redactionProfile
	if p == nil || p.enabled != r.Redacted || p.hash != r.RedactionProfileHash {
		return fmt.Errorf("replay: missing or mismatched verified redaction profile")
	}
	if !p.enabled {
		if len(r.RedactionLog) != 0 {
			return fmt.Errorf("replay: unredacted record cannot claim redaction events")
		}
		return nil
	}
	for _, field := range replayFields(r) {
		value, _, err := p.field(field)
		if err != nil || value != *field.value {
			return fmt.Errorf("replay: content violates the verified redaction profile")
		}
	}
	known := map[string]bool{"hostname": true, "url_components": true, "mime_credentials": true}
	for _, pattern := range p.patterns {
		known[pattern.name] = true
	}
	previous := ""
	for _, name := range r.RedactionLog {
		if !known[name] || name <= previous {
			return fmt.Errorf("replay: invalid redaction-log identity or order")
		}
		previous = name
	}
	return nil
}
