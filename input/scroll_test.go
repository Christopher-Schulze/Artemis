package input

import (
	"testing"
	"time"
)

// ==================== ScrollStrategy tests ====================

// TestScrollStrategyConstants verifies the strategy constants.
func TestScrollStrategyConstants(t *testing.T) {
	if ScrollStrategyDirect != "direct" {
		t.Error("ScrollStrategyDirect mismatch")
	}
	if ScrollStrategyEased != "eased" {
		t.Error("ScrollStrategyEased mismatch")
	}
}

// ==================== DetectInfiniteScroll tests ====================

// TestDetectInfiniteScrollNone verifies no indicators -> not
// infinite.
func TestDetectInfiniteScrollNone(t *testing.T) {
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

// TestDetectInfiniteScrollIO verifies IntersectionObserver ->
// infinite.
func TestDetectInfiniteScrollIO(t *testing.T) {
	result := DetectInfiniteScroll(true, false)
	if !result.IsInfiniteScroll {
		t.Error("IntersectionObserver should indicate infinite scroll")
	}
	if !result.HasIntersectionObserver {
		t.Error("HasIntersectionObserver should be true")
	}
}

// TestDetectInfiniteScrollListeners verifies scroll listeners
// -> infinite.
func TestDetectInfiniteScrollListeners(t *testing.T) {
	result := DetectInfiniteScroll(false, true)
	if !result.IsInfiniteScroll {
		t.Error("scroll listeners should indicate infinite scroll")
	}
	if !result.HasScrollListeners {
		t.Error("HasScrollListeners should be true")
	}
}

// TestDetectInfiniteScrollBoth verifies both indicators ->
// infinite.
func TestDetectInfiniteScrollBoth(t *testing.T) {
	result := DetectInfiniteScroll(true, true)
	if !result.IsInfiniteScroll {
		t.Error("both indicators should indicate infinite scroll")
	}
}

// TestDetectInfiniteScrollReason verifies reason is set.
func TestDetectInfiniteScrollReason(t *testing.T) {
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

// TestExecuteScrollInfinite verifies infinite scroll uses
// direct strategy (Infinite scroll -> direct scrollTo()).
func TestExecuteScrollInfinite(t *testing.T) {
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

// TestExecuteScrollNormal verifies normal scroll uses eased
// strategy (No infinite scroll -> easeInOut curve).
func TestExecuteScrollNormal(t *testing.T) {
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

// TestExecuteScrollDirectDuration verifies direct scroll has
// zero duration (instant).
func TestExecuteScrollDirectDuration(t *testing.T) {
	detection := DetectInfiniteScroll(true, false)
	result := ExecuteScroll(500, 10, detection)
	if result.Duration != 0 {
		t.Errorf("direct scroll duration: got %v, want 0", result.Duration)
	}
}

// TestExecuteScrollEasedDuration verifies eased scroll has
// non-zero duration.
func TestExecuteScrollEasedDuration(t *testing.T) {
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

// TestExecuteScrollDefaultSteps verifies default steps when
// steps<=0.
func TestExecuteScrollDefaultSteps(t *testing.T) {
	detection := DetectInfiniteScroll(false, false)
	result := ExecuteScroll(1000, 0, detection)
	if result.Steps != 10 {
		t.Errorf("default steps: got %d, want 10", result.Steps)
	}
	if len(result.Offsets) != 10 {
		t.Errorf("offsets: got %d, want 10", len(result.Offsets))
	}
}

// TestExecuteScrollNegativeSteps verifies negative steps use
// default.
func TestExecuteScrollNegativeSteps(t *testing.T) {
	detection := DetectInfiniteScroll(false, false)
	result := ExecuteScroll(1000, -5, detection)
	if result.Steps != 10 {
		t.Errorf("default steps: got %d, want 10", result.Steps)
	}
}

// ==================== Scroll (convenience) tests ====================

// TestScrollInfinite verifies the Scroll convenience function
// with infinite scroll.
func TestScrollInfinite(t *testing.T) {
	result := Scroll(800, 10, true, false)
	if !result.IsDirect() {
		t.Error("should use direct strategy")
	}
	if !result.IsInfinite {
		t.Error("should be infinite")
	}
}

// TestScrollNormal verifies the Scroll convenience function
// with normal scroll.
func TestScrollNormal(t *testing.T) {
	result := Scroll(800, 10, false, false)
	if !result.IsEased() {
		t.Error("should use eased strategy")
	}
	if result.IsInfinite {
		t.Error("should not be infinite")
	}
}

// ==================== ScrollResult method tests ====================

// TestScrollResultIsDirect verifies IsDirect method.
func TestScrollResultIsDirect(t *testing.T) {
	result := Scroll(100, 5, true, false)
	if !result.IsDirect() {
		t.Error("should be direct")
	}
	if result.IsEased() {
		t.Error("should not be eased")
	}
}

// TestScrollResultIsEased verifies IsEased method.
func TestScrollResultIsEased(t *testing.T) {
	result := Scroll(100, 5, false, false)
	if !result.IsEased() {
		t.Error("should be eased")
	}
	if result.IsDirect() {
		t.Error("should not be direct")
	}
}

// TestScrollResultTotalOffset verifies TotalOffset equals
// Distance.
func TestScrollResultTotalOffset(t *testing.T) {
	result := Scroll(1000, 10, false, false)
	total := result.TotalOffset()
	if total != 1000 {
		t.Errorf("total offset: got %f, want 1000", total)
	}
}

// TestScrollResultTotalOffsetDirect verifies TotalOffset for
// direct scroll.
func TestScrollResultTotalOffsetDirect(t *testing.T) {
	result := Scroll(500, 10, true, false)
	total := result.TotalOffset()
	if total != 500 {
		t.Errorf("total offset: got %f, want 500", total)
	}
}

// TestScrollResultString verifies String method.
func TestScrollResultString(t *testing.T) {
	result := Scroll(100, 5, false, false)
	s := result.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== parity test ====================

// TestScrollParity verifies full parity.
// (Scroll Simulation with INFINITE SCROLL GUARD).
func TestScrollParity(t *testing.T) {
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
