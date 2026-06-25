package bridge

import (
	"strings"
	"testing"
)

func TestDefaultErrorPatterns(t *testing.T) {
	patterns := DefaultErrorPatterns()
	if len(patterns) < 3 {
		t.Fatalf("patterns=%d", len(patterns))
	}
	names := map[string]bool{}
	for _, p := range patterns {
		if names[p.Name] {
			t.Fatalf("duplicate pattern %s", p.Name)
		}
		names[p.Name] = true
	}
	if !names["stale-daemon"] || !names["cdp-empty-snapshot"] || !names["screenshot-non-json"] {
		t.Fatalf("missing canonical patterns: %v", names)
	}
}

func TestDiagnoseStaleDaemon(t *testing.T) {
	r := NewDefaultErrorRecovery()
	action := r.Diagnose("empty stdout rc=0", "screenshot")
	if action != RecoveryRestart {
		t.Fatalf("action=%s want restart", action)
	}
	_, _, restarted, _, _, _ := r.StatsSnapshot()
	if restarted != 1 {
		t.Fatalf("restarted=%d", restarted)
	}
}

func TestDiagnoseCDPIssue(t *testing.T) {
	r := NewDefaultErrorRecovery()
	action := r.Diagnose("empty snapshot returned from CDP", "snapshot")
	if action != RecoveryReconnect {
		t.Fatalf("action=%s want reconnect", action)
	}
	_, _, _, reconnected, _, _ := r.StatsSnapshot()
	if reconnected != 1 {
		t.Fatalf("reconnected=%d", reconnected)
	}
}

func TestDiagnoseScreenshotNonJSON(t *testing.T) {
	r := NewDefaultErrorRecovery()
	action := r.Diagnose("screenshot returned non-JSON output", "screenshot")
	if action != RecoveryRetry {
		t.Fatalf("action=%s want retry", action)
	}
	_, retried, _, _, _, _ := r.StatsSnapshot()
	if retried != 1 {
		t.Fatalf("retried=%d", retried)
	}
}

func TestDiagnoseUnmatchedFails(t *testing.T) {
	r := NewDefaultErrorRecovery()
	action := r.Diagnose("some totally unknown error", "ctx")
	if action != RecoveryFail {
		t.Fatalf("action=%s want fail", action)
	}
	_, _, _, _, _, failed := r.StatsSnapshot()
	if failed != 1 {
		t.Fatalf("failed=%d", failed)
	}
}

func TestDiagnoseTotalCounter(t *testing.T) {
	r := NewDefaultErrorRecovery()
	r.Diagnose("empty stdout rc=0", "")
	r.Diagnose("unknown error", "")
	total, _, _, _, _, _ := r.StatsSnapshot()
	if total != 2 {
		t.Fatalf("total=%d", total)
	}
}

func TestMatchPatternFound(t *testing.T) {
	r := NewDefaultErrorRecovery()
	p := r.MatchPattern("empty stdout rc=0")
	if p == nil || p.Name != "stale-daemon" {
		t.Fatalf("pattern=%+v", p)
	}
}

func TestMatchPatternNotFound(t *testing.T) {
	r := NewDefaultErrorRecovery()
	if p := r.MatchPattern("nothing matches here xyzzy"); p != nil {
		t.Fatalf("expected nil, got %+v", p)
	}
}

func TestShouldRetryUnderLimit(t *testing.T) {
	r := NewDefaultErrorRecovery()
	p := r.MatchPattern("screenshot returned non-JSON output")
	if p == nil {
		t.Fatal("pattern not found")
	}
	if !r.ShouldRetry(p, 1) {
		t.Fatal("attempt 1 should retry")
	}
	if !r.ShouldRetry(p, 2) {
		t.Fatal("attempt 2 should retry")
	}
}

func TestShouldRetryAtLimit(t *testing.T) {
	r := NewDefaultErrorRecovery()
	p := r.MatchPattern("screenshot returned non-JSON output")
	if r.ShouldRetry(p, 3) {
		t.Fatal("attempt 3 should not retry")
	}
}

func TestShouldRetryNilPattern(t *testing.T) {
	r := NewDefaultErrorRecovery()
	if r.ShouldRetry(nil, 1) {
		t.Fatal("nil pattern should not retry")
	}
}

func TestShouldRetrySkipAction(t *testing.T) {
	r := NewErrorRecovery([]ErrorPattern{
		{Name: "skip-me", Match: "skip this", Action: RecoverySkip, MaxRetries: 5},
	})
	p := r.MatchPattern("skip this")
	if p == nil {
		t.Fatal("pattern not found")
	}
	if r.ShouldRetry(p, 1) {
		t.Fatal("skip action should not retry")
	}
}

func TestShouldRetryFailAction(t *testing.T) {
	r := NewErrorRecovery([]ErrorPattern{
		{Name: "fail-me", Match: "fail this", Action: RecoveryFail, MaxRetries: 5},
	})
	p := r.MatchPattern("fail this")
	if r.ShouldRetry(p, 1) {
		t.Fatal("fail action should not retry")
	}
}

func TestAddPattern(t *testing.T) {
	r := NewDefaultErrorRecovery()
	r.AddPattern(ErrorPattern{Name: "custom", Match: "custom error [a-z]+", Action: RecoveryRetry, MaxRetries: 1})
	p := r.MatchPattern("custom error abc")
	if p == nil || p.Name != "custom" {
		t.Fatalf("pattern=%+v", p)
	}
}

func TestPatternsReturnsCopy(t *testing.T) {
	r := NewDefaultErrorRecovery()
	ps := r.Patterns()
	ps[0].Name = "mutated"
	if r.Patterns()[0].Name == "mutated" {
		t.Fatal("Patterns() should return a copy")
	}
}

func TestDiagnoseCrashed(t *testing.T) {
	r := NewDefaultErrorRecovery()
	action := r.Diagnose("target closed: browser crashed", "")
	if action != RecoveryRestart {
		t.Fatalf("action=%s want restart", action)
	}
}

func TestDiagnoseNavigationTimeout(t *testing.T) {
	r := NewDefaultErrorRecovery()
	action := r.Diagnose("navigation timeout exceeded", "navigate")
	if action != RecoveryRetry {
		t.Fatalf("action=%s want retry", action)
	}
}

func TestDiagnoseContextEnrichesMatch(t *testing.T) {
	r := NewDefaultErrorRecovery()
	// Error string alone doesn't match, but context adds the snapshot signal.
	action := r.Diagnose("weird output", "empty snapshot from CDP")
	if action != RecoveryReconnect {
		t.Fatalf("action=%s want reconnect", action)
	}
}

func TestRecoveryStatsAllCounters(t *testing.T) {
	r := NewDefaultErrorRecovery()
	r.Diagnose("screenshot returned non-JSON output", "") // retry
	r.Diagnose("empty stdout rc=0", "")                   // restart
	r.Diagnose("empty snapshot from CDP", "")             // reconnect
	r.Diagnose("unknown error", "")                       // fail
	total, retried, restarted, reconnected, skipped, failed := r.StatsSnapshot()
	if total != 4 {
		t.Fatalf("total=%d", total)
	}
	if retried != 1 || restarted != 1 || reconnected != 1 || failed != 1 || skipped != 0 {
		t.Fatalf("retried=%d restarted=%d reconnected=%d skipped=%d failed=%d",
			retried, restarted, reconnected, skipped, failed)
	}
}

func TestRecoveryActionTypeStringValues(t *testing.T) {
	want := map[RecoveryActionType]bool{
		RecoveryRetry:     true,
		RecoveryRestart:   true,
		RecoveryReconnect: true,
		RecoverySkip:      true,
		RecoveryFail:      true,
	}
	for a := range want {
		if string(a) == "" {
			t.Fatalf("empty action")
		}
	}
}

func TestMatchPatternCaseInsensitiveFallback(t *testing.T) {
	r := NewErrorRecovery([]ErrorPattern{
		{Name: "literal", Match: "TimeoutError", Action: RecoveryRetry, MaxRetries: 1},
	})
	// Regex compilation succeeds, so this tests regex path. Add a pattern
	// with invalid regex to exercise the substring fallback.
	r.AddPattern(ErrorPattern{Name: "bad-regex", Match: "[", Action: RecoverySkip, MaxRetries: 1})
	p := r.MatchPattern("array[0] bad-regex here")
	if p == nil {
		t.Fatal("substring fallback should match")
	}
	if !strings.Contains(p.Name, "bad-regex") {
		t.Fatalf("pattern=%+v", p)
	}
}
