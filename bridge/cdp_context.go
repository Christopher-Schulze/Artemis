package bridge

import (
	"fmt"
	"sync"
)

// CDPContextNode is one node in the browser target hierarchy.
type CDPContextNode struct {
	ID       string
	ParentID string
	Kind     string
}

// CDPContextTree stores parent/child context relationships for CDP routing.
type CDPContextTree struct {
	mu    sync.RWMutex
	nodes map[string]CDPContextNode
}

func NewCDPContextTree() *CDPContextTree {
	return &CDPContextTree{nodes: make(map[string]CDPContextNode)}
}

func (t *CDPContextTree) Attach(node CDPContextNode) error {
	if node.ID == "" {
		return fmt.Errorf("cdp context: id required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if node.ParentID != "" {
		if _, ok := t.nodes[node.ParentID]; !ok && len(t.nodes) > 0 {
			return fmt.Errorf("cdp context: parent %q missing", node.ParentID)
		}
	}
	t.nodes[node.ID] = node
	return nil
}

func (t *CDPContextTree) Hierarchy(id string) ([]CDPContextNode, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var chain []CDPContextNode
	cur, ok := t.nodes[id]
	if !ok {
		return nil, fmt.Errorf("cdp context: %q not found", id)
	}
	for {
		chain = append([]CDPContextNode{cur}, chain...)
		if cur.ParentID == "" {
			break
		}
		parent, ok := t.nodes[cur.ParentID]
		if !ok {
			return nil, fmt.Errorf("cdp context: broken parent %q", cur.ParentID)
		}
		cur = parent
	}
	return chain, nil
}

func (t *CDPContextTree) RootID(id string) (string, error) {
	chain, err := t.Hierarchy(id)
	if err != nil {
		return "", err
	}
	if len(chain) == 0 {
		return "", fmt.Errorf("cdp context: empty hierarchy")
	}
	return chain[0].ID, nil
}
