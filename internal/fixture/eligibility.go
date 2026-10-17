package fixture

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type EligibilityState string

const (
	EligibilityReady       EligibilityState = "eligible"
	EligibilityQuarantined EligibilityState = "quarantined"
	EligibilityExpired     EligibilityState = "expired"
)

// Eligibility describes permission to attempt a case, never passing coverage.
// All matching restrictions are retained, including expired ownership records.
type Eligibility struct {
	Runner       string           `json:"runner"`
	ScenarioID   string           `json:"scenario_id"`
	State        EligibilityState `json:"state"`
	Restrictions []Quarantine     `json:"restrictions"`
}

func AssessEligibility(runner string, scenario Scenario, now time.Time) (Eligibility, error) {
	return assessEligibility(runner, scenario, now, DefaultQuarantines())
}

func assessEligibility(runner string, scenario Scenario, now time.Time, quarantines []Quarantine) (Eligibility, error) {
	if !knownFixtureRunner(runner) || scenario.ID == "" || len(scenario.ID) > 256 || !utf8.ValidString(scenario.ID) || strings.ContainsAny(scenario.ID, "\x00\r\n") || now.IsZero() {
		return Eligibility{}, fmt.Errorf("fixture: known runner, bounded scenario identity and reference time required")
	}
	if err := validateQuarantines(quarantines); err != nil {
		return Eligibility{}, err
	}
	result := Eligibility{Runner: runner, ScenarioID: scenario.ID, State: EligibilityReady, Restrictions: []Quarantine{}}
	for _, quarantine := range quarantines {
		if quarantine.Runner != runner || !quarantine.Match(scenario) {
			continue
		}
		result.Restrictions = append(result.Restrictions, quarantine)
		if !now.Before(quarantine.Expires) {
			result.State = EligibilityExpired
		} else if result.State != EligibilityExpired {
			result.State = EligibilityQuarantined
		}
	}
	return result, nil
}

func validateQuarantines(quarantines []Quarantine) error {
	if len(quarantines) > 128 {
		return fmt.Errorf("fixture: quarantine entry limit exceeded")
	}
	seen := make(map[string]map[string]bool)
	for _, quarantine := range quarantines {
		if !knownFixtureRunner(quarantine.Runner) || quarantine.Match == nil || quarantine.Expires.IsZero() || quarantine.Owner == "" || len(quarantine.Owner) > 256 || len(quarantine.Reason) == 0 || len(quarantine.Reason) > 4096 || !utf8.ValidString(quarantine.Owner) || !utf8.ValidString(quarantine.Reason) {
			return fmt.Errorf("fixture: incomplete or oversized quarantine ownership contract")
		}
		if strings.TrimSpace(quarantine.Owner) != quarantine.Owner || strings.TrimSpace(quarantine.Reason) == "" || strings.ContainsAny(quarantine.Owner, "\x00\r\n") || strings.ContainsAny(quarantine.Reason, "\x00\r\n") {
			return fmt.Errorf("fixture: invalid quarantine owner or reason text")
		}
		if seen[quarantine.Runner] == nil {
			seen[quarantine.Runner] = make(map[string]bool)
		}
		if seen[quarantine.Runner][quarantine.Reason] {
			return fmt.Errorf("fixture: duplicate quarantine contract")
		}
		seen[quarantine.Runner][quarantine.Reason] = true
	}
	return nil
}

func knownFixtureRunner(runner string) bool {
	switch runner {
	case "renderless", "chromium", "hybrid", "serve", "agent", "artemis":
		return true
	default:
		return false
	}
}
