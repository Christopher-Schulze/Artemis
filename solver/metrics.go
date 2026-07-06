package solver

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// MetricRow is one challenge_metrics record (spec ss28.6.1.2).
type MetricRow struct {
	Domain        string
	ChallengeType string
	StageSolved   sql.NullInt64
	VisionTokens  int
	DurationMS    int
	CreatedAt     time.Time
}

// MetricsStore persists solver health metrics in SQLite.
type MetricsStore struct {
	db *sql.DB
}

// OpenMetricsStore opens challenge_metrics at path.
func OpenMetricsStore(path string) (*MetricsStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("challenge metrics: open: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("challenge metrics: wal: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS challenge_metrics (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain TEXT NOT NULL,
  challenge_type TEXT NOT NULL,
  stage_solved INTEGER,
  vision_tokens INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER NOT NULL,
  created_at TEXT NOT NULL
)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("challenge metrics: schema: %w", err)
	}
	return &MetricsStore{db: db}, nil
}

// Record inserts a metric row.
func (s *MetricsStore) Record(row MetricRow) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("challenge metrics: nil store")
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO challenge_metrics(domain,challenge_type,stage_solved,vision_tokens,duration_ms,created_at)
		 VALUES(?,?,?,?,?,?)`,
		row.Domain, row.ChallengeType, row.StageSolved, row.VisionTokens, row.DurationMS,
		row.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("challenge metrics: insert: %w", err)
	}
	return nil
}

// SuccessRate returns solved fraction for challenge_type (stage_solved IS NOT NULL).
func (s *MetricsStore) SuccessRate(challengeType string) (float64, int, error) {
	if s == nil || s.db == nil {
		return 0, 0, fmt.Errorf("challenge metrics: nil store")
	}
	var total, solved int
	row := s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN stage_solved IS NOT NULL THEN 1 ELSE 0 END),0)
		 FROM challenge_metrics WHERE challenge_type=?`, challengeType,
	)
	if err := row.Scan(&total, &solved); err != nil {
		return 0, 0, fmt.Errorf("challenge metrics: rate: %w", err)
	}
	if total == 0 {
		return 0, 0, nil
	}
	return float64(solved) / float64(total), total, nil
}

// Close releases the database.
func (s *MetricsStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
