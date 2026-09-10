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
