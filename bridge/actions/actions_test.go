package actions

import (
	"context"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/input"
)

// ==================== click.go tests ====================

// TestNewClickAction verifies creation
// (click w/ human-like movement).
func TestNewClickAction(t *testing.T) {
	a := NewClickAction("e5")
	if a.Ref != "e5" {
		t.Errorf("ref: got %s, want e5", a.Ref)
	}
	if a.Button != "left" {
		t.Error("default button should be left")
	}
}

// TestClickExecute verifies execution
// (click w/ human-like movement).
func TestClickExecute(t *testing.T) {
	a := NewClickAction("e5")
	result := a.Execute(context.Background())
	if result.Success || result.Error == "" {
		t.Error("click without runtime must fail explicitly")
	}
	if result.Ref != "e5" {
		t.Errorf("ref: got %s, want e5", result.Ref)
	}
}

// TestClickExecuteEmptyRef verifies empty ref fails.
func TestClickExecuteEmptyRef(t *testing.T) {
	a := NewClickAction("")
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("empty ref should fail")
	}
}

// TestGenerateClickPath verifies path generation
// (click w/ human-like movement).
func TestGenerateClickPath(t *testing.T) {
	path := GenerateClickPath(
		input.MousePoint{X: 0, Y: 0},
		input.MousePoint{X: 100, Y: 100},
	)
	if len(path.Points) == 0 {
		t.Error("path should not be empty")
	}
}

// TestClickWithMovement verifies combined click
// (click w/ human-like movement).
func TestClickWithMovement(t *testing.T) {
	result := ClickWithMovement(
		context.Background(),
		nil,
		"e5",
		input.MousePoint{X: 0, Y: 0},
		input.MousePoint{X: 200, Y: 200},
	)
	if result.Success || result.Error == "" {
		t.Error("click without runtime must fail explicitly")
	}
}

// TestClickResultString verifies String.
func TestClickResultString(t *testing.T) {
	r := ClickResult{Success: true, Ref: "e5", Duration: 10 * time.Millisecond}
	s := r.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// ==================== type.go tests ====================

// TestNewTypeAction verifies creation
// (text input w/ keystroke timing).
func TestNewTypeAction(t *testing.T) {
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

// TestTypeExecute verifies execution
// (text input w/ keystroke timing).
func TestTypeExecute(t *testing.T) {
	a := NewTypeAction("e3", "hello")
	result := a.Execute(context.Background())
	if result.Success || result.Error == "" {
		t.Error("type without runtime must fail explicitly")
	}
	if result.CharsTyped != 0 {
		t.Errorf("chars: got %d, want 0", result.CharsTyped)
	}
}

// TestTypeExecuteEmptyRef verifies empty ref fails.
func TestTypeExecuteEmptyRef(t *testing.T) {
	a := NewTypeAction("", "hello")
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("empty ref should fail")
	}
}

// TestTypeExecuteEmptyText verifies empty text fails.
func TestTypeExecuteEmptyText(t *testing.T) {
	a := NewTypeAction("e3", "")
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("empty text should fail")
	}
}

// TestTypeEstimatedDuration verifies duration estimate
// (text input w/ keystroke timing).
func TestTypeEstimatedDuration(t *testing.T) {
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

// TestTypeEstimatedDurationEmpty verifies empty text.
func TestTypeEstimatedDurationEmpty(t *testing.T) {
	a := NewTypeAction("e3", "")
	if a.EstimatedDuration() != 0 {
		t.Error("empty text should have 0 duration")
	}
}

// ==================== form.go tests ====================

// TestFormActionTypes verifies action type constants
// (form fill/select/check/submit).
func TestFormActionTypes(t *testing.T) {
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

// TestNewFormFill verifies fill creation
// (form fill).
func TestNewFormFill(t *testing.T) {
	a := NewFormFill("e5", "test value")
	if a.Type != FormActionFill {
		t.Error("type should be fill")
	}
	if a.Value != "test value" {
		t.Error("value mismatch")
	}
}

// TestFormFillExecute verifies fill execution
// (form fill).
func TestFormFillExecute(t *testing.T) {
	a := NewFormFill("e5", "test")
	result := a.Execute(context.Background())
	if result.Success || result.Error == "" {
		t.Error("fill without runtime must fail explicitly")
	}
}

// TestFormFillEmptyValue verifies empty value fails.
func TestFormFillEmptyValue(t *testing.T) {
	a := NewFormFill("e5", "")
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("empty value should fail")
	}
}

// TestFormSelectExecute verifies select execution
// (select).
func TestFormSelectExecute(t *testing.T) {
	a := NewFormSelect("e5", "option1")
	result := a.Execute(context.Background())
	if result.Success || result.Error == "" {
		t.Error("select without runtime must fail explicitly")
	}
}

// TestFormCheckExecute verifies check execution
// (check).
func TestFormCheckExecute(t *testing.T) {
	a := NewFormCheck("e5")
	result := a.Execute(context.Background())
	if result.Success || result.Error == "" {
		t.Error("check without runtime must fail explicitly")
	}
}

// TestFormSubmitExecute verifies submit execution
// (submit).
func TestFormSubmitExecute(t *testing.T) {
	a := NewFormSubmit("e5")
	result := a.Execute(context.Background())
	if result.Success || result.Error == "" {
		t.Error("submit without runtime must fail explicitly")
	}
}

// TestFormEmptyRef verifies empty ref fails.
func TestFormEmptyRef(t *testing.T) {
	a := NewFormFill("", "value")
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("empty ref should fail")
	}
}

// TestFormBatch verifies batch execution
// (form fill/select/check/submit).
func TestFormBatch(t *testing.T) {
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
		if r.Success || r.Error == "" {
			t.Error("batch without runtime must fail explicitly")
		}
	}
}

// TestIsValidFormActionType verifies validation.
func TestIsValidFormActionType(t *testing.T) {
	if !IsValidFormActionType(FormActionFill) {
		t.Error("fill should be valid")
	}
	if IsValidFormActionType(FormActionType("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// ==================== scroll.go tests ====================

// TestScrollDirections verifies direction constants
// (scroll w/ easeInOut).
func TestScrollDirections(t *testing.T) {
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

// TestNewScrollAction verifies creation
// (scroll w/ easeInOut).
func TestNewScrollAction(t *testing.T) {
	a := NewScrollAction(ScrollDown, 500)
	if a.Direction != ScrollDown {
		t.Error("direction should be down")
	}
	if a.Amount != 500 {
		t.Error("amount should be 500")
	}
}

// TestScrollExecute verifies execution
// (scroll w/ easeInOut).
func TestScrollExecute(t *testing.T) {
	a := NewScrollAction(ScrollDown, 500)
	result := a.Execute(context.Background())
	if result.Success || result.Error == "" {
		t.Error("scroll without runtime must fail explicitly")
	}
}

// TestScrollExecuteZeroAmount verifies zero amount fails.
func TestScrollExecuteZeroAmount(t *testing.T) {
	a := NewScrollAction(ScrollDown, 0)
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("zero amount should fail")
	}
}

// TestScrollInvalidDirection verifies invalid direction fails.
func TestScrollInvalidDirection(t *testing.T) {
	a := NewScrollAction(ScrollDirection("invalid"), 100)
	result := a.Execute(context.Background())
	if result.Success {
		t.Error("invalid direction should fail")
	}
}

// TestEaseInOut verifies easeInOut function
// (scroll w/ easeInOut).
func TestEaseInOut(t *testing.T) {
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

// TestComputeScrollSteps verifies step computation
// (scroll w/ easeInOut).
func TestComputeScrollSteps(t *testing.T) {
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

// TestIsValidScrollDirection verifies validation.
func TestIsValidScrollDirection(t *testing.T) {
	if !IsValidScrollDirection(ScrollUp) {
		t.Error("up should be valid")
	}
	if IsValidScrollDirection(ScrollDirection("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// ==================== resolve.go tests ====================

// TestActionsResolveSelectorRef verifies element ref resolution
// (unified selector resolution).
func TestActionsResolveSelectorRef(t *testing.T) {
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

// TestResolveSelectorXPath verifies XPath resolution
// (unified selector resolution).
func TestResolveSelectorXPath(t *testing.T) {
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

// TestResolveSelectorCSS verifies CSS resolution
// (unified selector resolution).
func TestResolveSelectorCSS(t *testing.T) {
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

// TestResolveSelectorEmpty verifies empty selector errors.
func TestResolveSelectorEmpty(t *testing.T) {
	_, err := ResolveSelector("")
	if err == nil {
		t.Error("empty selector should error")
	}
}

// TestIsElementRef verifies element ref detection
// (unified selector resolution).
func TestIsElementRef(t *testing.T) {
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

// TestResolveAndValidate verifies validation
// (unified selector resolution).
func TestResolveAndValidate(t *testing.T) {
	r, err := ResolveAndValidate(context.Background(), "e5")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !r.IsRef() {
		t.Error("should be ref")
	}
}

// TestIsValidSelectorKind verifies validation.
func TestIsValidSelectorKind(t *testing.T) {
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

// TestResolvedSelectorIsMethods verifies Is* methods.
func TestResolvedSelectorIsMethods(t *testing.T) {
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

// ==================== parity test ====================

// TestActionsParity verifies all 5 spec-mandated files
// (click.go, type.go, form.go, scroll.go, resolve.go).
func TestActionsParity(t *testing.T) {
	ctx := context.Background()

	// 1. click.go - click w/ human-like movement
	clickResult := NewClickAction("e5").Execute(ctx)
	if clickResult.Success || clickResult.Error == "" {
		t.Error("click.go: runtime-less click must fail")
	}

	// 2. type.go - text input w/ keystroke timing
	typeResult := NewTypeAction("e3", "hello").Execute(ctx)
	if typeResult.Success || typeResult.Error == "" {
		t.Error("type.go: runtime-less type must fail")
	}

	// 3. form.go - form fill/select/check/submit
	formResult := NewFormFill("e5", "value").Execute(ctx)
	if formResult.Success || formResult.Error == "" {
		t.Error("form.go: runtime-less fill must fail")
	}

	// 4. scroll.go - scroll w/ easeInOut
	scrollResult := NewScrollAction(ScrollDown, 500).Execute(ctx)
	if scrollResult.Success || scrollResult.Error == "" {
		t.Error("scroll.go: runtime-less scroll must fail")
	}

	// 5. resolve.go - unified selector resolution
	resolved, err := ResolveSelector("e5")
	if err != nil || !resolved.IsRef() {
		t.Error("resolve.go: should resolve e5 as ref")
	}
}
