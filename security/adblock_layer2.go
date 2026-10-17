package security

import (
	"encoding/json"
	"fmt"
)

// adblock_layer2.go holds the engine-independent AdBlocker Layer 2 decision
// logic (spec L4208); the renderless engine and the Chromium session apply
// it to their pages.
//   - Case A: an adblocker-wall overlay (notice text plus notice CSS class
//     or a position:fixed z-index>=9999 layer) is reported for the user to
//     decide; the decision is a whitelist entry plus a reload.
//   - Case B: more than five blocked requests with script errors or missing
//     main content trigger a verification load without blocking; a better
//     load whitelists the site, records the breakage for list rollback and
//     replaces the broken page.

// Layer 2 cases and decisions.
const (
	AdBlockCaseOverlay  = "explicit_overlay"
	AdBlockCaseBreakage = "subtle_breakage"

	AdBlockDecisionUser        = "user_decision_required"
	AdBlockDecisionWhitelisted = "auto_whitelisted"
	AdBlockDecisionKept        = "blocking_kept"
	AdBlockDecisionUnavailable = "detection_failed"
)

// AdBlockFinding reports a Layer 2 decision for one page.
type AdBlockFinding struct {
	Case            string   `json:"case,omitempty"`
	Site            string   `json:"site"`
	BlockedRequests int64    `json:"blockedRequests"`
	Signals         []string `json:"signals,omitempty"`
	Decision        string   `json:"decision"`
}

// Clone returns an independent copy.
func (f *AdBlockFinding) Clone() *AdBlockFinding {
	if f == nil {
		return nil
	}
	copied := *f
	copied.Signals = append([]string(nil), f.Signals...)
	return &copied
}

// MinCriticalTextLength is the main-content floor below which a page with
// heavy blocking counts as missing its critical elements.
const MinCriticalTextLength = 200

// AdBlockProbeScript returns the script that reads the Layer 2 signals in
// one evaluation. computedStyle selects getComputedStyle for the fixed
// overlay check (Chromium); without it only inline styles are read, because
// the renderless cascade records unsupported computed properties as page
// failures.
func AdBlockProbeScript(computedStyle bool) string {
	style := "node.style"
	if computedStyle {
		style = "getComputedStyle(node)"
	}
	return `(() => {
  const body = document.body;
  const text = body ? (body.innerText || body.textContent || "") : "";
  const classes = [];
  let fixedOverlay = false;
  const nodes = document.getElementsByTagName("*");
  for (let i = 0; i < nodes.length && i < 5000; i++) {
    const node = nodes[i];
    const name = typeof node.className === "string" ? node.className : "";
    if (name || node.id) classes.push(name + " " + (node.id || ""));
    const style = ` + style + `;
    if (!fixedOverlay && style && style.position === "fixed" && parseInt(style.zIndex || "0", 10) >= 9999) fixedOverlay = true;
  }
  return JSON.stringify({text: text.slice(0, 20000), textLength: text.trim().length, classes: classes.join(" ").slice(0, 20000), fixedOverlay, nodes: nodes.length});
})()`
}

// AdBlockProbe is the result of AdBlockProbeScript.
type AdBlockProbe struct {
	Text         string `json:"text"`
	TextLength   int    `json:"textLength"`
	Classes      string `json:"classes"`
	FixedOverlay bool   `json:"fixedOverlay"`
	Nodes        int    `json:"nodes"`
}

// ParseAdBlockProbe decodes the probe script result.
func ParseAdBlockProbe(raw string) (AdBlockProbe, error) {
	var probe AdBlockProbe
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return AdBlockProbe{}, fmt.Errorf("adblock probe: %w", err)
	}
	return probe, nil
}

// ClassifyAdBlock applies the Layer 2 detectors; nil means neither case.
// Case B findings start as AdBlockDecisionKept until verification proves
// the unblocked load better.
func ClassifyAdBlock(probe AdBlockProbe, blocked int64, jsErrors int, site string) *AdBlockFinding {
	texts := MatchAdBlockerText(probe.Text)
	classes := MatchAdBlockerCSS(probe.Classes)
	if DetectExplicitOverlay(AdBlockerOverlaySignals{TextMatches: texts, CSSClassMatches: classes, HasFixedOverlay: probe.FixedOverlay}) {
		signals := append(append([]string{}, texts...), classes...)
		if probe.FixedOverlay {
			signals = append(signals, "fixed_overlay")
		}
		return &AdBlockFinding{Case: AdBlockCaseOverlay, Site: site, BlockedRequests: blocked, Signals: signals, Decision: AdBlockDecisionUser}
	}
	missing := 0
	if probe.TextLength < MinCriticalTextLength {
		missing = 1
	}
	if !DetectSubtleBreakage(AdBlockerBreakageSignals{BlockedRequestCount: int(blocked), JSErrors: jsErrors, MissingCriticalElements: missing}) {
		return nil
	}
	signals := []string{fmt.Sprintf("blocked_requests=%d", blocked)}
	if jsErrors > 0 {
		signals = append(signals, fmt.Sprintf("js_errors=%d", jsErrors))
	}
	if missing > 0 {
		signals = append(signals, fmt.Sprintf("main_text=%d", probe.TextLength))
	}
	return &AdBlockFinding{Case: AdBlockCaseBreakage, Site: site, BlockedRequests: blocked, Signals: signals, Decision: AdBlockDecisionKept}
}

// ImprovedWithoutBlocking compares the blocked and the verification load:
// restored main content or at least twice the content counts as better.
func ImprovedWithoutBlocking(blocked, unblocked AdBlockProbe) bool {
	if blocked.TextLength < MinCriticalTextLength && unblocked.TextLength >= MinCriticalTextLength {
		return true
	}
	return unblocked.TextLength >= 2*max(blocked.TextLength, MinCriticalTextLength)
}

// AcceptBreakage whitelists site and records the breakage that may roll a
// new list version back (Case B outcome).
func (a *AdBlocker) AcceptBreakage(site string) error {
	if err := a.whitelist.AddToWhitelist(site); err != nil {
		return err
	}
	a.RecordBreakage()
	return nil
}
