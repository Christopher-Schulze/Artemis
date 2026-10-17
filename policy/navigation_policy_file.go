package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

const (
	maxNavigationPolicyBytes        = 64 << 10
	maxNavigationPolicyRules        = 256
	maxNavigationPolicyPatternBytes = 2048
)

var ErrNavigationPolicyStopped = errors.New("navigation policy: stopped")

var errNavigationPolicyFileChanged = errors.New("navigation policy: file changed during read")

// LastReloadError returns the bounded candidate error, cleared by valid content.
func (p *NavigationPolicy) LastReloadError() error {
	if p == nil {
		return errors.New("navigation policy: policy required")
	}
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	return p.lastError
}

func readNavigationPolicyFile(path string) (data []byte, digest string, resultErr error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, "read:stat", errors.New("navigation policy: file unavailable")
	}
	if !before.Mode().IsRegular() || before.Size() > maxNavigationPolicyBytes {
		return nil, "read:type_or_size", errors.New("navigation policy: bounded regular file required")
	}
	file, err := openNavigationPolicyFile(path)
	if err != nil {
		return nil, "read:open", errors.New("navigation policy: file cannot be opened")
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, errors.New("navigation policy: file close failed"))
			digest = "read:close"
		}
	}()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		return nil, "read:identity", errors.New("navigation policy: invalid file descriptor")
	}
	if !os.SameFile(before, opened) {
		return nil, "read:changed", errNavigationPolicyFileChanged
	}
	data, err = io.ReadAll(io.LimitReader(file, maxNavigationPolicyBytes+1))
	if err != nil || len(data) > maxNavigationPolicyBytes {
		return nil, "read:content", errors.New("navigation policy: bounded read failed")
	}
	after, err := file.Stat()
	current, pathErr := os.Lstat(path)
	if err != nil || pathErr != nil || !os.SameFile(opened, current) || int64(len(data)) != opened.Size() || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		return nil, "read:changed", errNavigationPolicyFileChanged
	}
	return data, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func decodeNavigationPolicy(data []byte) (*PolicyFile, error) {
	if len(data) > maxNavigationPolicyBytes || !utf8.Valid(data) || len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return nil, errors.New("navigation policy: JSON object required")
	}
	if err := validatePolicyJSON(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document struct {
		Version         *int              `json:"version"`
		Default         *PolicyFileAction `json:"default"`
		Rules           []PolicyRule      `json:"rules"`
		Allow           []string          `json:"allow"`
		Deny            []string          `json:"deny"`
		Confirm         []string          `json:"confirm"`
		DomainAllowlist []string          `json:"domain_allowlist"`
	}
	if err := decoder.Decode(&document); err != nil {
		return nil, errors.New("navigation policy: invalid schema")
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return nil, errors.New("navigation policy: trailing JSON")
	}
	candidate := PolicyFile{Version: PolicyFileVersion, Default: PolicyFileDeny, Rules: document.Rules,
		Allow: document.Allow, Deny: document.Deny, Confirm: document.Confirm, DomainAllowlist: document.DomainAllowlist,
		actionConfigured: document.Rules == nil || document.Allow != nil || document.Deny != nil || document.Confirm != nil}
	if document.Version != nil {
		candidate.Version = *document.Version
	}
	if candidate.Version != PolicyFileVersion || len(candidate.Rules) > maxNavigationPolicyRules {
		return nil, errors.New("navigation policy: unsupported version or rule count")
	}
	if document.Default != nil {
		candidate.Default = PolicyFileAction(strings.ToLower(strings.TrimSpace(string(*document.Default))))
	}
	if !validPolicyFileAction(candidate.Default) {
		return nil, errors.New("navigation policy: invalid default")
	}
	for i := range candidate.Rules {
		if err := compileNavigationPolicyRule(&candidate.Rules[i]); err != nil {
			return nil, fmt.Errorf("navigation policy: rule %d: %w", i, err)
		}
	}
	if err := compilePolicyCategories(&candidate); err != nil {
		return nil, err
	}
	return &candidate, nil
}

// Validate duplicate object fields and null values before typed decoding.
func validatePolicyJSON(decoder *json.Decoder, depth int) error {
	if depth > 3 {
		return errors.New("navigation policy: JSON nesting limit")
	}
	token, err := decoder.Token()
	if err != nil || token == nil {
		return errors.New("navigation policy: invalid or null JSON value")
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	fields := make(map[string]struct{})
	for decoder.More() {
		if delimiter == '{' {
			key, keyErr := decoder.Token()
			name, valid := key.(string)
			if _, duplicate := fields[name]; keyErr != nil || !valid || duplicate {
				return errors.New("navigation policy: invalid or duplicate JSON field")
			}
			rootField := depth == 0 && (name == "version" || name == "default" || name == "rules" || name == "allow" || name == "deny" || name == "confirm" || name == "domain_allowlist")
			ruleField := depth == 2 && (name == "action" || name == "pattern")
			if !rootField && !ruleField {
				return errors.New("navigation policy: unknown or noncanonical JSON field")
			}
			fields[name] = struct{}{}
		}
		if err := validatePolicyJSON(decoder, depth+1); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return errors.New("navigation policy: incomplete JSON")
	}
	return nil
}

func validPolicyFileAction(action PolicyFileAction) bool {
	return action == PolicyFileAllow || action == PolicyFileDeny || action == PolicyFileChallenge
}

// Dots and URL query markers are literal unless another regexp marker exists.
// Brackets around an actual IPv6 authority are also literal.
func hasRegexMeta(pattern string) bool {
	if parsed, err := url.Parse(pattern); err == nil && net.ParseIP(parsed.Hostname()) != nil {
		pattern = strings.Replace(pattern, parsed.Host, "", 1)
	}
	return strings.ContainsAny(pattern, `^$*+()[]{}|\`)
}

func compileNavigationPolicyRule(rule *PolicyRule) error {
	if !validPolicyFileAction(rule.Action) || rule.Pattern == "" || len(rule.Pattern) > maxNavigationPolicyPatternBytes {
		return errors.New("invalid action or pattern size")
	}
	if hasRegexMeta(rule.Pattern) {
		compiled, err := regexp.Compile("^(?:" + rule.Pattern + ")$")
		if err != nil {
			return errors.New("invalid regular expression")
		}
		rule.compiled = compiled
		return nil
	}
	if _, err := parseNavigationPolicyURL(rule.Pattern); err != nil {
		return errors.New("invalid literal URL pattern")
	}
	rule.literal = true
	return nil
}

func parseNavigationPolicyURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || len(rawURL) > 8192 || parsed.User != nil || parsed.Opaque != "" || parsed.Host == "" || parsed.Hostname() == "" || strings.ContainsAny(rawURL, "\\\r\n\t ") || !utf8.ValidString(rawURL) {
		return nil, errors.New("invalid navigation URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("unsupported navigation scheme")
	}
	host := parsed.Hostname()
	if net.ParseIP(host) == nil {
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil || ascii == "" || strings.ContainsAny(ascii, "%/:@[]") {
			return nil, errors.New("invalid navigation host")
		}
	}
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("invalid navigation port")
		}
	}
	return parsed, nil
}

func matchLiteralPolicyURL(pattern, rawURL string) bool {
	base, err := parseNavigationPolicyURL(pattern)
	if err != nil {
		return false
	}
	target, err := parseNavigationPolicyURL(rawURL)
	if err != nil || base.Scheme != target.Scheme || !strings.EqualFold(base.Hostname(), target.Hostname()) || (base.Port() != "" && base.Port() != target.Port()) {
		return false
	}
	return strings.HasPrefix(target.EscapedPath(), base.EscapedPath()) && (base.RawQuery == "" || base.RawQuery == target.RawQuery) && (base.Fragment == "" || base.Fragment == target.Fragment)
}
