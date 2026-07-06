package bridge

import (
	"context"
	"testing"
	"time"
)

func TestBatcherAddAndFlush(t *testing.T) {
	var flushed [][]Command
	mock := func(_ context.Context, cmds []Command) ([]Response, error) {
		flushed = append(flushed, cmds)
		return nil, nil
	}

	b := NewBatcher(mock, 0)
	ctx := context.Background()
	id1, err := b.Add(ctx, "A.method", map[string]interface{}{"k": 1})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := b.Add(ctx, "B.method", map[string]interface{}{"k": 2})
	if err != nil {
		t.Fatal(err)
	}
	if b.Pending() != 2 {
		t.Fatalf("expected 2 pending, got %d", b.Pending())
	}
	if err := b.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if b.Pending() != 0 {
		t.Fatalf("expected 0 pending after flush, got %d", b.Pending())
	}
	if len(flushed) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(flushed))
	}
	if len(flushed[0]) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(flushed[0]))
	}
	if flushed[0][0].ID != id1 {
		t.Errorf("expected first id %d, got %d", id1, flushed[0][0].ID)
	}
	if flushed[0][1].ID != id2 {
		t.Errorf("expected second id %d, got %d", id2, flushed[0][1].ID)
	}
}

func TestBatcherAutoFlushOnMaxSize(t *testing.T) {
	var flushed int
	mock := func(_ context.Context, cmds []Command) ([]Response, error) {
		flushed++
		return nil, nil
	}

	b := NewBatcher(mock, 3)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := b.Add(ctx, "X", nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if flushed != 1 {
		t.Fatalf("expected auto-flush after 3 adds, got %d flushes", flushed)
	}
}

func TestBatchFillForm(t *testing.T) {
	var flushed [][]Command
	mock := func(_ context.Context, cmds []Command) ([]Response, error) {
		flushed = append(flushed, cmds)
		return nil, nil
	}

	b := NewBatcher(mock, 0)
	ctx := context.Background()
	ids, err := BatchFillForm(b, ctx, []FormFill{
		{Selector: "#user", Value: "alice"},
		{Selector: "#pass", Value: "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 4 {
		t.Fatalf("expected 4 ids (2 focus + 2 text), got %d", len(ids))
	}
	if err := b.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(flushed) != 1 || len(flushed[0]) != 4 {
		t.Fatalf("expected 1 flush of 4 commands, got %d flushes of %d commands", len(flushed), len(flushed[0]))
	}
}

func TestBatcherFlushEmpty(t *testing.T) {
	mock := func(_ context.Context, _ []Command) ([]Response, error) { return nil, nil }
	b := NewBatcher(mock, 0)
	if err := b.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBatcherTimeout(t *testing.T) {
	mock := func(ctx context.Context, _ []Command) ([]Response, error) {
		select {
		case <-time.After(100 * time.Millisecond):
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	b := NewBatcher(mock, 0)
	b.timeout = 1 * time.Millisecond
	b.Add(context.Background(), "slow", nil)
	err := b.Flush(context.Background())
	if err == nil {
		t.Error("expected timeout error")
	}
}
