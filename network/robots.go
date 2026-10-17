package network

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// RobotsRule is a single Allow/Disallow line.
type RobotsRule struct {
	Allow bool
	Path  string
}

// RobotsPolicy is the parsed robots.txt for an origin.
type RobotsPolicy struct {
	Groups      map[string][]RobotsRule // user-agent -> rules in declaration order
	CrawlDelays map[string]time.Duration
	Sitemaps    []string
	patterns    map[string]robotsPattern
}

// ParseRobots parses a robots.txt body. User-agent matching is
// case-insensitive; the most specific normalized literal match of
// the matching group wins; ties favour Allow.
func ParseRobots(r io.Reader) (*RobotsPolicy, error) {
	if r == nil {
		return nil, errors.New("robots: reader required")
	}
	body, err := readLimited(r, MaxRobotsDocumentBytes)
	if err != nil {
		return nil, fmt.Errorf("read robots: %w", err)
	}
	body = bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})
	body = bytes.ReplaceAll(body, []byte{'\r'}, []byte{'\n'})
	parser := newRobotsParser()
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 4096), int(MaxRobotsDocumentBytes)+1)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(strings.ToLower(k))
		v = strings.TrimSpace(v)
		if err := parser.record(k, v); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan robots: %w", err)
	}
	if err := parser.finishGroup(); err != nil {
		return nil, err
	}
	return parser.policy, nil
}

// Allowed reports whether the given user-agent is permitted to fetch
// the given URL path. Empty Disallow path means "allow everything".
func (p *RobotsPolicy) Allowed(userAgent, path string) bool {
	if p == nil {
		return true
	}
	if len(path) > 8192 {
		return false
	}
	rules := p.matchGroup(userAgent)
	if len(rules) == 0 {
		return true
	}
	bestLen := -1
	bestAllow := true
	path = normalizeRobotsOctets(path, false)
	if path == "/robots.txt" {
		return true
	}
	for _, r := range rules {
		if r.Path == "" {
			// Disallow with empty path means allow all.
			if !r.Allow {
				continue
			}
		}
		pattern, exists := p.patterns[r.Path]
		if !exists {
			var err error
			pattern, err = compileRobotsPattern(r.Path)
			if err != nil {
				continue
			}
		}
		if !pattern.matches(path) {
			continue
		}
		if pattern.specificity > bestLen || (pattern.specificity == bestLen && r.Allow) {
			bestLen = pattern.specificity
			bestAllow = r.Allow
		}
	}
	if bestLen < 0 {
		return true
	}
	return bestAllow
}

func (p *RobotsPolicy) matchGroup(userAgent string) []RobotsRule {
	var matched []RobotsRule
	for _, agent := range p.matchingAgents(userAgent) {
		matched = append(matched, p.Groups[agent]...)
	}
	return matched
}

func (p *RobotsPolicy) matchingAgents(userAgent string) []string {
	ua := strings.ToLower(userAgent)
	bestLength := -1
	var matched []string
	for agent := range p.Groups {
		if agent == "*" || agent == "" || !strings.Contains(ua, agent) {
			continue
		}
		if len(agent) > bestLength {
			bestLength = len(agent)
			matched = matched[:0]
		}
		if len(agent) == bestLength {
			matched = append(matched, agent)
		}
	}
	if bestLength < 0 {
		if _, exists := p.Groups["*"]; exists {
			return []string{"*"}
		}
		return nil
	}
	sort.Strings(matched)
	return matched
}

// RulesFor returns detached rules selected by the canonical group matcher.
func (p *RobotsPolicy) RulesFor(userAgent string) []RobotsRule {
	if p == nil {
		return nil
	}
	return p.matchGroup(userAgent)
}

// CrawlDelayFor selects the largest delay among equally specific groups.
func (p *RobotsPolicy) CrawlDelayFor(userAgent string) time.Duration {
	if p == nil {
		return 0
	}
	var delay time.Duration
	for _, agent := range p.matchingAgents(userAgent) {
		if candidate := p.CrawlDelays[agent]; candidate > delay {
			delay = candidate
		}
	}
	return delay
}

// robotsCache holds parsed robots policies per session, origin and agent. Entries expire after
// robotsCacheTTL and the map is capped at robotsCacheMaxHosts so long-running
// crawlers neither serve stale policies nor grow unboundedly.
type robotsCache struct {
	mu           sync.Mutex
	by           map[string]robotsEntry
	payloadBytes int
	closed       bool
	owners       map[string]*robotsOwner
	flights      map[string]*robotsFlight
	delayGates   map[string]*robotsDelayGate
}

type robotsEntry struct {
	policy       *RobotsPolicy
	fetchedAt    time.Time
	payloadBytes int
}

const (
	robotsCacheTTL      = 24 * time.Hour
	robotsCacheMaxHosts = 4096
	robotsCacheMaxBytes = 8 << 20
)

func newRobotsCache() *robotsCache {
	return &robotsCache{by: make(map[string]robotsEntry), owners: make(map[string]*robotsOwner), flights: make(map[string]*robotsFlight), delayGates: make(map[string]*robotsDelayGate)}
}

func (rc *robotsCache) get(host string) (*RobotsPolicy, bool) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	e, ok := rc.by[host]
	if !ok || rc.closed {
		return nil, false
	}
	if time.Since(e.fetchedAt) >= robotsCacheTTL {
		rc.removeLocked(host)
		return nil, false
	}
	return cloneRobotsPolicy(e.policy), true
}

func (rc *robotsCache) put(host string, p *RobotsPolicy) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.putLocked(host, p)
}

func (rc *robotsCache) putLocked(host string, p *RobotsPolicy) {
	if rc.closed {
		return
	}
	size := robotsPolicyPayloadBytes(p)
	if size > robotsCacheMaxBytes {
		return
	}
	rc.removeLocked(host)
	for len(rc.by) >= robotsCacheMaxHosts || rc.payloadBytes+size > robotsCacheMaxBytes {
		var stalest string
		var stalestAt time.Time
		for h, e := range rc.by {
			if stalest == "" || e.fetchedAt.Before(stalestAt) || e.fetchedAt.Equal(stalestAt) && h < stalest {
				stalest, stalestAt = h, e.fetchedAt
			}
		}
		rc.removeLocked(stalest)
	}
	rc.by[host] = robotsEntry{policy: cloneRobotsPolicy(p), fetchedAt: time.Now(), payloadBytes: size}
	rc.payloadBytes += size
}

// FetchRobots uses the policy-bound HTTP client and caches one origin/session.
// Only explicit absent-file statuses admit an empty policy; failures never do.
func (c *HTTPClient) FetchRobots(ctx context.Context, u *url.URL) (result *RobotsPolicy, resultErr error) {
	return c.fetchRobots(ctx, u, nil)
}

func (c *HTTPClient) fetchRobots(ctx context.Context, u *url.URL, userAgent *string) (*RobotsPolicy, error) {
	if c == nil || c.client == nil || c.robots == nil {
		return nil, errors.New("robots: initialized HTTP client required")
	}
	if ctx == nil {
		return nil, errors.New("robots: context required")
	}
	if u == nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("robots: absolute URL required")
	}
	// robots.txt itself is not robots-governed; its own redirects must not
	// recurse into another robots admission.
	ctx = withoutRobotsAgent(ctx)
	var headers http.Header
	agentIdentity := "default"
	if userAgent != nil {
		var err error
		headers, err = ValidatedHeaders(http.Header{"User-Agent": []string{*userAgent}})
		if err != nil {
			return nil, err
		}
		agentIdentity = "agent:" + *userAgent
	}
	sessionID := SessionID(ctx, c.cfg.SessionID)
	if _, err := c.CookieJarForSession(sessionID); err != nil {
		return nil, err
	}
	robotsURL, err := robotsOriginURL(u)
	if err != nil {
		return nil, err
	}
	if err := c.validateTarget(ctx, robotsURL, http.MethodGet, "", 0, TargetSubresource, sessionID); err != nil {
		return nil, err
	}
	key := robotsCacheIdentity(sessionID, robotsURL+"\x00"+agentIdentity)
	policy, flight, leader, err := c.robots.begin(sessionID, key)
	if err != nil || policy != nil {
		return policy, err
	}
	if !leader {
		return flight.wait(ctx)
	}
	flightCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.robots.attachCancel(flight, cancel)
	response, err := c.DoTarget(flightCtx, Request{URL: robotsURL, Headers: headers, MaxBodyBytes: MaxRobotsDocumentBytes}, TargetSubresource)
	if err != nil {
		return c.robots.complete(flight, nil, fmt.Errorf("fetch robots: %w", err))
	}
	policy, err = parseRobotsResponse(response)
	return c.robots.complete(flight, policy, err)
}

// ErrRobotsDisallowed is returned when robots.txt forbids the URL.
var ErrRobotsDisallowed = errors.New("robots.txt: disallowed")
