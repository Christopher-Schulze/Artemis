package actions

import (
	"context"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/input"
)

// ==================== click.go tests ====================

// TestTASK2253_NewClickAction verifies creation
// (spec L4020: click w/ human-like movement).
func TestTASK2253_NewClickAction(t *testing.T) {
	a := NewClickAction("e5")
	if a.Ref != "e5" {
		t.Errorf("ref: got %s, want e5", a.Ref)
	}
	if a.Button != "left" {
		t.Error("default button should be left")
	}
}

// TestTASK2253_ClickExecute verifies execution
// (spec L4020: click w/ human-like movement).
func TestTASK2253_ClickExecute(t *testing.T) {
	a := NewClickAction("e5")
	result := a.Execute(context.Background())
	if !result.Success {
		t.Error("click should succeed")
	}
	if result.Ref != "e5" {
		t.Errorf("ref: got %s, want e5", result.Ref)
	}
}

// TestTASK2253_ClickExecuteEmptyRef verifies empty ref fails.
func TestTASK2253_ClickExecuteEmptyRef(t *testing.T) {
	a := NewClickAction("")
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("empty ref should fail")
	}
}

// TestTASK2253_GenerateClickPath verifies path generation
// (spec L4020: click w/ human-like movement).
func TestTASK2253_GenerateClickPath(t *testing.T) {
	path := GenerateClickPath(
		input.MousePoint{X: 0, Y: 0},
		input.MousePoint{X: 100, Y: 100},
	)
	if len(path.Points) == 0 {
		t.Error("path should not be empty")
	}
}

// TestTASK2253_ClickWithMovement verifies combined click
// (spec L4020: click w/ human-like movement).
func TestTASK2253_ClickWithMovement(t *testing.T) {
	result := ClickWithMovement(
		context.Background(),
		"e5",
		input.MousePoint{X: 0, Y: 0},
		input.MousePoint{X: 200, Y: 200},
	)
	if !result.Success {
		t.Error("click with movement should succeed")
	}
}

// TestTASK2253_ClickResultString verifies String.
func TestTASK2253_ClickResultString(t *testing.T) {
	r := ClickResult{Success: true, Ref: "e5", Duration: 10 * time.Millisecond}
	s := r.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== type.go tests ====================

// TestTASK2253_NewTypeAction verifies creation
// (spec L4020: text input w/ keystroke timing).
func TestTASK2253_NewTypeAction(t *testing.T) {
	a := NewTypeAction("e3", "hello")
	if a.Ref != "e3" {
		t.Errorf("ref: got %s, want e3", a.Ref)
	}
	if a.Text != "hello" {
		t.Errorf("text: got %s, want hello", a.Text)
	}
	if a.Delay <= 0 {
		t.Error("delay should be positive")
	}
}

// TestTASK2253_TypeExecute verifies execution
// (spec L4020: text input w/ keystroke timing).
func TestTASK2253_TypeExecute(t *testing.T) {
	a := NewTypeAction("e3", "hello")
	result := a.Execute(context.Background())
	if !result.Success {
		t.Error("type should succeed")
	}
	if result.CharsTyped != 5 {
		t.Errorf("chars: got %d, want 5", result.CharsTyped)
	}
}

// TestTASK2253_TypeExecuteEmptyRef verifies empty ref fails.
func TestTASK2253_TypeExecuteEmptyRef(t *testing.T) {
	a := NewTypeAction("", "hello")
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("empty ref should fail")
	}
}

// TestTASK2253_TypeExecuteEmptyText verifies empty text fails.
func TestTASK2253_TypeExecuteEmptyText(t *testing.T) {
	a := NewTypeAction("e3", "")
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("empty text should fail")
	}
}

// TestTASK2253_TypeEstimatedDuration verifies duration estimate
// (spec L4020: text input w/ keystroke timing).
func TestTASK2253_TypeEstimatedDuration(t *testing.T) {
	a := NewTypeAction("e3", "hello")
	d := a.EstimatedDuration()
	if d <= 0 {
		t.Error("estimated duration should be positive")
	}
	// 5 chars * 50ms = 250ms
	if d != 250*time.Millisecond {
		t.Errorf("duration: got %v, want 250ms", d)
	}
}

// TestTASK2253_TypeEstimatedDurationEmpty verifies empty text.
func TestTASK2253_TypeEstimatedDurationEmpty(t *testing.T) {
	a := NewTypeAction("e3", "")
	if a.EstimatedDuration() != 0 {
		t.Error("empty text should have 0 duration")
	}
}

// ==================== form.go tests ====================

// TestTASK2253_FormActionTypes verifies action type constants
// (spec L4020: form fill/select/check/submit).
func TestTASK2253_FormActionTypes(t *testing.T) {
	if FormActionFill != "fill" {
		t.Error("fill mismatch")
	}
	if FormActionSelect != "select" {
		t.Error("select mismatch")
	}
	if FormActionCheck != "check" {
		t.Error("check mismatch")
	}
	if FormActionSubmit != "submit" {
		t.Error("submit mismatch")
	}
}

// TestTASK2253_NewFormFill verifies fill creation
// (spec L4020: form fill).
func TestTASK2253_NewFormFill(t *testing.T) {
	a := NewFormFill("e5", "test value")
	if a.Type != FormActionFill {
		t.Error("type should be fill")
	}
	if a.Value != "test value" {
		t.Error("value mismatch")
	}
}

// TestTASK2253_FormFillExecute verifies fill execution
// (spec L4020: form fill).
func TestTASK2253_FormFillExecute(t *testing.T) {
	a := NewFormFill("e5", "test")
	result := a.Execute(context.Background())
	if !result.Success {
		t.Error("fill should succeed")
	}
}

// TestTASK2253_FormFillEmptyValue verifies empty value fails.
func TestTASK2253_FormFillEmptyValue(t *testing.T) {
	a := NewFormFill("e5", "")
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("empty value should fail")
	}
}

// TestTASK2253_FormSelectExecute verifies select execution
// (spec L4020: select).
func TestTASK2253_FormSelectExecute(t *testing.T) {
	a := NewFormSelect("e5", "option1")
	result := a.Execute(context.Background())
	if !result.Success {
		t.Error("select should succeed")
	}
}

// TestTASK2253_FormCheckExecute verifies check execution
// (spec L4020: check).
func TestTASK2253_FormCheckExecute(t *testing.T) {
	a := NewFormCheck("e5")
	result := a.Execute(context.Background())
	if !result.Success {
		t.Error("check should succeed")
	}
}

// TestTASK2253_FormSubmitExecute verifies submit execution
// (spec L4020: submit).
func TestTASK2253_FormSubmitExecute(t *testing.T) {
	a := NewFormSubmit("e5")
	result := a.Execute(context.Background())
	if !result.Success {
		t.Error("submit should succeed")
	}
}

// TestTASK2253_FormEmptyRef verifies empty ref fails.
func TestTASK2253_FormEmptyRef(t *testing.T) {
	a := NewFormFill("", "value")
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("empty ref should fail")
	}
}

// TestTASK2253_FormBatch verifies batch execution
// (spec L4020: form fill/select/check/submit).
func TestTASK2253_FormBatch(t *testing.T) {
	actions := []FormAction{
		NewFormFill("e1", "user"),
		NewFormFill("e2", "pass"),
		NewFormSubmit("e3"),
	}
	results := FormBatch(context.Background(), actions)
	if len(results) != 3 {
		t.Errorf("results: got %d, want 3", len(results))
	}
	for _, r := range results {
		if !r.Success {
			t.Error("all results should succeed")
		}
	}
}

// TestTASK2253_IsValidFormActionType verifies validation.
func TestTASK2253_IsValidFormActionType(t *testing.T) {
	if !IsValidFormActionType(FormActionFill) {
		t.Error("fill should be valid")
	}
	if IsValidFormActionType(FormActionType("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// ==================== scroll.go tests ====================

// TestTASK2253_ScrollDirections verifies direction constants
// (spec L4020: scroll w/ easeInOut).
func TestTASK2253_ScrollDirections(t *testing.T) {
	if ScrollUp != "up" {
		t.Error("up mismatch")
	}
	if ScrollDown != "down" {
		t.Error("down mismatch")
	}
	if ScrollLeft != "left" {
		t.Error("left mismatch")
	}
	if ScrollRight != "right" {
		t.Error("right mismatch")
	}
}

// TestTASK2253_NewScrollAction verifies creation
// (spec L4020: scroll w/ easeInOut).
func TestTASK2253_NewScrollAction(t *testing.T) {
	a := NewScrollAction(ScrollDown, 500)
	if a.Direction != ScrollDown {
		t.Error("direction should be down")
	}
	if a.Amount != 500 {
		t.Error("amount should be 500")
	}
}

// TestTASK2253_ScrollExecute verifies execution
// (spec L4020: scroll w/ easeInOut).
func TestTASK2253_ScrollExecute(t *testing.T) {
	a := NewScrollAction(ScrollDown, 500)
	result := a.Execute(context.Background())
	if !result.Success {
		t.Error("scroll should succeed")
	}
}

// TestTASK2253_ScrollExecuteZeroAmount verifies zero amount fails.
func TestTASK2253_ScrollExecuteZeroAmount(t *testing.T) {
	a := NewScrollAction(ScrollDown, 0)
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("zero amount should fail")
	}
}

// TestTASK2253_ScrollInvalidDirection verifies invalid direction fails.
func TestTASK2253_ScrollInvalidDirection(t *testing.T) {
	a := NewScrollAction(ScrollDirection("invalid"), 100)
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("invalid direction should fail")
	}
}

// TestTASK2253_EaseInOut verifies easeInOut function
// (spec L4020: scroll w/ easeInOut).
func TestTASK2253_EaseInOut(t *testing.T) {
	// t=0 should return 0
	if EaseInOut(0) != 0 {
		t.Error("easeInOut(0) should be 0")
	}
	// t=1 should return 1
	if EaseInOut(1) != 1 {
		t.Error("easeInOut(1) should be 1")
	}
	// t=0.5 should return 0.5 (symmetric)
	val := EaseInOut(0.5)
	if val < 0.4 || val > 0.6 {
		t.Errorf("easeInOut(0.5) should be ~0.5, got %f", val)
	}
}

// TestTASK2253_ComputeScrollSteps verifies step computation
// (spec L4020: scroll w/ easeInOut).
func TestTASK2253_ComputeScrollSteps(t *testing.T) {
	steps := ComputeScrollSteps(1000, 10)
	if len(steps) != 10 {
		t.Errorf("steps: got %d, want 10", len(steps))
	}
	// Total should approximately equal 1000
	total := 0
	for _, s := range steps {
		total += s
	}
	if total != 1000 {
		t.Errorf("total: got %d, want 1000", total)
	}
}

// TestTASK2253_IsValidScrollDirection verifies validation.
func TestTASK2253_IsValidScrollDirection(t *testing.T) {
	if !IsValidScrollDirection(ScrollUp) {
		t.Error("up should be valid")
	}
	if IsValidScrollDirection(ScrollDirection("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// ==================== resolve.go tests ====================

// TestTASK2253_ResolveSelectorRef verifies element ref resolution
// (spec L4020: unified selector resolution).
func TestTASK2253_ResolveSelectorRef(t *testing.T) {
	r, err := ResolveSelector("e123")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r.Kind != SelectorKindRef {
		t.Errorf("kind: got %s, want ref", r.Kind)
	}
	if !r.IsRef() {
		t.Error("should be ref")
	}
}

// TestTASK2253_ResolveSelectorXPath verifies XPath resolution
// (spec L4020: unified selector resolution).
func TestTASK2253_ResolveSelectorXPath(t *testing.T) {
	r, err := ResolveSelector("//div[@id='main']")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r.Kind != SelectorKindXPath {
		t.Errorf("kind: got %s, want xpath", r.Kind)
	}
	if !r.IsXPath() {
		t.Error("should be xpath")
	}
}

// TestTASK2253_ResolveSelectorCSS verifies CSS resolution
// (spec L4020: unified selector resolution).
func TestTASK2253_ResolveSelectorCSS(t *testing.T) {
	r, err := ResolveSelector("div.container")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if r.Kind != SelectorKindCSS {
		t.Errorf("kind: got %s, want css", r.Kind)
	}
	if !r.IsCSS() {
		t.Error("should be css")
	}
}

// TestTASK2253_ResolveSelectorEmpty verifies empty selector errors.
func TestTASK2253_ResolveSelectorEmpty(t *testing.T) {
	_, err := ResolveSelector("")
	if err == nil {
		t.Error("empty selector should error")
	}
}

// TestTASK2253_IsElementRef verifies element ref detection
// (spec L4020: unified selector resolution).
func TestTASK2253_IsElementRef(t *testing.T) {
	if !IsElementRef("e5") {
		t.Error("e5 should be a ref")
	}
	if !IsElementRef("e123") {
		t.Error("e123 should be a ref")
	}
	if IsElementRef("div") {
		t.Error("div should NOT be a ref")
	}
	if IsElementRef("e") {
		t.Error("e alone should NOT be a ref")
	}
	if IsElementRef("eabc") {
		t.Error("eabc should NOT be a ref")
	}
}

// TestTASK2253_ResolveAndValidate verifies validation
// (spec L4020: unified selector resolution).
func TestTASK2253_ResolveAndValidate(t *testing.T) {
	r, err := ResolveAndValidate(context.Background(), "e5")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !r.IsRef() {
		t.Error("should be ref")
	}
}

// TestTASK2253_IsValidSelectorKind verifies validation.
func TestTASK2253_IsValidSelectorKind(t *testing.T) {
	if !IsValidSelectorKind(SelectorKindRef) {
		t.Error("ref should be valid")
	}
	if !IsValidSelectorKind(SelectorKindCSS) {
		t.Error("css should be valid")
	}
	if IsValidSelectorKind(SelectorKind("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestTASK2253_ResolvedSelectorIsMethods verifies Is* methods.
func TestTASK2253_ResolvedSelectorIsMethods(t *testing.T) {
	ref := ResolvedSelector{Kind: SelectorKindRef}
	css := ResolvedSelector{Kind: SelectorKindCSS}
	xpath := ResolvedSelector{Kind: SelectorKindXPath}
	text := ResolvedSelector{Kind: SelectorKindText}
	if !ref.IsRef() || ref.IsCSS() || ref.IsXPath() || ref.IsText() {
		t.Error("ref Is* methods wrong")
	}
	if !css.IsCSS() || css.IsRef() {
		t.Error("css Is* methods wrong")
	}
	if !xpath.IsXPath() || xpath.IsRef() {
		t.Error("xpath Is* methods wrong")
	}
	if !text.IsText() || text.IsRef() {
		t.Error("text Is* methods wrong")
	}
}

// ==================== full spec parity test ====================

// TestTASK2253_FullSpecParity verifies all 5 spec-mandated files
// (spec L4020: click.go, type.go, form.go, scroll.go, resolve.go).
func TestTASK2253_FullSpecParity(t *testing.T) {
	ctx := context.Background()

	// 1. click.go - click w/ human-like movement
	clickResult := NewClickAction("e5").Execute(ctx)
	if !clickResult.Success {
		t.Error("click.go: click should succeed")
	}

	// 2. type.go - text input w/ keystroke timing
	typeResult := NewTypeAction("e3", "hello").Execute(ctx)
	if !typeResult.Success {
		t.Error("type.go: type should succeed")
	}

	// 3. form.go - form fill/select/check/submit
	formResult := NewFormFill("e5", "value").Execute(ctx)
	if !formResult.Success {
		t.Error("form.go: fill should succeed")
	}

	// 4. scroll.go - scroll w/ easeInOut
	scrollResult := NewScrollAction(ScrollDown, 500).Execute(ctx)
	if !scrollResult.Success {
		t.Error("scroll.go: scroll should succeed")
	}

	// 5. resolve.go - unified selector resolution
	resolved, err := ResolveSelector("e5")
	if err != nil || !resolved.IsRef() {
		t.Error("resolve.go: should resolve e5 as ref")
	}
}
