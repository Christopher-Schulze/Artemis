package input

import (
	"fmt"
	"time"
)

// ScrollStrategy enumerates the scroll strategies
// (spec L4095: infinite scroll -> direct scrollTo, no infinite scroll
// -> easeInOut curve).
type ScrollStrategy string

const (
	// ScrollStrategyDirect uses direct scrollTo() for infinite-scroll
	// pages (spec L4095: Infinite scroll -> direct scrollTo()).
	ScrollStrategyDirect ScrollStrategy = "direct"
	// ScrollStrategyEased uses easeInOut curve for normal pages
	// (spec L4095: No infinite scroll -> easeInOut curve).
	ScrollStrategyEased ScrollStrategy = "eased"
)

// ScrollDetectionResult is the result of infinite scroll detection
// (spec L4095: detect IntersectionObserver/scroll-listeners).
type ScrollDetectionResult struct {
	HasIntersectionObserver bool
	HasScrollListeners      bool
	IsInfiniteScroll        bool
	Reason                  string
}

// DetectInfiniteScroll detects whether a page has infinite scroll
// behavior by checking for IntersectionObserver or scroll event
// listeners (spec L4095: Before scroll: detect
// IntersectionObserver/scroll-listeners).
//
// hasIntersectionObserver: true if the page has IntersectionObserver
// registered (common in infinite-scroll feeds).
// hasScrollListeners: true if the page has scroll event listeners
// that load more content on scroll.
//
// Returns IsInfiniteScroll=true if either detector is present.
func DetectInfiniteScroll(hasIntersectionObserver, hasScrollListeners bool) ScrollDetectionResult {
	result := ScrollDetectionResult{
		HasIntersectionObserver: hasIntersectionObserver,
		HasScrollListeners:      hasScrollListeners,
	}
	if hasIntersectionObserver || hasScrollListeners {
		result.IsInfiniteScroll = true
		if hasIntersectionObserver && hasScrollListeners {
			result.Reason = "IntersectionObserver + scroll listeners detected"
		} else if hasIntersectionObserver {
			result.Reason = "IntersectionObserver detected"
		} else {
			result.Reason = "Scroll listeners detected"
		}
	} else {
		result.IsInfiniteScroll = false
		result.Reason = "No infinite scroll indicators detected"
	}
	return result
}

// ScrollResult is the result of a scroll operation
// (spec L4095: Scroll Simulation).
type ScrollResult struct {
	Strategy   ScrollStrategy
	Distance   float64
	Steps      int
	Duration   time.Duration
	Offsets    []float64
	IsInfinite bool
	Reason     string
}

// ExecuteScroll performs a scroll operation using the appropriate
// strategy (spec L4095: Infinite scroll -> direct scrollTo(), no
// infinite scroll -> easeInOut curve). Activation: StealthStealth
// (Patch 24).
//
// distance: total scroll distance in pixels.
// steps: number of scroll increments (for eased strategy).
// detection: the result of DetectInfiniteScroll.
func ExecuteScroll(distance float64, steps int, detection ScrollDetectionResult) ScrollResult {
	if detection.IsInfiniteScroll {
		// Infinite scroll -> direct scrollTo() (spec L4095).
		return ScrollResult{
			Strategy:   ScrollStrategyDirect,
			Distance:   distance,
			Steps:      1,
			Duration:   0, // direct scrollTo is instant
			Offsets:    []float64{distance},
			IsInfinite: true,
			Reason:     detection.Reason,
		}
	}
	// No infinite scroll -> easeInOut curve (spec L4095).
	if steps <= 0 {
		steps = 10 // default steps
	}
	offsets := EaseInOutScroll(distance, steps)
	// Estimate duration: ~16ms per step (60fps frame budget).
	duration := time.Duration(steps*16) * time.Millisecond
	return ScrollResult{
		Strategy:   ScrollStrategyEased,
		Distance:   distance,
		Steps:      steps,
		Duration:   duration,
		Offsets:    offsets,
		IsInfinite: false,
		Reason:     detection.Reason,
	}
}

// Scroll performs the full scroll workflow: detect infinite scroll,
// then execute with the appropriate strategy (spec L4095).
//
// distance: total scroll distance in pixels.
// steps: number of scroll increments (for eased strategy).
// hasIntersectionObserver: true if page has IntersectionObserver.
// hasScrollListeners: true if page has scroll event listeners.
func Scroll(distance float64, steps int, hasIntersectionObserver, hasScrollListeners bool) ScrollResult {
	detection := DetectInfiniteScroll(hasIntersectionObserver, hasScrollListeners)
	return ExecuteScroll(distance, steps, detection)
}

// String returns a human-readable summary of the scroll result.
func (r ScrollResult) String() string {
	return fmt.Sprintf("ScrollResult{strategy:%s distance:%.0f steps:%d duration:%s infinite:%v reason:%s}",
		r.Strategy, r.Distance, r.Steps, r.Duration, r.IsInfinite, r.Reason)
}

// IsDirect reports whether the scroll used the direct strategy.
func (r ScrollResult) IsDirect() bool {
	return r.Strategy == ScrollStrategyDirect
}

// IsEased reports whether the scroll used the eased strategy.
func (r ScrollResult) IsEased() bool {
	return r.Strategy == ScrollStrategyEased
}

// TotalOffset returns the sum of all offsets (should equal Distance).
func (r ScrollResult) TotalOffset() float64 {
	var total float64
	for _, o := range r.Offsets {
		total += o
	}
	return total
}
