package artemis

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/cdpops"
	"github.com/Christopher-Schulze/Artemis/observe"
	"github.com/Christopher-Schulze/Artemis/security"
)

// adblock_layer2.go applies AdBlocker Layer 2 (spec L4208,
// security/adblock_layer2.go) to Chromium pages opened through a session and
// exposes the whitelist decision and maintenance path. Renderless pages get
// Layer 2 inside engine.Fetch. Verification runs only when blocking dropped
// at least one request of the page.

// adBlocker returns the agent's blocker, or nil when blocking is disabled.
func (a *Agent) adBlocker() *security.AdBlocker {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config.AdBlock
}

// AdBlockWhitelist lists, adds or removes whitelisted sites: the user
// decision for Case A and the operator maintenance path (spec L4208).
func (a *Agent) AdBlockWhitelist(operation, domain string) ([]string, *TaskError) {
	blocker := a.adBlocker()
	if blocker == nil {
		return nil, newTaskError(TaskErrorCapabilityUnavailable, "adblock_whitelist", errors.New("ad blocking is disabled"))
	}
	whitelist := blocker.Whitelist()
	var err error
	switch operation {
	case "list", "":
	case "add":
		err = whitelist.AddToWhitelist(domain)
	case "remove":
		err = whitelist.RemoveFromWhitelist(domain)
	default:
		err = fmt.Errorf("unknown whitelist operation %q", operation)
	}
	if err != nil {
		return nil, newTaskError(TaskErrorInvalidInput, "adblock_whitelist", err)
	}
	return whitelist.WhitelistDomains(), nil
}

// AdBlockFinding returns the Layer 2 finding recorded when pageID opened.
func (s *Session) AdBlockFinding(pageID string) *security.AdBlockFinding {
	if page := s.Page(pageID); page != nil {
		return page.AdBlockFinding()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.adBlockFindings[pageID].Clone()
}

func (s *Session) recordAdBlockFinding(pageID string, finding *security.AdBlockFinding) {
	if finding == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.adBlockFindings == nil {
		s.adBlockFindings = make(map[string]*security.AdBlockFinding)
	}
	s.adBlockFindings[pageID] = finding
}

type probeEvaluateParams struct {
	Expression    string `json:"expression"`
	ReturnByValue bool   `json:"returnByValue"`
}

type probeEvaluateResult struct {
	Result struct {
		Value string `json:"value"`
	} `json:"result"`
	ExceptionDetails *struct {
		Text string `json:"text"`
	} `json:"exceptionDetails"`
}

func chromiumAdBlockProbe(ctx context.Context, page *bridge.Page) (security.AdBlockProbe, error) {
	var result probeEvaluateResult
	if err := page.Call(ctx, "Runtime.evaluate", probeEvaluateParams{Expression: security.AdBlockProbeScript(true), ReturnByValue: true}, &result); err != nil {
		return security.AdBlockProbe{}, fmt.Errorf("adblock probe: %w", err)
	}
	if result.ExceptionDetails != nil {
		return security.AdBlockProbe{}, fmt.Errorf("adblock probe: %s", result.ExceptionDetails.Text)
	}
	return security.ParseAdBlockProbe(result.Result.Value)
}

func consoleErrors(evidence observe.ObservationEvidence) int {
	count := 0
	for _, entry := range evidence.Console {
		if entry.Level == observe.ConsoleLevelError {
			count++
		}
	}
	return count
}

func siteOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// chromiumAdBlockLayer2 applies Layer 2 to a session page and re-observes
// it after a whitelisting reload.
func (s *Session) chromiumAdBlockLayer2(ctx context.Context, page *ownedChromiumPage, targetURL string, evidence observe.ObservationEvidence) (observe.ObservationEvidence, *security.AdBlockFinding) {
	reloaded := false
	finding := ChromiumAdBlockLayer2(ctx, s.owner.adBlocker(), page.page, targetURL, consoleErrors(evidence), func(ctx context.Context) error {
		result := cdpops.NewNavigator(page.page).Navigate(ctx, cdpops.NavigationRequest{URL: targetURL, WaitUntil: cdpops.WaitLoad, Timeout: 30 * time.Second})
		if !result.Success {
			return fmt.Errorf("reload: %s", result.Error)
		}
		reloaded = true
		return nil
	})
	if !reloaded {
		return evidence, finding
	}
	recaptured, err := page.observer.CaptureEvidence(ctx)
	if err != nil {
		finding.Signals = append(finding.Signals, "reload_capture_failed: "+err.Error())
		return evidence, finding
	}
	return recaptured, finding
}

// ChromiumAdBlockLayer2 applies Layer 2 (spec L4208) to a loaded Chromium
// page. jsErrors counts the page's console errors and uncaught exceptions.
// Case B is verified in a sibling target without blocking, so the page
// stays untouched unless the unblocked load proves better; then the site is
// whitelisted, the breakage recorded and reload loads the page again.
func ChromiumAdBlockLayer2(ctx context.Context, blocker *security.AdBlocker, page *bridge.Page, targetURL string, jsErrors int, reload func(context.Context) error) *security.AdBlockFinding {
	blocked := page.AdBlockedRequests()
	if blocker == nil || !blocker.IsEnabled() || blocked == 0 {
		return nil
	}
	site := siteOf(targetURL)
	probe, err := chromiumAdBlockProbe(ctx, page)
	if err != nil {
		return &security.AdBlockFinding{Site: site, BlockedRequests: blocked, Signals: []string{err.Error()}, Decision: security.AdBlockDecisionUnavailable}
	}
	finding := security.ClassifyAdBlock(probe, blocked, jsErrors, site)
	if finding == nil || finding.Case != security.AdBlockCaseBreakage {
		return finding
	}
	retryProbe, err := verifyWithoutBlocking(ctx, page, targetURL)
	if err != nil || !security.ImprovedWithoutBlocking(probe, retryProbe) {
		if err != nil {
			finding.Signals = append(finding.Signals, "verification_failed: "+err.Error())
		} else {
			finding.Signals = append(finding.Signals, fmt.Sprintf("verification_text=%d", retryProbe.TextLength))
		}
		return finding
	}
	if err := blocker.AcceptBreakage(site); err != nil {
		finding.Signals = append(finding.Signals, "whitelist_failed: "+err.Error())
		return finding
	}
	finding.Decision = security.AdBlockDecisionWhitelisted
	finding.Signals = append(finding.Signals, fmt.Sprintf("verification_text=%d", retryProbe.TextLength))
	if err := reload(ctx); err != nil {
		finding.Signals = append(finding.Signals, "reload_failed: "+err.Error())
	}
	return finding
}

func verifyWithoutBlocking(ctx context.Context, page *bridge.Page, targetURL string) (probe security.AdBlockProbe, returnErr error) {
	sibling, err := page.NewSibling(ctx, "about:blank")
	if err != nil {
		return security.AdBlockProbe{}, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, sibling.Close())
	}()
	sibling.SetAdBlockBypass(true)
	result := cdpops.NewNavigator(sibling).Navigate(ctx, cdpops.NavigationRequest{URL: targetURL, WaitUntil: cdpops.WaitLoad, Timeout: 30 * time.Second})
	if !result.Success {
		return security.AdBlockProbe{}, fmt.Errorf("verification load: %s", result.Error)
	}
	return chromiumAdBlockProbe(ctx, sibling)
}
