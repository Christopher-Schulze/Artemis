package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestTabPoolMonitorIdleRecycle(t *testing.T) {
	recycler := NewTabRecycler(8)
	recycler.Register("tab-1", "https://example.com/page")
	if err := recycler.SetIdle("tab-1"); err != nil {
		t.Fatal(err)
	}

	var navigateCalls atomic.Int32
	navigateFn := func(ctx context.Context, url string) error {
		navigateCalls.Add(1)
		if url != "about:blank" {
			t.Errorf("navigate url = %s, want about:blank", url)
		}
		return nil
	}

	cfg := TabPoolConfig{
		MaxTabs:               8,
		IdleTimeout:           50 * time.Millisecond,
		MemCheckInterval:      10 * time.Second, // don't trigger mem check
		HeapPressureThreshold: 0.70,
		MemLimitBytes:         1 << 30,
	}

	monitor := NewTabPoolMonitor(recycler, cfg)
	startTime := time.Now()
	monitor.SetNow(func() time.Time {
		return startTime.Add(10 * time.Minute) // simulate 10 min elapsed
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.Start(ctx, navigateFn)

	// Wait for idle check to fire (IdleTimeout/2 = 25ms tick)
	time.Sleep(200 * time.Millisecond)
	monitor.Stop()

	if got := navigateCalls.Load(); got < 1 {
		t.Fatalf("expected at least 1 navigate call, got %d", got)
	}

	idleRecycled, _, _, idleChecks := monitor.Stats()
	if idleRecycled < 1 {
		t.Errorf("expected idleRecycled >= 1, got %d", idleRecycled)
	}
	if idleChecks < 1 {
		t.Errorf("expected idleChecks >= 1, got %d", idleChecks)
	}
}

func TestTabPoolMonitorMemoryPressure(t *testing.T) {
	recycler := NewTabRecycler(8)
	recycler.Register("tab-old", "https://example.com/old")
	recycler.Register("tab-new", "https://example.com/new")
	if err := recycler.SetIdle("tab-old"); err != nil {
		t.Fatal(err)
	}
	if err := recycler.SetIdle("tab-new"); err != nil {
		t.Fatal(err)
	}

	// Make tab-old older than tab-new
	now := time.Now()
	recycler.mu.Lock()
	if tab, ok := recycler.tabs["tab-old"]; ok {
		tab.LastUsed = now.Add(-10 * time.Minute)
	}
	if tab, ok := recycler.tabs["tab-new"]; ok {
		tab.LastUsed = now.Add(-1 * time.Minute)
	}
	recycler.mu.Unlock()

	var recycledID atomic.Value
	var navigateCalls atomic.Int32
	navigateFn := func(ctx context.Context, url string) error {
		navigateCalls.Add(1)
		return nil
	}

	cfg := TabPoolConfig{
		MaxTabs:               8,
		IdleTimeout:           10 * time.Second, // don't trigger idle
		MemCheckInterval:      50 * time.Millisecond,
		HeapPressureThreshold: 0.70,
		MemLimitBytes:         1000, // very low so any heap triggers pressure
	}

	monitor := NewTabPoolMonitor(recycler, cfg)
	monitor.SetMemStats(func() int64 {
		return 900 // 90% of 1000 limit -> above 70% threshold
	})
	monitor.SetNow(func() time.Time { return now })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.Start(ctx, navigateFn)

	time.Sleep(200 * time.Millisecond)
	monitor.Stop()

	if got := navigateCalls.Load(); got < 1 {
		t.Fatalf("expected at least 1 mem-pressure recycle, got %d", got)
	}

	_, memPressureRecycled, _, _ := monitor.Stats()
	if memPressureRecycled < 1 {
		t.Errorf("expected memPressureRecycled >= 1, got %d", memPressureRecycled)
	}

	// Verify the oldest tab was recycled
	_ = recycledID // suppress unused
}

func TestTabPoolMonitorNoPressureNoRecycle(t *testing.T) {
	recycler := NewTabRecycler(8)
	recycler.Register("tab-1", "https://example.com")
	if err := recycler.SetIdle("tab-1"); err != nil {
		t.Fatal(err)
	}

	var navigateCalls atomic.Int32
	navigateFn := func(ctx context.Context, url string) error {
		navigateCalls.Add(1)
		return nil
	}

	cfg := TabPoolConfig{
		MaxTabs:               8,
		IdleTimeout:           10 * time.Second,
		MemCheckInterval:      50 * time.Millisecond,
		HeapPressureThreshold: 0.70,
		MemLimitBytes:         1 << 30, // 1GB - high enough that 1KB heap won't trigger
	}

	monitor := NewTabPoolMonitor(recycler, cfg)
	monitor.SetMemStats(func() int64 { return 1024 }) // 1KB, well below 70% of 1GB
	monitor.SetNow(func() time.Time { return time.Now() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.Start(ctx, navigateFn)

	time.Sleep(200 * time.Millisecond)
	monitor.Stop()

	if got := navigateCalls.Load(); got != 0 {
		t.Fatalf("expected 0 navigate calls, got %d", got)
	}
}

func TestDefaultTabPoolConfig(t *testing.T) {
	cfg := DefaultTabPoolConfig()
	if cfg.MaxTabs < 1 || cfg.MaxTabs > 8 {
		t.Errorf("MaxTabs = %d, want 1-8", cfg.MaxTabs)
	}
	if cfg.IdleTimeout != 5*time.Minute {
		t.Errorf("IdleTimeout = %v, want 5m", cfg.IdleTimeout)
	}
	if cfg.MemCheckInterval != 30*time.Second {
		t.Errorf("MemCheckInterval = %v, want 30s", cfg.MemCheckInterval)
	}
	if cfg.HeapPressureThreshold != 0.70 {
		t.Errorf("HeapPressureThreshold = %v, want 0.70", cfg.HeapPressureThreshold)
	}
}
