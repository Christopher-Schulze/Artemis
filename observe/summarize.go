package observe

import (
	"strings"
	"sync"
	"unicode"
)

// summarize.go (spec L4278: Task-Aware Snapshot Summarization).
//
// When a captured snapshot exceeds a character threshold (default
// 8000), the summarizer extracts task-relevant sentences and falls
// back to truncation when no relevant content is found. Thread-safe
// via RWMutex and tracks basic stats.
//
// Reference: research/webstack/pinchtab-main/internal/bridge/observe/snapshot.go

// DefaultSummarizeThreshold is the spec-mandated character threshold
// above which snapshot content is summarized (spec L4278: >8000
// chars).
const DefaultSummarizeThreshold = 8000

// DefaultMaxTokens caps the approximate token budget of a summary.
// We approximate 1 token ≈ 4 characters for the simple keyword-based
// summarizer (spec L4278).
const DefaultMaxTokens = 2000

// DefaultFallbackTruncateLen is the character length used by the
// fallback truncation path when no task-relevant sentences match
// (spec L4278).
const DefaultFallbackTruncateLen = 4000

// SummarizeConfig controls the summarizer's thresholds
// (spec L4278).
type SummarizeConfig struct {
	Threshold        int `json:"threshold"`
	MaxTokens        int `json:"max_tokens"`
	FallbackTruncate int `json:"fallback_truncate"`
}

// DefaultSummarizeConfig returns the spec-mandated default
// configuration with Threshold=8000 (spec L4278).
func DefaultSummarizeConfig() SummarizeConfig {
	return SummarizeConfig{
		Threshold:        DefaultSummarizeThreshold,
		MaxTokens:        DefaultMaxTokens,
		FallbackTruncate: DefaultFallbackTruncateLen,
	}
}

// SummarizationResult captures the outcome of a summarization call
// (spec L4278).
type SummarizationResult struct {
	Original         string `json:"original"`
	Summarized       string `json:"summarized"`
	WasSummarized    bool   `json:"was_summarized"`
	OriginalLength   int    `json:"original_length"`
	SummarizedLength int    `json:"summarized_length"`
	Task             string `json:"task"`
}

// SummarizerStats tracks cumulative summarizer activity
// (spec L4278: stats Total/Summarized/Truncated).
type SummarizerStats struct {
	Total      int `json:"total"`
	Summarized int `json:"summarized"`
	Truncated  int `json:"truncated"`
}

// Summarizer performs task-aware summarization of snapshot content
// (spec L4278). It is safe for concurrent use.
type Summarizer struct {
	mu    sync.RWMutex
	cfg   SummarizeConfig
	stats SummarizerStats
}

// NewSummarizer returns a Summarizer with the given configuration.
// Zero-value fields in cfg are replaced with defaults.
func NewSummarizer(cfg SummarizeConfig) *Summarizer {
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultSummarizeThreshold
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	if cfg.FallbackTruncate <= 0 {
		cfg.FallbackTruncate = DefaultFallbackTruncateLen
	}
	return &Summarizer{cfg: cfg}
}

// NewDefaultSummarizer returns a Summarizer with the default
// configuration (spec L4278).
func NewDefaultSummarizer() *Summarizer {
	return NewSummarizer(DefaultSummarizeConfig())
}

// Config returns a copy of the current configuration.
func (s *Summarizer) Config() SummarizeConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// SetConfig updates the summarizer configuration. Zero-value fields
// are replaced with defaults.
func (s *Summarizer) SetConfig(cfg SummarizeConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultSummarizeThreshold
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	if cfg.FallbackTruncate <= 0 {
		cfg.FallbackTruncate = DefaultFallbackTruncateLen
	}
	s.cfg = cfg
}

// Stats returns a snapshot of the cumulative stats
// (spec L4278: Total/Summarized/Truncated).
func (s *Summarizer) Stats() SummarizerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// NeedsSummarization reports whether the content length exceeds the
// configured threshold (spec L4278).
func (s *Summarizer) NeedsSummarization(content string) bool {
	s.mu.RLock()
	threshold := s.cfg.Threshold
	s.mu.RUnlock()
	return len(content) > threshold
}

// Summarize returns a task-aware summary of the content
// (spec L4278). If the content is below the threshold it is returned
// unchanged with wasSummarized=false. Otherwise the summarizer
// extracts task-relevant sentences; if none match it falls back to
// truncation. The second return value reports whether summarization
// occurred.
func (s *Summarizer) Summarize(content string, task string) (string, bool) {
	s.mu.Lock()
	threshold := s.cfg.Threshold
	fallbackLen := s.cfg.FallbackTruncate
	maxChars := s.cfg.MaxTokens * 4
	s.mu.Unlock()

	s.mu.Lock()
	s.stats.Total++
	s.mu.Unlock()

	if len(content) <= threshold {
		return content, false
	}

	relevant := s.ExtractRelevant(content, task)
	if relevant != "" {
		// Cap to the token budget.
		if maxChars > 0 && len(relevant) > maxChars {
			relevant = TruncateContent(relevant, maxChars)
		}
		s.mu.Lock()
		s.stats.Summarized++
		s.mu.Unlock()
		return relevant, true
	}

	// Fallback: truncate the original content.
	truncated := TruncateContent(content, fallbackLen)
	s.mu.Lock()
	s.stats.Truncated++
	s.stats.Summarized++
	s.mu.Unlock()
	return truncated, true
}

// SummarizeResult is the same as Summarize but returns a full
// SummarizationResult with metadata (spec L4278).
func (s *Summarizer) SummarizeResult(content string, task string) SummarizationResult {
	out, wasSummarized := s.Summarize(content, task)
	return SummarizationResult{
		Original:         content,
		Summarized:       out,
		WasSummarized:    wasSummarized,
		OriginalLength:   len(content),
		SummarizedLength: len(out),
		Task:             task,
	}
}

// TruncateContent truncates content to maxLen characters and appends
// "..." if truncation occurred (spec L4278: fallback truncation).
// If maxLen <= 0 the content is returned unchanged. If the content
// is already shorter than or equal to maxLen it is returned as-is
// without a suffix.
func TruncateContent(content string, maxLen int) string {
	if maxLen <= 0 {
		return content
	}
	if len(content) <= maxLen {
		return content
	}
	const suffix = "..."
	if maxLen <= len(suffix) {
		return content[:maxLen]
	}
	return content[:maxLen-len(suffix)] + suffix
}

// ExtractRelevant returns the sentences from content that contain at
// least one task keyword (case-insensitive). Sentences are joined
// with single spaces. If no sentences match, an empty string is
// returned so the caller can fall back to truncation
// (spec L4278: simple keyword matching).
func (s *Summarizer) ExtractRelevant(content string, task string) string {
	keywords := extractKeywords(task)
	if len(keywords) == 0 {
		return ""
	}
	sentences := splitSentences(content)
	var matched []string
	for _, sentence := range sentences {
		lower := strings.ToLower(sentence)
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				matched = append(matched, strings.TrimSpace(sentence))
				break
			}
		}
	}
	return strings.Join(matched, " ")
}

// extractKeywords splits a task description into lowercase keywords,
// dropping punctuation and stopwords. Returns nil when the task is
// empty or contains no usable keywords.
func extractKeywords(task string) []string {
	task = strings.ToLower(strings.TrimSpace(task))
	if task == "" {
		return nil
	}
	fields := strings.FieldsFunc(task, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_'
	})
	stopwords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"of": true, "to": true, "in": true, "on": true, "for": true,
		"with": true, "is": true, "are": true, "be": true, "by": true,
		"this": true, "that": true, "it": true, "at": true, "as": true,
		"from": true, "into": true, "page": true, "element": true,
		"click": true, "find": true, "get": true, "do": true,
	}
	var out []string
	seen := make(map[string]bool)
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		if stopwords[f] {
			continue
		}
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// splitSentences splits content into sentences on . ! ? followed by
// whitespace, preserving the sentence text. Newlines are treated as
// sentence boundaries as well so snapshot dumps split sensibly.
func splitSentences(content string) []string {
	if content == "" {
		return nil
	}
	var sentences []string
	var current strings.Builder
	runes := []rune(content)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		current.WriteRune(r)
		if r == '\n' {
			// Newlines are always sentence boundaries.
			if strings.TrimSpace(current.String()) != "" {
				sentences = append(sentences, current.String())
			}
			current.Reset()
			continue
		}
		if r == '.' || r == '!' || r == '?' {
			// Boundary if followed by whitespace or end of content.
			if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) {
				if strings.TrimSpace(current.String()) != "" {
					sentences = append(sentences, current.String())
				}
				current.Reset()
			}
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		sentences = append(sentences, current.String())
	}
	return sentences
}
