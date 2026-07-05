package stealth

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

// HostLookup resolves a hostname to IP addresses (injectable for tests).
type HostLookup func(host string) ([]net.IP, error)

// StealthAck records operator consent for public stealth (spec gate).
type StealthAck struct {
	AcknowledgedAt time.Time
	LegalBasis     string
	Purpose        string
	DomainAllow    []string
	ExpiresAt      time.Time
}

// StealthPolicy configures public-network stealth defaults.
type StealthPolicy struct {
	PublicDefault StealthLevel
	Requested     StealthLevel
	Ack           StealthAck
}

// DetermineStealthLevel applies the local-network gate and legal ack rules.
func DetermineStealthLevel(targetURL string, policy StealthPolicy, lookup HostLookup) (StealthLevel, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return StealthDefault, fmt.Errorf("stealth: parse url: %w", err)
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return StealthDefault, fmt.Errorf("stealth: empty host")
	}
	if isLocalHostname(host) {
		return StealthDefault, nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if network.IsPrivateOrLocal(ip) {
			return StealthDefault, nil
		}
	} else if lookup != nil {
		ips, err := lookup(host)
		if err == nil {
			for _, ip := range ips {
				if network.IsPrivateOrLocal(ip) {
					return StealthDefault, nil
				}
			}
		}
	}
	level := policy.PublicDefault
	if policy.Requested != "" {
		level = policy.Requested
	}
	if level == StealthStealth || level == StealthParanoid {
		if !ackValid(policy.Ack, host) {
			return StealthDefault, nil
		}
	}
	return level, nil
}

func isLocalHostname(host string) bool {
	if strings.HasSuffix(host, ".local") {
		return true
	}
	return host == "localhost"
}

func ackValid(ack StealthAck, host string) bool {
	if ack.AcknowledgedAt.IsZero() || ack.LegalBasis == "" || ack.Purpose == "" {
		return false
	}
	if !ack.ExpiresAt.IsZero() && time.Now().After(ack.ExpiresAt) {
		return false
	}
	if len(ack.DomainAllow) == 0 {
		return true
	}
	for _, p := range ack.DomainAllow {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "*.") {
			suffix := strings.TrimPrefix(p, "*.")
			if strings.HasSuffix(host, suffix) || host == suffix {
				return true
			}
			continue
		}
		if host == p {
			return true
		}
	}
	return false
}
