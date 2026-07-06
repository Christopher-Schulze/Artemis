package scraper

import "testing"

func TestNewParseWorkerPoolExplicit(t *testing.T) {
	p := NewParseWorkerPool(8)
	if p == nil {
		t.Fatal("pool must not be nil")
	}
	if p.Workers() != 8 {
		t.Fatalf("expected 8 workers, got %d", p.Workers())
	}
}

func TestNewParseWorkerPoolDefaultOnZero(t *testing.T) {
	p := NewParseWorkerPool(0)
	if p.Workers() != 2 {
		t.Fatalf("expected default 2 workers on zero, got %d", p.Workers())
	}
}

func TestNewParseWorkerPoolDefaultOnNegative(t *testing.T) {
	p := NewParseWorkerPool(-5)
	if p.Workers() != 2 {
		t.Fatalf("expected default 2 workers on negative, got %d", p.Workers())
	}
}

func TestNewSnapshotBuilderPoolExplicit(t *testing.T) {
	p := NewSnapshotBuilderPool(16)
	if p == nil {
		t.Fatal("pool must not be nil")
	}
	if p.Cap() != 16 {
		t.Fatalf("expected cap 16, got %d", p.Cap())
	}
}

func TestNewSnapshotBuilderPoolDefaultOnZero(t *testing.T) {
	p := NewSnapshotBuilderPool(0)
	if p.Cap() != 4 {
		t.Fatalf("expected default cap 4 on zero, got %d", p.Cap())
	}
}

func TestNewSnapshotBuilderPoolDefaultOnNegative(t *testing.T) {
	p := NewSnapshotBuilderPool(-1)
	if p.Cap() != 4 {
		t.Fatalf("expected default cap 4 on negative, got %d", p.Cap())
	}
}
