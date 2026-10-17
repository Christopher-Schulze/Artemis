package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/Christopher-Schulze/Artemis/network"
)

// ReserveMemory extends the existing session owner with live transient input/image
// admission. Cancellation does not release capacity before actual unmapping.
func (c *sessionBudgetController) ReserveMemory(ctx context.Context, size int64) (func() error, error) {
	releaseSession, err := c.reserveTransientMemory(ctx, size)
	if err != nil || c.sharedMemory == nil {
		return releaseSession, err
	}
	releaseShared, err := c.sharedMemory.ReserveMemory(ctx, size)
	if err != nil {
		return nil, errors.Join(err, releaseSession())
	}
	if releaseShared == nil {
		return nil, errors.Join(errors.New("engine: shared memory admission supplied no release"), releaseSession())
	}
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			releaseErr = errors.Join(releaseShared(), releaseSession())
		})
		return releaseErr
	}, nil
}

func (c *sessionBudgetController) reserveTransientMemory(ctx context.Context, size int64) (func() error, error) {
	if ctx == nil || size <= 0 {
		return nil, fmt.Errorf("engine: transient memory context and positive size required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return nil, err
	}
	if size > c.limits.MaxMemoryBytes-c.memoryBytes {
		observed := int64(math.MaxInt64)
		if size <= math.MaxInt64-c.memoryBytes {
			observed = c.memoryBytes + size
		}
		// Transient memory is engine-wide admission: refuse this work only.
		err := &BudgetError{Resource: BudgetMemoryBytes, Limit: c.limits.MaxMemoryBytes, Observed: observed}
		c.mu.Unlock()
		return nil, err
	}
	c.memoryBytes += size
	c.mu.Unlock()
	return c.transientMemoryRelease(size), nil
}

func (c *sessionBudgetController) transientMemoryRelease(size int64) func() error {
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.memoryBytes < size {
				releaseErr = errors.New("engine: transient memory ownership underflow")
				return
			}
			c.memoryBytes -= size
		})
		return releaseErr
	}
}

// NewMemoryAdmission creates the same validated session owner without a V8
// runtime. The caller must invoke closeOwner after all borrowed work joins.
func NewMemoryAdmission(limits SessionBudget) (owner network.MemoryAdmission, closeOwner func() error, err error) {
	limits.applyDefaults(DefaultDownloadDiskBytes)
	if err := limits.validate(); err != nil {
		return nil, nil, err
	}
	controller := newSessionBudgetController(limits, "", nil)
	return controller, controller.close, nil
}
