package bridge

import (
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

// RecoveryActionType is the action an agent should take for a matched error
// pattern (spec L4285). Named RecoveryActionType to avoid colliding with the
// RecoveryAction interface in health.go.
type RecoveryActionType string

const (
	RecoveryRetry     RecoveryActionType = "retry"
	RecoveryRestart   RecoveryActionType = "restart"
	RecoveryReconnect RecoveryActionType = "reconnect"
	RecoverySkip      RecoveryActionType = "skip"
	RecoveryFail      RecoveryActionType = "fail"
)

// ErrorPattern pairs a matcher with a recovery action and retry budget.
type ErrorPattern struct {
	Name       string
	Match      string
	Action     RecoveryActionType
	MaxRetries int
	compiled   *regexp.Regexp
}

// RecoveryStats tracks recovery decisions over the lifetime of an
// ErrorRecovery instance.
type RecoveryStats struct {
	Total       atomic.Int64
	Retried     atomic.Int64
	Restarted   atomic.Int64
	Reconnected atomic.Int64
	Skipped     atomic.Int64
	Failed      atomic.Int64
}

// ErrorRecovery diagnoses raw error strings and maps them to recovery
// actions using a configurable pattern table.
type ErrorRecovery struct {
	mu       sync.RWMutex
	patterns []ErrorPattern
	stats    RecoveryStats
}

// NewErrorRecovery builds an ErrorRecovery with the supplied patterns.
func NewErrorRecovery(patterns []ErrorPattern) *ErrorRecovery {
	r := &ErrorRecovery{patterns: make([]ErrorPattern, len(patterns))}
	copy(r.patterns, patterns)
	r.compilePatterns()
	return r
}

// NewDefaultErrorRecovery builds an ErrorRecovery with DefaultErrorPatterns.
func NewDefaultErrorRecovery() *ErrorRecovery {
	return NewErrorRecovery(DefaultErrorPatterns())
}

func (r *ErrorRecovery) compilePatterns() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.patterns {
		p := &r.patterns[i]
		if p.Match == "" {
			continue
		}
		if re, err := regexp.Compile(p.Match); err == nil {
			p.compiled = re
		}
	}
}

// DefaultErrorPatterns returns the canonical recovery pattern table:
//   - empty stdout with rc=0 -> stale daemon (restart)
//   - empty snapshot -> CDP issue (reconnect)
//   - screenshot returns non-JSON -> regex recovery (retry)
func DefaultErrorPatterns() []ErrorPattern {
	return []ErrorPattern{
		{
			Name:       "stale-daemon",
			Match:      `empty stdout.*rc=0|rc=0.*empty stdout|process exited.*empty output`,
			Action:     RecoveryRestart,
			MaxRetries: 1,
		},
		{
			Name:       "cdp-empty-snapshot",
			Match:      `empty snapshot|snapshot.*empty|CDDP snapshot|CDP.*snapshot.*empty`,
			Action:     RecoveryReconnect,
			MaxRetries: 2,
		},
		{
			Name:       "screenshot-non-json",
			Match:      `screenshot.*non-JSON|non-JSON.*screenshot|invalid character.*screenshot|screenshot.*not JSON`,
			Action:     RecoveryRetry,
			MaxRetries: 3,
		},
		{
			Name:       "element-not-found",
			Match:      `not found|no node|element.*missing`,
			Action:     RecoveryRetry,
			MaxRetries: 2,
		},
		{
			Name:       "navigation-timeout",
			Match:      `navigation.*timeout|timeout.*navigation`,
			Action:     RecoveryRetry,
			MaxRetries: 3,
		},
		{
			Name:       "crashed",
			Match:      `target closed|session closed|browser crashed`,
			Action:     RecoveryRestart,
			MaxRetries: 1,
		},
	}
}

// MatchPattern returns the first pattern whose matcher accepts the error
// string, or nil if none match.
func (r *ErrorRecovery) MatchPattern(errorMsg string) *ErrorPattern {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := range r.patterns {
		p := &r.patterns[i]
		if p.compiled != nil && p.compiled.MatchString(errorMsg) {
			return p
		}
		if p.compiled == nil && p.Match != "" && strings.Contains(strings.ToLower(errorMsg), strings.ToLower(p.Match)) {
			return p
		}
	}
	return nil
}

// Diagnose maps an error string to a recovery action, updating stats. The
// context string provides additional signal (e.g. which command produced
// the error). Returns RecoveryFail when no pattern matches.
func (r *ErrorRecovery) Diagnose(errorMsg, context string) RecoveryActionType {
	r.stats.Total.Add(1)
	combined := errorMsg
	if context != "" {
		combined = errorMsg + " " + context
	}
	p := r.MatchPattern(combined)
	if p == nil {
		r.stats.Failed.Add(1)
		return RecoveryFail
	}
	switch p.Action {
	case RecoveryRetry:
		r.stats.Retried.Add(1)
	case RecoveryRestart:
		r.stats.Restarted.Add(1)
	case RecoveryReconnect:
		r.stats.Reconnected.Add(1)
	case RecoverySkip:
		r.stats.Skipped.Add(1)
	case RecoveryFail:
		r.stats.Failed.Add(1)
	}
	return p.Action
}

// ShouldRetry reports whether the caller should retry given the matched
// pattern and the current attempt number (1-based). Attempts beyond
// MaxRetries return false.
func (r *ErrorRecovery) ShouldRetry(pattern *ErrorPattern, attempt int) bool {
	if pattern == nil {
		return false
	}
	if pattern.Action != RecoveryRetry && pattern.Action != RecoveryReconnect && pattern.Action != RecoveryRestart {
		return false
	}
	if attempt >= pattern.MaxRetries {
		return false
	}
	return true
}

// Stats returns a snapshot of the recovery counters.
func (r *ErrorRecovery) Stats() RecoveryStats {
	return RecoveryStats{
		Total:       atomic.Int64{},
		Retried:     atomic.Int64{},
		Restarted:   atomic.Int64{},
		Reconnected: atomic.Int64{},
		Skipped:     atomic.Int64{},
		Failed:      atomic.Int64{},
	}
}

// StatsSnapshot returns the current stat values as a plain struct.
func (r *ErrorRecovery) StatsSnapshot() (total, retried, restarted, reconnected, skipped, failed int64) {
	return r.stats.Total.Load(),
		r.stats.Retried.Load(),
		r.stats.Restarted.Load(),
		r.stats.Reconnected.Load(),
		r.stats.Skipped.Load(),
		r.stats.Failed.Load()
}

// AddPattern appends a new pattern and recompiles it.
func (r *ErrorRecovery) AddPattern(p ErrorPattern) {
	r.mu.Lock()
	r.patterns = append(r.patterns, p)
	r.mu.Unlock()
	r.compilePatterns()
}

// Patterns returns a copy of the registered patterns.
func (r *ErrorRecovery) Patterns() []ErrorPattern {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ErrorPattern, len(r.patterns))
	copy(out, r.patterns)
	return out
}
