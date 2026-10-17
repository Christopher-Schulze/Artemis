package network

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
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

// DNSPrefetchCache is the bounded L1 memory + optional L2 SQLite DNS cache
// (spec L4512). It implements Resolver, so a Policy validates and dials the
// same cached answer: admission and connection stay consistent.
type DNSPrefetchCache struct {
	mu       sync.RWMutex
	l1       map[string]dnsEntry
	ttl      time.Duration
	db       *sql.DB
	inserts  int
	resolver func(ctx context.Context, host string) ([]net.IP, error)
}

// maxDNSCacheEntries bounds both cache levels (spec L4512: max 50K entries).
const maxDNSCacheEntries = 50_000

// dnsCachePruneInterval amortizes L2 expiry/size pruning over inserts.
const dnsCachePruneInterval = 256

var _ Resolver = (*DNSPrefetchCache)(nil)

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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("dns cache: directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("dns cache: open: %w", err)
	}
	// Engines sharing one runtime-home cache wait briefly instead of failing a lookup on a lock.
	if _, err := db.ExecContext(context.Background(), `PRAGMA busy_timeout=5000`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("dns cache: busy timeout: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), `PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("dns cache: wal: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), `
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
	if ctx == nil {
		return nil, fmt.Errorf("dns cache: context required")
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
		err := c.db.QueryRowContext(ctx, `SELECT ips, expires_at FROM dns_cache WHERE host=?`, host).Scan(&ipsCSV, &exp)
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
	if c.db != nil {
		if _, err := c.db.ExecContext(ctx,
			`INSERT OR REPLACE INTO dns_cache(host,ips,expires_at) VALUES(?,?,?)`,
			host, strings.Join(strs, ","), exp.Unix(),
		); err != nil {
			return nil, fmt.Errorf("dns cache: persist %s: %w", host, err)
		}
	}
	c.mu.Lock()
	if len(c.l1) >= maxDNSCacheEntries {
		c.evictLocked(now)
	}
	c.l1[host] = dnsEntry{ips: strs, expiresAt: exp}
	c.inserts++
	prune := c.db != nil && c.inserts%dnsCachePruneInterval == 0
	c.mu.Unlock()
	if prune {
		if err := c.pruneL2(ctx, now); err != nil {
			return nil, err
		}
	}
	return ips, nil
}

// evictLocked drops expired L1 entries, then the earliest-expiring one when
// the cache is still full.
func (c *DNSPrefetchCache) evictLocked(now time.Time) {
	var oldestHost string
	var oldest time.Time
	for host, entry := range c.l1 {
		if !now.Before(entry.expiresAt) {
			delete(c.l1, host)
			continue
		}
		if oldestHost == "" || entry.expiresAt.Before(oldest) {
			oldestHost, oldest = host, entry.expiresAt
		}
	}
	if len(c.l1) >= maxDNSCacheEntries && oldestHost != "" {
		delete(c.l1, oldestHost)
	}
}

// pruneL2 removes expired rows and keeps the newest maxDNSCacheEntries.
func (c *DNSPrefetchCache) pruneL2(ctx context.Context, now time.Time) error {
	if _, err := c.db.ExecContext(ctx, `DELETE FROM dns_cache WHERE expires_at <= ?`, now.Unix()); err != nil {
		return fmt.Errorf("dns cache: prune expired: %w", err)
	}
	if _, err := c.db.ExecContext(ctx, `DELETE FROM dns_cache WHERE host NOT IN (SELECT host FROM dns_cache ORDER BY expires_at DESC LIMIT ?)`, maxDNSCacheEntries); err != nil {
		return fmt.Errorf("dns cache: prune size: %w", err)
	}
	return nil
}

// LookupNetIP serves Policy admission and dialing from the cache.
func (c *DNSPrefetchCache) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	ips, err := c.Resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	addresses := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		address, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		address = address.Unmap()
		if network == "ip4" && !address.Is4() || network == "ip6" && !address.Is6() {
			continue
		}
		addresses = append(addresses, address)
	}
	if len(addresses) == 0 {
		return nil, &net.DNSError{Err: "no cached address for network", Name: host, IsNotFound: true}
	}
	return addresses, nil
}

// Prefetch resolves hosts concurrently (max workers).
func (c *DNSPrefetchCache) Prefetch(ctx context.Context, hosts []string, workers int) error {
	if c == nil {
		return fmt.Errorf("dns cache: nil")
	}
	if ctx == nil {
		return fmt.Errorf("dns cache: context required")
	}
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
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		}
		wg.Add(1)
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
