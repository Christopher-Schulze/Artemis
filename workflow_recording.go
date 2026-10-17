package artemis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/observe"
)

// workflow_recording.go is the browser producer of the Workflow Capture
// Recorder (spec L4551-L4563): while a page records, every ActChromiumPage
// action appends an owned before/action/after record with stable selectors
// and, for low-confidence selectors, real fallback pixels. Typed and filled
// values and AX node values are never recorded.

const maxRecordedAXNodes = 200

// StartWorkflowRecording begins recording actions on a session page.
func (s *Session) StartWorkflowRecording(pageID, operatorID, name string, config observe.WorkflowCaptureConfig) *TaskError {
	s.chromiumMu.Lock()
	defer s.chromiumMu.Unlock()
	page := s.chromiumPages[pageID]
	if page == nil {
		return newTaskError(TaskErrorPageNotFound, "workflow_record", fmt.Errorf("unknown page %q", pageID))
	}
	if page.recorder != nil {
		return newTaskError(TaskErrorInvalidTransition, "workflow_record", errors.New("page is already recording"))
	}
	config.Enabled = true
	config.CaptureScreenshot = func(ctx context.Context, reason string) (*observe.CapturedScreenshot, error) {
		var shot struct {
			Data string `json:"data"`
		}
		if err := page.page.Call(ctx, "Page.captureScreenshot", screenshotParams{Format: "png"}, &shot); err != nil {
			return nil, err
		}
		if shot.Data == "" {
			return nil, errors.New("browser returned no pixels")
		}
		return &observe.CapturedScreenshot{DataB64: shot.Data, Reason: reason, Timestamp: time.Now().UTC()}, nil
	}
	page.recorder = observe.NewWorkflowCaptureRecorder(config, operatorID, name)
	return nil
}

type screenshotParams struct {
	Format string `json:"format"`
}

// StopWorkflowRecording finalizes and returns the page's recording.
func (s *Session) StopWorkflowRecording(pageID string) (*observe.WorkflowRecording, *TaskError) {
	s.chromiumMu.Lock()
	page := s.chromiumPages[pageID]
	var recorder *observe.WorkflowCaptureRecorder
	if page != nil {
		recorder, page.recorder = page.recorder, nil
	}
	s.chromiumMu.Unlock()
	if recorder == nil {
		return nil, newTaskError(TaskErrorInvalidTransition, "workflow_record", fmt.Errorf("page %q is not recording", pageID))
	}
	return recorder.Stop(), nil
}

func (s *Session) pageRecorder(page *ownedChromiumPage) *observe.WorkflowCaptureRecorder {
	s.chromiumMu.Lock()
	defer s.chromiumMu.Unlock()
	return page.recorder
}

// recordedAX projects a snapshot without node values.
func recordedAX(snapshot bridgeobserve.Snapshot) *observe.CapturedAXSnapshot {
	captured := &observe.CapturedAXSnapshot{Timestamp: snapshot.CapturedAt, NodeCount: len(snapshot.Nodes)}
	for _, node := range snapshot.Nodes {
		if len(captured.Nodes) == maxRecordedAXNodes {
			break
		}
		if !node.Interactive && node.Name == "" {
			continue
		}
		captured.Nodes = append(captured.Nodes, observe.AXNode{ID: node.Ref, BackendNodeID: node.BackendNodeID, Role: node.Role, Name: node.Name, Focused: node.Focused, Disabled: node.Disabled, Visible: node.Visible, Depth: node.Depth, FrameID: node.FrameID})
	}
	return captured
}

// recordedSelectors derives stable selectors for the acted node: its
// accessible role/name (confidence by uniqueness) and its element id.
func recordedSelectors(snapshot bridgeobserve.Snapshot, ref string) []observe.CapturedSelector {
	if ref == "" {
		return nil
	}
	var acted *bridgeobserve.Node
	for index := range snapshot.Nodes {
		if snapshot.Nodes[index].Ref == ref {
			acted = &snapshot.Nodes[index]
			break
		}
	}
	if acted == nil {
		return []observe.CapturedSelector{{Kind: observe.SelectorAX, Value: ref, Confidence: 0}}
	}
	same := 0
	for _, node := range snapshot.Nodes {
		if node.Role == acted.Role && node.Name == acted.Name {
			same++
		}
	}
	confidence := 1.0
	if same > 1 || acted.Name == "" {
		confidence = 0.4
	}
	selectors := []observe.CapturedSelector{{Kind: observe.SelectorAX, Value: acted.Role + ":" + acted.Name, Confidence: confidence}}
	if id := acted.Attributes["id"]; id != "" {
		selectors = append(selectors, observe.CapturedSelector{Kind: observe.SelectorCSS, Value: "[id=" + fmt.Sprintf("%q", id) + "]", Confidence: 0.9})
	}
	return selectors
}

type recordedArgs struct {
	Kind actions.Kind `json:"kind"`
	Ref  string       `json:"ref,omitempty"`
	URL  string       `json:"url,omitempty"`
	Key  string       `json:"key,omitempty"`
	Text string       `json:"text,omitempty"`
	// Sensitive marks input into a password field.
	Sensitive bool `json:"sensitive,omitempty"`
}

// recordedResult keeps only the postcondition type and verdict: expected and
// actual values may carry typed input.
type recordedResult struct {
	Success    bool                 `json:"success"`
	Failure    actions.FailureClass `json:"failure,omitempty"`
	Effect     actions.EffectState  `json:"effect,omitempty"`
	PostType   string               `json:"postconditionType,omitempty"`
	PostPassed bool                 `json:"postconditionPassed"`
}

// recordAction appends one owned record; the returned error is reported on
// the outcome, the action itself has already happened.
func recordAction(ctx context.Context, recorder *observe.WorkflowCaptureRecorder, before, after bridgeobserve.Snapshot, request actions.Request, outcome actions.Outcome, started time.Time) error {
	args := recordedArgs{Kind: request.Kind, Ref: request.Ref, URL: request.URL, Key: request.Key}
	if request.Text != "" || request.Value != "" {
		args.Text = "[redacted]"
	}
	for _, node := range before.Nodes {
		if node.Ref == request.Ref && request.Ref != "" && strings.EqualFold(node.Attributes["type"], "password") {
			args.Sensitive = true
		}
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return err
	}
	resultJSON, err := json.Marshal(recordedResult{Success: outcome.Success, Failure: outcome.Failure, Effect: outcome.Evidence.Effect, PostType: outcome.Evidence.Postcondition.Type, PostPassed: outcome.Evidence.Postcondition.Passed})
	if err != nil {
		return err
	}
	return recorder.RecordContext(ctx, observe.ActionCapture{
		ToolCall:      observe.CapturedToolCall{Tool: string(request.Kind), ArgsJSON: string(argsJSON), ResultJSON: string(resultJSON), ErrorClass: string(outcome.Failure), DurationMs: time.Since(started).Milliseconds()},
		AXBefore:      recordedAX(before),
		AXAfter:       recordedAX(after),
		Selectors:     recordedSelectors(before, request.Ref),
		IsDestructive: destructiveActions[request.Kind],
	})
}

// WorkflowFromRecording turns a finished recording into a reusable
// definition for origins: every recorded input becomes a param (password
// fields sensitive), targets come from the recorded AX selectors and failed
// actions are dropped. Destructive steps carry no compensation note, so the
// definition stays inactive until an operator adds one.
func WorkflowFromRecording(recording *observe.WorkflowRecording, origins []string) (WorkflowDefinition, error) {
	if recording == nil || len(recording.Actions) == 0 {
		return WorkflowDefinition{}, errors.New("workflow recording has no actions")
	}
	def := WorkflowDefinition{Version: WorkflowVersion, Name: recording.Name, Origins: append([]string(nil), origins...)}
	for _, action := range recording.Actions {
		var args recordedArgs
		if err := json.Unmarshal([]byte(action.ToolCall.ArgsJSON), &args); err != nil {
			return WorkflowDefinition{}, fmt.Errorf("workflow recording action %d: %w", action.Index, err)
		}
		var result recordedResult
		if err := json.Unmarshal([]byte(action.ToolCall.ResultJSON), &result); err != nil {
			return WorkflowDefinition{}, fmt.Errorf("workflow recording action %d: %w", action.Index, err)
		}
		if !result.Success || !workflowActions[args.Kind] {
			continue
		}
		step := WorkflowStep{Action: args.Kind, URL: args.URL, Key: args.Key}
		if args.Ref != "" {
			target, err := recordedTarget(action, args.Ref)
			if err != nil {
				return WorkflowDefinition{}, fmt.Errorf("workflow recording action %d: %w", action.Index, err)
			}
			step.Target = target
		}
		if args.Text != "" {
			name := fmt.Sprintf("input_%d", action.Index)
			def.Params = append(def.Params, WorkflowParam{Name: name, Sensitive: args.Sensitive})
			step.Param = name
		}
		def.Steps = append(def.Steps, step)
	}
	if len(def.Steps) == 0 {
		return WorkflowDefinition{}, errors.New("workflow recording has no successful replayable actions")
	}
	return def, nil
}

func recordedTarget(action observe.ActionCapture, ref string) (*WorkflowTarget, error) {
	if action.AXBefore != nil {
		for _, node := range action.AXBefore.Nodes {
			if node.ID == ref && node.Role != "" && node.Name != "" {
				return &WorkflowTarget{Role: node.Role, Name: node.Name}, nil
			}
		}
	}
	return nil, fmt.Errorf("target %s has no recorded role and name", ref)
}
