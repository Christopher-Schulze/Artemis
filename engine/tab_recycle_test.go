package engine

import (
	"context"
	"errors"
	"testing"
)

func TestTabRecyclerRegister(t *testing.T) {
	r := NewTabRecycler(8)
	tab := r.Register("tab1", "https://example.com")
	if tab.ID != "tab1" {
		t.Fatalf("expected tab1, got %s", tab.ID)
	}
	if tab.State != TabStateActive {
		t.Fatalf("expected active, got %s", tab.State)
	}
}

func TestTabRecyclerGet(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	tab, ok := r.Get("tab1")
	if !ok {
		t.Fatal("expected tab found")
	}
	if tab.ID != "tab1" {
		t.Fatalf("expected tab1, got %s", tab.ID)
	}
	_, ok = r.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestTabRecyclerRecycle(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")

	navigated := false
	navigateFn := func(ctx context.Context, url string) error {
		if url != "about:blank" {
			t.Fatalf("expected about:blank, got %s", url)
		}
		navigated = true
		return nil
	}

	tab, err := r.Recycle(context.Background(), "tab1", navigateFn)
	if err != nil {
		t.Fatal(err)
	}
	if !navigated {
		t.Fatal("expected navigate to be called")
	}
	if tab.State != TabStateRecycled {
		t.Fatalf("expected recycled, got %s", tab.State)
	}
	if tab.URL != "about:blank" {
		t.Fatalf("expected about:blank, got %s", tab.URL)
	}
	if tab.RecycledCount != 1 {
		t.Fatalf("expected 1 recycle, got %d", tab.RecycledCount)
	}
}

func TestTabRecyclerRecycleNotFound(t *testing.T) {
	r := NewTabRecycler(8)
	_, err := r.Recycle(context.Background(), "nonexistent", func(ctx context.Context, url string) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for nonexistent tab")
	}
}

func TestTabRecyclerRecycleNilNavigate(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	_, err := r.Recycle(context.Background(), "tab1", nil)
	if err == nil {
		t.Fatal("expected error for nil navigate function")
	}
}

func TestTabRecyclerRecycleNavigateError(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	navigateFn := func(ctx context.Context, url string) error {
		return errors.New("CDP error")
	}
	_, err := r.Recycle(context.Background(), "tab1", navigateFn)
	if err == nil {
		t.Fatal("expected error for navigate failure")
	}
}

func TestTabRecyclerReuse(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	r.Recycle(context.Background(), "tab1", func(ctx context.Context, url string) error {
		return nil
	})

	tab, err := r.Reuse("tab1", "https://newsite.com")
	if err != nil {
		t.Fatal(err)
	}
	if tab.State != TabStateActive {
		t.Fatalf("expected active, got %s", tab.State)
	}
	if tab.URL != "https://newsite.com" {
		t.Fatalf("expected newsite.com, got %s", tab.URL)
	}
}

func TestTabRecyclerReuseNotRecycled(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	_, err := r.Reuse("tab1", "https://new.com")
	if err == nil {
		t.Fatal("expected error for non-recycled tab")
	}
}

func TestTabRecyclerReuseNotFound(t *testing.T) {
	r := NewTabRecycler(8)
	_, err := r.Reuse("nonexistent", "https://new.com")
	if err == nil {
		t.Fatal("expected error for nonexistent tab")
	}
}

func TestTabRecyclerClose(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	err := r.Close("tab1")
	if err != nil {
		t.Fatal(err)
	}
	_, ok := r.Get("tab1")
	if ok {
		t.Fatal("expected tab to be removed")
	}
}

func TestTabRecyclerCloseNotFound(t *testing.T) {
	r := NewTabRecycler(8)
	err := r.Close("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent tab")
	}
}

func TestTabRecyclerAvailable(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	r.Register("tab2", "https://other.com")

	if r.Available() != nil {
		t.Fatal("expected no available tabs")
	}

	r.Recycle(context.Background(), "tab1", func(ctx context.Context, url string) error {
		return nil
	})

	tab := r.Available()
	if tab == nil {
		t.Fatal("expected available tab")
	}
	if tab.ID != "tab1" {
		t.Fatalf("expected tab1, got %s", tab.ID)
	}
}

func TestTabRecyclerCount(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	r.Register("tab2", "https://other.com")
	r.Recycle(context.Background(), "tab1", func(ctx context.Context, url string) error {
		return nil
	})

	active, idle, recycled, _ := r.Count()
	if active != 1 {
		t.Fatalf("expected 1 active, got %d", active)
	}
	if recycled != 1 {
		t.Fatalf("expected 1 recycled, got %d", recycled)
	}
	_ = idle
}

func TestTabRecyclerRecycledCount(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	r.Recycle(context.Background(), "tab1", func(ctx context.Context, url string) error {
		return nil
	})
	r.Reuse("tab1", "https://new.com")
	r.Recycle(context.Background(), "tab1", func(ctx context.Context, url string) error {
		return nil
	})

	if r.RecycledCount() != 2 {
		t.Fatalf("expected 2 recycles, got %d", r.RecycledCount())
	}
}

func TestTabRecyclerClosedCount(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	r.Close("tab1")
	if r.ClosedCount() != 1 {
		t.Fatalf("expected 1 closed, got %d", r.ClosedCount())
	}
}

func TestTabRecyclerMaxTabs(t *testing.T) {
	r := NewTabRecycler(16)
	if r.MaxTabs() != 16 {
		t.Fatalf("expected 16, got %d", r.MaxTabs())
	}
}

func TestTabRecyclerDefaultMaxTabs(t *testing.T) {
	r := NewTabRecycler(0)
	if r.MaxTabs() != 8 {
		t.Fatalf("expected 8 default, got %d", r.MaxTabs())
	}
}

func TestTabRecyclerSetIdle(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	err := r.SetIdle("tab1")
	if err != nil {
		t.Fatal(err)
	}
	tab, _ := r.Get("tab1")
	if tab.State != TabStateIdle {
		t.Fatalf("expected idle, got %s", tab.State)
	}
}

func TestTabRecyclerSetIdleNotFound(t *testing.T) {
	r := NewTabRecycler(8)
	err := r.SetIdle("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent tab")
	}
}

func TestTabRecyclerAll(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	r.Register("tab2", "https://other.com")
	tabs := r.All()
	if len(tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(tabs))
	}
}

func TestTabStateString(t *testing.T) {
	tests := []struct {
		state TabState
		want  string
	}{
		{TabStateActive, "active"},
		{TabStateIdle, "idle"},
		{TabStateRecycled, "recycled"},
		{TabStateClosed, "closed"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("got %s, want %s", got, tt.want)
		}
	}
}

func TestTabRecyclerRecycleClosedTab(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")
	r.Close("tab1")
	_, err := r.Recycle(context.Background(), "tab1", func(ctx context.Context, url string) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error for closed tab")
	}
}

func TestTabRecyclerMultipleRecycles(t *testing.T) {
	r := NewTabRecycler(8)
	r.Register("tab1", "https://example.com")

	for i := 0; i < 5; i++ {
		r.Recycle(context.Background(), "tab1", func(ctx context.Context, url string) error {
			return nil
		})
		r.Reuse("tab1", "https://new.com")
	}

	tab, _ := r.Get("tab1")
	if tab.RecycledCount != 5 {
		t.Fatalf("expected 5 recycles, got %d", tab.RecycledCount)
	}
	if r.RecycledCount() != 5 {
		t.Fatalf("expected 5 total recycles, got %d", r.RecycledCount())
	}
}
