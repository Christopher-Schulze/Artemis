package scraper

import (
	"net/url"
	"sync"
	"time"
)

// DomainRateLimiter enforces per-host request spacing with impact halving (spec L4438).
type DomainRateLimiter struct {
	mu       sync.Mutex
	interval map[string]time.Duration
	last     map[string]time.Time
	base     time.Duration
}

func NewDomainRateLimiter(baseInterval time.Duration) *DomainRateLimiter {
	if baseInterval <= 0 {
		baseInterval = 500 * time.Millisecond
	}
	return &DomainRateLimiter{
		interval: make(map[string]time.Duration),
		last:     make(map[string]time.Time),
		base:     baseInterval,
	}
}

func (l *DomainRateLimiter) host(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}

// Wait blocks until the host bucket allows the next request. The sleep
// happens outside the mutex so throttling one host never serializes
// unrelated hosts; on re-lock the stamp uses max(now, last+interval) to
// keep spacing deterministic under contention.
func (l *DomainRateLimiter) Wait(host string) {
	l.mu.Lock()
	iv := l.interval[host]
	if iv <= 0 {
		iv = l.base
	}
	var waitFor time.Duration
	if last, ok := l.last[host]; ok {
		waitFor = iv - time.Since(last)
	}
	l.mu.Unlock()
	if waitFor > 0 {
		time.Sleep(waitFor)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if last, ok := l.last[host]; ok && now.Before(last.Add(iv)) {
		now = last.Add(iv)
	}
	l.last[host] = now
	l.evictLocked(now)
}

// maxRateLimitHosts bounds the per-host maps so long-running sessions do not
// grow unboundedly. Eviction drops the stalest `last` entries; hosts whose
// interval was actively tuned are kept since tuning is the expensive state.
const maxRateLimitHosts = 4096

func (l *DomainRateLimiter) evictLocked(now time.Time) {
	if len(l.last) <= maxRateLimitHosts {
		return
	}
	// Pass 1: drop entries whose interval already elapsed — losing their
	// stamp is free since the next Wait would not throttle anyway.
	for host, last := range l.last {
		if len(l.last) <= maxRateLimitHosts {
			return
		}
		if now.Sub(last) > l.base {
			delete(l.last, host)
		}
	}
	// Pass 2: still over budget (pathological burst) — drop untuned hosts.
	for host := range l.last {
		if len(l.last) <= maxRateLimitHosts {
			return
		}
		if _, tuned := l.interval[host]; !tuned {
			delete(l.last, host)
		}
	}
}

// RecordImpact halves the interval when high-impact responses are detected.
func (l *DomainRateLimiter) RecordImpact(rawURL string, highImpact bool) {
	host := l.host(rawURL)
	l.mu.Lock()
	defer l.mu.Unlock()
	iv := l.interval[host]
	if iv <= 0 {
		iv = l.base
	}
	if highImpact {
		iv = iv / 2
		if iv < 50*time.Millisecond {
			iv = 50 * time.Millisecond
		}
	} else if iv < l.base {
		iv = iv + 50*time.Millisecond
		if iv > l.base {
			iv = l.base
		}
	}
	l.interval[host] = iv
}

func (l *DomainRateLimiter) Interval(host string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if iv, ok := l.interval[host]; ok && iv > 0 {
		return iv
	}
	return l.base
}
