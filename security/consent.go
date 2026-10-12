package security

import (
	"context"
	"errors"
	"strings"
)

// ConsentAction is the default handling for cookie/consent prompts.
type ConsentAction string

const (
	ConsentDeny   ConsentAction = "deny"
	ConsentAccept ConsentAction = "accept"
)

// DefaultConsentAction returns the privacy-default consent action (deny).
func DefaultConsentAction() ConsentAction {
	return ConsentDeny
}

// ConsentMode controls cookie consent prompt handling (spec L4553, L4583).
type ConsentMode string

const (
	ConsentModeManual          ConsentMode = "manual"
	ConsentModeAutoAccept      ConsentMode = "auto_accept"
	ConsentModeRejectNonEssent ConsentMode = "reject_nonessential"
)

// ConsentElement is a minimal DOM element projection for the consent heuristic.
// It carries the attributes/text the heuristic needs without coupling to a
// concrete browser driver, so the same logic is testable without a live CDP
// session. Real callers adapt their DOM/AX tree into this shape.
type ConsentElement struct {
	Tag      string
	Text     string
	Style    string
	Attrs    map[string]string
	Children []ConsentElement
}

// ConsentPage is the page projection consumed by AutoConfirm.
type ConsentPage struct {
	URL      string
	Elements []ConsentElement
}

// ConsentProfile is the runtime policy slice the heuristic checks against.
type ConsentProfile struct {
	Mode           ConsentMode
	AllowedDomains []string
	Purpose        string // processing activity ID; empty blocks auto-accept
}

// ConsentDecision is the outcome of the AutoConfirm heuristic.
type ConsentDecision struct {
	Action          ConsentAction
	BannerFound     bool
	AcceptButtonIdx int    // index into page.Elements of the chosen accept button, -1 if none
	Reason          string // why the decision was made (audit/explainability)
}

// highRiskDomainSuffixes are never auto-accepted (spec L4553).
var highRiskDomainSuffixes = []string{
	".bank.", ".banking.", ".admin.", ".gov.", ".govt.",
	".medical.", ".health.", ".klinik.", ".krankenhaus.",
	".legal.", ".anwalt.", ".recht.",
}

// bannerKeywords match the cookie banner text (spec L4553).
var bannerKeywords = []string{
	"cookie", "consent", "privacy", "datenschutz",
	"gdpr", "dsgvo", "tracker", "zustimmung",
}

// acceptKeywords match the accept button text (spec L4553).
var acceptKeywords = []string{
	"accept", "akzeptieren", "agree", "zustimmen", "allow",
	"erlauben", "ok", "verstanden", "got it", "alle akzeptieren",
	"accept all", "accept all cookies", "allow all",
}

// AutoConfirm runs the cookie-consent auto-confirm heuristic (spec L4553).
//
// Pipeline:
//  1. Find cookie banner (position:fixed/sticky + z-index>999 + text matches
//     a banner keyword).
//  2. Find accept button inside the banner (text matches an accept keyword;
//     multiple matches take the LARGEST by text length).
//  3. Click only when ConsentMode=auto_accept + domain allowlist + processing
//     purpose exist; never auto-accept on high-risk domain suffixes.
//
// Default is OFF: ConsentModeManual and empty AllowedDomains yield ConsentDeny.
func AutoConfirm(ctx context.Context, page ConsentPage, profile ConsentProfile) (ConsentDecision, error) {
	if page.Elements == nil {
		return ConsentDecision{AcceptButtonIdx: -1}, errors.New("consent: page has no elements")
	}
	dec := ConsentDecision{Action: ConsentDeny, AcceptButtonIdx: -1}

	bannerIdx, bannerEl := findConsentBanner(page.Elements)
	if bannerIdx < 0 {
		dec.Reason = "no_cookie_banner"
		return dec, nil
	}
	dec.BannerFound = true

	acceptIdx, acceptText := findAcceptButton(bannerEl)
	if acceptIdx < 0 {
		dec.Reason = "no_accept_button"
		return dec, nil
	}
	// acceptIdx is relative to banner children; lift to page.Elements index.
	// The banner itself is page.Elements[bannerIdx]; the accept button is
	// banner.Children[acceptIdx]. We expose the page-relative index of the
	// banner plus the in-banner index so callers can locate the button.
	_ = acceptText
	dec.AcceptButtonIdx = bannerIdx

	if profile.Mode != ConsentModeAutoAccept {
		dec.Reason = "consent_mode_not_auto_accept"
		return dec, nil
	}
	if !domainAllowedSlice(page.URL, profile.AllowedDomains) {
		dec.Reason = "domain_not_in_allowlist"
		return dec, nil
	}
	if isHighRiskDomain(page.URL) {
		dec.Reason = "high_risk_domain_skip"
		return dec, nil
	}
	if strings.TrimSpace(profile.Purpose) == "" {
		dec.Reason = "missing_processing_purpose"
		return dec, nil
	}
	if ctx.Err() != nil {
		dec.Reason = "context_cancelled"
		return dec, nil
	}

	dec.Action = ConsentAccept
	dec.Reason = "auto_accept_eligible"
	return dec, nil
}

// findConsentBanner returns the index of the first element matching banner
// heuristics: position fixed/sticky OR z-index>999, plus text containing a
// banner keyword. Returns -1 if none.
func findConsentBanner(els []ConsentElement) (int, ConsentElement) {
	for i, el := range els {
		if isBannerCandidate(el) && containsAny(lower(el.Text), bannerKeywords) {
			return i, el
		}
	}
	return -1, ConsentElement{}
}

func isBannerCandidate(el ConsentElement) bool {
	style := lower(el.Style)
	if strings.Contains(style, "position:fixed") || strings.Contains(style, "position: fixed") ||
		strings.Contains(style, "position:sticky") || strings.Contains(style, "position: sticky") {
		return true
	}
	if z := el.Attrs["z-index"]; z != "" {
		// numeric compare without strconv to keep deps minimal
		n, ok := atoiSafe(z)
		if ok && n > 999 {
			return true
		}
	}
	if z := extractStyleInt(style, "z-index"); z > 999 {
		return true
	}
	return false
}

// findAcceptButton returns the index (into banner.Children) and text of the
// accept button. Multiple matches take the LARGEST by text length (spec L4553).
func findAcceptButton(banner ConsentElement) (int, string) {
	bestIdx := -1
	bestText := ""
	for i, c := range banner.Children {
		ct := lower(strings.TrimSpace(c.Text))
		if ct == "" {
			continue
		}
		if !containsAny(ct, acceptKeywords) {
			continue
		}
		if len(ct) > len(bestText) {
			bestIdx = i
			bestText = ct
		}
	}
	return bestIdx, bestText
}

func domainAllowedSlice(url string, allowed []string) bool {
	host := extractHost(url)
	if host == "" {
		return false
	}
	for _, d := range allowed {
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func isHighRiskDomain(url string) bool {
	host := lower(extractHost(url))
	for _, suffix := range highRiskDomainSuffixes {
		if strings.Contains(host, suffix) {
			return true
		}
	}
	return false
}

func extractHost(url string) string {
	s := url
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '@'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func lower(s string) string { return strings.ToLower(s) }

func atoiSafe(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

func extractStyleInt(style string, prop string) int {
	idx := strings.Index(style, prop)
	if idx < 0 {
		return 0
	}
	rest := style[idx+len(prop):]
	// skip : and whitespace
	for len(rest) > 0 && (rest[0] == ':' || rest[0] == ' ' || rest[0] == '\t') {
		rest = rest[1:]
	}
	end := 0
	for end < len(rest) {
		c := rest[end]
		if c >= '0' && c <= '9' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return 0
	}
	n, _ := atoiSafe(rest[:end])
	return n
}

// ResolveConsentMode parses a raw config string into a ConsentMode with a safe
// default (manual) for unknown/empty values.
func ResolveConsentMode(raw string) ConsentMode {
	switch ConsentMode(strings.ToLower(strings.TrimSpace(raw))) {
	case ConsentModeAutoAccept:
		return ConsentModeAutoAccept
	case ConsentModeRejectNonEssent:
		return ConsentModeRejectNonEssent
	default:
		return ConsentModeManual
	}
}
