package fixture

import (
	"testing"
	"time"
)

func TestQuarantineCheckKnownSkips(t *testing.T) {
	sc := Scenario{ID: "spa-001", AsyncFetch: true}
	ok, reason := Check("chromium", sc)
	if !ok || reason != "" {
		t.Fatalf("proven native async attempt was denied: %v %q", ok, reason)
	}

	ok, reason = Check("hybrid", Scenario{ID: "js-001", Expect: Expect{Eval: "1+1"}})
	if !ok || reason != "" {
		t.Errorf("proven hybrid eval attempt denied: ok=%v reason=%q", ok, reason)
	}
	ok, reason = Check("hybrid", sc)
	if !ok || reason != "" {
		t.Errorf("proven hybrid async attempt denied: ok=%v reason=%q", ok, reason)
	}
	ok, reason = Check("agent", sc)
	if !ok || reason != "" {
		t.Errorf("proven Agent async attempt denied: ok=%v reason=%q", ok, reason)
	}
	ok, reason = Check("agent", Scenario{ID: "js-001", Expect: Expect{Eval: "1+1"}})
	if !ok || reason != "" {
		t.Errorf("proven Agent eval attempt denied: ok=%v reason=%q", ok, reason)
	}
	ok, reason = Check("serve", sc)
	if !ok || reason != "" {
		t.Errorf("proven Serve async attempt denied: ok=%v reason=%q", ok, reason)
	}

	ok, reason = Check("artemis", Scenario{ID: "redirect-001", Expect: Expect{Status: 302}})
	if !ok || reason != "" {
		t.Errorf("native observed-status attempt was denied: ok=%v reason=%q", ok, reason)
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
	sc := Scenario{ID: "non-200", Status: 500}
	q, ok := isQuarantined("hybrid", sc, "router cannot verify non-200 HTTP status", QuarantineExpiry().Add(-time.Nanosecond), historicalStatusQuarantine())
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
	sc := Scenario{ID: "non-200", Status: 500}
	_, ok := isQuarantined("hybrid", sc, "no such reason", QuarantineExpiry().Add(-time.Nanosecond), historicalStatusQuarantine())
	if ok {
		t.Error("expected no quarantine for wrong reason")
	}
}

func TestQuarantineExpired(t *testing.T) {
	sc := Scenario{ID: "non-200", Status: 500}
	past := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	_, ok := isQuarantined("hybrid", sc, "router cannot verify non-200 HTTP status", past, historicalStatusQuarantine())
	if ok {
		t.Error("expected expired quarantine")
	}
}

func TestQuarantineValidBeforeExpiryAndInvalidAtBoundary(t *testing.T) {
	sc := Scenario{ID: "non-200", Status: 500}
	now := QuarantineExpiry().Add(-time.Nanosecond)
	_, ok := isQuarantined("hybrid", sc, "router cannot verify non-200 HTTP status", now, historicalStatusQuarantine())
	if !ok {
		t.Error("expected active quarantine strictly before its declared deadline")
	}
	if _, ok := isQuarantined("hybrid", sc, "router cannot verify non-200 HTTP status", QuarantineExpiry(), historicalStatusQuarantine()); ok {
		t.Fatal("quarantine remained valid at its exact expiry instant")
	}
}

func TestQuarantineEntriesComplete(t *testing.T) {
	contracts := historicalStatusQuarantine()
	if err := validateQuarantines(contracts); err != nil {
		t.Fatal(err)
	}
	for _, q := range contracts {
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

func historicalStatusQuarantine() []Quarantine {
	return []Quarantine{{Runner: "hybrid", Reason: "router cannot verify non-200 HTTP status", Owner: "devin", Expires: QuarantineExpiry(), Match: func(sc Scenario) bool { return sc.Status != 0 && sc.Status != 200 }}}
}

func TestProvenStatusCapabilityHasNoProductionQuarantine(t *testing.T) {
	if len(DefaultQuarantines()) != 0 {
		t.Fatal("retired capability restrictions still suppress real execution")
	}
	for _, status := range []int{400, 401, 403, 418, 429, 500, 503} {
		scenario := Scenario{ID: "actual-non-200", Status: status}
		if ok, reason := Check("hybrid", scenario); !ok || reason != "" {
			t.Fatalf("proven status %d denied before execution: %v %s", status, ok, reason)
		}
		if _, ok := IsQuarantinedAtTime("hybrid", scenario, "router cannot verify non-200 HTTP status", QuarantineExpiry().Add(-time.Nanosecond)); ok {
			t.Fatal("retired restriction was restored at a historical reference time")
		}
	}
}

func TestQuarantineNoSkipEnforcement(t *testing.T) {
	// An unknown runner cannot gain attempt permission through absence from
	// the restrictions table or claim an owned quarantine.
	sc := Scenario{ID: "unsupported", AsyncFetch: true}
	ok, reason := Check("fake-runner", sc)
	if ok || reason == "" {
		t.Fatalf("unknown runner was admitted without ownership: ok=%v reason=%q", ok, reason)
	}
	_, qok := IsQuarantined("fake-runner", sc, "any reason")
	if qok {
		t.Error("fake-runner should not be quarantined")
	}
}
