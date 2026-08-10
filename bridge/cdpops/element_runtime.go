package cdpops

import (
	"context"
	"errors"
	"fmt"
)

type Caller interface {
	Call(context.Context, string, any, any) error
}

var ErrCallerRequired = errors.New("cdpops: CDP caller required")

type ElementClient struct{ caller Caller }

func NewElementClient(caller Caller) (*ElementClient, error) {
	if caller == nil {
		return nil, ErrCallerRequired
	}
	return &ElementClient{caller: caller}, nil
}

func (c *ElementClient) QuerySelector(ctx context.Context, selector string) ([]ElementInfo, error) {
	if ctx == nil {
		return nil, errors.New("element query: context required")
	}
	if selector == "" {
		return nil, errors.New("element query: selector required")
	}
	var document getDocumentResult
	if err := c.caller.Call(ctx, "DOM.getDocument", getDocumentParams{Depth: -1, Pierce: true}, &document); err != nil {
		return nil, fmt.Errorf("element query document: %w", err)
	}
	var matches querySelectorAllResult
	params := querySelectorAllParams{NodeID: document.Root.NodeID, Selector: selector}
	if err := c.caller.Call(ctx, "DOM.querySelectorAll", params, &matches); err != nil {
		return nil, fmt.Errorf("element query selector: %w", err)
	}
	out := make([]ElementInfo, 0, len(matches.NodeIDs))
	for _, nodeID := range matches.NodeIDs {
		var described describeNodeResult
		if err := c.caller.Call(ctx, "DOM.describeNode", describeNodeParams{NodeID: nodeID}, &described); err != nil {
			return nil, fmt.Errorf("element query describe node %d: %w", nodeID, err)
		}
		box, err := c.GetBoxModel(ctx, described.Node.BackendNodeID)
		if err != nil {
			return nil, err
		}
		attributes := attributePairs(described.Node.Attributes)
		out = append(out, ElementInfo{Ref: fmt.Sprintf("backend:%d", described.Node.BackendNodeID), TagName: described.Node.NodeName, Type: attributes["type"], ID: attributes["id"], Name: attributes["name"], Value: attributes["value"], Visible: IsElementVisible(box), Clickable: IsElementVisible(box), Box: box})
	}
	return out, nil
}

func (c *ElementClient) GetBoxModel(ctx context.Context, backendNodeID int64) (*BoxModel, error) {
	if ctx == nil {
		return nil, errors.New("box model: context required")
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

func attributePairs(values []string) map[string]string {
	out := make(map[string]string, len(values)/2)
	for i := 0; i+1 < len(values); i += 2 {
		out[values[i]] = values[i+1]
	}
	return out
}
