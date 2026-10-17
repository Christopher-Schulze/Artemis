package security

import (
	"context"
	"embed"
	"fmt"
	"hash/fnv"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
	"golang.org/x/net/publicsuffix"
)

// adblock.go is the Artemis ad/tracker blocker (spec L4206-L4210). Layer 1
// matches every admitted subresource against the embedded base list, custom
// operator rules and the active EasyList/EasyPrivacy version; a newly
// downloaded version is rolled out to half of all sessions for the rollout
// delay and promoted only without excessive breakage. Layer 2 consumers
// whitelist sites through the persistent whitelist and report breakages.
// The blocker implements network.RequestFilter, so the Chromium Fetch
// interception and the renderless subresource path share one decision.

// AdBlockCategory enumerates blocklist categories.
type AdBlockCategory string

const (
	AdBlockCategoryAd      AdBlockCategory = "ad"
	AdBlockCategoryTracker AdBlockCategory = "tracker"
	AdBlockCategorySocial  AdBlockCategory = "social"
	AdBlockCategoryConsent AdBlockCategory = "consent"
)

// AdBlockResult is the result of an ad/tracker check.
type AdBlockResult struct {
	Blocked  bool            `json:"blocked"`
	Category AdBlockCategory `json:"category,omitempty"`
	Pattern  string          `json:"pattern,omitempty"`
	Reason   string          `json:"reason,omitempty"`
}

//go:embed adblock_base.txt
var adBlockBaseFS embed.FS

type categorizedRules struct {
	category AdBlockCategory
	rules    *AdBlockRuleSet
}

// AdBlockList is one compiled downloaded list version.
type AdBlockList struct {
	Manifest  AdBlockListManifest
	sets      []categorizedRules
	activated time.Time
}

// AdBlockRolloutConfig bounds the gradual rollout of a new list version.
type AdBlockRolloutConfig struct {
	Percent        int           // sessions on a candidate during the delay, default 50
	Delay          time.Duration // candidate phase, default 1h
	MaxBreakages   int           // rollback above this many breakages, default 5
	BreakageWindow time.Duration // default 1h
}

func (c AdBlockRolloutConfig) withDefaults() AdBlockRolloutConfig {
	if c.Percent <= 0 || c.Percent > 100 {
		c.Percent = 50
	}
	if c.Delay <= 0 {
		c.Delay = time.Hour
	}
	if c.MaxBreakages <= 0 {
		c.MaxBreakages = 5
	}
	if c.BreakageWindow <= 0 {
		c.BreakageWindow = time.Hour
	}
	return c
}

// AdBlocker is safe for concurrent use.
type AdBlocker struct {
	enabled   atomic.Bool
	blocked   atomic.Int64
	whitelist *AdBlockerDetection
	rollout   AdBlockRolloutConfig
	now       func() time.Time

	mu          sync.RWMutex
	base        []categorizedRules
	custom      *AdBlockRuleSet
	customLines []string
	stable      *AdBlockList
	previous    *AdBlockList
	candidate   *AdBlockList
	rejected    map[string]bool // list SHA-256 values rolled back for breakage
	breakages   []time.Time
}

var _ network.RequestFilter = (*AdBlocker)(nil)

// NewAdBlocker returns an enabled blocker with the embedded base list and an
// in-memory site whitelist.
func NewAdBlocker() *AdBlocker {
	blocker, err := NewAdBlockerWithConfig(nil, AdBlockRolloutConfig{})
	if err != nil {
		// The embedded list is part of the binary; failing to compile it is a
		// build defect caught by tests, never a runtime condition.
		return &AdBlocker{rollout: AdBlockRolloutConfig{}.withDefaults(), now: time.Now, rejected: map[string]bool{}, whitelist: &AdBlockerDetection{whitelist: map[string]bool{}}}
	}
	return blocker
}

// NewAdBlockerWithConfig returns an enabled blocker using whitelist for site
// exceptions; nil keeps the whitelist in memory.
func NewAdBlockerWithConfig(whitelist *AdBlockerDetection, rollout AdBlockRolloutConfig) (*AdBlocker, error) {
	base, err := loadBaseAdBlockRules()
	if err != nil {
		return nil, err
	}
	if whitelist == nil {
		if whitelist, err = NewAdBlockerDetection(""); err != nil {
			return nil, err
		}
	}
	blocker := &AdBlocker{whitelist: whitelist, rollout: rollout.withDefaults(), now: time.Now, base: base, rejected: map[string]bool{}}
	blocker.enabled.Store(true)
	return blocker, nil
}

func loadBaseAdBlockRules() ([]categorizedRules, error) {
	raw, err := adBlockBaseFS.ReadFile("adblock_base.txt")
	if err != nil {
		return nil, fmt.Errorf("adblock base list: %w", err)
	}
	sections := map[AdBlockCategory]*strings.Builder{}
	order := []AdBlockCategory{}
	category := AdBlockCategoryAd
	for _, line := range strings.Split(string(raw), "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "! category: "); ok {
			category = AdBlockCategory(name)
			continue
		}
		if sections[category] == nil {
			sections[category] = &strings.Builder{}
			order = append(order, category)
		}
		sections[category].WriteString(line + "\n")
	}
	base := make([]categorizedRules, 0, len(order))
	for _, name := range order {
		rules, err := ParseAdBlockRules(strings.NewReader(sections[name].String()))
		if err != nil {
			return nil, fmt.Errorf("adblock base list %s: %w", name, err)
		}
		base = append(base, categorizedRules{category: name, rules: rules})
	}
	return base, nil
}

// Enable turns blocking on (the default).
func (a *AdBlocker) Enable() {
	if a != nil {
		a.enabled.Store(true)
	}
}

// Disable turns blocking off (spec L4210: disableable in settings).
func (a *AdBlocker) Disable() {
	if a != nil {
		a.enabled.Store(false)
	}
}

// IsEnabled reports whether blocking is on.
func (a *AdBlocker) IsEnabled() bool {
	return a != nil && a.enabled.Load()
}

// AddPattern blocks domain and its subdomains (operator rule).
func (a *AdBlocker) AddPattern(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || strings.ContainsAny(domain, "/^*|$ ") {
		return fmt.Errorf("adblock pattern: invalid domain %q", domain)
	}
	return a.AddRules("||" + domain + "^")
}

// AddRules adds operator filters in ABP syntax ahead of the lists. Lines the
// engine cannot apply are rejected so an operator rule is never silently
// ignored.
func (a *AdBlocker) AddRules(lines ...string) error {
	if a == nil {
		return fmt.Errorf("adblock rules: blocker required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	candidate := append(append([]string(nil), a.customLines...), lines...)
	rules, err := ParseAdBlockRules(strings.NewReader(strings.Join(candidate, "\n")))
	if err != nil {
		return fmt.Errorf("adblock rules: %w", err)
	}
	if stats := rules.Stats(); stats.Unsupported > 0 || stats.Cosmetic > 0 {
		return fmt.Errorf("adblock rules: %d unsupported or cosmetic filters", stats.Unsupported+stats.Cosmetic)
	}
	a.customLines, a.custom = candidate, rules
	return nil
}

// PatternCount returns the number of active blocking and exception rules.
func (a *AdBlocker) PatternCount() int {
	if a == nil {
		return 0
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	count := 0
	for _, set := range a.base {
		count += set.rules.stats.Blocking + set.rules.stats.Exceptions
	}
	if a.custom != nil {
		count += a.custom.stats.Blocking
	}
	if a.stable != nil {
		count += a.stable.Manifest.Rules
	}
	return count
}

// BlockedTotal returns how many requests this blocker dropped.
func (a *AdBlocker) BlockedTotal() int64 {
	if a == nil {
		return 0
	}
	return a.blocked.Load()
}

// Whitelist returns the persistent site whitelist, or nil.
func (a *AdBlocker) Whitelist() *AdBlockerDetection {
	if a == nil {
		return nil
	}
	return a.whitelist
}

// Check reports whether a third-party request to domain is blocked.
func (a *AdBlocker) Check(domain string) AdBlockResult {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !a.IsEnabled() || domain == "" {
		return AdBlockResult{}
	}
	return a.match(AdBlockRequest{URL: "https://" + domain + "/", Host: domain, Resource: ResourceOther, ThirdParty: true}, "")
}

// IsBlocked reports whether a third-party request to domain is blocked.
func (a *AdBlocker) IsBlocked(domain string) bool {
	return a.Check(domain).Blocked
}

// BlockRequest implements network.RequestFilter.
func (a *AdBlocker) BlockRequest(ctx context.Context, target *url.URL, kind network.TargetKind, sessionID string) bool {
	if !a.IsEnabled() || target == nil {
		return false
	}
	scope, _ := network.AdBlockScopeFrom(ctx)
	if scope.Bypass {
		return false
	}
	site := strings.ToLower(scope.SiteHost)
	if site != "" && a.whitelist != nil && a.whitelist.IsWhitelisted(site) {
		return false
	}
	host := strings.ToLower(target.Hostname())
	request := AdBlockRequest{
		URL:        target.String(),
		Host:       host,
		SiteHost:   site,
		Resource:   adBlockResource(scope.Resource, kind),
		ThirdParty: isThirdParty(host, site),
	}
	if !a.match(request, sessionID).Blocked {
		return false
	}
	a.blocked.Add(1)
	if scope.Blocked != nil {
		scope.Blocked.Add(1)
	}
	return true
}

func (a *AdBlocker) match(request AdBlockRequest, sessionID string) AdBlockResult {
	a.mu.RLock()
	sets := make([]categorizedRules, 0, len(a.base)+4)
	sets = append(sets, a.base...)
	if a.custom != nil {
		sets = append(sets, categorizedRules{category: AdBlockCategoryAd, rules: a.custom})
	}
	list := a.listForSessionLocked(sessionID)
	a.mu.RUnlock()
	if list != nil {
		sets = append(sets, list.sets...)
	}
	for _, set := range sets {
		if blocked, rule := set.rules.Match(request); blocked {
			return AdBlockResult{Blocked: true, Category: set.category, Pattern: rule, Reason: "matched " + string(set.category) + " filter"}
		}
	}
	return AdBlockResult{}
}

// listForSessionLocked returns the candidate for sessions inside the rollout
// bucket, otherwise the stable version. Promotion happens in Install and
// Advance, never under a read lock.
func (a *AdBlocker) listForSessionLocked(sessionID string) *AdBlockList {
	if a.candidate != nil && rolloutBucket(sessionID) < a.rollout.Percent {
		return a.candidate
	}
	return a.stable
}

func rolloutBucket(sessionID string) int {
	hash := fnv.New32a()
	if _, err := hash.Write([]byte(sessionID)); err != nil {
		return 0
	}
	return int(hash.Sum32() % 100)
}

// Install activates a verified list. The first version becomes stable at
// once; later versions start as rollout candidates. A version rejected for
// breakage is never reinstalled.
func (a *AdBlocker) Install(list *AdBlockList) error {
	if a == nil || list == nil {
		return fmt.Errorf("adblock install: blocker and list required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rejected[list.Manifest.SHA256] {
		return fmt.Errorf("adblock install: version %s was rolled back for breakage", list.Manifest.SHA256)
	}
	if list.activated.IsZero() {
		list.activated = a.now()
	}
	if a.stable == nil {
		a.stable = list
		return nil
	}
	if a.stable.Manifest.SHA256 == list.Manifest.SHA256 {
		return nil
	}
	a.candidate = list
	a.breakages = a.breakages[:0]
	return nil
}

// Advance promotes a candidate whose rollout delay passed without excessive
// breakage and returns the active manifests.
func (a *AdBlocker) Advance() (stable, candidate *AdBlockListManifest) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.candidate != nil && a.now().Sub(a.candidate.activated) >= a.rollout.Delay && !a.overBreakageLocked() {
		a.previous, a.stable, a.candidate = a.stable, a.candidate, nil
	}
	return manifestOf(a.stable), manifestOf(a.candidate)
}

func manifestOf(list *AdBlockList) *AdBlockListManifest {
	if list == nil {
		return nil
	}
	manifest := list.Manifest
	return &manifest
}

// RecordBreakage records a page that broke under blocking (Layer 2 Case B).
// More than MaxBreakages within the window rolls the newest version back:
// a candidate is dropped, a recently promoted version reverts to the
// previous one. It returns true when a rollback happened.
func (a *AdBlocker) RecordBreakage() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	a.breakages = append(a.breakages, now)
	if !a.overBreakageLocked() {
		return false
	}
	switch {
	case a.candidate != nil:
		a.rejected[a.candidate.Manifest.SHA256] = true
		a.candidate = nil
	case a.stable != nil && a.previous != nil && now.Sub(a.stable.activated) < a.rollout.Delay+a.rollout.BreakageWindow:
		a.rejected[a.stable.Manifest.SHA256] = true
		a.stable, a.previous = a.previous, nil
	default:
		return false
	}
	a.breakages = a.breakages[:0]
	return true
}

func (a *AdBlocker) overBreakageLocked() bool {
	cutoff := a.now().Add(-a.rollout.BreakageWindow)
	kept := a.breakages[:0]
	for _, at := range a.breakages {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	a.breakages = kept
	return len(kept) > a.rollout.MaxBreakages
}

// IsRejected reports whether a list SHA-256 was rolled back for breakage.
func (a *AdBlocker) IsRejected(sha string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rejected[sha]
}

func adBlockResource(resource string, kind network.TargetKind) AdBlockResource {
	switch strings.ToLower(resource) {
	case "script":
		return ResourceScript
	case "image", "imageset":
		return ResourceImage
	case "stylesheet":
		return ResourceStylesheet
	case "font":
		return ResourceFont
	case "media", "texttrack":
		return ResourceMedia
	case "xhr", "fetch", "eventsource", "xmlhttprequest":
		return ResourceXHR
	case "ping", "cspviolationreport", "beacon":
		return ResourcePing
	case "websocket":
		return ResourceWebSocket
	case "document", "subdocument":
		return ResourceSubdocument
	case "object":
		return ResourceObject
	}
	switch kind {
	case network.TargetSubframe:
		return ResourceSubdocument
	case network.TargetWebSocket:
		return ResourceWebSocket
	case network.TargetWorker:
		return ResourceScript
	}
	return ResourceOther
}

// isThirdParty compares registrable domains; an unknown site counts as
// third party.
func isThirdParty(host, site string) bool {
	if site == "" {
		return true
	}
	if host == site {
		return false
	}
	hostDomain, hostErr := publicsuffix.EffectiveTLDPlusOne(host)
	siteDomain, siteErr := publicsuffix.EffectiveTLDPlusOne(site)
	if hostErr != nil || siteErr != nil {
		return true
	}
	return hostDomain != siteDomain
}

// String returns a diagnostic summary.
func (a *AdBlocker) String() string {
	if a == nil {
		return "AdBlocker(nil)"
	}
	return fmt.Sprintf("AdBlocker{enabled:%v patterns:%d}", a.IsEnabled(), a.PatternCount())
}

// String returns a diagnostic summary.
func (r AdBlockResult) String() string {
	return fmt.Sprintf("AdBlockResult{blocked:%v category:%s pattern:%s}", r.Blocked, r.Category, r.Pattern)
}
