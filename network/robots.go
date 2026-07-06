package network

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	policy := &RobotsPolicy{Groups: make(map[string][]RobotsRule)}
	current := []string{"*"}
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
			current = []string{strings.ToLower(v)}
		case "allow":
			for _, ua := range current {
				policy.Groups[ua] = append(policy.Groups[ua], RobotsRule{Allow: true, Path: v})
			}
		case "disallow":
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
	if rules, ok := p.Groups[ua]; ok {
		return rules
	}
	for k, rules := range p.Groups {
		if k != "*" && strings.Contains(ua, k) {
			return rules
		}
	}
	return p.Groups["*"]
}

// robotsCache holds parsed robots policies per host.
type robotsCache struct {
	mu sync.Mutex
	by map[string]*RobotsPolicy
}

func newRobotsCache() *robotsCache {
	return &robotsCache{by: make(map[string]*RobotsPolicy)}
}

func (rc *robotsCache) get(host string) (*RobotsPolicy, bool) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	p, ok := rc.by[host]
	return p, ok
}

func (rc *robotsCache) put(host string, p *RobotsPolicy) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.by[host] = p
}

// FetchRobots fetches and caches the robots.txt for the host of u.
// Network errors and 404s are treated as "no policy = everything allowed".
func (c *HTTPClient) FetchRobots(ctx context.Context, u *url.URL) (*RobotsPolicy, error) {
	if c.robots == nil {
		c.robots = newRobotsCache()
	}
	if p, ok := c.robots.get(u.Host); ok {
		return p, nil
	}
	robotsURL := *u
	robotsURL.Path = "/robots.txt"
	robotsURL.RawQuery = ""
	robotsURL.Fragment = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build robots req: %w", err)
	}
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		// Network error: treat as no policy.
		empty := &RobotsPolicy{Groups: map[string][]RobotsRule{}}
		c.robots.put(u.Host, empty)
		return empty, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		empty := &RobotsPolicy{Groups: map[string][]RobotsRule{}}
		c.robots.put(u.Host, empty)
		return empty, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read robots: %w", err)
	}
	policy, err := ParseRobots(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	c.robots.put(u.Host, policy)
	return policy, nil
}

// ErrRobotsDisallowed is returned when robots.txt forbids the URL.
var ErrRobotsDisallowed = errors.New("robots.txt: disallowed")
