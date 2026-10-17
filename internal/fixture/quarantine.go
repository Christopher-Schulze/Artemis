package fixture

import "time"

// Quarantine records a known, time-bounded skip for a runner/scenario pair.
// A quarantine is not an excuse to ignore a failure: it is a tracked, owned,
// expiring contract that prevents skipped scenarios from disappearing into an
// indefinite skip list.
type Quarantine struct {
	Runner  string              `json:"runner"`
	Reason  string              `json:"reason"`
	Owner   string              `json:"owner"`
	Expires time.Time           `json:"expires"`
	Match   func(Scenario) bool `json:"-"`
}

// quarantineExpiryStr retains the deadline of the retired capability contracts.
const quarantineExpiryStr = "2026-09-30T00:00:00Z"

// QuarantineExpiry returns the historical capability-contract deadline.
func QuarantineExpiry() time.Time {
	return time.Date(2026, time.September, 30, 0, 0, 0, 0, time.UTC)
}

// DefaultQuarantines returns the current declared capability restrictions.
// Retired restrictions do not mask observed runtime or semantic failures.
func DefaultQuarantines() []Quarantine {
	return []Quarantine{}
}

// Check describes whether a known runner may attempt the scenario under the
// declared restrictions. It does not prove actual capability or passing coverage.
func Check(runner string, sc Scenario) (bool, string) {
	eligibility, err := AssessEligibility(runner, sc, time.Now())
	if err != nil {
		return false, err.Error()
	}
	if eligibility.State == EligibilityReady {
		return true, ""
	}
	return false, eligibility.Restrictions[0].Reason
}

// IsQuarantined reports whether a runner's skip reason is covered by a
// non-expired quarantine entry. It returns the matching quarantine and true
// when the skip is valid.
func IsQuarantined(runner string, sc Scenario, reason string) (Quarantine, bool) {
	return isQuarantined(runner, sc, reason, time.Now(), DefaultQuarantines())
}

func isQuarantined(runner string, sc Scenario, reason string, now time.Time, list []Quarantine) (Quarantine, bool) {
	eligibility, err := assessEligibility(runner, sc, now, list)
	if err != nil {
		return Quarantine{}, false
	}
	for _, q := range eligibility.Restrictions {
		if q.Runner != runner || q.Reason != reason {
			continue
		}
		if !now.Before(q.Expires) {
			continue
		}
		return q, true
	}
	return Quarantine{}, false
}

// IsQuarantinedAtTime is the same as IsQuarantined but uses an explicit
// reference time. It is exported to allow tests and tooling to evaluate
// quarantine state at a chosen date.
func IsQuarantinedAtTime(runner string, sc Scenario, reason string, now time.Time) (Quarantine, bool) {
	return isQuarantined(runner, sc, reason, now, DefaultQuarantines())
}
