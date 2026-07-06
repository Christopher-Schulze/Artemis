package scraper

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// SQLiteQuery is a prepared statement invocation.
type SQLiteQuery struct {
	SQL  string
	Args []any
}

// SQLiteResult is the async query outcome.
type SQLiteResult struct {
	Rows *sql.Rows
	Err  error
}

// SQLiteWorker runs SQLite on a dedicated goroutine (spec P7.1).
type SQLiteWorker struct {
	db      *sql.DB
	queries chan SQLiteQuery
	results chan SQLiteResult
	done    chan struct{}
	wg      sync.WaitGroup
	once    sync.Once
}

// StartSQLiteWorker opens path and starts the worker loop.
func StartSQLiteWorker(ctx context.Context, path string, queue int) (*SQLiteWorker, error) {
	if queue <= 0 {
		queue = 64
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite worker: open: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite worker: wal: %w", err)
	}
	w := &SQLiteWorker{
		db: db, queries: make(chan SQLiteQuery, queue),
		results: make(chan SQLiteResult, queue), done: make(chan struct{}),
	}
	w.wg.Add(1)
	go w.loop(ctx)
	return w, nil
}

func (w *SQLiteWorker) loop(ctx context.Context) {
	defer w.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.done:
			return
		case q := <-w.queries:
			rows, err := w.db.Query(q.SQL, q.Args...)
			select {
			case w.results <- SQLiteResult{Rows: rows, Err: err}:
			case <-ctx.Done():
				if rows != nil {
					_ = rows.Close()
				}
				return
			}
		}
	}
}

// Query enqueues a query and waits for one result.
func (w *SQLiteWorker) Query(ctx context.Context, q SQLiteQuery) (SQLiteResult, error) {
	if w == nil {
		return SQLiteResult{}, fmt.Errorf("sqlite worker: nil")
	}
	select {
	case w.queries <- q:
	case <-ctx.Done():
		return SQLiteResult{}, ctx.Err()
	}
	select {
	case r := <-w.results:
		return r, r.Err
	case <-ctx.Done():
		return SQLiteResult{}, ctx.Err()
	}
}

// Close stops the worker and database.
func (w *SQLiteWorker) Close() error {
	if w == nil {
		return nil
	}
	w.once.Do(func() { close(w.done) })
	w.wg.Wait()
	if w.db != nil {
		return w.db.Close()
	}
	return nil
}
