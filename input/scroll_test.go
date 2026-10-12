package input

import (
	"testing"
	"time"
)

// ==================== ScrollStrategy tests ====================

// TestTASK2242_ScrollStrategyConstants verifies the strategy constants
// (spec L4095).
func TestTASK2242_ScrollStrategyConstants(t *testing.T) {
	if ScrollStrategyDirect != "direct" {
		t.Error("ScrollStrategyDirect mismatch")
	}
	if ScrollStrategyEased != "eased" {
		t.Error("ScrollStrategyEased mismatch")
	}
}

// ==================== DetectInfiniteScroll tests ====================

// TestTASK2242_DetectInfiniteScrollNone verifies no indicators -> not
// infinite (spec L4095).
func TestTASK2242_DetectInfiniteScrollNone(t *testing.T) {
	result := DetectInfiniteScroll(false, false)
	if result.IsInfiniteScroll {
		t.Error("no indicators should not be infinite scroll")
	}
	if result.HasIntersectionObserver {
		t.Error("HasIntersectionObserver should be false")
	}
	if result.HasScrollListeners {
		t.Error("HasScrollListeners should be false")
	}
}

// TestTASK2242_DetectInfiniteScrollIO verifies IntersectionObserver ->
// infinite (spec L4095).
func TestTASK2242_DetectInfiniteScrollIO(t *testing.T) {
	result := DetectInfiniteScroll(true, false)
	if !result.IsInfiniteScroll {
		t.Error("IntersectionObserver should indicate infinite scroll")
	}
	if !result.HasIntersectionObserver {
		t.Error("HasIntersectionObserver should be true")
	}
}

// TestTASK2242_DetectInfiniteScrollListeners verifies scroll listeners
// -> infinite (spec L4095).
func TestTASK2242_DetectInfiniteScrollListeners(t *testing.T) {
	result := DetectInfiniteScroll(false, true)
	if !result.IsInfiniteScroll {
		t.Error("scroll listeners should indicate infinite scroll")
	}
	if !result.HasScrollListeners {
		t.Error("HasScrollListeners should be true")
	}
}

// TestTASK2242_DetectInfiniteScrollBoth verifies both indicators ->
// infinite (spec L4095).
func TestTASK2242_DetectInfiniteScrollBoth(t *testing.T) {
	result := DetectInfiniteScroll(true, true)
	if !result.IsInfiniteScroll {
		t.Error("both indicators should indicate infinite scroll")
	}
}

// TestTASK2242_DetectInfiniteScrollReason verifies reason is set.
func TestTASK2242_DetectInfiniteScrollReason(t *testing.T) {
	result := DetectInfiniteScroll(true, true)
	if result.Reason == "" {
		t.Error("reason should not be empty")
	}
	result2 := DetectInfiniteScroll(false, false)
	if result2.Reason == "" {
		t.Error("reason should not be empty for non-infinite")
	}
}

// ==================== ExecuteScroll tests ====================

// TestTASK2242_ExecuteScrollInfinite verifies infinite scroll uses
// direct strategy (spec L4095: Infinite scroll -> direct scrollTo()).
func TestTASK2242_ExecuteScrollInfinite(t *testing.T) {
	detection := DetectInfiniteScroll(true, false)
	result := ExecuteScroll(1000, 10, detection)
	if result.Strategy != ScrollStrategyDirect {
		t.Errorf("strategy: got %s, want direct", result.Strategy)
	}
	if result.IsInfinite != true {
		t.Error("should be infinite")
	}
	if len(result.Offsets) != 1 {
		t.Errorf("direct scroll should have 1 offset, got %d", len(result.Offsets))
	}
	if result.Offsets[0] != 1000 {
		t.Errorf("offset: got %f, want 1000", result.Offsets[0])
	}
}

// TestTASK2242_ExecuteScrollNormal verifies normal scroll uses eased
// strategy (spec L4095: No infinite scroll -> easeInOut curve).
func TestTASK2242_ExecuteScrollNormal(t *testing.T) {
	detection := DetectInfiniteScroll(false, false)
	result := ExecuteScroll(1000, 10, detection)
	if result.Strategy != ScrollStrategyEased {
		t.Errorf("strategy: got %s, want eased", result.Strategy)
	}
	if result.IsInfinite != false {
		t.Error("should not be infinite")
	}
	if len(result.Offsets) != 10 {
		t.Errorf("eased scroll should have 10 offsets, got %d", len(result.Offsets))
	}
}

// TestTASK2242_ExecuteScrollDirectDuration verifies direct scroll has
// zero duration (instant).
func TestTASK2242_ExecuteScrollDirectDuration(t *testing.T) {
	detection := DetectInfiniteScroll(true, false)
	result := ExecuteScroll(500, 10, detection)
	if result.Duration != 0 {
		t.Errorf("direct scroll duration: got %v, want 0", result.Duration)
	}
}

// TestTASK2242_ExecuteScrollEasedDuration verifies eased scroll has
// non-zero duration.
func TestTASK2242_ExecuteScrollEasedDuration(t *testing.T) {
	detection := DetectInfiniteScroll(false, false)
	result := ExecuteScroll(500, 10, detection)
	if result.Duration <= 0 {
		t.Error("eased scroll should have positive duration")
	}
	// ~16ms per step * 10 steps = 160ms
	expected := 10 * 16 * time.Millisecond
	if result.Duration != expected {
		t.Errorf("duration: got %v, want %v", result.Duration, expected)
	}
}

// TestTASK2242_ExecuteScrollDefaultSteps verifies default steps when
// steps<=0.
func TestTASK2242_ExecuteScrollDefaultSteps(t *testing.T) {
	detection := DetectInfiniteScroll(false, false)
	result := ExecuteScroll(1000, 0, detection)
	if result.Steps != 10 {
		t.Errorf("default steps: got %d, want 10", result.Steps)
	}
	if len(result.Offsets) != 10 {
		t.Errorf("offsets: got %d, want 10", len(result.Offsets))
	}
}

// TestTASK2242_ExecuteScrollNegativeSteps verifies negative steps use
// default.
func TestTASK2242_ExecuteScrollNegativeSteps(t *testing.T) {
	detection := DetectInfiniteScroll(false, false)
	result := ExecuteScroll(1000, -5, detection)
	if result.Steps != 10 {
		t.Errorf("default steps: got %d, want 10", result.Steps)
	}
}

// ==================== Scroll (convenience) tests ====================

// TestTASK2242_ScrollInfinite verifies the Scroll convenience function
// with infinite scroll.
func TestTASK2242_ScrollInfinite(t *testing.T) {
	result := Scroll(800, 10, true, false)
	if !result.IsDirect() {
		t.Error("should use direct strategy")
	}
	if !result.IsInfinite {
		t.Error("should be infinite")
	}
}

// TestTASK2242_ScrollNormal verifies the Scroll convenience function
// with normal scroll.
func TestTASK2242_ScrollNormal(t *testing.T) {
	result := Scroll(800, 10, false, false)
	if !result.IsEased() {
		t.Error("should use eased strategy")
	}
	if result.IsInfinite {
		t.Error("should not be infinite")
	}
}

// ==================== ScrollResult method tests ====================

// TestTASK2242_ScrollResultIsDirect verifies IsDirect method.
func TestTASK2242_ScrollResultIsDirect(t *testing.T) {
	result := Scroll(100, 5, true, false)
	if !result.IsDirect() {
		t.Error("should be direct")
	}
	if result.IsEased() {
		t.Error("should not be eased")
	}
}

// TestTASK2242_ScrollResultIsEased verifies IsEased method.
func TestTASK2242_ScrollResultIsEased(t *testing.T) {
	result := Scroll(100, 5, false, false)
	if !result.IsEased() {
		t.Error("should be eased")
	}
	if result.IsDirect() {
		t.Error("should not be direct")
	}
}

// TestTASK2242_ScrollResultTotalOffset verifies TotalOffset equals
// Distance.
func TestTASK2242_ScrollResultTotalOffset(t *testing.T) {
	result := Scroll(1000, 10, false, false)
	total := result.TotalOffset()
	if total != 1000 {
		t.Errorf("total offset: got %f, want 1000", total)
	}
}

// TestTASK2242_ScrollResultTotalOffsetDirect verifies TotalOffset for
// direct scroll.
func TestTASK2242_ScrollResultTotalOffsetDirect(t *testing.T) {
	result := Scroll(500, 10, true, false)
	total := result.TotalOffset()
	if total != 500 {
		t.Errorf("total offset: got %f, want 500", total)
	}
}

// TestTASK2242_ScrollResultString verifies String method.
func TestTASK2242_ScrollResultString(t *testing.T) {
	result := Scroll(100, 5, false, false)
	s := result.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== full spec parity test ====================

// TestTASK2242_FullSpecParity verifies full spec parity for L4095
// (spec L4095: Scroll Simulation with INFINITE SCROLL GUARD).
func TestTASK2242_FullSpecParity(t *testing.T) {
	// 1. Detect infinite scroll
	detection := DetectInfiniteScroll(true, false)
	if !detection.IsInfiniteScroll {
		t.Error("IntersectionObserver should be detected as infinite")
	}

	// 2. Direct strategy for infinite scroll
	result := ExecuteScroll(1000, 10, detection)
	if result.Strategy != ScrollStrategyDirect {
		t.Error("infinite scroll should use direct strategy")
	}

	// 3. Eased strategy for normal scroll
	detection2 := DetectInfiniteScroll(false, false)
	result2 := ExecuteScroll(1000, 10, detection2)
	if result2.Strategy != ScrollStrategyEased {
		t.Error("normal scroll should use eased strategy")
	}

	// 4. Eased scroll has multiple steps
	if result2.Steps <= 1 {
		t.Error("eased scroll should have multiple steps")
	}

	// 5. Direct scroll is instant
	if result.Duration != 0 {
		t.Error("direct scroll should be instant")
	}

	// 6. Eased scroll has duration
	if result2.Duration <= 0 {
		t.Error("eased scroll should have duration")
	}

	// 7. Total offset equals distance
	if result.TotalOffset() != 1000 {
		t.Error("direct total offset should equal distance")
	}
	if result2.TotalOffset() != 1000 {
		t.Error("eased total offset should equal distance")
	}

	// 8. Scroll convenience function works
	r := Scroll(500, 10, true, true)
	if !r.IsInfinite || !r.IsDirect() {
		t.Error("Scroll with both indicators should be infinite+direct")
	}
}
