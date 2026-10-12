package solver

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestChallengeMetricsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.db")
	store, err := OpenMetricsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close metrics store: %v", closeErr)
		}
	}()
	if recordErr := store.Record(MetricRow{
		Domain: "cf.example.com", ChallengeType: string(TypeCloudflare),
		StageSolved: sql.NullInt64{Int64: 1, Valid: true},
		DurationMS:  1200,
	}); recordErr != nil {
		t.Fatal(recordErr)
	}
	rate, n, err := store.SuccessRate(string(TypeCloudflare))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || rate != 1.0 {
		t.Fatalf("expected 100%% success, rate=%f n=%d", rate, n)
	}
}
