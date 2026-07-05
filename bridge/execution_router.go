package bridge

import (
	"context"
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Artemis/scraper"
)

// ExecutionRouterMode enumerates the 5 execution modes from spec ss28.3a.
type ExecutionRouterMode string

const (
	// ModeStaticFetch: HTTP-only fetch, no JS execution.
	// Route: public no-JS/HTML/API/RSS pages.
	ModeStaticFetch ExecutionRouterMode = "static_fetch"
	// ModeRenderlessJS: in-process V8 DOM/WebAPI, no real layout/paint.
	// Route: pages needing simple script execution, API-backed DOM
	// mutation, cookies, forms-as-HTML, JSON-LD/OpenGraph/Twitter/
	// semantic extraction, no layout/pixel/gesture semantics.
	ModeRenderlessJS ExecutionRouterMode = "renderless_js"
	// ModeChromiumCDP: full Chromium via CDP, real layout/hit-testing.
	// Route: pages needing real layout, coordinates, screenshot,
	// canvas/media, shadow/layout observers, browser-native actionability.
	ModeChromiumCDP ExecutionRouterMode = "chromium_cdp"
	// ModeStealth: Chromium with stealth patches (anti-detection).
	// Route: pages with bot/WAF/CAPTCHA signals or fingerprint detection.
	ModeStealth ExecutionRouterMode = "stealth"
	// ModeScrape: general scrape mode (alias for the extraction
	// pipeline that can use any of the above).
	ModeScrape ExecutionRouterMode = "scrape"
)

// RouterSignals carries the input signals from which the router
// selects an execution mode (spec L3990 route rules).
type RouterSignals struct {
	// ContentType is the HTTP Content-Type header.
	ContentType string
	// IsHTML reports whether the response is HTML.
	IsHTML bool
	// IsAPI reports whether the response is JSON/API (non-HTML).
	IsAPI bool
	// IsRSS reports whether the response is RSS/Atom feed.
	IsRSS bool
	// ScriptCount is the number of <script> tags in the HTML.
	ScriptCount int
	// HasExternalScripts reports whether external <script src=...> exist.
	HasExternalScripts bool
	// HasFetch reports whether the page uses fetch().
	HasFetch bool
	// HasXHR reports whether the page uses XMLHttpRequest.
	HasXHR bool
	// HasDocumentWrite reports whether the page uses document.write().
	HasDocumentWrite bool
	// HasDOMMutation reports whether the page mutates DOM via JS.
	HasDOMMutation bool
	// NeedsLayout reports whether the task requires real layout
	// (getBoundingClientRect, coordinates, hit-testing).
	NeedsLayout bool
	// NeedsCanvas reports whether the task requires canvas/media.
	NeedsCanvas bool
	// NeedsScreenshot reports whether the task requires a screenshot.
	NeedsScreenshot bool
	// HasWebAuthn reports whether the page uses WebAuthn/MFA.
	HasWebAuthn bool
	// HasCAPTCHA reports whether CAPTCHA/challenge elements are present.
	HasCAPTCHA bool
	// HasBotDetection reports whether bot-detection signals are present
	// (Cloudflare, "access denied", "bot detected", etc.).
	HasBotDetection bool
	// HasWAF reports whether WAF/protection signals are present.
	HasWAF bool
	// NeedsAuthProfile reports whether the page requires a login
	// session profile.
	NeedsAuthProfile bool
	// NeedsFormActionability reports whether the task requires
	// browser-native form actionability (visibility, box-model checks).
	NeedsFormActionability bool
	// HasShadowDOM reports whether the page uses shadow DOM.
	HasShadowDOM bool
	// HasLayoutObservers reports whether the page uses Intersection/
	// Resize observers.
	HasLayoutObservers bool
	// HasServiceWorker reports whether the page registers a service worker.
	HasServiceWorker bool
	// HasWebComponents reports whether the page uses customElements.define.
	HasWebComponents bool
	// OperatorOverride is a non-empty mode string set by the operator
	// to force a specific mode.
	OperatorOverride string
}

// RouterDecision is the output of the execution router: the selected
// mode and the reason for the selection.
type RouterDecision struct {
	Mode   ExecutionRouterMode
	Reason string
}

// FullExecutionRouter is the deterministic 5-mode selector from
// spec ss28.3a. It selects static_fetch|renderless_js|chromium_cdp|
// stealth|scrape from the full signal set: content-type, HTML/JS
// markers, script count/source class, fetch|XHR|document.write|DOM
// mutation signals, URL/domain policy, auth/profile requirement,
// form/actionability requirement, layout/canvas/media/WebAuthn/
// CAPTCHA/bot/WAF signals, and operator override.
type FullExecutionRouter struct{}

// NewFullExecutionRouter creates a new FullExecutionRouter.
func NewFullExecutionRouter() *FullExecutionRouter { return &FullExecutionRouter{} }

// Route selects an execution mode from the given signals.
// Route rules (spec L3990):
//   - public no-JS/HTML/API/RSS -> static_fetch
//   - simple script execution, API-backed DOM mutation, cookies, forms,
//     JSON-LD/OpenGraph/Twitter/semantic, no layout/pixel/gesture -> renderless_js
//   - real layout, hit-testing, coordinates, screenshot, canvas/media,
//     WebAuthn/MFA, CAPTCHA, extension/fingerprint, login session,
//     shadow/layout observers, browser-native actionability -> chromium_cdp/stealth
//   - bot/WAF/CAPTCHA/fingerprint detection -> stealth
func (r *FullExecutionRouter) Route(s RouterSignals) RouterDecision {
	// 1. Operator override wins (spec: "operator override").
	if s.OperatorOverride != "" {
		mode := ExecutionRouterMode(s.OperatorOverride)
		if IsValidExecutionRouterMode(mode) {
			return RouterDecision{Mode: mode, Reason: "operator_override"}
		}
	}

	// 2. Bot/WAF/CAPTCHA -> stealth (spec: "CAPTCHA/bot/WAF signals").
	if s.HasBotDetection || s.HasWAF || s.HasCAPTCHA {
		return RouterDecision{Mode: ModeStealth, Reason: "bot_waf_captcha_detected"}
	}

	// 3. WebAuthn/MFA, auth/profile requirement -> stealth or chromium_cdp.
	if s.HasWebAuthn || s.NeedsAuthProfile {
		return RouterDecision{Mode: ModeStealth, Reason: "auth_profile_required"}
	}

	// 4. Layout/canvas/screenshot/shadow/layout-observers/service-worker/
	//    web-components -> chromium_cdp (spec: "real layout, hit-testing,
	//    coordinates, screenshot, canvas/media, shadow/layout observers").
	if s.NeedsLayout || s.NeedsCanvas || s.NeedsScreenshot ||
		s.HasShadowDOM || s.HasLayoutObservers || s.HasServiceWorker {
		return RouterDecision{Mode: ModeChromiumCDP, Reason: "layout_or_render_required"}
	}

	// 5. Form actionability -> chromium_cdp (spec: "browser-native
	//    actionability").
	if s.NeedsFormActionability {
		return RouterDecision{Mode: ModeChromiumCDP, Reason: "form_actionability_required"}
	}

	// 6. Script-heavy pages with fetch/XHR/DOM-mutation/document.write
	//    but no layout needs -> renderless_js (spec: "pages needing
	//    simple script execution, API-backed DOM mutation").
	if s.ScriptCount > 0 || s.HasExternalScripts || s.HasFetch ||
		s.HasXHR || s.HasDocumentWrite || s.HasDOMMutation ||
		s.HasWebComponents {
		return RouterDecision{Mode: ModeRenderlessJS, Reason: "script_execution_needed"}
	}

	// 7. API/RSS (non-HTML) -> static_fetch.
	if s.IsAPI || s.IsRSS {
		return RouterDecision{Mode: ModeStaticFetch, Reason: "api_or_rss_content"}
	}

	// 8. HTML with no scripts -> static_fetch (spec: "public no-JS/HTML").
	if s.IsHTML {
		return RouterDecision{Mode: ModeStaticFetch, Reason: "static_html_no_scripts"}
	}

	// 9. Default: static_fetch (fail-safe, cheapest path).
	return RouterDecision{Mode: ModeStaticFetch, Reason: "default_static"}
}

// EscalateMode implements fail-closed-upward escalation (spec L3990:
// "Fallback is fail-closed upward, never silent"). On any failure in
// a lower mode, escalate to the next higher mode. The escalation
// order is: static_fetch -> renderless_js -> chromium_cdp -> stealth.
// scrape is a meta-mode and is not in the escalation chain.
func (r *FullExecutionRouter) EscalateMode(current ExecutionRouterMode, fallbackReason string) (ExecutionRouterMode, error) {
	switch current {
	case ModeStaticFetch:
		return ModeRenderlessJS, nil
	case ModeRenderlessJS:
		return ModeChromiumCDP, nil
	case ModeChromiumCDP:
		return ModeStealth, nil
	case ModeStealth:
		return "", fmt.Errorf("execution router: no further escalation after stealth (reason: %s)", fallbackReason)
	case ModeScrape:
		return ModeChromiumCDP, nil
	default:
		return ModeChromiumCDP, nil
	}
}

// EscalateReasons enumerates the reasons for escalation per spec L3990:
// "unsupported WebAPI, script timeout, module/worker/layout-query
// reliance, auth/profile requirement, policy denial or extraction
// confidence below threshold".
type EscalateReason string

const (
	EscalateUnsupportedWebAPI   EscalateReason = "unsupported_webapi"
	EscalateScriptTimeout       EscalateReason = "script_timeout"
	EscalateLayoutQueryReliance EscalateReason = "layout_query_reliance"
	EscalateAuthProfileRequired EscalateReason = "auth_profile_required"
	EscalatePolicyDenied        EscalateReason = "policy_denied"
	EscalateLowConfidence       EscalateReason = "extraction_confidence_below_threshold"
)

// ShouldEscalateMode reports whether the given reason warrants
// escalation from renderless_js to chromium_cdp (spec L3990).
func ShouldEscalateMode(reason EscalateReason) bool {
	switch reason {
	case EscalateUnsupportedWebAPI,
		EscalateScriptTimeout,
		EscalateLayoutQueryReliance,
		EscalateAuthProfileRequired,
		EscalatePolicyDenied,
		EscalateLowConfidence:
		return true
	}
	return false
}

// IsValidExecutionRouterMode reports whether a mode string is a
// valid ExecutionRouterMode.
func IsValidExecutionRouterMode(mode ExecutionRouterMode) bool {
	switch mode {
	case ModeStaticFetch, ModeRenderlessJS, ModeChromiumCDP, ModeStealth, ModeScrape:
		return true
	}
	return false
}

// ToExtractionMode converts an ExecutionRouterMode to the
// scraper.ExtractionMode enum (which uses the same string values).
func ToExtractionMode(mode ExecutionRouterMode) scraper.ExtractionMode {
	return scraper.ExtractionMode(mode)
}

// RouterSignalsFromEscalation converts the scraper.EscalationSignals
// (from static_escalation.go) into RouterSignals so the router can
// be used with the existing escalation detection logic.
func RouterSignalsFromEscalation(s scraper.EscalationSignals) RouterSignals {
	return RouterSignals{
		IsHTML:           true, // EscalationSignals assumes HTML input
		ScriptCount:      s.ScriptCount,
		HasCAPTCHA:       s.HasCAPTCHA,
		HasBotDetection:  s.HasCAPTCHA, // CAPTCHA implies bot detection
		HasServiceWorker: s.HasServiceWorker,
		HasWebComponents: s.HasWebComponents,
		NeedsAuthProfile: s.HasLoginWall,
		HasDOMMutation:   s.IsSPA,
	}
}

// RouteFromEscalation is a convenience function that converts
// EscalationSignals to RouterSignals and routes them.
func (r *FullExecutionRouter) RouteFromEscalation(s scraper.EscalationSignals) RouterDecision {
	return r.Route(RouterSignalsFromEscalation(s))
}

// ContextKey is used to store the router decision in the context.
type ContextKey struct{}

// DecisionFromContext retrieves the router decision from the context,
// if present.
func DecisionFromContext(ctx context.Context) (*RouterDecision, bool) {
	v, ok := ctx.Value(ContextKey{}).(*RouterDecision)
	return v, ok
}

// WithDecision stores the router decision in the context.
func WithDecision(ctx context.Context, d RouterDecision) context.Context {
	return context.WithValue(ctx, ContextKey{}, &d)
}

// FallbackEvent is an OCSF browser.execution_mode_escalated event
// (spec L3990: "emit OCSF browser.execution_mode_escalated").
type FallbackEvent struct {
	EventType           string              `json:"eventType"` // always "browser.execution_mode_escalated"
	FromMode            ExecutionRouterMode `json:"fromMode"`
	ToMode              ExecutionRouterMode `json:"toMode"`
	FallbackReason      string              `json:"fallbackReason"`
	UnsupportedFeatures []string            `json:"unsupportedFeatures,omitempty"`
	Timestamp           string              `json:"timestamp"`
}

// NewFallbackEvent creates an OCSF browser.execution_mode_escalated event.
func NewFallbackEvent(from, to ExecutionRouterMode, reason string, unsupported []string) FallbackEvent {
	return FallbackEvent{
		EventType:           "browser.execution_mode_escalated",
		FromMode:            from,
		ToMode:              to,
		FallbackReason:      reason,
		UnsupportedFeatures: unsupported,
	}
}

// String returns a human-readable summary of the fallback event.
func (e FallbackEvent) String() string {
	return fmt.Sprintf("OCSF %s: %s -> %s (reason: %s, unsupported: %d)",
		e.EventType, e.FromMode, e.ToMode, e.FallbackReason, len(e.UnsupportedFeatures))
}

// FormatFallbackReason formats a fallback reason string for the
// SourceSnapshot.FallbackReason field (spec L3990: "with fallback_reason").
func FormatFallbackReason(reason EscalateReason, detail string) string {
	if detail == "" {
		return string(reason)
	}
	return string(reason) + ": " + detail
}

// EnsureNoBlank ensures a non-blank mode string, defaulting to static_fetch.
func EnsureNoBlank(mode ExecutionRouterMode) ExecutionRouterMode {
	if strings.TrimSpace(string(mode)) == "" {
		return ModeStaticFetch
	}
	return mode
}
