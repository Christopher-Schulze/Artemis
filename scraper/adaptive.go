package scraper

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// AdaptiveEntry is a cached selector for a URL pattern on a domain.
type AdaptiveEntry struct {
	Domain     string
	URLPattern string
	Selector   string
	Confidence float64
	UpdatedAt  time.Time
}

// AdaptiveSelectorCache is L1 memory + L2 SQLite (spec ss28.12b.2).
type AdaptiveSelectorCache struct {
	mu    sync.RWMutex
	l1    map[l1Key]AdaptiveEntry
	db    *sql.DB
	maxL1 int
}

// l1Key keys the in-memory tier without concatenating a string per lookup.
type l1Key struct {
	domain  string
	pattern string
}

// OpenAdaptiveCache opens SQLite backing store at path.
func OpenAdaptiveCache(path string, maxL1 int) (*AdaptiveSelectorCache, error) {
	if maxL1 <= 0 {
		maxL1 = 1000
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("adaptive cache: open: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), `PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("adaptive cache: wal: %w", errors.Join(err, db.Close()))
	}
	if _, err := db.ExecContext(context.Background(), `
CREATE TABLE IF NOT EXISTS adaptive (
  url_pattern TEXT NOT NULL,
  selector TEXT NOT NULL,
  confidence REAL NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (url_pattern)
)`); err != nil {
		return nil, fmt.Errorf("adaptive cache: schema: %w", errors.Join(err, db.Close()))
	}
	return &AdaptiveSelectorCache{l1: make(map[l1Key]AdaptiveEntry), db: db, maxL1: maxL1}, nil
}

func cacheKey(domain, pattern string) string {
	return strings.ToLower(domain) + "|" + pattern
}

// Get returns a cached selector using a background context for compatibility.
func (c *AdaptiveSelectorCache) Get(domain, urlPattern string) (AdaptiveEntry, bool) {
	return c.GetContext(context.Background(), domain, urlPattern)
}

// GetContext preserves the legacy miss-on-error contract. Critical consumers
// use LookupContext to retain cancellation and persistence failures.
func (c *AdaptiveSelectorCache) GetContext(ctx context.Context, domain, urlPattern string) (AdaptiveEntry, bool) {
	entry, found, err := c.LookupContext(ctx, domain, urlPattern)
	return entry, found && err == nil
}

// LookupContext returns actual cache misses separately from failed reads.
func (c *AdaptiveSelectorCache) LookupContext(ctx context.Context, domain, urlPattern string) (AdaptiveEntry, bool, error) {
	if c == nil || ctx == nil {
		return AdaptiveEntry{}, false, fmt.Errorf("adaptive cache: cache and context required")
	}
	if err := ctx.Err(); err != nil {
		return AdaptiveEntry{}, false, err
	}
	k := l1Key{domain: strings.ToLower(domain), pattern: urlPattern}
	c.mu.RLock()
	if e, ok := c.l1[k]; ok {
		c.mu.RUnlock()
		return e, true, nil
	}
	c.mu.RUnlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.l1[k]; ok {
		return e, true, nil
	}
	if c.db == nil {
		return AdaptiveEntry{}, false, nil
	}
	row := c.db.QueryRowContext(ctx,
		`SELECT selector, confidence, updated_at FROM adaptive WHERE url_pattern=?`, cacheKey(domain, urlPattern),
	)
	var sel string
	var conf float64
	var updated string
	if err := row.Scan(&sel, &conf, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AdaptiveEntry{}, false, nil
		}
		return AdaptiveEntry{}, false, fmt.Errorf("adaptive cache: lookup: %w", err)
	}
	t, err := time.Parse(time.RFC3339, updated)
	if err != nil || math.IsNaN(conf) || math.IsInf(conf, 0) || conf < 0 || conf > 1 {
		return AdaptiveEntry{}, false, fmt.Errorf("adaptive cache: invalid stored metadata")
	}
	e := AdaptiveEntry{
		Domain: domain, URLPattern: urlPattern, Selector: sel,
		Confidence: conf, UpdatedAt: t,
	}
	c.putL1(k, e)
	return e, true, nil
}

// Put stores a selector using a background context for compatibility.
func (c *AdaptiveSelectorCache) Put(e AdaptiveEntry) error {
	return c.PutContext(context.Background(), e)
}

// PutContext stores a selector and binds the SQLite write to ctx.
func (c *AdaptiveSelectorCache) PutContext(ctx context.Context, e AdaptiveEntry) error {
	if c == nil || c.l1 == nil || c.maxL1 < 1 || ctx == nil {
		return fmt.Errorf("adaptive cache: initialized cache and context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if math.IsNaN(e.Confidence) || math.IsInf(e.Confidence, 0) || e.Confidence < 0 || e.Confidence > 1 {
		return fmt.Errorf("adaptive cache: invalid selector confidence")
	}
	key := cacheKey(e.Domain, e.URLPattern)
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = time.Now().UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db == nil {
		c.putL1(l1Key{domain: strings.ToLower(e.Domain), pattern: e.URLPattern}, e)
		return nil
	}
	_, err := c.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO adaptive(url_pattern,selector,confidence,updated_at) VALUES(?,?,?,?)`,
		key, e.Selector, e.Confidence, e.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("adaptive cache: put: %w", err)
	}
	c.putL1(l1Key{domain: strings.ToLower(e.Domain), pattern: e.URLPattern}, e)
	return nil
}

func (c *AdaptiveSelectorCache) putL1(key l1Key, e AdaptiveEntry) {
	if _, exists := c.l1[key]; !exists && len(c.l1) >= c.maxL1 {
		for k := range c.l1 {
			delete(c.l1, k)
			break
		}
	}
	c.l1[key] = e
}

// DeleteContext invalidates both cache tiers; failed persistence is explicit.
func (c *AdaptiveSelectorCache) DeleteContext(ctx context.Context, domain, urlPattern string) error {
	if c == nil || ctx == nil {
		return fmt.Errorf("adaptive cache: cache and context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db != nil {
		if _, err := c.db.ExecContext(ctx, `DELETE FROM adaptive WHERE url_pattern=?`, cacheKey(domain, urlPattern)); err != nil {
			return fmt.Errorf("adaptive cache: invalidate: %w", err)
		}
	}
	delete(c.l1, l1Key{domain: strings.ToLower(domain), pattern: urlPattern})
	return nil
}

// Close closes SQLite.
func (c *AdaptiveSelectorCache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}
