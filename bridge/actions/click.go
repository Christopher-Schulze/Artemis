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

// Execute rejects execution without an owned Chromium runtime.
func (a ClickAction) Execute(ctx context.Context) ClickResult {
	start := time.Now()
	if a.Ref == "" {
		return ClickResult{
			Success:  false,
			Error:    "click: empty ref",
			Duration: time.Since(start),
		}
	}
	return ClickResult{Ref: a.Ref, Duration: time.Since(start), Error: "click: owned Runtime required"}
}

func (a ClickAction) ExecuteWith(ctx context.Context, runtime *Runtime) ClickResult {
	start := time.Now()
	if runtime == nil {
		return ClickResult{Ref: a.Ref, Duration: time.Since(start), Error: "click: runtime required"}
	}
	human := &HumanProfile{MovePath: a.MovePath.Points, PreClickDelay: a.Wait}
	out := runtime.Execute(ctx, Request{Kind: KindClick, Ref: a.Ref, Button: a.Button, Human: human})
	return ClickResult{Success: out.Success, Ref: a.Ref, Duration: out.Evidence.Duration, Error: out.Error}
}

// GenerateClickPath generates a human-like mouse path for clicking
// (spec L4020: click w/ human-like movement).
func GenerateClickPath(start, end input.MousePoint) input.MousePath {
	return input.MoveMouse(start, end)
}

// ClickWithMovement clicks ref on runtime after moving along a generated
// human-like path from start to target (spec L4020: click w/ human-like movement).
func ClickWithMovement(ctx context.Context, runtime *Runtime, ref string, start, target input.MousePoint) ClickResult {
	action := NewClickAction(ref)
	action.MovePath = GenerateClickPath(start, target)
	return action.ExecuteWith(ctx, runtime)
}

// String returns a diagnostic summary.
func (a ClickAction) String() string {
	return fmt.Sprintf("ClickAction{ref:%s button:%s wait:%v}", a.Ref, a.Button, a.Wait)
}

// String returns a diagnostic summary.
func (r ClickResult) String() string {
	return fmt.Sprintf("ClickResult{success:%v ref:%s duration:%v}", r.Success, r.Ref, r.Duration)
}
