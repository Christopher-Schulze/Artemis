package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

// Telemetry receives redacted route evidence after every completed attempt.
type Telemetry interface {
	Record(RouteEvidence)
}

type discardTelemetry struct{}

func (discardTelemetry) Record(RouteEvidence) {}

// Config constructs one immutable router with its executor seams.
type Config struct {
	Executors map[Mode]Executor
	Telemetry Telemetry
	Now       func() time.Time
}

// HybridRouter owns deterministic selection, state lineage, fallback, and
// per-mode circuit breakers. It is safe for concurrent use.
type HybridRouter struct {
	selector  *bridge.FullExecutionRouter
	executors map[Mode]Executor
	breakers  map[Mode]*circuitBreaker
	telemetry Telemetry
	now       func() time.Time
	mu        sync.RWMutex
}

func New(config Config) (*HybridRouter, error) {
	if len(config.Executors) == 0 {
		return nil, fmt.Errorf("router: at least one executor is required")
	}
	for mode, executor := range config.Executors {
		if mode == ModeScrape {
			return nil, fmt.Errorf("router: scrape is a meta-mode and cannot be an executor")
		}
		if executor == nil {
			return nil, fmt.Errorf("router: nil executor for %s", mode)
		}
	}
	telemetry := config.Telemetry
	if telemetry == nil {
		telemetry = discardTelemetry{}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	executors := make(map[Mode]Executor, len(config.Executors))
	for mode, executor := range config.Executors {
		executors[mode] = executor
	}
	return &HybridRouter{
		selector: bridge.NewFullExecutionRouter(), executors: executors,
		breakers: make(map[Mode]*circuitBreaker), telemetry: telemetry, now: now,
	}, nil
}

// Decide returns the policy-validated first mode without executing it.
func (r *HybridRouter) Decide(req RouteRequest) (Decision, error) {
	req, err := validateRequest(req)
	if err != nil {
		return Decision{}, &RouteError{Code: ErrorInvalidInput, Cause: err}
	}
	return r.decide(req)
}

// Decision is the redacted first routing decision.
type Decision struct {
	Mode    Mode
	Reason  string
	URLHash string
	Policy  string
}

func (r *HybridRouter) decide(req RouteRequest) (Decision, error) {
	if req.ForceMode != "" {
		if req.Auth.browserRequired() && isLowerMode(req.ForceMode) {
			return Decision{}, &RouteError{Code: ErrorPolicyDenied, Mode: req.ForceMode, Cause: ErrAuthSemanticDowngrade}
		}
		if !req.Policy.allows(req.ForceMode) {
			return Decision{}, &RouteError{Code: ErrorPolicyDenied, Mode: req.ForceMode, Cause: fmt.Errorf("forced mode is not admitted by policy")}
		}
		return Decision{Mode: req.ForceMode, Reason: "caller_override", URLHash: hashURL(req.URL), Policy: "allow"}, nil
	}
	signals := req.Signals
	if req.Auth.browserRequired() {
		signals.NeedsAuthProfile = true
	}
	for _, api := range req.RequiredWebAPIs {
		if req.Capabilities != nil && req.Capabilities.RequiresEscalation(api, req.NeedsRealSemantics) {
			signals.NeedsLayout = true
		}
	}
	selected := r.selector.Route(signals)
	if req.Policy.allows(selected.Mode) {
		return Decision{Mode: selected.Mode, Reason: selected.Reason, URLHash: hashURL(req.URL), Policy: "allow"}, nil
	}
	for next := nextMode(selected.Mode); next != ""; next = nextMode(next) {
		if req.Auth.browserRequired() && isLowerMode(next) {
			continue
		}
		if req.Policy.allows(next) {
			return Decision{Mode: next, Reason: "policy_escalation:" + selected.Reason, URLHash: hashURL(req.URL), Policy: "allow"}, nil
		}
	}
	return Decision{}, &RouteError{Code: ErrorPolicyDenied, Mode: selected.Mode, Cause: fmt.Errorf("selected mode %s is not admitted and no higher mode is allowed", selected.Mode)}
}

func isLowerMode(mode Mode) bool {
	return mode == ModeStaticFetch || mode == ModeRenderlessJS
}

func nextMode(mode Mode) Mode {
	switch mode {
	case ModeStaticFetch:
		return ModeRenderlessJS
	case ModeRenderlessJS:
		return ModeChromiumCDP
	case ModeChromiumCDP:
		return ModeStealth
	default:
		return ""
	}
}

func (r *HybridRouter) breaker(mode Mode, policy Policy) *circuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()
	breaker := r.breakers[mode]
	if breaker == nil {
		breaker = newCircuitBreaker(policy.CircuitFailureThreshold, policy.CircuitOpenWindow)
		r.breakers[mode] = breaker
	}
	return breaker
}

// Execute runs the selected mode and escalates only through policy-admitted
// higher modes. Every successful output must be verified by its executor.
func (r *HybridRouter) Execute(ctx context.Context, request RouteRequest) (RouteResult, error) {
	started := r.now()
	if ctx == nil {
		return RouteResult{}, &RouteError{Code: ErrorInvalidInput, Cause: fmt.Errorf("context is required")}
	}
	req, err := validateRequest(request)
	if err != nil {
		return RouteResult{}, &RouteError{Code: ErrorInvalidInput, Cause: err}
	}
	decision, err := r.decide(req)
	if err != nil {
		return RouteResult{Evidence: r.baseEvidence(req, started)}, err
	}
	evidence := r.baseEvidence(req, started)
	evidence.InitialMode = decision.Mode
	evidence.Decision = decision.Reason
	mode := decision.Mode
	state := req.State.Clone()
	for attempt := 1; attempt <= req.Policy.MaxAttempts; attempt++ {
		evidence.Attempts = attempt
		if err := ctx.Err(); err != nil {
			return r.finishFailure(evidence, mode, ErrorCancelled, err)
		}
		if mode == "" || mode == ModeScrape {
			return r.finishFailure(evidence, mode, ErrorUnavailable, fmt.Errorf("no executable mode remains"))
		}
		if !req.Policy.allows(mode) {
			return r.finishFailure(evidence, mode, ErrorPolicyDenied, fmt.Errorf("mode is not admitted by policy"))
		}
		executor, ok := r.executor(mode)
		if !ok {
			next := nextMode(mode)
			if next == "" || len(evidence.Fallbacks) >= req.Policy.MaxFallbacks {
				return r.finishFailure(evidence, mode, ErrorUnavailable, fmt.Errorf("no executor registered for %s", mode))
			}
			if !req.Policy.allows(next) {
				return r.finishFailure(evidence, next, ErrorPolicyDenied, fmt.Errorf("fallback mode %s is not admitted", next))
			}
			evidence.Fallbacks = append(evidence.Fallbacks, FallbackEvidence{From: mode, To: next, Reason: "executor_unavailable"})
			mode = next
			continue
		}
		breaker := r.breaker(mode, req.Policy)
		if !breaker.allow(r.now()) {
			evidence.Fallbacks = append(evidence.Fallbacks, FallbackEvidence{From: mode, Reason: "circuit_open"})
			mode = nextMode(mode)
			continue
		}
		execRequest := ExecutionRequest{
			Mode: mode, URL: req.URL, Method: req.Method, Headers: req.Headers.Clone(),
			Body: append([]byte(nil), req.Body...), Action: req.Action, Signals: req.Signals,
			Auth: req.Auth, State: state.Clone(), TraceID: req.TraceID, EvidenceID: req.EvidenceID,
		}
		output, executeErr := executor.Execute(ctx, execRequest)
		if executeErr == nil {
			if err := validateOutput(output, req, state); err != nil {
				closeResource(output.Resource)
				return r.finishFailure(evidence, mode, ErrorStateTransfer, err)
			}
			if req.Policy.MaxCostUnit > 0 && evidence.CostUnit+output.CostUnit > req.Policy.MaxCostUnit {
				closeResource(output.Resource)
				return r.finishFailure(evidence, mode, ErrorResourceBudget, fmt.Errorf("route cost budget exceeded"))
			}
		}
		if executeErr != nil {
			closeResource(output.Resource)
		}
		if executeErr == nil {
			breaker.success()
			state = normalizedState(output.State, state, req.Auth, output.Page.URL)
			evidence.FinalMode = mode
			evidence.CostUnit += output.CostUnit
			evidence.ResultQuality = output.Quality
			evidence.State = stateEvidence(state, req.Auth)
			return r.finishSuccess(evidence, output.Page, output.Resource)
		}
		breaker.failure(r.now())
		failure := classifyFailure(executeErr)
		next := nextMode(mode)
		if !failure.Retryable || next == "" || len(evidence.Fallbacks) >= req.Policy.MaxFallbacks {
			return r.finishFailure(evidence, mode, failureCode(failure), executeErr)
		}
		if !req.Policy.allows(next) {
			return r.finishFailure(evidence, next, ErrorPolicyDenied, fmt.Errorf("fallback mode %s is not admitted", next))
		}
		if req.Auth.browserRequired() && isLowerMode(next) {
			return r.finishFailure(evidence, next, ErrorPolicyDenied, ErrAuthSemanticDowngrade)
		}
		evidence.Fallbacks = append(evidence.Fallbacks, FallbackEvidence{From: mode, To: next, Reason: failure.Reason})
		mode = next
	}
	return r.finishFailure(evidence, mode, ErrorExecution, fmt.Errorf("attempt budget exhausted"))
}

func (r *HybridRouter) executor(mode Mode) (Executor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	executor, ok := r.executors[mode]
	return executor, ok
}

func (r *HybridRouter) baseEvidence(req RouteRequest, started time.Time) RouteEvidence {
	return RouteEvidence{
		TraceID: req.TraceID, EvidenceID: req.EvidenceID, URLHash: hashURL(req.URL),
		State: stateEvidence(req.State, req.Auth), StartedAt: started,
	}
}

func (r *HybridRouter) finishSuccess(evidence RouteEvidence, output PageOutput, resource Resource) (RouteResult, error) {
	evidence.Duration = r.now().Sub(evidence.StartedAt)
	r.telemetry.Record(evidence)
	return RouteResult{Success: true, Output: output, Evidence: evidence, Resource: resource}, nil
}

func (r *HybridRouter) finishFailure(evidence RouteEvidence, mode Mode, code string, err error) (RouteResult, error) {
	evidence.FinalMode = mode
	evidence.Duration = r.now().Sub(evidence.StartedAt)
	r.telemetry.Record(evidence)
	return RouteResult{Evidence: evidence}, &RouteError{Code: code, Mode: mode, Cause: err}
}

func closeResource(resource Resource) {
	if resource != nil {
		_ = resource.Close()
	}
}

func validateOutput(output ExecutionOutput, req RouteRequest, previous BrowserState) error {
	if !output.Verified {
		return fmt.Errorf("executor did not verify the requested outcome")
	}
	if output.Page.URL == "" || output.Page.StatusCode < 100 || output.Page.StatusCode > 599 {
		return fmt.Errorf("executor returned incomplete page evidence")
	}
	if err := normalizedState(output.State, previous, req.Auth, output.Page.URL).validate(req.Auth); err != nil {
		return fmt.Errorf("state transfer: %w", err)
	}
	return nil
}

func normalizedState(output, previous BrowserState, auth AuthState, pageURL string) BrowserState {
	state := output.Clone()
	if state.Headers == nil {
		state.Headers = previous.Headers.Clone()
	}
	if state.Cookies == nil {
		state.Cookies = previous.Clone().Cookies
	}
	if state.LocalStorage == nil {
		state.LocalStorage = cloneStringMap(previous.LocalStorage)
	}
	if state.SessionStorage == nil {
		state.SessionStorage = cloneStringMap(previous.SessionStorage)
	}
	if state.SessionID == "" {
		state.SessionID = previous.SessionID
	}
	if state.ProfileID == "" {
		state.ProfileID = previous.ProfileID
	}
	if state.OwnerUserRef == "" {
		state.OwnerUserRef = previous.OwnerUserRef
	}
	if state.CookieScope == "" {
		state.CookieScope = previous.CookieScope
	}
	if state.StorageScope == "" {
		state.StorageScope = previous.StorageScope
	}
	if state.URL == "" {
		state.URL = pageURL
	}
	if auth.SessionID != "" && state.SessionID == "" {
		state.SessionID = auth.SessionID
	}
	if auth.ProfileID != "" && state.ProfileID == "" {
		state.ProfileID = auth.ProfileID
	}
	return state
}

func classifyFailure(err error) RouteFailure {
	var failure *RouteFailure
	if errors.As(err, &failure) && failure != nil {
		if failure.Reason == "" {
			failure.Reason = "execution_error"
		}
		return *failure
	}
	return RouteFailure{Reason: "execution_error", Retryable: true, Cause: err}
}

func failureCode(failure RouteFailure) string {
	if failure.Reason == "policy_denied" {
		return ErrorPolicyDenied
	}
	return ErrorExecution
}

// RouteResult is the public typed result and redacted evidence pair.
type RouteResult struct {
	Success  bool
	Output   PageOutput
	Evidence RouteEvidence
	Resource Resource
}

// Close releases a transferred engine resource and is idempotent per result.
func (r *RouteResult) Close() error {
	if r == nil || r.Resource == nil {
		return nil
	}
	resource := r.Resource
	r.Resource = nil
	return resource.Close()
}

// Downgrade validates an explicit Chromium-to-renderless transition. Execute
// never performs this operation implicitly.
func (r *HybridRouter) Downgrade(req RouteRequest, from, to Mode) (Decision, error) {
	req, err := validateRequest(req)
	if err != nil {
		return Decision{}, &RouteError{Code: ErrorInvalidInput, Cause: err}
	}
	if from != ModeChromiumCDP && from != ModeStealth {
		return Decision{}, &RouteError{Code: ErrorInvalidInput, Mode: from, Cause: fmt.Errorf("only browser modes can downgrade")}
	}
	if to != ModeStaticFetch && to != ModeRenderlessJS {
		return Decision{}, &RouteError{Code: ErrorInvalidInput, Mode: to, Cause: fmt.Errorf("downgrade target must be static or renderless")}
	}
	if req.Auth.browserRequired() || len(req.State.Cookies) > 0 || len(req.State.LocalStorage) > 0 || len(req.State.SessionStorage) > 0 {
		return Decision{}, &RouteError{Code: ErrorPolicyDenied, Mode: to, Cause: ErrAuthSemanticDowngrade}
	}
	if !req.Policy.allows(to) {
		return Decision{}, &RouteError{Code: ErrorPolicyDenied, Mode: to, Cause: fmt.Errorf("downgrade target is not admitted")}
	}
	return Decision{Mode: to, Reason: "explicit_safe_downgrade", URLHash: hashURL(req.URL), Policy: "allow"}, nil
}
