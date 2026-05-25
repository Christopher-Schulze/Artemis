package scraper

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// AdaptiveEntry is a cached selector for a URL pattern on a domain.
type AdaptiveEntry struct {
	Domain      string
	URLPattern  string
	Selector    string
	Confidence  float64
	UpdatedAt   time.Time
}

// AdaptiveSelectorCache is L1 memory + L2 SQLite (spec ss28.12b.2).
type AdaptiveSelectorCache struct {
	mu    sync.RWMutex
	l1    map[string]AdaptiveEntry
	db    *sql.DB
	maxL1 int
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
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("adaptive cache: wal: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS adaptive (
  url_pattern TEXT NOT NULL,
  selector TEXT NOT NULL,
  confidence REAL NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (url_pattern)
)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("adaptive cache: schema: %w", err)
	}
	return &AdaptiveSelectorCache{l1: make(map[string]AdaptiveEntry), db: db, maxL1: maxL1}, nil
}

func cacheKey(domain, pattern string) string {
	return strings.ToLower(domain) + "|" + pattern
}

// Get returns a cached selector.
func (c *AdaptiveSelectorCache) Get(domain, urlPattern string) (AdaptiveEntry, bool) {
	key := cacheKey(domain, urlPattern)
	c.mu.RLock()
	if e, ok := c.l1[key]; ok {
		c.mu.RUnlock()
		return e, true
	}
	c.mu.RUnlock()
	if c.db == nil {
		return AdaptiveEntry{}, false
	}
	row := c.db.QueryRow(
		`SELECT selector, confidence, updated_at FROM adaptive WHERE url_pattern=?`, key,
	)
	var sel string
	var conf float64
	var updated string
	if err := row.Scan(&sel, &conf, &updated); err != nil {
		return AdaptiveEntry{}, false
	}
	t, _ := time.Parse(time.RFC3339, updated)
	e := AdaptiveEntry{
		Domain: domain, URLPattern: urlPattern, Selector: sel,
		Confidence: conf, UpdatedAt: t,
	}
	c.mu.Lock()
	c.putL1(key, e)
	c.mu.Unlock()
	return e, true
}

// Put stores selector for domain+pattern.
func (c *AdaptiveSelectorCache) Put(e AdaptiveEntry) error {
	key := cacheKey(e.Domain, e.URLPattern)
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = time.Now().UTC()
	}
	c.mu.Lock()
	c.putL1(key, e)
	c.mu.Unlock()
	if c.db == nil {
		return nil
	}
	_, err := c.db.Exec(
		`INSERT OR REPLACE INTO adaptive(url_pattern,selector,confidence,updated_at) VALUES(?,?,?,?)`,
		key, e.Selector, e.Confidence, e.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("adaptive cache: put: %w", err)
	}
	return nil
}

func (c *AdaptiveSelectorCache) putL1(key string, e AdaptiveEntry) {
	if len(c.l1) >= c.maxL1 {
		for k := range c.l1 {
			delete(c.l1, k)
			break
		}
	}
	c.l1[key] = e
}

// Close closes SQLite.
func (c *AdaptiveSelectorCache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}
