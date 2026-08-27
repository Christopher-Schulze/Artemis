package scraper

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// FingerprintStore persists scrape_fingerprints (spec ss28.12b.13).
type FingerprintStore struct {
	db *sql.DB
}

// OpenFingerprintStore opens the fingerprint database.
func OpenFingerprintStore(path string) (*FingerprintStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("fingerprint store: open: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), `PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("fingerprint store: wal: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), `
CREATE TABLE IF NOT EXISTS scrape_fingerprints (
  url_canonical_hash TEXT NOT NULL,
  region_id TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  last_seen_at INTEGER NOT NULL,
  etag TEXT,
  last_modified TEXT,
  customer_id TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (url_canonical_hash, region_id, customer_id)
)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("fingerprint store: schema: %w", err)
	}
	return &FingerprintStore{db: db}, nil
}

// LoadInto hydrates an in-memory DiffEngine from SQLite using a background
// context for compatibility.
func (s *FingerprintStore) LoadInto(e *DiffEngine, url string) error {
	return s.LoadIntoContext(context.Background(), e, url)
}

// LoadIntoContext hydrates an in-memory DiffEngine and binds the query to ctx.
func (s *FingerprintStore) LoadIntoContext(ctx context.Context, e *DiffEngine, url string) error {
	if s == nil || s.db == nil || e == nil {
		return fmt.Errorf("fingerprint store: nil")
	}
	if ctx == nil {
		return fmt.Errorf("fingerprint store: context required")
	}
	hash := hashString(url)
	rows, err := s.db.QueryContext(ctx,
		`SELECT region_id, content_hash, last_seen_at, etag, last_modified
		 FROM scrape_fingerprints WHERE url_canonical_hash=?`, hash,
	)
	if err != nil {
		return fmt.Errorf("fingerprint store: load: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var regionID, contentHash string
		var etag, lastMod sql.NullString
		var lastSeen int64
		if err := rows.Scan(&regionID, &contentHash, &lastSeen, &etag, &lastMod); err != nil {
			return fmt.Errorf("fingerprint store: scan: %w", err)
		}
		hash := contentHash
		if regionID == "__global__" && etag.Valid && etag.String != "" {
			hash = etag.String
		}
		key := url + "|" + regionID
		e.fingerprints[key] = RegionFingerprint{
			RegionID: regionID, ContentHash: hash, LastSeenAt: lastSeen,
		}
	}
	return rows.Err()
}

// SaveRegion persists one region fingerprint using a background context for
// compatibility.
func (s *FingerprintStore) SaveRegion(url, regionID, contentHash, customerID string) error {
	return s.SaveRegionContext(context.Background(), url, regionID, contentHash, customerID)
}

// SaveRegionContext persists one region fingerprint and binds the write to ctx.
func (s *FingerprintStore) SaveRegionContext(ctx context.Context, url, regionID, contentHash, customerID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("fingerprint store: nil")
	}
	if ctx == nil {
		return fmt.Errorf("fingerprint store: context required")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO scrape_fingerprints
		 (url_canonical_hash,region_id,content_hash,last_seen_at,customer_id)
		 VALUES(?,?,?,?,?)`,
		hashString(url), regionID, contentHash, time.Now().Unix(), customerID,
	)
	if err != nil {
		return fmt.Errorf("fingerprint store: save: %w", err)
	}
	return nil
}

// Close closes the DB.
func (s *FingerprintStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// PersistDiffEngine writes all fingerprints from engine for url using a
// background context for compatibility.
func PersistDiffEngine(s *FingerprintStore, e *DiffEngine, url, customerID string) error {
	return PersistDiffEngineContext(context.Background(), s, e, url, customerID)
}

// PersistDiffEngineContext writes all fingerprints from engine for url and
// binds every SQLite write to ctx.
func PersistDiffEngineContext(ctx context.Context, s *FingerprintStore, e *DiffEngine, url, customerID string) error {
	if s == nil || e == nil {
		return fmt.Errorf("fingerprint store: nil engine")
	}
	if ctx == nil {
		return fmt.Errorf("fingerprint store: context required")
	}
	prefix := url + "|"
	for k, fp := range e.fingerprints {
		if !hasPrefix(k, prefix) {
			continue
		}
		if err := s.SaveRegionContext(ctx, url, fp.RegionID, fp.ContentHash, customerID); err != nil {
			return err
		}
	}
	return nil
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
