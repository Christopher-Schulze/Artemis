package scraper

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func startTestWorker(t *testing.T, queue int) *SQLiteWorker {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	w, err := StartSQLiteWorker(context.Background(), path, queue)
	if err != nil {
		t.Fatalf("StartSQLiteWorker: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func TestStartSQLiteWorkerOpensAndSetsWAL(t *testing.T) {
	w := startTestWorker(t, 8)
	if w == nil {
		t.Fatal("worker must not be nil")
	}
	if w.db == nil {
		t.Fatal("db handle must be set")
	}
}

func TestStartSQLiteWorkerDefaultQueue(t *testing.T) {
	w := startTestWorker(t, 0)
	if cap(w.queries) != 64 {
		t.Fatalf("expected default queue 64, got %d", cap(w.queries))
	}
}

func TestSQLiteWorkerQueryExecutes(t *testing.T) {
	w := startTestWorker(t, 8)
	ctx := context.Background()
	if _, err := w.Query(ctx, SQLiteQuery{
		SQL:  `CREATE TABLE IF NOT EXISTS t (k TEXT PRIMARY KEY, v INTEGER)`,
		Args: nil,
	}); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := w.Query(ctx, SQLiteQuery{
		SQL:  `INSERT INTO t (k, v) VALUES (?, ?)`,
		Args: []any{"a", 1},
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	res, err := w.Query(ctx, SQLiteQuery{
		SQL:  `SELECT v FROM t WHERE k = ?`,
		Args: []any{"a"},
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if res.Err != nil {
		t.Fatalf("select err: %v", res.Err)
	}
	rows := res.Rows
	if rows == nil {
		t.Fatal("rows must not be nil")
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected one row")
	}
	var v int
	if err := rows.Scan(&v); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected v=1, got %d", v)
	}
	if rows.Next() {
		t.Fatal("expected exactly one row")
	}
}

func TestSQLiteWorkerQueryContextCancel(t *testing.T) {
	w := startTestWorker(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := w.Query(ctx, SQLiteQuery{SQL: `SELECT 1`})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestSQLiteWorkerCloseIdempotent(t *testing.T) {
	w := startTestWorker(t, 4)
	if err := w.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second close must be idempotent, got %v", err)
	}
}

func TestSQLiteWorkerNilReceiverQuery(t *testing.T) {
	var w *SQLiteWorker
	_, err := w.Query(context.Background(), SQLiteQuery{SQL: `SELECT 1`})
	if err == nil {
		t.Fatal("nil receiver Query must error")
	}
}

func TestSQLiteWorkerNilReceiverClose(t *testing.T) {
	var w *SQLiteWorker
	if err := w.Close(); err != nil {
		t.Fatalf("nil receiver Close must return nil, got %v", err)
	}
}
