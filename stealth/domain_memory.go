package stealth

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DomainMemoryEntry records that a domain needed paranoid stealth.
type DomainMemoryEntry struct {
	Domain    string
	Purpose   string
	AckID     string
	ExpiresAt time.Time
	Level     StealthLevel
}

// DomainMemory persists per-domain stealth hints (SQLite, TTL 7d default).
type DomainMemory struct {
	db *sql.DB
}

// OpenDomainMemory opens or creates the domain memory database.
func OpenDomainMemory(path string) (*DomainMemory, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("domain memory: open: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("domain memory: wal: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS domain_stealth (
  domain TEXT PRIMARY KEY,
  purpose TEXT NOT NULL,
  ack_id TEXT NOT NULL,
  level TEXT NOT NULL,
  expires_at TEXT NOT NULL
)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("domain memory: schema: %w", err)
	}
	return &DomainMemory{db: db}, nil
}

// Remember stores a domain-level stealth preference.
func (m *DomainMemory) Remember(e DomainMemoryEntry) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("domain memory: nil store")
	}
	domain := strings.ToLower(strings.TrimSpace(e.Domain))
	if domain == "" {
		return fmt.Errorf("domain memory: empty domain")
	}
	if e.ExpiresAt.IsZero() {
		e.ExpiresAt = time.Now().Add(7 * 24 * time.Hour)
	}
	_, err := m.db.Exec(
		`INSERT OR REPLACE INTO domain_stealth(domain,purpose,ack_id,level,expires_at) VALUES(?,?,?,?,?)`,
		domain, e.Purpose, e.AckID, string(e.Level), e.ExpiresAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("domain memory: insert: %w", err)
	}
	return nil
}

// Lookup returns a non-expired entry for domain.
func (m *DomainMemory) Lookup(domain string) (DomainMemoryEntry, bool, error) {
	if m == nil || m.db == nil {
		return DomainMemoryEntry{}, false, fmt.Errorf("domain memory: nil store")
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	row := m.db.QueryRow(
		`SELECT purpose, ack_id, level, expires_at FROM domain_stealth WHERE domain=?`, domain,
	)
	var purpose, ackID, level, exp string
	if err := row.Scan(&purpose, &ackID, &level, &exp); err != nil {
		if err == sql.ErrNoRows {
			return DomainMemoryEntry{}, false, nil
		}
		return DomainMemoryEntry{}, false, fmt.Errorf("domain memory: lookup: %w", err)
	}
	t, err := time.Parse(time.RFC3339, exp)
	if err != nil {
		return DomainMemoryEntry{}, false, fmt.Errorf("domain memory: expires: %w", err)
	}
	if time.Now().After(t) {
		return DomainMemoryEntry{}, false, nil
	}
	return DomainMemoryEntry{
		Domain: domain, Purpose: purpose, AckID: ackID,
		Level: StealthLevel(level), ExpiresAt: t,
	}, true, nil
}

// Close releases the database handle.
func (m *DomainMemory) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}
