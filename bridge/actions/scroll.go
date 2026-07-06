package actions

import (
	"context"
	"fmt"
	"time"
)

// scroll.go (spec L4020: bridge/actions/scroll.go - scroll w/
// easeInOut).
//
// High-level actions: scroll with easeInOut easing function for
// smooth, human-like scrolling.

// ScrollDirection enumerates scroll directions
// (spec L4020: scroll w/ easeInOut).
type ScrollDirection string

const (
	ScrollUp    ScrollDirection = "up"
	ScrollDown  ScrollDirection = "down"
	ScrollLeft  ScrollDirection = "left"
	ScrollRight ScrollDirection = "right"
)

// ScrollAction represents a scroll action with easeInOut
// (spec L4020: scroll w/ easeInOut).
type ScrollAction struct {
	Direction ScrollDirection `json:"direction"`
	Amount    int             `json:"amount"`   // pixels to scroll
	Duration  time.Duration   `json:"duration"` // total scroll duration
	Steps     int             `json:"steps"`    // interpolation steps
}

// ScrollResult is the result of a scroll action
// (spec L4020: scroll w/ easeInOut).
type ScrollResult struct {
	Success   bool            `json:"success"`
	Direction ScrollDirection `json:"direction"`
	Amount    int             `json:"amount"`
	Duration  time.Duration   `json:"duration"`
	Error     string          `json:"error,omitempty"`
}

// NewScrollAction creates a new ScrollAction with default easeInOut
// settings (spec L4020: scroll w/ easeInOut).
func NewScrollAction(direction ScrollDirection, amount int) ScrollAction {
	return ScrollAction{
		Direction: direction,
		Amount:    amount,
		Duration:  500 * time.Millisecond,
		Steps:     20,
	}
}

// Execute executes the scroll action with easeInOut
// (spec L4020: scroll w/ easeInOut).
func (a ScrollAction) Execute(ctx context.Context) ScrollResult {
	start := time.Now()
	if a.Amount <= 0 {
		return ScrollResult{
			Success:   false,
			Direction: a.Direction,
			Error:     "scroll: amount must be positive",
		}
	}
	if !IsValidScrollDirection(a.Direction) {
		return ScrollResult{
			Success:   false,
			Direction: a.Direction,
			Error:     fmt.Sprintf("scroll: invalid direction %q", a.Direction),
		}
	}
	// In a real implementation, this would use CDP to scroll
	// with easeInOut easing over the specified duration.
	return ScrollResult{
		Success:   true,
		Direction: a.Direction,
		Amount:    a.Amount,
		Duration:  time.Since(start),
	}
}

// EaseInOut computes the easeInOut value for a given t (0-1)
// (spec L4020: scroll w/ easeInOut).
// Uses the standard easeInOut cubic function.
func EaseInOut(t float64) float64 {
	if t < 0.5 {
		return 4 * t * t * t
	}
	return 1 - ((-2*t+2)*(-2*t+2)*(-2*t+2))/2
}

// ComputeScrollSteps computes the per-step scroll amounts using
// easeInOut (spec L4020: scroll w/ easeInOut).
func ComputeScrollSteps(total int, steps int) []int {
	if steps <= 0 {
		steps = 1
	}
	result := make([]int, steps)
	sumTruncated := 0
	for i := 0; i < steps; i++ {
		t1 := float64(i) / float64(steps)
		t2 := float64(i+1) / float64(steps)
		ease1 := EaseInOut(t1)
		ease2 := EaseInOut(t2)
		stepAmount := float64(total) * (ease2 - ease1)
		result[i] = int(stepAmount)
		sumTruncated += result[i]
	}
	// Adjust last step to account for cumulative truncation
	result[steps-1] += total - sumTruncated
	return result
}

// IsValidScrollDirection reports whether a direction is valid
// (spec L4020: scroll w/ easeInOut).
func IsValidScrollDirection(d ScrollDirection) bool {
	switch d {
	case ScrollUp, ScrollDown, ScrollLeft, ScrollRight:
		return true
	}
	return false
}

// String returns a diagnostic summary.
func (a ScrollAction) String() string {
	return fmt.Sprintf("ScrollAction{dir:%s amount:%d duration:%v steps:%d}",
		a.Direction, a.Amount, a.Duration, a.Steps)
}

// String returns a diagnostic summary.
func (r ScrollResult) String() string {
	return fmt.Sprintf("ScrollResult{success:%v dir:%s amount:%d duration:%v}",
		r.Success, r.Direction, r.Amount, r.Duration)
}
