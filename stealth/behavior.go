package stealth

import (
	"fmt"
	"net/url"
	"strings"
)

// ReferrerForDomain returns a static Referer header for paranoid mode (Patch 29).
func ReferrerForDomain(rawURL string, mem *DomainMemory) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("referrer: parse: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if isLocalHostname(host) {
		return "", nil
	}
	if mem != nil {
		if e, ok, err := mem.Lookup(host); err == nil && ok && e.Level == StealthParanoid {
			return googleReferrer(host), nil
		}
	}
	return googleReferrer(host), nil
}

func googleReferrer(host string) string {
	return "https://www.google.com/search?q=" + url.QueryEscape(host)
}
