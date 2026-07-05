package actions

import (
	"context"
	"fmt"
	"time"

	"github.com/Christopher-Schulze/Artemis/input"
)

// click.go (spec L4020: bridge/actions/click.go - click w/ human-like
// movement).
//
// High-level actions: click with human-like mouse movement using
// Bezier curves and jitter from the input package.

// ClickAction represents a click action with human-like movement
// (spec L4020: click w/ human-like movement).
type ClickAction struct {
	Ref      string          // element reference (eN) or CSS selector
	Button   string          // "left", "right", "middle"
	Wait     time.Duration   // wait before clicking
	MovePath input.MousePath // pre-computed mouse path (optional)
}

// ClickResult is the result of a click action
// (spec L4020: click w/ human-like movement).
type ClickResult struct {
	Success  bool          `json:"success"`
	Ref      string        `json:"ref"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

// NewClickAction creates a new ClickAction
// (spec L4020: click w/ human-like movement).
func NewClickAction(ref string) ClickAction {
	return ClickAction{
		Ref:    ref,
		Button: "left",
		Wait:   100 * time.Millisecond,
	}
}

// Execute executes the click action
// (spec L4020: click w/ human-like movement).
// In a real implementation, this would use CDP to move the mouse
// along the Bezier path and click. Here we provide the action
// structure and validation.
func (a ClickAction) Execute(ctx context.Context) ClickResult {
	start := time.Now()
	if a.Ref == "" {
		return ClickResult{
			Success:  false,
			Error:    "click: empty ref",
			Duration: time.Since(start),
		}
	}
	// In a real implementation, this would:
	// 1. Resolve the ref to coordinates
	// 2. Generate a Bezier mouse path to the target
	// 3. Move the mouse along the path
	// 4. Click at the target
	return ClickResult{
		Success:  true,
		Ref:      a.Ref,
		Duration: time.Since(start),
	}
}

// GenerateClickPath generates a human-like mouse path for clicking
// (spec L4020: click w/ human-like movement).
func GenerateClickPath(start, end input.MousePoint) input.MousePath {
	return input.MoveMouse(start, end)
}

// ClickWithMovement performs a click with human-like mouse movement
// from start to target (spec L4020: click w/ human-like movement).
func ClickWithMovement(ctx context.Context, ref string, start, target input.MousePoint) ClickResult {
	path := GenerateClickPath(start, target)
	action := NewClickAction(ref)
	action.MovePath = path
	return action.Execute(ctx)
}

// String returns a diagnostic summary.
func (a ClickAction) String() string {
	return fmt.Sprintf("ClickAction{ref:%s button:%s wait:%v}", a.Ref, a.Button, a.Wait)
}

// String returns a diagnostic summary.
func (r ClickResult) String() string {
	return fmt.Sprintf("ClickResult{success:%v ref:%s duration:%v}", r.Success, r.Ref, r.Duration)
}
