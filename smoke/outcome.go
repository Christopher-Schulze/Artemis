package smoke

import "time"

// Outcome keeps product, observed network, harness and deliberate skips distinct.
type Outcome string

const (
	OutcomePass               Outcome = "pass"
	OutcomeProductFailure     Outcome = "product_failure"
	OutcomeNetworkFailure     Outcome = "network_failure"
	OutcomeNetworkSkip        Outcome = "network_skip"
	OutcomeMissingEnvironment Outcome = "missing_environment"
	OutcomeHarnessFailure     Outcome = "harness_failure"
	OutcomeInterrupted        Outcome = "interrupted"
)

func (r *ScenarioResult) finalize(elapsed time.Duration, interrupted error) {
	r.Duration = elapsed.Milliseconds()
	switch {
	case interrupted != nil:
		r.Pass, r.Skipped, r.SkipReason = false, false, ""
		r.Outcome = OutcomeInterrupted
	case r.HarnessError != "":
		r.Pass, r.Skipped, r.SkipReason = false, false, ""
		r.Outcome = OutcomeHarnessFailure
	case r.Skipped && r.NetworkFail:
		r.Pass, r.Outcome = false, OutcomeNetworkSkip
	case r.Skipped:
		r.Pass, r.Outcome = false, OutcomeMissingEnvironment
	case r.Pass:
		r.Outcome = OutcomePass
	case r.NetworkFail:
		r.Outcome = OutcomeNetworkFailure
	default:
		r.Outcome = OutcomeProductFailure
	}
}
