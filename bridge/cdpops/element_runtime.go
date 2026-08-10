package cdpops

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

type Caller interface {
	Call(context.Context, string, any, any) error
}

var ErrCallerRequired = errors.New("cdpops: CDP caller required")

type actionabilitySource interface {
	Capture(context.Context, bridgeobserve.Mode, string) (bridgeobserve.Snapshot, error)
}

type ElementClient struct {
	caller        Caller
	actionability actionabilitySource
}

func NewElementClient(caller Caller) (*ElementClient, error) {
	if caller == nil {
		return nil, ErrCallerRequired
	}
	collector, err := bridgeobserve.NewCollector(caller, bridgeobserve.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("element query actionability: %w", err)
	}
	return newElementClientWithActionability(caller, collector)
}

func (c *ElementClient) QuerySelector(ctx context.Context, selector string) ([]ElementInfo, error) {
	return c.Query(ctx, ElementQuery{Selector: selector})
}

// Query resolves a selector in the main document or an attached frame and
// projects canonical observe-owned actionability evidence for every match.
func (c *ElementClient) Query(ctx context.Context, query ElementQuery) ([]ElementInfo, error) {
	selector, err := c.validateQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	pending, err := c.resolveMatches(ctx, query.FrameID, selector)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return make([]ElementInfo, 0), nil
	}
	if allExplicitlyDetached(pending) {
		return projectElements(pending, bridgeobserve.Snapshot{}, query), nil
	}
	snapshot, err := c.actionability.Capture(ctx, bridgeobserve.ModeFull, "")
	if err != nil {
		return nil, fmt.Errorf("element query actionability: %w", err)
	}
	return projectElements(pending, snapshot, query), nil
}

func (c *ElementClient) validateQuery(ctx context.Context, query ElementQuery) (string, error) {
	if ctx == nil {
		return "", errors.New("element query: context required")
	}
	if c == nil || c.caller == nil || c.actionability == nil {
		return "", ErrCallerRequired
	}
	selector := strings.TrimSpace(query.Selector)
	if selector == "" {
		return "", errors.New("element query: selector required")
	}
	return selector, nil
}

func (c *ElementClient) resolveMatches(ctx context.Context, frameID, selector string) ([]pendingElement, error) {
	var document getDocumentResult
	if err := c.call(ctx, frameID, "DOM.getDocument", getDocumentParams{Depth: -1, Pierce: true}, &document); err != nil {
		return nil, fmt.Errorf("element query document: %w", err)
	}
	if document.Root.NodeID <= 0 {
		return nil, errors.New("element query document: invalid root node")
	}
	var matches querySelectorAllResult
	params := querySelectorAllParams{NodeID: document.Root.NodeID, Selector: selector}
	if err := c.call(ctx, frameID, "DOM.querySelectorAll", params, &matches); err != nil {
		return nil, fmt.Errorf("element query selector: %w", err)
	}
	pending := make([]pendingElement, 0, len(matches.NodeIDs))
	for _, nodeID := range matches.NodeIDs {
		match, err := c.describeMatch(ctx, frameID, nodeID)
		if err != nil {
			return nil, err
		}
		pending = append(pending, match)
	}
	return pending, nil
}

func (c *ElementClient) describeMatch(ctx context.Context, frameID string, nodeID int64) (pendingElement, error) {
	if nodeID <= 0 {
		return pendingElement{}, fmt.Errorf("element query selector: invalid matched node ID %d", nodeID)
	}
	var described describeNodeResult
	if err := c.call(ctx, frameID, "DOM.describeNode", describeNodeParams{NodeID: nodeID}, &described); err != nil {
		if isDetachedNodeError(err) {
			return pendingElement{nodeID: nodeID, detached: true}, nil
		}
		return pendingElement{}, fmt.Errorf("element query describe node %d: %w", nodeID, err)
	}
	if described.Node.BackendNodeID <= 0 || strings.TrimSpace(described.Node.NodeName) == "" {
		return pendingElement{}, fmt.Errorf("element query describe node %d: malformed node identity", nodeID)
	}
	attributes, err := attributePairs(described.Node.Attributes)
	if err != nil {
		return pendingElement{}, fmt.Errorf("element query describe node %d: %w", nodeID, err)
	}
	return pendingElement{nodeID: nodeID, backendNodeID: described.Node.BackendNodeID, nodeName: described.Node.NodeName, attributes: attributes}, nil
}

func projectElements(pending []pendingElement, snapshot bridgeobserve.Snapshot, query ElementQuery) []ElementInfo {
	observed := indexObservedNodes(snapshot.Nodes)
	out := make([]ElementInfo, 0, len(pending))
	for _, match := range pending {
		info := elementInfoFromObservation(match, observed[match.backendNodeID], snapshot, query.FrameID)
		if query.Visible != nil && info.Visible != *query.Visible {
			continue
		}
		out = append(out, info)
	}
	return out
}

type pendingElement struct {
	nodeID        int64
	backendNodeID int64
	nodeName      string
	attributes    map[string]string
	detached      bool
}

func newElementClientWithActionability(caller Caller, source actionabilitySource) (*ElementClient, error) {
	if caller == nil || source == nil {
		return nil, ErrCallerRequired
	}
	return &ElementClient{caller: caller, actionability: source}, nil
}

func (c *ElementClient) call(ctx context.Context, frameID, method string, params, result any) error {
	if frameID == "" {
		return c.caller.Call(ctx, method, params, result)
	}
	frameCaller, ok := c.caller.(bridgeobserve.FrameCaller)
	if !ok {
		return fmt.Errorf("element query frame %q: frame-aware CDP caller required", frameID)
	}
	return frameCaller.CallFrame(ctx, frameID, method, params, result)
}

func elementInfoFromObservation(match pendingElement, node *bridgeobserve.Node, snapshot bridgeobserve.Snapshot, requestedFrame string) ElementInfo {
	if match.detached {
		return ElementInfo{
			Ref: fmt.Sprintf("dom:%d", match.nodeID), FrameID: requestedFrame, TagName: match.nodeName,
			Type: match.attributes["type"], Classes: strings.Fields(match.attributes["class"]),
			ID: match.attributes["id"], Name: match.attributes["name"],
			Attachment: ElementDetached, Layout: ElementLayoutAbsent, Hit: bridgeobserve.HitUnknown,
			Actionability: ActionabilityDetached,
		}
	}
	if node == nil {
		state := bridgeobserve.ClassifyMissingActionability(snapshot)
		attachment := ElementDetached
		layout := ElementLayoutAbsent
		if state == ActionabilityUnavailable {
			attachment = ElementAttachmentUnknown
			layout = ElementLayoutUnknown
		}
		return ElementInfo{
			Ref: fmt.Sprintf("dom:%d", match.nodeID), FrameID: requestedFrame, TagName: match.nodeName,
			Type: match.attributes["type"], Classes: strings.Fields(match.attributes["class"]),
			ID: match.attributes["id"], Name: match.attributes["name"],
			Attachment: attachment, Layout: layout, Hit: bridgeobserve.HitUnknown, Actionability: state,
		}
	}
	box := boxModelFromObservedRect(node.Box)
	layout := ElementLayoutAbsent
	if box != nil {
		layout = ElementLayoutPresent
	}
	state := bridgeobserve.ClassifyActionability(node)
	attributes := node.Attributes
	info := ElementInfo{
		Ref: fmt.Sprintf("backend:%d", node.BackendNodeID), FrameID: node.FrameID,
		TagName: match.nodeName, Type: attributes["type"], Text: node.Name, Role: node.Role,
		Classes: strings.Fields(attributes["class"]), ID: attributes["id"],
		Name: attributes["name"], Value: node.Value,
		Attachment: ElementAttached, Layout: layout, Visible: node.Visible, Interactive: node.Interactive,
		Disabled: node.Disabled, Hit: node.Hit, Actionability: state, Box: box,
	}
	info.Clickable = IsElementClickable(&info)
	return info
}

func allExplicitlyDetached(elements []pendingElement) bool {
	for _, element := range elements {
		if !element.detached {
			return false
		}
	}
	return true
}

func indexObservedNodes(nodes []bridgeobserve.Node) map[int64]*bridgeobserve.Node {
	observed := make(map[int64]*bridgeobserve.Node, len(nodes))
	for i := range nodes {
		observed[nodes[i].BackendNodeID] = &nodes[i]
	}
	return observed
}

func boxModelFromObservedRect(rect *bridgeobserve.Rect) *BoxModel {
	if rect == nil {
		return nil
	}
	quad := Quad{
		X1: rect.X, Y1: rect.Y, X2: rect.X + rect.Width, Y2: rect.Y,
		X3: rect.X + rect.Width, Y3: rect.Y + rect.Height, X4: rect.X, Y4: rect.Y + rect.Height,
	}
	return &BoxModel{Content: quad, Padding: quad, Border: quad, Margin: quad, Width: int(math.Round(rect.Width)), Height: int(math.Round(rect.Height))}
}

func isDetachedNodeError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{"could not find node", "no node with given id", "node with given id does not belong", "node is detached"} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func (c *ElementClient) GetBoxModel(ctx context.Context, backendNodeID int64) (*BoxModel, error) {
	if ctx == nil {
		return nil, errors.New("box model: context required")
	}
	if c == nil || c.caller == nil {
		return nil, ErrCallerRequired
	}
	if backendNodeID <= 0 {
		return nil, errors.New("box model: positive backend node ID required")
	}
	var result getBoxModelResult
	if err := c.caller.Call(ctx, "DOM.getBoxModel", getBoxModelParams{BackendNodeID: backendNodeID}, &result); err != nil {
		return nil, fmt.Errorf("box model backend node %d: %w", backendNodeID, err)
	}
	content, err := quadFromSlice(result.Model.Content)
	if err != nil {
		return nil, fmt.Errorf("box model content: %w", err)
	}
	padding, err := quadFromSlice(result.Model.Padding)
	if err != nil {
		return nil, fmt.Errorf("box model padding: %w", err)
	}
	border, err := quadFromSlice(result.Model.Border)
	if err != nil {
		return nil, fmt.Errorf("box model border: %w", err)
	}
	margin, err := quadFromSlice(result.Model.Margin)
	if err != nil {
		return nil, fmt.Errorf("box model margin: %w", err)
	}
	return &BoxModel{Content: content, Padding: padding, Border: border, Margin: margin, Width: result.Model.Width, Height: result.Model.Height}, nil
}

func quadFromSlice(values []float64) (Quad, error) {
	if len(values) != 8 {
		return Quad{}, fmt.Errorf("expected 8 coordinates, got %d", len(values))
	}
	return Quad{X1: values[0], Y1: values[1], X2: values[2], Y2: values[3], X3: values[4], Y3: values[5], X4: values[6], Y4: values[7]}, nil
}

func attributePairs(values []string) (map[string]string, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("malformed attribute pairs: got %d values", len(values))
	}
	out := make(map[string]string, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		name := strings.ToLower(strings.TrimSpace(values[i]))
		if name == "" {
			return nil, fmt.Errorf("malformed attribute pair %d: empty name", i/2)
		}
		out[name] = values[i+1]
	}
	return out, nil
}
