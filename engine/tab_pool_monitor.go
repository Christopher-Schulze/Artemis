// Package engine implements idle tab teardown with memory monitoring
// (spec ss28.15.2 P0b.6 Tab Pool Cap).
//
// P0b.6: Max tabs: min(CPU_cores, 8). Idle >5min -> recycle (about:blank).
// runtime.ReadMemStats every 30s; HeapAlloc >70% GOMEMLIMIT -> recycle oldest.
package engine

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// TabPoolConfig controls the tab pool idle-teardown behavior.
type TabPoolConfig struct {
	// MaxTabs is the maximum number of concurrent tabs. Default: min(NumCPU, 8).
	MaxTabs int
	// IdleTimeout is how long a tab can be idle before it's recycled.
	// Default: 5 minutes.
	IdleTimeout time.Duration
	// MemCheckInterval is how often to check heap memory pressure.
	// Default: 30 seconds.
	MemCheckInterval time.Duration
	// HeapPressureThreshold is the fraction of GOMEMLIMIT at which tabs
	// are force-recycled. Default: 0.70 (70%).
	HeapPressureThreshold float64
	// MemLimitBytes is the effective memory limit for pressure calculation.
	// If 0, uses runtime.GOMAXPROCS-based heuristic (4GB default).
	MemLimitBytes int64
}

// DefaultTabPoolConfig returns the spec-mandated defaults.
func DefaultTabPoolConfig() TabPoolConfig {
	maxTabs := runtime.NumCPU()
	if maxTabs > 8 {
		maxTabs = 8
	}
	if maxTabs < 1 {
		maxTabs = 1
	}
	return TabPoolConfig{
		MaxTabs:               maxTabs,
		IdleTimeout:           5 * time.Minute,
		MemCheckInterval:      30 * time.Second,
		HeapPressureThreshold: 0.70,
		MemLimitBytes:         4 * 1024 * 1024 * 1024, // 4GB default
	}
}

// TabPoolStats reports counters for the pool monitor.
type TabPoolStats struct {
	IdleRecycled        atomic.Int64
	MemPressureRecycled atomic.Int64
	MemChecks           atomic.Int64
	IdleChecks          atomic.Int64
}

// TabPoolMonitor watches the TabRecycler for idle tabs and memory pressure,
// recycling tabs that exceed the idle timeout or when heap memory pressure
// exceeds the threshold. It is safe for concurrent use.
type TabPoolMonitor struct {
	cfg      TabPoolConfig
	recycler *TabRecycler
	stats    TabPoolStats
	mu       sync.Mutex
	stop     chan struct{}
	done     chan struct{}
	now      func() time.Time
	memStats func() (heapAlloc int64)
}

// NewTabPoolMonitor creates a monitor for the given recycler. The monitor
// is not started; call Start to begin background monitoring.
func NewTabPoolMonitor(recycler *TabRecycler, cfg TabPoolConfig) *TabPoolMonitor {
	if cfg.MaxTabs <= 0 {
		cfg = DefaultTabPoolConfig()
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	if cfg.MemCheckInterval <= 0 {
		cfg.MemCheckInterval = 30 * time.Second
	}
	if cfg.HeapPressureThreshold <= 0 || cfg.HeapPressureThreshold > 1 {
		cfg.HeapPressureThreshold = 0.70
	}
	if cfg.MemLimitBytes <= 0 {
		cfg.MemLimitBytes = 4 * 1024 * 1024 * 1024
	}
	return &TabPoolMonitor{
		cfg:      cfg,
		recycler: recycler,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		now:      time.Now,
		memStats: readHeapAlloc,
	}
}

// Start begins background monitoring. Calling Start twice without Stop
// panics.
func (m *TabPoolMonitor) Start(ctx context.Context, navigateFn func(ctx context.Context, url string) error) {
	m.mu.Lock()
	if m.done == nil {
		m.mu.Unlock()
		panic("tab pool monitor: already started")
	}
	done := m.done
	m.mu.Unlock()

	go m.run(ctx, navigateFn, done)
}

// Stop signals the monitor to stop and waits for it to finish.
func (m *TabPoolMonitor) Stop() {
	m.mu.Lock()
	if m.done == nil {
		m.mu.Unlock()
		return
	}
	close(m.stop)
	d := m.done
	m.done = nil
	m.mu.Unlock()
	<-d
}

func (m *TabPoolMonitor) run(ctx context.Context, navigateFn func(ctx context.Context, url string) error, done chan struct{}) {
	defer close(done)

	idleTicker := time.NewTicker(m.cfg.IdleTimeout / 2)
	defer idleTicker.Stop()
	memTicker := time.NewTicker(m.cfg.MemCheckInterval)
	defer memTicker.Stop()

	for {
		select {
		case <-m.stop:
			return
		case <-ctx.Done():
			return
		case <-idleTicker.C:
			m.checkIdle(ctx, navigateFn)
		case <-memTicker.C:
			m.checkMemoryPressure(ctx, navigateFn)
		}
	}
}

func (m *TabPoolMonitor) checkIdle(ctx context.Context, navigateFn func(ctx context.Context, url string) error) {
	m.stats.IdleChecks.Add(1)
	now := m.now()
	tabs := m.recycler.All()
	for _, tab := range tabs {
		tab.mu.Lock()
		state := tab.State
		lastUsed := tab.LastUsed
		tab.mu.Unlock()

		if state == TabStateIdle && now.Sub(lastUsed) > m.cfg.IdleTimeout {
			if _, err := m.recycler.Recycle(ctx, tab.ID, navigateFn); err == nil {
				m.stats.IdleRecycled.Add(1)
			}
		}
	}
}

func (m *TabPoolMonitor) checkMemoryPressure(ctx context.Context, navigateFn func(ctx context.Context, url string) error) {
	m.stats.MemChecks.Add(1)
	heapAlloc := m.memStats()
	threshold := int64(float64(m.cfg.MemLimitBytes) * m.cfg.HeapPressureThreshold)
	if heapAlloc <= threshold {
		return
	}

	// Memory pressure: recycle the oldest idle/recycled tab
	tabs := m.recycler.All()
	var oldest *Tab
	var oldestTime time.Time
	for _, tab := range tabs {
		tab.mu.Lock()
		state := tab.State
		lastUsed := tab.LastUsed
		tab.mu.Unlock()
		if state == TabStateIdle || state == TabStateRecycled {
			if oldest == nil || lastUsed.Before(oldestTime) {
				oldest = tab
				oldestTime = lastUsed
			}
		}
	}
	if oldest != nil {
		if _, err := m.recycler.Recycle(ctx, oldest.ID, navigateFn); err == nil {
			m.stats.MemPressureRecycled.Add(1)
		}
	}
}

// Stats returns a snapshot of the monitor's counters.
func (m *TabPoolMonitor) Stats() (idleRecycled, memPressureRecycled, memChecks, idleChecks int64) {
	return m.stats.IdleRecycled.Load(),
		m.stats.MemPressureRecycled.Load(),
		m.stats.MemChecks.Load(),
		m.stats.IdleChecks.Load()
}

// SetNow replaces the clock function (for testing).
func (m *TabPoolMonitor) SetNow(fn func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fn != nil {
		m.now = fn
	}
}

// SetMemStats replaces the memory-stats function (for testing).
func (m *TabPoolMonitor) SetMemStats(fn func() (heapAlloc int64)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fn != nil {
		m.memStats = fn
	}
}

// readHeapAlloc returns the current HeapAlloc from runtime.ReadMemStats.
func readHeapAlloc() int64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return int64(ms.HeapAlloc)
}
