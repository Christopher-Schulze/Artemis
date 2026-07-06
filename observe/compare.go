package observe

import (
	"errors"
	"strings"
	"time"
)

// ChangeDetector performs AX-tree-only diff with threshold and semantic
// filtering (spec L4559). NO pixel diff.
type ChangeDetector struct {
	Threshold      int           // >N changed nodes -> notify
	SemanticFilter []string      // role/name substrings to ignore (ads, timestamps)
	Interval       time.Duration // periodic snapshot interval
	baseline       []AXNode
}

// NewChangeDetector creates a detector with threshold and semantic filter.
func NewChangeDetector(threshold int, semanticFilter []string, interval time.Duration) *ChangeDetector {
	if threshold < 0 {
		threshold = 0
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &ChangeDetector{
		Threshold:      threshold,
		SemanticFilter: semanticFilter,
		Interval:       interval,
	}
}

// SetBaseline records the baseline AX snapshot for future diffs.
func (d *ChangeDetector) SetBaseline(nodes []AXNode) {
	if d == nil {
		return
	}
	d.baseline = filterSemantic(nodes, d.SemanticFilter)
}

// DiffResult is the outcome of a Compare call.
type DiffResult struct {
	Changed      bool
	ChangedNodes int
	AddedNodes   []AXNode
	RemovedNodes []AXNode
	Notify       bool
	Reason       string
}

// Compare diffs the new snapshot against the baseline, applies semantic
// filtering, and notifies when changed-node count exceeds threshold
// (spec L4559).
func (d *ChangeDetector) Compare(newNodes []AXNode) (DiffResult, error) {
	if d == nil {
		return DiffResult{}, errors.New("compare: nil detector")
	}
	if d.baseline == nil {
		return DiffResult{Reason: "no_baseline"}, errors.New("compare: baseline not set")
	}
	filtered := filterSemantic(newNodes, d.SemanticFilter)
	added, removed := symmetricDiff(d.baseline, filtered)
	changed := len(added) + len(removed)
	res := DiffResult{
		ChangedNodes: changed,
		AddedNodes:   added,
		RemovedNodes: removed,
		Changed:      changed > 0,
	}
	if changed > d.Threshold {
		res.Notify = true
		res.Reason = "threshold_exceeded"
	} else if changed > 0 {
		res.Reason = "below_threshold"
	} else {
		res.Reason = "no_change"
	}
	return res, nil
}

// filterSemantic removes nodes whose role or name contains any semantic
// filter substring (e.g. "advertisement", "timestamp", "ads").
func filterSemantic(nodes []AXNode, filters []string) []AXNode {
	if len(filters) == 0 {
		out := make([]AXNode, len(nodes))
		copy(out, nodes)
		return out
	}
	out := make([]AXNode, 0, len(nodes))
	for _, n := range nodes {
		combined := strings.ToLower(n.Role + " " + n.Name)
		skip := false
		for _, f := range filters {
			if strings.Contains(combined, strings.ToLower(f)) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, n)
		}
	}
	return out
}

// symmetricDiff returns added (in new, not in baseline) and removed (in
// baseline, not in new) nodes, keyed by ID+Role+Name.
func symmetricDiff(baseline, current []AXNode) (added, removed []AXNode) {
	baseMap := make(map[string]AXNode, len(baseline))
	curMap := make(map[string]AXNode, len(current))
	for _, n := range baseline {
		baseMap[nodeKey(n)] = n
	}
	for _, n := range current {
		curMap[nodeKey(n)] = n
	}
	for k, n := range curMap {
		if _, ok := baseMap[k]; !ok {
			added = append(added, n)
		}
	}
	for k, n := range baseMap {
		if _, ok := curMap[k]; !ok {
			removed = append(removed, n)
		}
	}
	return added, removed
}

func nodeKey(n AXNode) string {
	return n.ID + "|" + n.Role + "|" + n.Name
}
