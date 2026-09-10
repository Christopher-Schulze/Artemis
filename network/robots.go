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
	"strings"
	"sync"
	"time"
)

// RobotsRule is a single Allow/Disallow line.
type RobotsRule struct {
	Allow bool
	Path  string
}

// RobotsPolicy is the parsed robots.txt for a single host.
type RobotsPolicy struct {
	Groups map[string][]RobotsRule // user-agent -> rules in declaration order
}

// ParseRobots parses a robots.txt body. User-agent matching is
// case-insensitive; the most specific match (longest path prefix) of
// the matching group wins; ties favour Allow.
func ParseRobots(r io.Reader) (*RobotsPolicy, error) {
	if r == nil {
		return nil, errors.New("robots: reader required")
	}
	policy := &RobotsPolicy{Groups: make(map[string][]RobotsRule)}
	current := []string{"*"}
	rulesSeen := false
	scanner := bufio.NewScanner(r)
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
		switch k {
		case "user-agent":
			userAgent := strings.ToLower(v)
			if userAgent == "" {
				continue
			}
			if rulesSeen {
				current = nil
				rulesSeen = false
			}
			current = append(current, userAgent)
		case "allow":
			rulesSeen = true
			for _, ua := range current {
				policy.Groups[ua] = append(policy.Groups[ua], RobotsRule{Allow: true, Path: v})
			}
		case "disallow":
			rulesSeen = true
			for _, ua := range current {
				policy.Groups[ua] = append(policy.Groups[ua], RobotsRule{Allow: false, Path: v})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan robots: %w", err)
	}
	return policy, nil
}

// Allowed reports whether the given user-agent is permitted to fetch
// the given URL path. Empty Disallow path means "allow everything".
func (p *RobotsPolicy) Allowed(userAgent, path string) bool {
	if p == nil {
		return true
	}
	rules := p.matchGroup(userAgent)
	if len(rules) == 0 {
		return true
	}
	bestLen := -1
	bestAllow := true
	for _, r := range rules {
		if r.Path == "" {
			// Disallow with empty path means allow all.
			if !r.Allow {
				continue
			}
		}
		if !strings.HasPrefix(path, r.Path) {
			continue
		}
		if len(r.Path) > bestLen || (len(r.Path) == bestLen && r.Allow) {
			bestLen = len(r.Path)
			bestAllow = r.Allow
		}
	}
	if bestLen < 0 {
		return true
	}
	return bestAllow
}

func (p *RobotsPolicy) matchGroup(userAgent string) []RobotsRule {
	ua := strings.ToLower(userAgent)
	bestLength := -1
	var matched []RobotsRule
	for agent, rules := range p.Groups {
		if agent == "*" || !strings.Contains(ua, agent) {
			continue
		}
		if len(agent) > bestLength {
			bestLength = len(agent)
			matched = append(matched[:0], rules...)
		} else if len(agent) == bestLength {
			matched = append(matched, rules...)
		}
	}
	if bestLength >= 0 {
		return matched
	}
	return p.Groups["*"]
}

// robotsCache holds parsed robots policies per host. Entries expire after
// robotsCacheTTL and the map is capped at robotsCacheMaxHosts so long-running
// crawlers neither serve stale policies nor grow unboundedly.
type robotsCache struct {
	mu sync.Mutex
	by map[string]robotsEntry
}

type robotsEntry struct {
	policy    *RobotsPolicy
	fetchedAt time.Time
}

const (
	robotsCacheTTL      = time.Hour
	robotsCacheMaxHosts = 4096
)

func newRobotsCache() *robotsCache {
	return &robotsCache{by: make(map[string]robotsEntry)}
}

func (rc *robotsCache) get(host string) (*RobotsPolicy, bool) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	e, ok := rc.by[host]
	if !ok {
		return nil, false
	}
	if time.Since(e.fetchedAt) > robotsCacheTTL {
		delete(rc.by, host)
		return nil, false
	}
	return e.policy, true
}

func (rc *robotsCache) put(host string, p *RobotsPolicy) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if _, exists := rc.by[host]; !exists && len(rc.by) >= robotsCacheMaxHosts {
		// Evict the stalest entry; if nothing is stale drop an arbitrary one.
		var stalest string
		var stalestAt time.Time
		for h, e := range rc.by {
			if stalest == "" || e.fetchedAt.Before(stalestAt) {
				stalest, stalestAt = h, e.fetchedAt
			}
		}
		delete(rc.by, stalest)
	}
	rc.by[host] = robotsEntry{policy: p, fetchedAt: time.Now()}
}

// FetchRobots fetches and caches the robots.txt for the host of u.
// Network errors and 404s are treated as "no policy = everything allowed".
func (c *HTTPClient) FetchRobots(ctx context.Context, u *url.URL) (result *RobotsPolicy, resultErr error) {
	if c == nil || c.client == nil || c.robots == nil {
		return nil, errors.New("robots: initialized HTTP client required")
	}
	if ctx == nil {
		return nil, errors.New("robots: context required")
	}
	if u == nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("robots: absolute URL required")
	}
	if p, ok := c.robots.get(u.Host); ok {
		return p, nil
	}
	robotsURL := *u
	robotsURL.Path = "/robots.txt"
	robotsURL.RawQuery = ""
	robotsURL.Fragment = ""
	requestCtx, finish, err := c.beginRequest(ctx)
	if err != nil {
		return nil, err
	}
	var responseBytes int64
	defer func() {
		if finishErr := finish(responseBytes); finishErr != nil {
			result = nil
			resultErr = errors.Join(resultErr, finishErr)
		}
	}()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, robotsURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build robots req: %w", err)
	}
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if cause := context.Cause(requestCtx); cause != nil {
			return nil, cause
		}
		// Network error: treat as no policy.
		empty := &RobotsPolicy{Groups: map[string][]RobotsRule{}}
		c.robots.put(u.Host, empty)
		return empty, nil
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			result = nil
			resultErr = errors.Join(resultErr, fmt.Errorf("close robots response: %w", closeErr))
		}
	}()
	if resp.StatusCode != 200 {
		empty := &RobotsPolicy{Groups: map[string][]RobotsRule{}}
		c.robots.put(u.Host, empty)
		return empty, nil
	}
	countedBody := &countingReader{reader: resp.Body}
	body, err := readLimited(countedBody, 1<<20)
	responseBytes = countedBody.bytes
	if err != nil {
		return nil, fmt.Errorf("read robots: %w", err)
	}
	policy, err := ParseRobots(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.robots.put(u.Host, policy)
	return policy, nil
}

// ErrRobotsDisallowed is returned when robots.txt forbids the URL.
var ErrRobotsDisallowed = errors.New("robots.txt: disallowed")
