package network

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DNSResolverMode selects system vs DoH resolver policy (spec P4.1).
type DNSResolverMode string

const (
	DNSResolverSystem DNSResolverMode = "system"
	DNSResolverDoH    DNSResolverMode = "doh"
)

// DNSPrefetchCache is L1 sync.Map + L2 SQLite with TTL (spec ss28.15.7 P4.1).
type DNSPrefetchCache struct {
	mu       sync.RWMutex
	l1       map[string]dnsEntry
	ttl      time.Duration
	db       *sql.DB
	resolver func(ctx context.Context, host string) ([]net.IP, error)
}

type dnsEntry struct {
	ips       []string
	expiresAt time.Time
}

// OpenDNSPrefetchCache opens optional SQLite backing at path (empty = memory only).
func OpenDNSPrefetchCache(path string, ttl time.Duration) (*DNSPrefetchCache, error) {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	c := &DNSPrefetchCache{
		l1:  make(map[string]dnsEntry),
		ttl: ttl,
		resolver: func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		},
	}
	if path == "" {
		return c, nil
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("dns cache: open: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("dns cache: wal: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS dns_cache (
  host TEXT PRIMARY KEY,
  ips TEXT NOT NULL,
  expires_at INTEGER NOT NULL
)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("dns cache: schema: %w", err)
	}
	c.db = db
	return c, nil
}

// Resolve returns cached IPs or performs lookup.
func (c *DNSPrefetchCache) Resolve(ctx context.Context, host string) ([]net.IP, error) {
	if c == nil {
		return nil, fmt.Errorf("dns cache: nil")
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, fmt.Errorf("dns cache: empty host")
	}
	now := time.Now()
	c.mu.RLock()
	if e, ok := c.l1[host]; ok && now.Before(e.expiresAt) {
		c.mu.RUnlock()
		return parseIPs(e.ips), nil
	}
	c.mu.RUnlock()
	if c.db != nil {
		var ipsCSV string
		var exp int64
		err := c.db.QueryRow(`SELECT ips, expires_at FROM dns_cache WHERE host=?`, host).Scan(&ipsCSV, &exp)
		if err == nil && now.Unix() < exp {
			ips := strings.Split(ipsCSV, ",")
			c.mu.Lock()
			c.l1[host] = dnsEntry{ips: ips, expiresAt: time.Unix(exp, 0)}
			c.mu.Unlock()
			return parseIPs(ips), nil
		}
	}
	if c.resolver == nil {
		return nil, fmt.Errorf("dns cache: no resolver")
	}
	ips, err := c.resolver(ctx, host)
	if err != nil {
		return nil, err
	}
	strs := make([]string, 0, len(ips))
	for _, ip := range ips {
		strs = append(strs, ip.String())
	}
	exp := now.Add(c.ttl)
	c.mu.Lock()
	c.l1[host] = dnsEntry{ips: strs, expiresAt: exp}
	c.mu.Unlock()
	if c.db != nil {
		if _, err := c.db.Exec(
			`INSERT OR REPLACE INTO dns_cache(host,ips,expires_at) VALUES(?,?,?)`,
			host, strings.Join(strs, ","), exp.Unix(),
		); err != nil {
			return nil, fmt.Errorf("dns cache: persist %s: %w", host, err)
		}
	}
	return ips, nil
}

// Prefetch resolves hosts concurrently (max workers).
func (c *DNSPrefetchCache) Prefetch(ctx context.Context, hosts []string, workers int) error {
	if workers <= 0 {
		workers = 10
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(host string) {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := c.Resolve(ctx, host); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}(h)
	}
	wg.Wait()
	return firstErr
}

// Close closes SQLite.
func (c *DNSPrefetchCache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func parseIPs(strs []string) []net.IP {
	out := make([]net.IP, 0, len(strs))
	for _, s := range strs {
		if ip := net.ParseIP(strings.TrimSpace(s)); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}
