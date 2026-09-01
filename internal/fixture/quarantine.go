package fixture

import (
	"fmt"
	"time"
)

// Quarantine records a known, time-bounded skip for a runner/scenario pair.
// A quarantine is not an excuse to ignore a failure: it is a tracked, owned,
// expiring contract that prevents skipped scenarios from disappearing into an
// indefinite skip list.
type Quarantine struct {
	Runner  string
	Reason  string
	Owner   string
	Expires time.Time
	Match   func(Scenario) bool
}

// quarantineExpiry is the default deadline for currently quarantined skips.
// Quarantined behavior must be either fixed or re-approved before this date.
const quarantineExpiryStr = "2026-09-30T00:00:00Z"

// QuarantineExpiry returns the default quarantine expiration time.
func QuarantineExpiry() time.Time {
	// Parsing is safe because the string is a constant; on failure, the init
	// panic makes the failure immediate and loud.
	t, err := time.Parse(time.RFC3339, quarantineExpiryStr)
	if err != nil {
		panic(fmt.Sprintf("fixture: bad quarantineExpiryStr: %v", err))
	}
	return t
}

// DefaultQuarantines returns the canonical set of known, time-bounded skips
// for the cross-adapter fixture corpus. The list matches the adapter
// limitations encoded in integration_test.go runners.
func DefaultQuarantines() []Quarantine {
	exp := QuarantineExpiry()
	owner := "devin"
	return []Quarantine{
		{
			Runner:  "chromium",
			Reason:  "bridge does not support AsyncFetch interception",
			Owner:   owner,
			Expires: exp,
			Match:   func(sc Scenario) bool { return sc.AsyncFetch },
		},
		{
			Runner:  "chromium",
			Reason:  "bridge cannot verify non-200 HTTP status",
			Owner:   owner,
			Expires: exp,
			Match:   func(sc Scenario) bool { return sc.Expect.Status != 0 && sc.Expect.Status != 200 },
		},
		{
			Runner:  "hybrid",
			Reason:  "router does not support eval",
			Owner:   owner,
			Expires: exp,
			Match:   func(sc Scenario) bool { return sc.Expect.Eval != "" },
		},
		{
			Runner:  "hybrid",
			Reason:  "router does not support WaitForIdle or AsyncFetch",
			Owner:   owner,
			Expires: exp,
			Match:   func(sc Scenario) bool { return sc.WaitForIdle || sc.AsyncFetch },
		},
		{
			Runner:  "hybrid",
			Reason:  "router cannot verify non-200 HTTP status",
			Owner:   owner,
			Expires: exp,
			Match:   func(sc Scenario) bool { return sc.Status != 0 && sc.Status != 200 },
		},
		{
			Runner:  "serve",
			Reason:  "serve page.open does not enable AsyncFetch",
			Owner:   owner,
			Expires: exp,
			Match:   func(sc Scenario) bool { return sc.AsyncFetch },
		},
		{
			Runner:  "agent",
			Reason:  "Agent.ExecuteTask does not support Eval",
			Owner:   owner,
			Expires: exp,
			Match:   func(sc Scenario) bool { return sc.Expect.Eval != "" },
		},
		{
			Runner:  "agent",
			Reason:  "Agent.ExecuteTask does not support WaitForIdle or AsyncFetch",
			Owner:   owner,
			Expires: exp,
			Match:   func(sc Scenario) bool { return sc.WaitForIdle || sc.AsyncFetch },
		},
		{
			Runner:  "omnimus",
			Reason:  "BrowserRuntime does not support AsyncFetch interception",
			Owner:   owner,
			Expires: exp,
			Match:   func(sc Scenario) bool { return sc.AsyncFetch },
		},
		{
			Runner:  "omnimus",
			Reason:  "BrowserRuntime cannot verify non-200 HTTP status",
			Owner:   owner,
			Expires: exp,
			Match:   func(sc Scenario) bool { return sc.Expect.Status != 0 && sc.Expect.Status != 200 },
		},
	}
}

// Check returns whether a runner is capable of exercising the scenario, and
// if not, the reason from the quarantine list. If the scenario is not
// quarantined, the runner is considered supported.
func Check(runner string, sc Scenario) (bool, string) {
	for _, q := range DefaultQuarantines() {
		if q.Runner == runner && q.Match(sc) {
			return false, q.Reason
		}
	}
	return true, ""
}

// IsQuarantined reports whether a runner's skip reason is covered by a
// non-expired quarantine entry. It returns the matching quarantine and true
// when the skip is valid.
func IsQuarantined(runner string, sc Scenario, reason string) (Quarantine, bool) {
	return isQuarantined(runner, sc, reason, time.Now(), DefaultQuarantines())
}

func isQuarantined(runner string, sc Scenario, reason string, now time.Time, list []Quarantine) (Quarantine, bool) {
	for _, q := range list {
		if q.Runner != runner || q.Reason != reason {
			continue
		}
		if !q.Match(sc) {
			continue
		}
		if now.After(q.Expires) {
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
