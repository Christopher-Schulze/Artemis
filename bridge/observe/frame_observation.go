package observe

import (
	"context"
	"fmt"
)

type capturedFrame struct {
	dom      domSnapshotResult
	ax       map[int64]axNode
	document domNode
}

func captureFrame(ctx context.Context, router FrameCaller, frameID string, params captureSnapshotParams) (capturedFrame, error) {
	for _, method := range []string{"Accessibility.enable", "DOM.enable"} {
		if _, err := callCDPFrame[emptyResult](ctx, router, frameID, method, emptyParams{}); err != nil {
			return capturedFrame{}, fmt.Errorf("frame %s %s: %w", frameID, method, err)
		}
	}
	dom, err := callCDPFrame[domSnapshotResult](ctx, router, frameID, "DOMSnapshot.captureSnapshot", params)
	if err != nil {
		return capturedFrame{}, fmt.Errorf("frame %s DOMSnapshot.captureSnapshot: %w", frameID, err)
	}
	if len(dom.Documents) == 0 {
		return capturedFrame{}, fmt.Errorf("frame %s DOMSnapshot.captureSnapshot: no documents", frameID)
	}
	tree, err := callCDPFrame[frameTreeResult](ctx, router, frameID, "Page.getFrameTree", emptyParams{})
	if err != nil {
		return capturedFrame{}, fmt.Errorf("frame %s Page.getFrameTree: %w", frameID, err)
	}
	if tree.FrameTree.Frame.ID != frameID {
		return capturedFrame{}, fmt.Errorf("frame %s Page.getFrameTree: foreign root", frameID)
	}
	axByBackend, allowed, err := captureFrameAX(ctx, router, frameID, tree.FrameTree)
	if err != nil {
		return capturedFrame{}, err
	}
	document, err := callCDPFrame[documentResult](ctx, router, frameID, "DOM.getDocument", getDocumentParams{Depth: -1, Pierce: true})
	if err != nil {
		return capturedFrame{}, fmt.Errorf("frame %s DOM.getDocument: %w", frameID, err)
	}
	if document.Root.BackendNodeID <= 0 {
		return capturedFrame{}, fmt.Errorf("frame %s DOM.getDocument: no live document", frameID)
	}
	dom = admittedDocuments(dom, allowed)
	return capturedFrame{dom: dom, ax: axByBackend, document: document.Root}, nil
}

func captureFrameAX(ctx context.Context, router FrameCaller, frameID string, tree frameTree) (map[int64]axNode, map[string]bool, error) {
	axByBackend := make(map[int64]axNode)
	allowed := make(map[string]bool)
	sessions := router.FrameSessions()
	for _, localFrame := range flattenFrames(tree) {
		if localFrame != frameID && sessions[localFrame] != "" {
			continue
		}
		ax, err := callCDPFrame[axTreeResult](ctx, router, frameID, "Accessibility.getFullAXTree", getAXTreeParams{FrameID: localFrame})
		if err != nil {
			return nil, nil, fmt.Errorf("frame %s Accessibility.getFullAXTree: %w", localFrame, err)
		}
		if len(ax.Nodes) == 0 {
			return nil, nil, fmt.Errorf("frame %s Accessibility.getFullAXTree: no nodes", localFrame)
		}
		allowed[localFrame] = true
		for _, node := range ax.Nodes {
			if !node.Ignored && node.BackendNodeID != 0 {
				axByBackend[node.BackendNodeID] = node
			}
		}
	}
	return axByBackend, allowed, nil
}

func admittedDocuments(dom domSnapshotResult, allowed map[string]bool) domSnapshotResult {
	documents := make([]documentSnapshot, 0, len(dom.Documents))
	for _, document := range dom.Documents {
		if allowed[stringAt(dom.Strings, document.FrameID)] {
			documents = append(documents, document)
		}
	}
	dom.Documents = documents
	return dom
}

func applyShadowPaths(nodes []Node, paths map[int64][]string) {
	for i := range nodes {
		if path := paths[nodes[i].BackendNodeID]; len(path) > 0 {
			nodes[i].ShadowPath = path
		}
	}
}
