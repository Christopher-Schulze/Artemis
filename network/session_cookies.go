package network

import (
	"errors"
	"net/http"

	"github.com/Christopher-Schulze/Artemis/network/cookiejar"

	"golang.org/x/net/publicsuffix"
)

// MaxCookieSessions bounds retained cookie partitions without evicting live state.
const MaxCookieSessions = 256

// CookieJarForSession returns the exact owner partition. Empty or configured
// default identity retains the original client jar for single-session clients.
// Transports, policy and connection pools remain shared; authentication does not.
func (c *HTTPClient) CookieJarForSession(sessionID string) (http.CookieJar, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("network: initialized HTTP client required")
	}
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.closed {
		return nil, errors.New("network: cookie access on closed client")
	}
	if sessionID == "" || sessionID == c.cfg.SessionID {
		return c.jar, nil
	}
	if jar := c.sessionJars[sessionID]; jar != nil {
		return jar, nil
	}
	if len(c.sessionJars) >= MaxCookieSessions {
		return nil, errors.New("network: cookie session limit exceeded")
	}
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	if c.sessionJars == nil {
		c.sessionJars = make(map[string]http.CookieJar)
	}
	c.sessionJars[sessionID] = jar
	return jar, nil
}

// ReleaseSession forgets an owner after its in-flight requests and pages stop.
// A later trusted reuse starts empty; it cannot recover the old partition.
func (c *HTTPClient) ReleaseSession(sessionID string) error {
	if c == nil {
		return nil
	}
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if !c.closed && (sessionID == "" || sessionID == c.cfg.SessionID) {
		jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
		if err != nil {
			return err
		}
		c.jar = jar
	}
	delete(c.sessionJars, sessionID)
	if c.robots != nil {
		cacheOwner := sessionID
		if cacheOwner == "" {
			cacheOwner = c.cfg.SessionID
		}
		c.robots.releaseSession(cacheOwner)
	}
	return nil
}
