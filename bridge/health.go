package bridge

import (
	"fmt"
	"sync"
	"time"
)

// HealthState is the bridge subsystem health surface (spec ss28.16 / bridge health.go).
type HealthState string

const (
	HealthHealthy    HealthState = "healthy"
	HealthDegraded   HealthState = "degraded"
	HealthRecovering HealthState = "recovering"
)

// HealthChecker tracks consecutive probe failures and recovery transitions.
type HealthChecker struct {
	mu        sync.Mutex
	threshold int
	failures  int
	state     HealthState
	lastProbe time.Time
}

func NewHealthChecker(failureThreshold int) *HealthChecker {
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	return &HealthChecker{threshold: failureThreshold, state: HealthHealthy}
}

func (h *HealthChecker) RecordProbe(ok bool) HealthState {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastProbe = time.Now()
	if ok {
		if h.state == HealthDegraded || h.state == HealthRecovering {
			h.state = HealthRecovering
			h.failures = 0
			return HealthRecovering
		}
		h.state = HealthHealthy
		h.failures = 0
		return HealthHealthy
	}
	h.failures++
	if h.failures >= h.threshold {
		h.state = HealthDegraded
		return HealthDegraded
	}
	return h.state
}

func (h *HealthChecker) Check() HealthState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

func (h *HealthChecker) Recover() HealthState {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures = 0
	h.state = HealthHealthy
	return h.state
}

func (h *HealthChecker) FailureCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failures
}

// CheckHealthRecovery runs probeFn until healthy or recovery budget elapses.
func CheckHealthRecovery(probeFn func() bool, checker *HealthChecker, attempts int) (HealthState, error) {
	if checker == nil {
		return "", fmt.Errorf("bridge health: nil checker")
	}
	if attempts <= 0 {
		attempts = 3
	}
	var last HealthState
	for i := 0; i < attempts; i++ {
		last = checker.RecordProbe(probeFn())
		if last == HealthHealthy || last == HealthRecovering {
			return checker.Recover(), nil
		}
	}
	return last, fmt.Errorf("bridge health: recovery failed after %d probes", attempts)
}
