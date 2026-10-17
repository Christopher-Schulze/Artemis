package bridge

import (
	"context"
	"fmt"
	"sort"
)

type routingFrameTree struct {
	Frame struct {
		ID             string `json:"id"`
		ParentID       string `json:"parentId,omitempty"`
		URL            string `json:"url"`
		URLFragment    string `json:"urlFragment,omitempty"`
		SecurityOrigin string `json:"securityOrigin"`
	} `json:"frame"`
	ChildFrames []routingFrameTree `json:"childFrames,omitempty"`
}

type routingFrameTreeResult struct {
	FrameTree routingFrameTree `json:"frameTree"`
}

type routingFrameParams struct {
	FrameID string `json:"frameId"`
}

// frameSession never substitutes a detached/foreign frame with the main target.
func (p *Page) frameSession(ctx context.Context, frameID string) (string, error) {
	sessions := p.FrameSessions()
	if session := sessions[frameID]; session != "" {
		return session, nil
	}
	var tree routingFrameTreeResult
	if err := p.Call(ctx, "Page.getFrameTree", nil, &tree); err != nil {
		return "", err
	}
	parents := make(map[string]string)
	collectRoutingParents(tree.FrameTree, parents)
	if _, exists := parents[frameID]; !exists {
		if err := p.collectAttachedFrameParents(ctx, sessions, parents); err != nil {
			return "", err
		}
	}
	if _, exists := parents[frameID]; !exists {
		return "", fmt.Errorf("frame %s is detached or foreign", frameID)
	}
	seen := make(map[string]bool)
	for current := frameID; current != ""; current = parents[current] {
		if seen[current] {
			return "", fmt.Errorf("frame hierarchy cycle at %s", current)
		}
		seen[current] = true
		if session := sessions[current]; session != "" {
			return session, nil
		}
	}
	if frameID != tree.FrameTree.Frame.ID {
		var membership struct {
			Nodes []struct{} `json:"nodes"`
		}
		if err := p.Call(ctx, "Accessibility.getFullAXTree", routingFrameParams{FrameID: frameID}, &membership); err != nil {
			return "", fmt.Errorf("frame %s is not owned by the main renderer: %w", frameID, err)
		}
	}
	return p.sessionID, nil
}

func (p *Page) collectAttachedFrameParents(ctx context.Context, sessions, parents map[string]string) error {
	frameIDs := make([]string, 0, len(sessions))
	for root := range sessions {
		frameIDs = append(frameIDs, root)
	}
	sort.Strings(frameIDs)
	for _, root := range frameIDs {
		var child routingFrameTreeResult
		if err := p.owner.browser.transport.CallSession(ctx, sessions[root], "Page.getFrameTree", nil, &child); err != nil {
			return fmt.Errorf("frame %s Page.getFrameTree: %w", root, err)
		}
		if child.FrameTree.Frame.ID != root || child.FrameTree.Frame.ParentID == "" {
			return fmt.Errorf("frame %s has no verified root/parent identity", root)
		}
		collectRoutingParents(child.FrameTree, parents)
	}
	return nil
}

func collectRoutingParents(tree routingFrameTree, parents map[string]string) {
	parents[tree.Frame.ID] = tree.Frame.ParentID
	for _, child := range tree.ChildFrames {
		child.Frame.ParentID = tree.Frame.ID
		collectRoutingParents(child, parents)
	}
}
