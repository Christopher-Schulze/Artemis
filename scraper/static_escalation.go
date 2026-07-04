package scraper

import "strings"

// RenderMode selects static vs escalated browser rendering.
type RenderMode string

const (
	RenderModeStatic    RenderMode = "static"
	RenderModeEscalated RenderMode = "escalated"
)

// EscalationSignals are content-level signals extracted from a fetched
// page that inform the static→browser escalation decision. The zero
// value is valid (all signals off). Fields mirror spec ss28.15.9 P6.4
// renderless escalation rules.
type EscalationSignals struct {
	// StatusCode is the HTTP response status code.
	StatusCode int
	// BodyLen is the response body length in bytes.
	BodyLen int
	// InfiniteScroll indicates the page uses infinite scroll patterns.
	InfiniteScroll bool
	// HasCAPTCHA indicates a CAPTCHA or bot-challenge was detected.
	HasCAPTCHA bool
	// IsSPA indicates the page is a single-page-app shell (very little
	// server-rendered content, heavy client-side rendering).
	IsSPA bool
	// HasWebComponents indicates custom-elements / shadow-DOM usage.
	HasWebComponents bool
	// HasServiceWorker indicates a service-worker registration.
	HasServiceWorker bool
	// BodyHasContent indicates the <body> has meaningful content beyond
	// a <div id="root"></div> or <div id="app"></div> shell.
	BodyHasContent bool
	// ScriptCount is the number of <script> tags in the HTML.
	ScriptCount int
	// HasLoginWall indicates a login/redirect wall before content.
	HasLoginWall bool
}

// StaticRenderlessEscalation decides when static fetch must escalate to full browser.
// Deprecated: use ShouldEscalate with EscalationSignals for the full
// signal set. This function is kept for backward compatibility.
func StaticRenderlessEscalation(statusCode int, bodyLen int, infiniteScroll bool) RenderMode {
	return ShouldEscalate(EscalationSignals{
		StatusCode:     statusCode,
		BodyLen:        bodyLen,
		InfiniteScroll: infiniteScroll,
	})
}

// ShouldEscalate evaluates the full escalation-signal set and returns
// RenderModeEscalated when the page requires a full browser path, or
// RenderModeStatic when the renderless/static path is sufficient.
//
// Escalation triggers (any one is sufficient):
//   - HTTP status >= 400 (error pages may be JS-rendered challenges)
//   - Body length < 64 bytes (empty/shell response)
//   - Infinite scroll pattern detected
//   - CAPTCHA or bot-challenge detected
//   - SPA shell with no meaningful body content
//   - Web Components (custom elements, shadow DOM)
//   - Service Worker registration
//   - Login wall / auth redirect
//   - Very high script count (>15) with no body content (JS-heavy shell)
func ShouldEscalate(s EscalationSignals) RenderMode {
	if s.StatusCode >= 400 || s.BodyLen < 64 {
		return RenderModeEscalated
	}
	if s.InfiniteScroll || s.HasCAPTCHA || s.IsSPA {
		return RenderModeEscalated
	}
	if s.HasWebComponents || s.HasServiceWorker || s.HasLoginWall {
		return RenderModeEscalated
	}
	// JS-heavy shell: many scripts but no real body content
	if !s.BodyHasContent && s.ScriptCount > 15 {
		return RenderModeEscalated
	}
	return RenderModeStatic
}

// DetectEscalationSignals scans an HTML body and extracts escalation
// signals for the ShouldEscalate decision. This is a fast string-scan
// pass (no full parse) that runs after the static fetch.
func DetectEscalationSignals(statusCode int, body []byte) EscalationSignals {
	s := EscalationSignals{
		StatusCode: statusCode,
		BodyLen:    len(body),
	}
	bodyStr := string(body)
	lower := strings.ToLower(bodyStr)

	// Infinite scroll: common patterns
	if strings.Contains(lower, "infinite-scroll") ||
		strings.Contains(lower, "infinite_scroll") ||
		strings.Contains(lower, "load-more") ||
		strings.Contains(lower, "data-infinite") ||
		strings.Contains(lower, "intersectionobserver") {
		s.InfiniteScroll = true
	}

	// CAPTCHA / bot challenge
	if strings.Contains(lower, "captcha") ||
		strings.Contains(lower, "recaptcha") ||
		strings.Contains(lower, "hcaptcha") ||
		strings.Contains(lower, "turnstile") ||
		strings.Contains(lower, "challenge-platform") ||
		strings.Contains(lower, "cf-challenge") ||
		strings.Contains(lower, "px-captcha") {
		s.HasCAPTCHA = true
	}

	// SPA detection: common root mounts with minimal content
	if strings.Contains(lower, `<div id="root"`) ||
		strings.Contains(lower, `<div id="app"`) ||
		strings.Contains(lower, `<div id="__next"`) ||
		strings.Contains(lower, `<div id="__nuxt"`) {
		// Check if body has meaningful content beyond the mount point.
		// Heuristic: if <body> has < 200 chars of text outside script/style
		// tags, it's likely a shell.
		s.IsSPA = true
	}

	// Web Components
	if strings.Contains(lower, "customelements.define") ||
		strings.Contains(lower, "custom-elements") ||
		strings.Contains(lower, "shadowroot") ||
		strings.Contains(lower, "attachshadow") {
		s.HasWebComponents = true
	}

	// Service Worker
	if strings.Contains(lower, "serviceworker.register") ||
		strings.Contains(lower, "navigator.serviceworker") {
		s.HasServiceWorker = true
	}

	// Login wall: redirect to login, auth-required meta
	if strings.Contains(lower, `redirect="/login"`) ||
		strings.Contains(lower, "login required") ||
		strings.Contains(lower, "authentication required") ||
		strings.Contains(lower, `meta http-equiv="refresh"`) && strings.Contains(lower, "login") {
		s.HasLoginWall = true
	}

	// Script count (rough count of <script tags)
	s.ScriptCount = strings.Count(lower, "<script")

	// Body has content: check for <article>, <main>, or substantial <p> content
	s.BodyHasContent = strings.Contains(lower, "<article") ||
		strings.Contains(lower, "<main") ||
		strings.Count(lower, "<p") >= 3 ||
		strings.Count(lower, "<h1") >= 1 && strings.Count(lower, "<p") >= 1

	return s
}
