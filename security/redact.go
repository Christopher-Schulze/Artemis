// Package security: redact.go implements the Secret Redaction Pipeline
// (spec L4279): a 3-point redaction system that scrubs secrets at
// (1) URL validation before navigation, (2) snapshot text before LLM
// ingestion, and (3) vision LLM output after generation.
//
// Patterns are precompiled regexes applied in order; the first match at
// a position wins so that overlapping patterns (e.g. an API key that
// also looks like a credit card) are redacted once and consistently.
package security

import (
	"fmt"
	"regexp"
	"sync"
)

// RedactionPoint identifies one of the three pipeline stages (spec L4279).
type RedactionPoint string

const (
	// RedactionPointURLValidation is point 1: validate/redact URLs
	// before navigation so secrets in query strings never reach the
	// browser/network layer.
	RedactionPointURLValidation RedactionPoint = "url_validation"
	// RedactionPointSnapshotText is point 2: redact DOM/snapshot text
	// before it is sent to an LLM context window.
	RedactionPointSnapshotText RedactionPoint = "snapshot_text"
	// RedactionPointVisionOutput is point 3: redact secrets that an LLM
	// may have echoed back in vision/OCR output.
	RedactionPointVisionOutput RedactionPoint = "vision_output"
)

// defaultReplacement is the token substituted for matched secrets.
const defaultReplacement = "[REDACTED]"

// RedactionConfig configures the Redactor (spec L4279).
type RedactionConfig struct {
	// Enabled gates redaction. When false, Redact returns the content
	// unchanged with empty PatternsMatched.
	Enabled bool
	// Patterns is the ordered list of named regex patterns to apply.
	Patterns []RedactionPattern
	// Replacement is the substitution string for matches. Defaults to
	// "[REDACTED]" when empty.
	Replacement string
}

// RedactionPattern is a named, precompiled secret pattern.
type RedactionPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

// RedactionResult is the outcome of a single Redact call.
type RedactionResult struct {
	Original        string
	Redacted        string
	PatternsMatched []string
	Point           RedactionPoint
}

// RedactionStats tracks redactor throughput for observability.
type RedactionStats struct {
	Total           uint64 // total Redact calls
	Redacted        uint64 // calls that changed content
	PatternsMatched uint64 // total individual pattern matches across calls
}

// DefaultRedactionConfig returns an enabled config with the standard
// secret patterns (spec L4279): API keys, bearer tokens, passwords,
// SSNs, credit cards, emails, and phone numbers. Patterns are
// case-insensitive where appropriate and intentionally conservative to
// minimize false positives on ordinary prose.
func DefaultRedactionConfig() RedactionConfig {
	return RedactionConfig{
		Enabled:     true,
		Replacement: defaultReplacement,
		Patterns:    defaultPatterns(),
	}
}

// defaultPatterns returns the ordered, precompiled secret patterns.
// Order matters: more specific/higher-entropy patterns first so that a
// long API key is not partially consumed by a looser pattern.
func defaultPatterns() []RedactionPattern {
	return []RedactionPattern{
		{
			Name: "api_key",
			// Common key prefixes with 20+ alphanumerics/underscores/dashes
			// (e.g. sk_test_4eC39..., AKIA..., ghp_..., xoxb-...).
			Pattern: regexp.MustCompile(`(?i)(?:api[_-]?key|sk|pk|AKIA|ghp|gho|ghs|xox[baprs])[_-]?[A-Za-z0-9_\-]{20,}`),
		},
		{
			Name: "bearer_token",
			// Authorization: Bearer <token> and bare bearer tokens.
			Pattern: regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-._~+\/]{16,}={0,2}`),
		},
		{
			Name: "password",
			// password=... or password: ... up to a delimiter.
			Pattern: regexp.MustCompile(`(?i)(?:password|passwd|pwd)\s*[:=]\s*[^\s&;"']{4,}`),
		},
		{
			Name: "ssn",
			// US Social Security Number: NNN-NN-NNNN.
			Pattern: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		},
		{
			Name: "credit_card",
			// 13-19 digit groups separated by spaces or dashes.
			Pattern: regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`),
		},
		{
			Name:    "email",
			Pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
		},
		{
			Name: "phone",
			// International and US phone formats.
			Pattern: regexp.MustCompile(`\+?\d[\d\s().\-]{8,}\d`),
		},
	}
}

// Redactor applies the configured secret patterns at the three pipeline
// points (spec L4279). It is safe for concurrent use.
type Redactor struct {
	mu   sync.RWMutex
	cfg  RedactionConfig
	stat RedactionStats
	// compiled caches the compiled patterns from cfg so repeated Redact
	// calls don't recompile. Built once at construction.
	compiled []RedactionPattern
}

// NewRedactor builds a Redactor from cfg. If cfg.Patterns contains
// entries with nil Pattern, they are dropped. An empty Replacement
// defaults to "[REDACTED]".
func NewRedactor(cfg RedactionConfig) *Redactor {
	if cfg.Replacement == "" {
		cfg.Replacement = defaultReplacement
	}
	compiled := make([]RedactionPattern, 0, len(cfg.Patterns))
	for _, p := range cfg.Patterns {
		if p.Pattern == nil {
			continue
		}
		compiled = append(compiled, p)
	}
	return &Redactor{cfg: cfg, compiled: compiled}
}

// Redact applies all enabled patterns to content at the given pipeline
// point and returns the redacted text plus the names of patterns that
// matched at least once.
func (r *Redactor) Redact(content string, point RedactionPoint) RedactionResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stat.Total++

	result := RedactionResult{
		Original: content,
		Redacted: content,
		Point:    point,
	}

	if !r.cfg.Enabled || len(r.compiled) == 0 {
		return result
	}

	matched := make(map[string]struct{})
	redacted := content
	totalMatches := uint64(0)
	for _, p := range r.compiled {
		matches := p.Pattern.FindAllString(redacted, -1)
		if len(matches) > 0 {
			matched[p.Name] = struct{}{}
			totalMatches += uint64(len(matches))
			redacted = p.Pattern.ReplaceAllString(redacted, r.cfg.Replacement)
		}
	}

	if redacted != content {
		r.stat.Redacted++
	}
	r.stat.PatternsMatched += totalMatches

	result.Redacted = redacted
	for name := range matched {
		result.PatternsMatched = append(result.PatternsMatched, name)
	}
	// Stable ordering by pattern definition order for deterministic output.
	order := make(map[string]int, len(r.compiled))
	for i, p := range r.compiled {
		order[p.Name] = i
	}
	// Insertion-sort the small matched slice for deterministic order.
	for i := 1; i < len(result.PatternsMatched); i++ {
		for j := i; j > 0 && order[result.PatternsMatched[j]] < order[result.PatternsMatched[j-1]]; j-- {
			result.PatternsMatched[j], result.PatternsMatched[j-1] = result.PatternsMatched[j-1], result.PatternsMatched[j]
		}
	}
	return result
}

// ValidateURL is redaction point 1 (spec L4279): redact secrets in a URL
// before navigation. The URL is treated as raw content so query-string
// secrets (e.g. ?api_key=...) are scrubbed.
func (r *Redactor) ValidateURL(url string) RedactionResult {
	return r.Redact(url, RedactionPointURLValidation)
}

// RedactSnapshot is redaction point 2 (spec L4279): redact secrets in
// DOM/snapshot text before it is sent to an LLM.
func (r *Redactor) RedactSnapshot(text string) RedactionResult {
	return r.Redact(text, RedactionPointSnapshotText)
}

// RedactVisionOutput is redaction point 3 (spec L4279): redact secrets
// that an LLM may have echoed back in vision/OCR output.
func (r *Redactor) RedactVisionOutput(text string) RedactionResult {
	return r.Redact(text, RedactionPointVisionOutput)
}

// Stats returns a snapshot of the redactor stats.
func (r *Redactor) Stats() RedactionStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stat
}

// Config returns a copy of the current configuration.
func (r *Redactor) Config() RedactionConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg
}

// String renders a RedactionResult for debug logging without re-exposing
// the original secret-bearing content.
func (res RedactionResult) String() string {
	return fmt.Sprintf("RedactionResult{point=%s, matched=%v, redacted_len=%d}", res.Point, res.PatternsMatched, len(res.Redacted))
}

// HasMatch reports whether any pattern matched in this result.
func (res RedactionResult) HasMatch() bool {
	return len(res.PatternsMatched) > 0
}

// Changed reports whether redaction altered the content.
func (res RedactionResult) Changed() bool {
	return res.Original != res.Redacted
}
