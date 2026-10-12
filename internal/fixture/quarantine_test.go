package fixture

import (
	"testing"
	"time"
)

func TestQuarantineCheckKnownSkips(t *testing.T) {
	sc := Scenario{ID: "spa-001", AsyncFetch: true}
	ok, reason := Check("chromium", sc)
	if ok {
		t.Fatalf("chromium should be quarantined for AsyncFetch")
	}
	if reason != "bridge does not support AsyncFetch interception" {
		t.Errorf("reason = %q, want bridge does not support AsyncFetch interception", reason)
	}

	ok, reason = Check("hybrid", Scenario{ID: "js-001", Expect: Expect{Eval: "1+1"}})
	if ok || reason != "router does not support eval" {
		t.Errorf("hybrid eval: ok=%v reason=%q", ok, reason)
	}

	ok, reason = Check("artemis", Scenario{ID: "redirect-001", Expect: Expect{Status: 302}})
	if ok || reason != "BrowserRuntime cannot verify non-200 HTTP status" {
		t.Errorf("artemis non-200: ok=%v reason=%q", ok, reason)
	}
}

func TestQuarantineCheckSupported(t *testing.T) {
	sc := Scenario{ID: "html-001"}
	ok, reason := Check("renderless", sc)
	if !ok {
		t.Errorf("renderless should run html-001: %q", reason)
	}
	ok, reason = Check("chromium", sc)
	if !ok {
		t.Errorf("chromium should run html-001: %q", reason)
	}
}

func TestQuarantineIsQuarantined(t *testing.T) {
	sc := Scenario{ID: "spa-001", AsyncFetch: true}
	q, ok := IsQuarantined("chromium", sc, "bridge does not support AsyncFetch interception")
	if !ok {
		t.Fatal("expected quarantine")
	}
	if q.Owner == "" {
		t.Error("Owner is empty")
	}
	if q.Expires.IsZero() {
		t.Error("Expires is zero")
	}
}

func TestQuarantineNotQuarantined(t *testing.T) {
	sc := Scenario{ID: "spa-001", AsyncFetch: true}
	_, ok := IsQuarantined("chromium", sc, "no such reason")
	if ok {
		t.Error("expected no quarantine for wrong reason")
	}
}

func TestQuarantineExpired(t *testing.T) {
	sc := Scenario{ID: "spa-001", AsyncFetch: true}
	past := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	_, ok := IsQuarantinedAtTime("chromium", sc, "bridge does not support AsyncFetch interception", past)
	if ok {
		t.Error("expected expired quarantine")
	}
}

func TestQuarantineNotExpiredAtDefault(t *testing.T) {
	sc := Scenario{ID: "redirect-001", Expect: Expect{Status: 302}}
	now := time.Now()
	_, ok := IsQuarantinedAtTime("chromium", sc, "bridge cannot verify non-200 HTTP status", now)
	if !ok {
		t.Error("expected active quarantine at current time")
	}
}

func TestQuarantineEntriesComplete(t *testing.T) {
	for _, q := range DefaultQuarantines() {
		if q.Runner == "" {
			t.Error("empty Runner")
		}
		if q.Reason == "" {
			t.Error("empty Reason")
		}
		if q.Owner == "" {
			t.Error("empty Owner")
		}
		if q.Expires.IsZero() {
			t.Error("zero Expires")
		}
		if q.Match == nil {
			t.Error("nil Match")
		}
	}
}

func TestQuarantineNoSkipEnforcement(t *testing.T) {
	// A scenario that is skipped by a runner but has no matching quarantine
	// is a no-skip violation. Simulate a custom runner by calling Check with a
	// fake runner and then verifying IsQuarantined is false.
	sc := Scenario{ID: "unsupported", AsyncFetch: true}
	ok, reason := Check("fake-runner", sc)
	if !ok {
		t.Fatalf("fake-runner should not be in the quarantine list, got reason %q", reason)
	}
	_, qok := IsQuarantined("fake-runner", sc, "any reason")
	if qok {
		t.Error("fake-runner should not be quarantined")
	}
}
