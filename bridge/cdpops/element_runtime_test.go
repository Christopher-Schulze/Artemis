package cdpops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

type elementDescription struct {
	backendNodeID int64
	nodeName      string
	attributes    []string
}

type elementCaller struct {
	nodeIDs          []int64
	descriptions     map[int64]elementDescription
	describeErrors   map[int64]error
	malformedBox     bool
	frameCalls       []string
	unexpectedMethod string
}

func (c *elementCaller) Call(_ context.Context, method string, params any, result any) error {
	return c.respond(method, params, result)
}

func (c *elementCaller) CallFrame(_ context.Context, frameID, method string, params, result any) error {
	c.frameCalls = append(c.frameCalls, frameID+":"+method)
	return c.respond(method, params, result)
}

func (c *elementCaller) FrameSessions() map[string]string {
	return map[string]string{"child": "session-child"}
}

func (c *elementCaller) respond(method string, params any, result any) error {
	var raw []byte
	switch method {
	case "DOM.getDocument":
		raw = []byte(`{"root":{"nodeId":1}}`)
	case "DOM.querySelectorAll":
		encoded, err := json.Marshal(querySelectorAllResult{NodeIDs: c.nodeIDs})
		if err != nil {
			return err
		}
		raw = encoded
	case "DOM.describeNode":
		request, ok := params.(describeNodeParams)
		if !ok {
			return fmt.Errorf("describe params = %T", params)
		}
		if err := c.describeErrors[request.NodeID]; err != nil {
			return err
		}
		description, ok := c.descriptions[request.NodeID]
		if !ok {
			return fmt.Errorf("description missing for node %d", request.NodeID)
		}
		target, ok := result.(*describeNodeResult)
		if !ok {
			return fmt.Errorf("describe result = %T", result)
		}
		target.Node.BackendNodeID = description.backendNodeID
		target.Node.NodeName = description.nodeName
		target.Node.Attributes = append([]string(nil), description.attributes...)
		return nil
	case "DOM.getBoxModel":
		if c.malformedBox {
			raw = []byte(`{"model":{"content":[1,2],"padding":[1,2],"border":[1,2],"margin":[1,2],"width":100,"height":40}}`)
		} else {
			raw = []byte(`{"model":{"content":[10,20,110,20,110,60,10,60],"padding":[10,20,110,20,110,60,10,60],"border":[10,20,110,20,110,60,10,60],"margin":[10,20,110,20,110,60,10,60],"width":100,"height":40}}`)
		}
	default:
		c.unexpectedMethod = method
		return fmt.Errorf("unexpected method %s", method)
	}
	return json.Unmarshal(raw, result)
}

type staticActionabilitySource struct {
	snapshot bridgeobserve.Snapshot
	err      error
}

func (s staticActionabilitySource) Capture(context.Context, bridgeobserve.Mode, string) (bridgeobserve.Snapshot, error) {
	return s.snapshot, s.err
}

func TestElementClientUsesCanonicalActionabilityAndGeometry(t *testing.T) {
	caller := &elementCaller{
		nodeIDs: []int64{7},
		descriptions: map[int64]elementDescription{7: {
			backendNodeID: 42, nodeName: "BUTTON",
			attributes: []string{"id", "submit", "type", "button", "class", "primary action"},
		}},
	}
	source := staticActionabilitySource{snapshot: bridgeobserve.Snapshot{Nodes: []bridgeobserve.Node{{
		BackendNodeID: 42, FrameID: "main", Role: "button", Name: "Submit",
		Attributes: map[string]string{"id": "submit", "type": "button", "class": "primary action"},
		Box:        &bridgeobserve.Rect{X: 10, Y: 20, Width: 100, Height: 40},
		Visible:    true, Interactive: true, Interactable: true, Hit: bridgeobserve.HitClear,
	}}}}
	client, err := newElementClientWithActionability(caller, source)
	if err != nil {
		t.Fatal(err)
	}
	elements, err := client.QuerySelector(context.Background(), "button")
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 1 || elements[0].Box.Width != 100 || elements[0].Box.Content.X1 != 10 ||
		elements[0].ID != "submit" || elements[0].Actionability != ActionabilityReady || !elements[0].Clickable {
		t.Fatalf("unexpected elements: %#v", elements)
	}
	if len(elements[0].Classes) != 2 || elements[0].Classes[0] != "primary" || elements[0].Classes[1] != "action" {
		t.Fatalf("classes = %#v", elements[0].Classes)
	}
	x, y := GetElementCenter(elements[0].Box)
	if x != 60 || y != 40 {
		t.Fatalf("center=(%v,%v), want (60,40)", x, y)
	}
}

func TestElementClientRejectsMalformedGeometry(t *testing.T) {
	client, _ := newElementClientWithActionability(&elementCaller{malformedBox: true}, staticActionabilitySource{})
	if _, err := client.GetBoxModel(context.Background(), 42); err == nil {
		t.Fatal("malformed CDP quad accepted")
	}
}

func TestElementClientPreservesMixedLayoutAndActionabilityStates(t *testing.T) {
	caller := &elementCaller{
		nodeIDs: []int64{7, 8, 12, 9, 10, 11},
		descriptions: map[int64]elementDescription{
			7: {backendNodeID: 42, nodeName: "BUTTON"}, 8: {backendNodeID: 43, nodeName: "BUTTON"},
			12: {backendNodeID: 47, nodeName: "BUTTON"},
			9:  {backendNodeID: 44, nodeName: "BUTTON", attributes: []string{"disabled", ""}},
			10: {backendNodeID: 45, nodeName: "BUTTON"}, 11: {backendNodeID: 46, nodeName: "DIV"},
		},
	}
	box := &bridgeobserve.Rect{X: 0, Y: 0, Width: 80, Height: 30}
	source := staticActionabilitySource{snapshot: bridgeobserve.Snapshot{Nodes: []bridgeobserve.Node{
		{BackendNodeID: 42, Box: box, Visible: true, Interactive: true, Hit: bridgeobserve.HitClear},
		{BackendNodeID: 43, Interactive: true, Hit: bridgeobserve.HitUnknown},
		{BackendNodeID: 47, Box: box, Visible: false, Interactive: true, Hit: bridgeobserve.HitUnknown},
		{BackendNodeID: 44, Box: box, Visible: true, Interactive: true, Disabled: true, Hit: bridgeobserve.HitClear},
		{BackendNodeID: 45, Box: box, Visible: true, Interactive: true, Hit: bridgeobserve.HitCovered},
		{BackendNodeID: 46, Box: box, Visible: true, Interactive: false, Hit: bridgeobserve.HitClear},
	}}}
	client, err := newElementClientWithActionability(caller, source)
	if err != nil {
		t.Fatal(err)
	}
	elements, err := client.QuerySelector(context.Background(), "button,div")
	if err != nil {
		t.Fatal(err)
	}
	want := []ActionabilityState{ActionabilityReady, ActionabilityNoLayout, ActionabilityHidden, ActionabilityDisabled, ActionabilityCovered, ActionabilityNonInteractive}
	if len(elements) != len(want) {
		t.Fatalf("elements = %d, want %d: %#v", len(elements), len(want), elements)
	}
	for i, state := range want {
		if elements[i].Actionability != state {
			t.Fatalf("element %d actionability = %s, want %s", i, elements[i].Actionability, state)
		}
		if elements[i].Clickable != (state == ActionabilityReady) {
			t.Fatalf("element %d clickable = %v for %s", i, elements[i].Clickable, state)
		}
	}
	if caller.unexpectedMethod != "" {
		t.Fatalf("query used unexpected per-node protocol method %s", caller.unexpectedMethod)
	}
}

func TestElementClientPreservesDetachedMatchesAndRejectsProtocolFailure(t *testing.T) {
	caller := &elementCaller{
		nodeIDs: []int64{7, 8}, descriptions: map[int64]elementDescription{8: {backendNodeID: 44, nodeName: "BUTTON"}},
		describeErrors: map[int64]error{7: errors.New("Could not find node with given id")},
	}
	client, _ := newElementClientWithActionability(caller, staticActionabilitySource{snapshot: bridgeobserve.Snapshot{}})
	elements, err := client.QuerySelector(context.Background(), "button")
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 2 || elements[0].Actionability != ActionabilityDetached || elements[1].Actionability != ActionabilityDetached {
		t.Fatalf("detached matches = %#v", elements)
	}
	caller.describeErrors[7] = errors.New("transport closed")
	if _, err := client.QuerySelector(context.Background(), "button"); err == nil {
		t.Fatal("protocol failure was classified as a detached node")
	}
}

func TestElementClientDetachedFastPathAndCanonicalRedaction(t *testing.T) {
	detachedCaller := &elementCaller{
		nodeIDs: []int64{7}, describeErrors: map[int64]error{7: errors.New("node is detached")},
	}
	detachedClient, _ := newElementClientWithActionability(detachedCaller, staticActionabilitySource{err: errors.New("must not capture")})
	elements, err := detachedClient.QuerySelector(context.Background(), "input")
	if err != nil || len(elements) != 1 || elements[0].Actionability != ActionabilityDetached {
		t.Fatalf("detached fast path = %#v, %v", elements, err)
	}
	attachedCaller := &elementCaller{
		nodeIDs: []int64{9}, descriptions: map[int64]elementDescription{9: {backendNodeID: 45, nodeName: "INPUT"}},
	}
	attachedClient, _ := newElementClientWithActionability(attachedCaller, staticActionabilitySource{err: errors.New("snapshot transport failed")})
	if _, queryErr := attachedClient.QuerySelector(context.Background(), "input"); queryErr == nil {
		t.Fatal("canonical observation protocol failure was converted into element state")
	}

	caller := &elementCaller{
		nodeIDs: []int64{8}, descriptions: map[int64]elementDescription{8: {
			backendNodeID: 44, nodeName: "INPUT",
			attributes: []string{"id", "secret", "type", "password", "value", "raw-secret"},
		}},
	}
	source := staticActionabilitySource{snapshot: bridgeobserve.Snapshot{Nodes: []bridgeobserve.Node{{
		BackendNodeID: 44, Tag: "input", Value: "[REDACTED]",
		Attributes: map[string]string{"id": "secret", "type": "password", "value": "[REDACTED]"},
		Box:        &bridgeobserve.Rect{Width: 80, Height: 20}, Visible: true, Interactive: true, Hit: bridgeobserve.HitClear,
	}}}}
	client, _ := newElementClientWithActionability(caller, source)
	elements, err = client.QuerySelector(context.Background(), "input")
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 1 || elements[0].Value != "[REDACTED]" || elements[0].ID != "secret" {
		t.Fatalf("canonical metadata = %#v", elements)
	}
}

func TestElementClientRejectsMalformedAttributesAndRoutesFrameQueries(t *testing.T) {
	malformed := &elementCaller{
		nodeIDs:      []int64{7},
		descriptions: map[int64]elementDescription{7: {backendNodeID: 42, nodeName: "BUTTON", attributes: []string{"id"}}},
	}
	client, _ := newElementClientWithActionability(malformed, staticActionabilitySource{})
	if _, err := client.QuerySelector(context.Background(), "button"); err == nil {
		t.Fatal("malformed attribute pairs were accepted")
	}

	frameCaller := &elementCaller{
		nodeIDs:      []int64{7},
		descriptions: map[int64]elementDescription{7: {backendNodeID: 42, nodeName: "BUTTON"}},
	}
	frameSource := staticActionabilitySource{snapshot: bridgeobserve.Snapshot{Nodes: []bridgeobserve.Node{{
		BackendNodeID: 42, FrameID: "child", Box: &bridgeobserve.Rect{Width: 50, Height: 20},
		Visible: true, Interactive: true, Hit: bridgeobserve.HitUnknown,
	}}}}
	client, _ = newElementClientWithActionability(frameCaller, frameSource)
	elements, err := client.Query(context.Background(), ElementQuery{Selector: "button", FrameID: "child"})
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 1 || elements[0].FrameID != "child" || elements[0].Actionability != ActionabilityHitUnknown || elements[0].Clickable {
		t.Fatalf("frame element = %#v", elements)
	}
	wantCalls := []string{"child:DOM.getDocument", "child:DOM.querySelectorAll", "child:DOM.describeNode"}
	if fmt.Sprint(frameCaller.frameCalls) != fmt.Sprint(wantCalls) {
		t.Fatalf("frame calls = %v, want %v", frameCaller.frameCalls, wantCalls)
	}
}

func TestElementClientVisibilityFilterAndNilBoxClient(t *testing.T) {
	caller := &elementCaller{
		nodeIDs: []int64{7, 8},
		descriptions: map[int64]elementDescription{
			7: {backendNodeID: 42, nodeName: "BUTTON"},
			8: {backendNodeID: 43, nodeName: "BUTTON"},
		},
	}
	box := &bridgeobserve.Rect{Width: 40, Height: 20}
	source := staticActionabilitySource{snapshot: bridgeobserve.Snapshot{Nodes: []bridgeobserve.Node{
		{BackendNodeID: 42, Box: box, Visible: true, Interactive: true, Hit: bridgeobserve.HitClear},
		{BackendNodeID: 43, Box: box, Visible: false, Interactive: true, Hit: bridgeobserve.HitUnknown},
	}}}
	client, _ := newElementClientWithActionability(caller, source)
	visible := true
	elements, err := client.Query(context.Background(), ElementQuery{Selector: "button", Visible: &visible})
	if err != nil || len(elements) != 1 || !elements[0].Visible {
		t.Fatalf("visible filter = %#v, %v", elements, err)
	}
	var nilClient *ElementClient
	if _, err := nilClient.GetBoxModel(context.Background(), 42); !errors.Is(err, ErrCallerRequired) {
		t.Fatalf("nil box client error = %v", err)
	}
}

func TestElementClientPreservesMatchWhenObservationBudgetIsIncomplete(t *testing.T) {
	caller := &elementCaller{
		nodeIDs: []int64{7}, descriptions: map[int64]elementDescription{7: {backendNodeID: 42, nodeName: "BUTTON"}},
	}
	client, _ := newElementClientWithActionability(caller, staticActionabilitySource{snapshot: bridgeobserve.Snapshot{Truncated: true}})
	elements, err := client.QuerySelector(context.Background(), "button")
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 1 || elements[0].Attachment != ElementAttachmentUnknown ||
		elements[0].Layout != ElementLayoutUnknown || elements[0].Actionability != ActionabilityUnavailable || elements[0].Clickable {
		t.Fatalf("incomplete observation match = %#v", elements)
	}
}
