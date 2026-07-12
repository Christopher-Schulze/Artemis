package security

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// SecretScrubber holds short-lived exact secret values and replaces them in
// diagnostics without exposing the values through its API. It is intended for
// credential, MFA, trace and model-output boundaries.
type SecretScrubber struct {
	mu          sync.RWMutex
	values      [][]byte
	replacement string
	closed      bool
}

func NewSecretScrubber(replacement string) *SecretScrubber {
	if replacement == "" {
		replacement = "[REDACTED]"
	}
	return &SecretScrubber{replacement: replacement}
}

// Add registers a copy of a non-empty secret. Callers must Close the scrubber
// as soon as the credentialed operation ends.
func (s *SecretScrubber) Add(secret []byte) error {
	if s == nil {
		return errors.New("secret scrubber: nil scrubber")
	}
	if len(secret) == 0 {
		return errors.New("secret scrubber: empty secret")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("secret scrubber: closed")
	}
	s.values = append(s.values, append([]byte(nil), secret...))
	return nil
}

// Redact replaces exact registered secrets and never returns the original
// value separately.
func (s *SecretScrubber) Redact(content string) string {
	if s == nil || content == "" {
		return content
	}
	s.mu.RLock()
	values := make([]string, 0, len(s.values))
	for _, value := range s.values {
		values = append(values, string(value))
	}
	replacement := s.replacement
	s.mu.RUnlock()
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	redacted := content
	for _, value := range values {
		if value != "" {
			redacted = strings.ReplaceAll(redacted, value, replacement)
		}
	}
	return redacted
}

func (s *SecretScrubber) Contains(content string) bool {
	return s != nil && s.Redact(content) != content
}

// Close wipes all registered secret bytes and makes the scrubber unusable.
func (s *SecretScrubber) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, value := range s.values {
		for i := range value {
			value[i] = 0
		}
	}
	s.values = nil
	s.closed = true
}
