package observe

import (
	"fmt"
	"strings"
)

type scopeAnchor struct {
	node  Node
	epoch uint64
}

type nodeScope struct {
	options      QueryOptions
	root, target *scopeAnchor
	parents      map[string]map[int64]int64
	frames       map[string]bool
	matched      int
	foundRoot    bool
	semanticOnly bool
}

func (c *Collector) newNodeScope(options QueryOptions) (*nodeScope, error) {
	scope := &nodeScope{options: options, parents: make(map[string]map[int64]int64), frames: make(map[string]bool)}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.refs[options.SubtreeRef]; entry != nil {
		scope.root = &scopeAnchor{node: cloneNode(entry.node), epoch: entry.epoch}
	}
	if options.SubtreeRef != "" && scope.root == nil {
		return nil, fmt.Errorf("query: subtree reference is stale")
	}
	if entry := c.refs[options.TargetRef]; entry != nil {
		scope.target = &scopeAnchor{node: cloneNode(entry.node), epoch: entry.epoch}
	}
	scope.semanticOnly = scope.target != nil && len(options.Roles) == 0 && options.Text == "" && (options.Mode == ModeInteractive || options.Mode == ModeEvidence)
	if scope.root != nil && options.FrameID != "" && scope.root.node.FrameID != options.FrameID {
		return nil, fmt.Errorf("query: subtree and requested frame disagree")
	}
	if scope.target != nil && options.FrameID != "" && scope.target.node.FrameID != options.FrameID {
		return nil, fmt.Errorf("query: target and requested frame disagree")
	}
	return scope, nil
}

func addSnapshotParents(dom domSnapshotResult, parents map[string]map[int64]int64) {
	for _, document := range dom.Documents {
		frameID := stringAt(dom.Strings, document.FrameID)
		if parents[frameID] == nil {
			parents[frameID] = make(map[int64]int64, len(document.Nodes.BackendNodeID))
		}
		for i, backend := range document.Nodes.BackendNodeID {
			parent := int64(0)
			if i < len(document.Nodes.ParentIndex) {
				index := document.Nodes.ParentIndex[i]
				if index >= 0 && index < len(document.Nodes.BackendNodeID) {
					parent = document.Nodes.BackendNodeID[index]
				}
			}
			parents[frameID][backend] = parent
		}
	}
}

func (s *nodeScope) matches(node Node, threshold float64) bool {
	s.frames[node.FrameID] = true
	if s.root != nil && node.FrameID == s.root.node.FrameID && node.BackendNodeID == s.root.node.BackendNodeID {
		s.foundRoot = true
	}
	if s.options.FrameID != "" && node.FrameID != s.options.FrameID {
		return false
	}
	if s.root != nil && (node.FrameID != s.root.node.FrameID || !isDescendantOf(node.BackendNodeID, s.root.node.BackendNodeID, s.parents[node.FrameID])) {
		return false
	}
	if s.isTarget(node) {
		return true
	}
	if (s.options.Mode == ModeInteractive || s.options.Mode == ModeEvidence) && !node.Interactive {
		return false
	}
	if len(s.options.Roles) > 0 && !contains(s.options.Roles, strings.ToLower(node.Role)) {
		return false
	}
	if s.options.Text != "" && !strings.Contains(strings.ToLower(node.Name+" "+node.Value), s.options.Text) {
		return false
	}
	if s.semanticOnly {
		candidate := node
		candidate.FrameID = s.target.node.FrameID
		return similarity(s.target.node, candidate) >= threshold
	}
	return true
}

func (s *nodeScope) isTarget(node Node) bool {
	return s.target != nil && node.FrameID == s.target.node.FrameID && node.BackendNodeID == s.target.node.BackendNodeID
}

func (s *nodeScope) prioritizeTarget(nodes []Node) {
	for i, node := range nodes {
		if s.isTarget(node) {
			copy(nodes[1:i+1], nodes[:i])
			nodes[0] = node
			return
		}
	}
}
