package scraper

import (
	"context"
	"sync"
	"time"
)

// rateGate enforces a minimum interval between requests without a background
// ticker: it sleeps on demand, so an idle fetcher holds no timer or goroutine.
type rateGate struct {
	mu   sync.Mutex
	min  time.Duration
	last time.Time
}

func newRateGate(rps float64) *rateGate {
	if rps <= 0 {
		return nil
	}
	return &rateGate{min: time.Duration(float64(time.Second) / rps)}
}

// wait blocks until the minimum interval since the last admitted request has
// elapsed, or ctx is done.
func (g *rateGate) wait(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	wait := g.min - time.Since(g.last)
	if wait <= 0 {
		g.last = time.Now()
		g.mu.Unlock()
		return nil
	}
	g.mu.Unlock()
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	g.mu.Lock()
	now := time.Now()
	if now.Before(g.last.Add(g.min)) {
		now = g.last.Add(g.min)
	}
	g.last = now
	g.mu.Unlock()
	return nil
}
