package network

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrRobotsUnavailable = errors.New("robots.txt: unavailable")

func cloneRobotsPolicy(policy *RobotsPolicy) *RobotsPolicy {
	if policy == nil {
		return nil
	}
	copy := &RobotsPolicy{Groups: make(map[string][]RobotsRule, len(policy.Groups)), CrawlDelays: make(map[string]time.Duration, len(policy.CrawlDelays)), Sitemaps: append([]string(nil), policy.Sitemaps...), patterns: policy.patterns}
	for agent, rules := range policy.Groups {
		copy.Groups[agent] = append([]RobotsRule(nil), rules...)
	}
	for agent, delay := range policy.CrawlDelays {
		copy.CrawlDelays[agent] = delay
	}
	return copy
}

func robotsPolicyPayloadBytes(policy *RobotsPolicy) int {
	if policy == nil {
		return 0
	}
	size := 0
	for agent, rules := range policy.Groups {
		size += len(agent) + 16
		for _, rule := range rules {
			size += len(rule.Path) + 16
		}
	}
	for agent := range policy.CrawlDelays {
		size += len(agent) + 8
	}
	for _, sitemap := range policy.Sitemaps {
		size += len(sitemap) + 16
	}
	return size
}

func (rc *robotsCache) removeLocked(key string) {
	if entry, exists := rc.by[key]; exists {
		rc.payloadBytes -= entry.payloadBytes
		delete(rc.by, key)
	}
}

func (rc *robotsCache) releaseSession(sessionID string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if owner := rc.owners[sessionID]; owner != nil {
		close(owner.done)
		delete(rc.owners, sessionID)
	}
	for key, flight := range rc.flights {
		if flight.sessionID == sessionID {
			if flight.cancel != nil {
				flight.cancel()
			}
			delete(rc.flights, key)
		}
	}
	for key := range rc.by {
		if strings.HasPrefix(key, robotsCacheIdentity(sessionID, "")) {
			rc.removeLocked(key)
		}
	}
}

func (rc *robotsCache) close() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.closed {
		return
	}
	rc.closed = true
	for _, owner := range rc.owners {
		close(owner.done)
	}
	for _, flight := range rc.flights {
		if flight.cancel != nil {
			flight.cancel()
		}
	}
	clear(rc.owners)
	clear(rc.flights)
	clear(rc.by)
	rc.payloadBytes = 0
}

func robotsOriginURL(input *url.URL) (string, error) {
	parsed, err := parsePolicyURL(input.String())
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: invalid robots origin", ErrPolicyDenied)
	}
	host, err := normalizePolicyHost(parsed.Hostname())
	if err != nil {
		return "", fmt.Errorf("%w: invalid robots host", ErrPolicyDenied)
	}
	port, err := policyPort(parsed)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("%w: invalid robots port", ErrPolicyDenied)
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: "/robots.txt"}).String(), nil
}

func parseRobotsResponse(response *Response) (*RobotsPolicy, error) {
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return &RobotsPolicy{Groups: make(map[string][]RobotsRule)}, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrRobotsUnavailable, response.StatusCode)
	}
	if len(response.Body) > 0 {
		mediaType, params, err := mime.ParseMediaType(response.Headers.Get("Content-Type"))
		charset := strings.ToLower(params["charset"])
		if err != nil || mediaType != "text/plain" || charset != "" && charset != "utf-8" && charset != "utf8" {
			return nil, fmt.Errorf("%w: invalid robots media type", ErrRobotsUnavailable)
		}
	}
	policy, err := ParseRobots(bytes.NewReader(response.Body))
	if err != nil {
		return nil, fmt.Errorf("parse robots: %w", err)
	}
	return policy, nil
}
