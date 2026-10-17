package fixture

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEligibilityRetainsEveryRestrictionAndExactExpiryBoundary(t *testing.T) {
	if QuarantineExpiry().Format(time.RFC3339) != quarantineExpiryStr {
		t.Fatal("declared deadline changed")
	}
	scenario := Scenario{ID: "all-restrictions", AsyncFetch: true, WaitForIdle: true, Status: 403, Expect: Expect{Eval: "1+1", EvalContains: "2"}}
	// Controlled contracts retain overlap proof when real capabilities retire
	// production restrictions. Assessment and exact boundary checks stay real.
	contracts := []Quarantine{
		{Runner: "agent", Reason: "independent evaluation constraint", Owner: "devin", Expires: QuarantineExpiry(), Match: func(sc Scenario) bool { return sc.Expect.Eval != "" }},
		{Runner: "agent", Reason: "independent async constraint", Owner: "devin", Expires: QuarantineExpiry(), Match: func(sc Scenario) bool { return sc.AsyncFetch }},
	}
	for _, reference := range []struct {
		time  time.Time
		state EligibilityState
	}{{QuarantineExpiry().Add(-time.Nanosecond), EligibilityQuarantined}, {QuarantineExpiry(), EligibilityExpired}, {QuarantineExpiry().Add(time.Nanosecond), EligibilityExpired}} {
		eligibility, err := assessEligibility("agent", scenario, reference.time, contracts)
		if err != nil || eligibility.State != reference.state || len(eligibility.Restrictions) != 2 {
			t.Fatalf("lost capability restrictions: %+v %v", eligibility, err)
		}
		for _, restriction := range eligibility.Restrictions {
			if restriction.Owner != "devin" || restriction.Expires != QuarantineExpiry() {
				t.Fatal("restriction owner/deadline was lost or fabricated")
			}
		}
		data, err := json.Marshal(eligibility)
		if err != nil || !strings.Contains(string(data), `"owner":"devin"`) || !strings.Contains(string(data), quarantineExpiryStr) {
			t.Fatalf("structured ownership metadata missing: %s %v", data, err)
		}
	}
	ready, err := AssessEligibility("renderless", Scenario{ID: "attempt-only"}, QuarantineExpiry())
	if err != nil || ready.State != EligibilityReady || len(ready.Restrictions) != 0 {
		t.Fatalf("restriction-free attempt was denied: %+v %v", ready, err)
	}
}

func TestEligibilityRejectsUnownedMalformedOrDuplicateContracts(t *testing.T) {
	valid := Quarantine{Runner: "chromium", Reason: "actual unsupported capability", Owner: "actual-owner", Expires: QuarantineExpiry(), Match: func(scenario Scenario) bool { return scenario.AsyncFetch }}
	scenario := Scenario{ID: "eligibility", AsyncFetch: true}
	cases := []struct {
		name   string
		change func(*Quarantine)
	}{
		{"nil predicate", func(q *Quarantine) { q.Match = nil }},
		{"missing owner", func(q *Quarantine) { q.Owner = "" }},
		{"blank owner", func(q *Quarantine) { q.Owner = " " }},
		{"blank reason", func(q *Quarantine) { q.Reason = " " }},
		{"control owner", func(q *Quarantine) { q.Owner = "owner\n" }},
		{"missing expiry", func(q *Quarantine) { q.Expires = time.Time{} }},
		{"unknown runner", func(q *Quarantine) { q.Runner = "unknown" }},
		{"missing reason", func(q *Quarantine) { q.Reason = "" }},
		{"oversized owner", func(q *Quarantine) { q.Owner = strings.Repeat("x", 257) }},
		{"oversized reason", func(q *Quarantine) { q.Reason = strings.Repeat("x", 4097) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			invalid := valid
			test.change(&invalid)
			if state, err := assessEligibility("chromium", scenario, QuarantineExpiry(), []Quarantine{invalid}); err == nil || state.State != "" {
				t.Fatalf("invalid ownership contract admitted: %+v %v", state, err)
			}
			if _, ok := isQuarantined("chromium", scenario, valid.Reason, QuarantineExpiry().Add(-time.Second), []Quarantine{invalid}); ok {
				t.Fatal("invalid contract granted quarantine permission")
			}
		})
	}
	if _, err := assessEligibility("chromium", scenario, QuarantineExpiry(), []Quarantine{valid, valid}); err == nil {
		t.Fatal("duplicate contracts accepted")
	}
	if _, err := assessEligibility("chromium", scenario, QuarantineExpiry(), make([]Quarantine, 129)); err == nil {
		t.Fatal("excess ownership table accepted")
	}
	for _, name := range []string{"", "unknown"} {
		if _, err := AssessEligibility(name, scenario, QuarantineExpiry()); err == nil {
			t.Fatal("unregistered runner was admitted")
		}
	}
	if _, err := AssessEligibility("chromium", scenario, time.Time{}); err == nil {
		t.Fatal("unknown reference time was accepted")
	}
}
