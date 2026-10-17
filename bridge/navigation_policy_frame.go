package bridge

import (
	"context"
	"fmt"
	"strings"
)

// FramePolicyURL reads the document URL through its verified owning renderer.
// An inherited blank/srcdoc/blob realm uses only its native security origin;
// opaque origins and unavailable/foreign frames cannot inherit a main URL.
func (p *Page) FramePolicyURL(ctx context.Context, frameID string) (string, error) {
	if p == nil || ctx == nil {
		return "", fmt.Errorf("frame policy: page and context required")
	}
	var tree routingFrameTreeResult
	if err := p.CallFrame(ctx, frameID, "Page.getFrameTree", nil, &tree); err != nil {
		return "", fmt.Errorf("frame policy: native tree: %w", err)
	}
	if frameID == "" {
		frameID = tree.FrameTree.Frame.ID
	}
	found, err := findPolicyFrame(&tree.FrameTree, frameID)
	if err != nil {
		return "", err
	}
	target := found.Frame.URL + found.Frame.URLFragment
	if _, err := ParsePolicyURL(target); err == nil {
		return target, nil
	}
	if found.Frame.URL == "about:blank" || found.Frame.URL == "about:srcdoc" || strings.HasPrefix(found.Frame.URL, "blob:") {
		if _, err := ParsePolicyURL(found.Frame.SecurityOrigin); err == nil {
			return found.Frame.SecurityOrigin, nil
		}
	}
	return "", fmt.Errorf("frame policy: document realm unavailable")
}

func findPolicyFrame(root *routingFrameTree, frameID string) (*routingFrameTree, error) {
	stack := []*routingFrameTree{root}
	var found *routingFrameTree
	seen := make(map[string]bool)
	for len(stack) > 0 {
		if len(seen) >= 256 {
			return nil, fmt.Errorf("frame policy: frame budget exceeded")
		}
		frame := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if frame.Frame.ID == "" || seen[frame.Frame.ID] {
			return nil, fmt.Errorf("frame policy: invalid frame identity")
		}
		seen[frame.Frame.ID] = true
		if frame.Frame.ID == frameID {
			found = frame
		}
		for i := range frame.ChildFrames {
			stack = append(stack, &frame.ChildFrames[i])
		}
	}
	if found == nil {
		return nil, fmt.Errorf("frame policy: requested frame unavailable")
	}
	return found, nil
}
