package tabs

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"runtime/debug"
	"sort"
	"sync/atomic"
	"time"
)

// recycle.go returns idle pool tabs to about:blank (spec L4482 P0b.6 Tab Pool
// Cap: idle >5 min recycles, HeapAlloc above 70% of GOMEMLIMIT recycles the
// oldest, checked every 30 s; L4522 P5.2: about:blank instead of close,
// clearing cookies and localStorage while the browser keeps DNS, TLS and
// HTTP/2 state). Only tabs this registry created are recyclable: session
// pages opened elsewhere in the same browser context are never touched, and
// the active tab never is.

const blankURL = "about:blank"

// TargetRecycler is a TargetSource that can clear a live target's origin
// data and navigate it to about:blank.
type TargetRecycler interface {
	RecycleTarget(ctx context.Context, id string) error
}

// RecyclePolicy configures pool tab recycling. Zero fields take the spec
// defaults; HeapAlloc, HeapLimit and Now are injectable for tests.
type RecyclePolicy struct {
	IdleAfter    time.Duration
	Interval     time.Duration
	HeapFraction float64
	HeapAlloc    func() uint64
	HeapLimit    func() int64
	Now          func() time.Time
}

func (p RecyclePolicy) withDefaults() RecyclePolicy {
	if p.IdleAfter <= 0 {
		p.IdleAfter = 5 * time.Minute
	}
	if p.Interval <= 0 {
		p.Interval = 30 * time.Second
	}
	if p.HeapFraction <= 0 || p.HeapFraction > 1 {
		p.HeapFraction = 0.70
	}
	if p.HeapAlloc == nil {
		p.HeapAlloc = func() uint64 {
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			return stats.HeapAlloc
		}
	}
	if p.HeapLimit == nil {
		p.HeapLimit = func() int64 { return debug.SetMemoryLimit(-1) }
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	return p
}

type recycleState struct {
	total   atomic.Int64
	lastErr atomic.Pointer[string]
}

// RecycledTotal returns how many tabs this registry recycled.
func (r *TabRegistry) RecycledTotal() int64 {
	return r.recycle.total.Load()
}

// LastRecycleError returns the error of the latest failed recycle tick.
func (r *TabRegistry) LastRecycleError() string {
	if message := r.recycle.lastErr.Load(); message != nil {
		return *message
	}
	return ""
}

func (r *TabRegistry) markCreated(id string) {
	r.mu.Lock()
	if r.pool == nil {
		r.pool = map[string]struct{}{}
	}
	r.pool[id] = struct{}{}
	r.mu.Unlock()
}

// RecycleTick recycles the registry's idle pool tabs and, under heap
// pressure, the least recently active one. It returns the recycled IDs.
func (r *TabRegistry) RecycleTick(ctx context.Context, policy RecyclePolicy) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("tabs: recycle context required")
	}
	recycler, ok := r.source.(TargetRecycler)
	if !ok {
		return nil, errors.New("tabs: target source cannot recycle targets")
	}
	policy = policy.withDefaults()
	unlock, err := r.lockOperation(ctx)
	if err != nil {
		return nil, fmt.Errorf("tabs: wait for lifecycle operation: %w", err)
	}
	defer unlock()
	if err := r.Sync(ctx); err != nil {
		return nil, err
	}
	now := policy.Now()
	candidates := r.recyclable()
	var chosen []Tab
	for _, tab := range candidates {
		if now.Sub(lastUse(tab)) >= policy.IdleAfter {
			chosen = append(chosen, tab)
		}
	}
	if limit := policy.HeapLimit(); limit > 0 && limit < math.MaxInt64 && float64(policy.HeapAlloc()) > policy.HeapFraction*float64(limit) && len(chosen) < len(candidates) {
		chosen = append(chosen, candidates[len(chosen)])
	}
	var recycled []string
	var errs []error
	for _, tab := range chosen {
		if err := recycler.RecycleTarget(ctx, tab.ID); err != nil {
			errs = append(errs, fmt.Errorf("tabs: recycle %s: %w", tab.ID, err))
			continue
		}
		r.mu.Lock()
		if current, exists := r.tabs[tab.ID]; exists {
			current.URL, current.Title, current.State = blankURL, "", TabStateIdle
			r.tabs[tab.ID] = current
		}
		r.mu.Unlock()
		r.recycle.total.Add(1)
		recycled = append(recycled, tab.ID)
	}
	return recycled, errors.Join(errs...)
}

// recyclable returns the live pool tabs that hold a page, least recently
// active first (idle ones always precede the pressure choice).
func (r *TabRegistry) recyclable() []Tab {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var tabs []Tab
	for id := range r.pool {
		tab, exists := r.tabs[id]
		if !exists || tab.URL == blankURL {
			continue
		}
		switch tab.State {
		case TabStateActive, TabStateClosed, TabStateCrashed, TabStateLost:
			continue
		}
		tabs = append(tabs, tab)
	}
	sort.Slice(tabs, func(i, j int) bool {
		left, right := lastUse(tabs[i]), lastUse(tabs[j])
		if left.Equal(right) {
			return tabs[i].ID < tabs[j].ID
		}
		return left.Before(right)
	})
	return tabs
}

// lastUse is the latest observed activity, or the creation time of a tab
// that has seen none yet.
func lastUse(tab Tab) time.Time {
	if tab.LastActive.After(tab.CreatedAt) {
		return tab.LastActive
	}
	return tab.CreatedAt
}

// RunRecycler runs RecycleTick every policy interval until ctx ends. Tick
// failures are kept in LastRecycleError and do not stop the loop.
func (r *TabRegistry) RunRecycler(ctx context.Context, policy RecyclePolicy) {
	policy = policy.withDefaults()
	ticker := time.NewTicker(policy.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := r.RecycleTick(ctx, policy); err != nil && ctx.Err() == nil {
				message := err.Error()
				r.recycle.lastErr.Store(&message)
			}
		}
	}
}
