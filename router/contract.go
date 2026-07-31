package router

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/observe"
	"github.com/Christopher-Schulze/Artemis/solver"
)

// Mode is the execution engine selected for one browser operation.
type Mode = bridge.ExecutionRouterMode

const (
	ModeStaticFetch  = bridge.ModeStaticFetch
	ModeRenderlessJS = bridge.ModeRenderlessJS
	ModeChromiumCDP  = bridge.ModeChromiumCDP
	ModeStealth      = bridge.ModeStealth
	ModeScrape       = bridge.ModeScrape
)

// Signals is the deterministic page and action signal set consumed by the
// existing browser execution selector.
type Signals = bridge.RouterSignals

// Action identifies the semantic contract requested by the caller.
type Action string

const (
	ActionFetch        Action = "fetch"
	ActionNavigate     Action = "navigate"
	ActionExtract      Action = "extract"
	ActionObserve      Action = "observe"
	ActionInteract     Action = "interact"
	ActionAuthenticate Action = "authenticate"
	ActionChallenge    Action = "challenge"
)

// AuthState is the minimum identity context required to preserve
// authentication semantics while a route escalates.
type AuthState struct {
	Required      bool   `json:"required"`
	Authenticated bool   `json:"authenticated"`
	SessionID     string `json:"session_id,omitempty"`
	ProfileID     string `json:"profile_id,omitempty"`
	OwnerUserRef  string `json:"owner_user_ref,omitempty"`
	CookieScope   string `json:"cookie_scope,omitempty"`
	StorageScope  string `json:"storage_scope,omitempty"`
}

func (a AuthState) browserRequired() bool {
	return a.Required || a.Authenticated || a.SessionID != "" || a.ProfileID != ""
}

// BrowserState is transferred between executors. Secret values are kept in
// the execution request only and are never copied into RouteEvidence.
type BrowserState struct {
	URL            string
	Headers        http.Header
	Cookies        []*http.Cookie
	LocalStorage   map[string]string
	SessionStorage map[string]string
	SessionID      string
	ProfileID      string
	OwnerUserRef   string
	CookieScope    string
	StorageScope   string
}

// Clone creates an independent state snapshot for one executor attempt.
func (s BrowserState) Clone() BrowserState {
	clone := BrowserState{
		URL:          s.URL,
		Headers:      s.Headers.Clone(),
		SessionID:    s.SessionID,
		ProfileID:    s.ProfileID,
		OwnerUserRef: s.OwnerUserRef,
		CookieScope:  s.CookieScope,
		StorageScope: s.StorageScope,
	}
	if s.Cookies != nil {
		clone.Cookies = make([]*http.Cookie, 0, len(s.Cookies))
		for _, cookie := range s.Cookies {
			if cookie == nil {
				continue
			}
			copyCookie := *cookie
			clone.Cookies = append(clone.Cookies, &copyCookie)
		}
	}
	clone.LocalStorage = cloneStringMap(s.LocalStorage)
	clone.SessionStorage = cloneStringMap(s.SessionStorage)
	return clone
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func (s BrowserState) validate(auth AuthState) error {
	if auth.SessionID != "" && s.SessionID != auth.SessionID {
		return fmt.Errorf("session identity changed during route")
	}
	if auth.ProfileID != "" && s.ProfileID != auth.ProfileID {
		return fmt.Errorf("profile identity changed during route")
	}
	if auth.OwnerUserRef != "" && s.OwnerUserRef != auth.OwnerUserRef {
		return fmt.Errorf("session owner changed during route")
	}
	if auth.CookieScope != "" && s.CookieScope != auth.CookieScope {
		return fmt.Errorf("cookie scope changed during route")
	}
	if auth.StorageScope != "" && s.StorageScope != auth.StorageScope {
		return fmt.Errorf("storage scope changed during route")
	}
	if auth.Required && auth.ProfileID != "" && s.ProfileID == "" {
		return fmt.Errorf("required authentication profile was not transferred")
	}
	return nil
}

// CapabilityResolver allows the production-derived renderless capability profile to
// participate without coupling this package to its concrete implementation.
type CapabilityResolver interface {
	RequiresEscalation(api string, needsRealSemantics bool) bool
}

// CapabilityAuthority optionally exposes the measured renderless category so
// route decisions and redacted evidence use the same fail-closed authority.
type CapabilityAuthority interface {
	CapabilityResolver
	CapabilityCategoryName(api string) string
}

// CapabilityEvidence records one required WebAPI decision without page data
// or credentials.
type CapabilityEvidence struct {
	API                string `json:"api"`
	Category           string `json:"category"`
	NeedsRealSemantics bool   `json:"needs_real_semantics"`
	Escalate           bool   `json:"escalate"`
}

// RouteRequest is the complete deterministic input to one route execution.
type RouteRequest struct {
	URL                string
	Method             string
	Headers            http.Header
	Body               []byte
	Action             Action
	Signals            Signals
	RequiredWebAPIs    []string
	Capabilities       CapabilityResolver
	NeedsRealSemantics bool
	ForceMode          Mode
	Auth               AuthState
	State              BrowserState
	Policy             Policy
	TraceID            string
	EvidenceID         string
}

// Policy limits engine selection and retry behavior. An empty AllowedModes
// uses the safe built-in set; callers can narrow it explicitly.
type Policy struct {
	AllowedModes            map[Mode]bool
	MaxAttempts             int
	MaxFallbacks            int
	MaxCostUnit             int64
	CircuitFailureThreshold int
	CircuitOpenWindow       time.Duration
}

func (p Policy) withDefaults() Policy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 4
	}
	if p.MaxFallbacks < 0 {
		p.MaxFallbacks = 0
	}
	if p.MaxFallbacks == 0 {
		p.MaxFallbacks = 3
	}
	if p.CircuitFailureThreshold <= 0 {
		p.CircuitFailureThreshold = 3
	}
	if p.CircuitOpenWindow <= 0 {
		p.CircuitOpenWindow = time.Minute
	}
	if p.AllowedModes != nil {
		p.AllowedModes = cloneModeSet(p.AllowedModes)
	}
	return p
}

func cloneModeSet(values map[Mode]bool) map[Mode]bool {
	clone := make(map[Mode]bool, len(values))
	for mode, allowed := range values {
		clone[mode] = allowed
	}
	return clone
}

func (p Policy) allows(mode Mode) bool {
	if p.AllowedModes != nil {
		return p.AllowedModes[mode]
	}
	return mode == ModeStaticFetch || mode == ModeRenderlessJS || mode == ModeChromiumCDP || mode == ModeStealth
}

// ExecutionRequest is passed to exactly one engine attempt.
type ExecutionRequest struct {
	Mode       Mode
	URL        string
	Method     string
	Headers    http.Header
	Body       []byte
	Action     Action
	Signals    Signals
	Auth       AuthState
	State      BrowserState
	TraceID    string
	EvidenceID string
}

// PageOutput is the common result shape shared by renderless and Chromium.
type PageOutput struct {
	URL        string
	StatusCode int
	Headers    http.Header
	HTML       string
	Text       string
	Markdown   string
	Title      string
	Links      []agent.Link
}

// ExecutionOutput is a verified engine result. Verified must be true; an
// engine that only attempted an operation cannot be presented as success.
type ExecutionOutput struct {
	Page        PageOutput
	State       BrowserState
	Resource    Resource
	Observation *observe.ObservationEvidence
	CostUnit    int64
	Quality     string
	Verified    bool
}

// ChallengeDetector is the route boundary for deterministic challenge
// detection. The solver package provides the production implementation.
type ChallengeDetector interface {
	Detect(context.Context, solver.PageSignals) (*solver.ChallengeInfo, error)
}

// ChallengeResolver is the route boundary for policy-governed challenge
// handling. A resolver must verify the postcondition before returning solved.
type ChallengeResolver interface {
	Resolve(context.Context, solver.ChallengeInfo) (solver.ChallengeOutcome, error)
}

// ObservationProvider supplies bounded live evidence for a Chromium result.
// observe.LiveCollector is the canonical implementation.
type ObservationProvider interface {
	CaptureEvidence(context.Context) (observe.ObservationEvidence, error)
}

const (
	ResultQualityVerified = "verified"
	ResultQualityObserved = "observed"
)

// Executor is implemented by a real renderless, Chromium, or stealth
// engine. The router owns policy, state lineage, fallback, and evidence.
type Executor interface {
	Execute(context.Context, ExecutionRequest) (ExecutionOutput, error)
}

// Resource is an engine-owned page/session handle transferred with a
// successful route result. The consuming caller owns Close.
type Resource interface {
	Close() error
}

// RouteFailure lets an executor classify whether the next higher engine can
// safely retry the operation.
type RouteFailure struct {
	Reason    string
	Retryable bool
	Cause     error
}

func (f *RouteFailure) Error() string {
	if f == nil {
		return "route failure"
	}
	if f.Cause == nil {
		return "route failure: " + f.Reason
	}
	return "route failure: " + f.Reason + ": " + f.Cause.Error()
}

func (f *RouteFailure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Cause
}

// RouteError is a stable failure returned by Execute.
type RouteError struct {
	Code  string
	Mode  Mode
	Cause error
}

const (
	ErrorInvalidInput   = "invalid_input"
	ErrorPolicyDenied   = "policy_denied"
	ErrorUnavailable    = "executor_unavailable"
	ErrorCircuitOpen    = "circuit_open"
	ErrorStateTransfer  = "state_transfer"
	ErrorResourceBudget = "resource_budget"
	ErrorExecution      = "execution_failed"
	ErrorCancelled      = "cancelled"
	ErrorChallenge      = "challenge_detected"
	ErrorChallengeSolve = "challenge_unresolved"
)

func (e *RouteError) Error() string {
	if e == nil {
		return "route error"
	}
	if e.Cause == nil {
		return "route: " + e.Code
	}
	return fmt.Sprintf("route: %s (%s): %v", e.Code, e.Mode, e.Cause)
}

func (e *RouteError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

var (
	ErrAuthSemanticDowngrade      = errors.New("route would change authentication semantics")
	ErrCapabilityEscalationNeeded = errors.New("route requires a higher capability mode")
	ErrChallengeDetected          = errors.New("route output contains a detected challenge")
	ErrChallengeUnresolved        = errors.New("route challenge was not resolved and verified")
)

// RouteEvidence is intentionally secret-free and suitable for telemetry.
type RouteEvidence struct {
	TraceID       string               `json:"trace_id,omitempty"`
	EvidenceID    string               `json:"evidence_id,omitempty"`
	URLHash       string               `json:"url_hash"`
	InitialMode   Mode                 `json:"initial_mode"`
	FinalMode     Mode                 `json:"final_mode"`
	Decision      string               `json:"decision"`
	Policy        string               `json:"policy"`
	Capabilities  []CapabilityEvidence `json:"capabilities,omitempty"`
	Fallbacks     []FallbackEvidence   `json:"fallbacks,omitempty"`
	Attempts      int                  `json:"attempts"`
	CostUnit      int64                `json:"cost_unit"`
	ResultQuality string               `json:"result_quality,omitempty"`
	State         StateEvidence        `json:"state"`
	Observation   *ObservationSummary  `json:"observation,omitempty"`
	Challenge     *ChallengeEvidence   `json:"challenge,omitempty"`
	StartedAt     time.Time            `json:"started_at"`
	Duration      time.Duration        `json:"duration_ns"`
}

// ObservationSummary is the secret-free route projection of bounded live
// evidence. Full evidence remains attached to RouteResult for the consumer.
type ObservationSummary struct {
	Schema            string `json:"schema"`
	SnapshotEpoch     uint64 `json:"snapshot_epoch"`
	SnapshotNodes     int    `json:"snapshot_nodes"`
	SnapshotTruncated bool   `json:"snapshot_truncated"`
	NetworkCount      int    `json:"network_count"`
	ConsoleCount      int    `json:"console_count"`
	RequestCount      int    `json:"request_count"`
	Truncated         bool   `json:"truncated"`
}

// ChallengeEvidence records only bounded, non-secret challenge state.
type ChallengeEvidence struct {
	Type        solver.ChallengeType          `json:"type"`
	Status      solver.ChallengeOutcomeStatus `json:"status"`
	Strategy    solver.SolverStrategy         `json:"strategy,omitempty"`
	Attempts    int                           `json:"attempts"`
	Confidence  float64                       `json:"confidence"`
	SignalCount int                           `json:"signal_count"`
	DomainHash  string                        `json:"domain_hash,omitempty"`
	Reason      string                        `json:"reason"`
}

type FallbackEvidence struct {
	From   Mode   `json:"from"`
	To     Mode   `json:"to"`
	Reason string `json:"reason"`
	Error  string `json:"error,omitempty"`
}

type StateEvidence struct {
	SessionID          string `json:"session_id,omitempty"`
	ProfileID          string `json:"profile_id,omitempty"`
	CookieScope        string `json:"cookie_scope,omitempty"`
	StorageScope       string `json:"storage_scope,omitempty"`
	CookieCount        int    `json:"cookie_count"`
	LocalStorageKeys   int    `json:"local_storage_keys"`
	SessionStorageKeys int    `json:"session_storage_keys"`
	Authenticated      bool   `json:"authenticated"`
}

func stateEvidence(state BrowserState, auth AuthState) StateEvidence {
	return StateEvidence{
		SessionID: state.SessionID, ProfileID: state.ProfileID,
		CookieScope: state.CookieScope, StorageScope: state.StorageScope,
		CookieCount: len(state.Cookies), LocalStorageKeys: len(state.LocalStorage),
		SessionStorageKeys: len(state.SessionStorage), Authenticated: auth.Authenticated,
	}
}

func hashURL(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func validateRequest(req RouteRequest) (RouteRequest, error) {
	if strings.TrimSpace(req.URL) == "" {
		return RouteRequest{}, fmt.Errorf("URL is required")
	}
	parsed, err := url.ParseRequestURI(req.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return RouteRequest{}, fmt.Errorf("invalid HTTP URL %q", req.URL)
	}
	if req.Method == "" {
		req.Method = http.MethodGet
	}
	req.Method = strings.ToUpper(req.Method)
	if req.Action == "" {
		req.Action = ActionFetch
	}
	if req.ForceMode != "" && !bridge.IsValidExecutionRouterMode(req.ForceMode) {
		return RouteRequest{}, fmt.Errorf("unknown force mode %q", req.ForceMode)
	}
	req.Policy = req.Policy.withDefaults()
	req.Headers = req.Headers.Clone()
	req.Body = append([]byte(nil), req.Body...)
	req.State = req.State.Clone()
	if req.Auth.SessionID != "" && req.State.SessionID == "" {
		req.State.SessionID = req.Auth.SessionID
	}
	if req.Auth.ProfileID != "" && req.State.ProfileID == "" {
		req.State.ProfileID = req.Auth.ProfileID
	}
	if req.Auth.OwnerUserRef != "" && req.State.OwnerUserRef == "" {
		req.State.OwnerUserRef = req.Auth.OwnerUserRef
	}
	if req.Auth.CookieScope != "" && req.State.CookieScope == "" {
		req.State.CookieScope = req.Auth.CookieScope
	}
	if req.Auth.StorageScope != "" && req.State.StorageScope == "" {
		req.State.StorageScope = req.Auth.StorageScope
	}
	if err := req.State.validate(req.Auth); err != nil {
		return RouteRequest{}, err
	}
	return req, nil
}
