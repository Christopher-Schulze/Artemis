package observe

import (
	"fmt"
	"strings"
)

// AXNode is a simplified accessibility tree node for diffing.
// Extended per spec L4180: compact A11yNode with ref IDs (e5, e6...)
// + backend DOM node IDs, interactive roles, pierce support.
type AXNode struct {
	ID            string   `json:"id"` // ref ID (e5, e6...)
	BackendNodeID int64    `json:"backendNodeId,omitempty"`
	Role          string   `json:"role"`
	Name          string   `json:"name"`
	Value         string   `json:"value,omitempty"`
	Focused       bool     `json:"focused,omitempty"`
	Disabled      bool     `json:"disabled,omitempty"`
	Visible       bool     `json:"visible,omitempty"`
	Depth         int      `json:"depth,omitempty"`
	FrameID       string   `json:"frameId,omitempty"` // for multi-frame merge
	ChildIDs      []string `json:"childIds,omitempty"`
}

// AXSnapshotConfig controls AX tree snapshot extraction
// (spec L4180: Accessibility.getFullAXTree with pierce:true).
type AXSnapshotConfig struct {
	// Pierce penetrates iframes and Shadow DOM (spec L4180).
	Pierce bool `json:"pierce"`
	// MaxDepth limits the tree depth (0 = unlimited). Controls
	// response token size (spec L4181: depth limiting configurable).
	MaxDepth int `json:"maxDepth,omitempty"`
	// FilterByRole filters to only the given roles (empty = all).
	FilterByRole []string `json:"filterByRole,omitempty"`
	// FilterByVisibility filters to only visible nodes when true.
	FilterByVisibility bool `json:"filterByVisibility,omitempty"`
	// BackendNodeFilter extracts only nodes matching the given
	// backend DOM node IDs (spec L4181: optional scoped subtree).
	BackendNodeFilter []int64 `json:"backendNodeFilter,omitempty"`
}

// DefaultAXSnapshotConfig returns the default config: pierce=true,
// no depth limit, no role filter, no visibility filter.
func DefaultAXSnapshotConfig() AXSnapshotConfig {
	return AXSnapshotConfig{
		Pierce:             true,
		FilterByVisibility: true,
	}
}

// InteractiveAXRoles are the interactive roles per spec L4180.
var InteractiveAXRoles = map[string]bool{
	"button":   true,
	"link":     true,
	"textbox":  true,
	"combobox": true,
	"checkbox": true,
	"radio":    true,
	"option":   true,
	"menuitem": true,
	"tab":      true,
}

// IsInteractiveRole reports whether a role is interactive
// (spec L4180: interactive roles).
func IsInteractiveRole(role string) bool {
	return InteractiveAXRoles[role]
}

// FilterAXSnapshot filters an AX snapshot by depth, role, and visibility
// (spec L4180: filter by depth/role/visibility).
func FilterAXSnapshot(nodes []AXNode, cfg AXSnapshotConfig) []AXNode {
	out := make([]AXNode, 0, len(nodes))
	roleFilter := make(map[string]bool, len(cfg.FilterByRole))
	for _, r := range cfg.FilterByRole {
		roleFilter[r] = true
	}
	backendFilter := make(map[int64]bool, len(cfg.BackendNodeFilter))
	for _, id := range cfg.BackendNodeFilter {
		backendFilter[id] = true
	}
	for _, n := range nodes {
		if cfg.MaxDepth > 0 && n.Depth > cfg.MaxDepth {
			continue
		}
		if len(roleFilter) > 0 && !roleFilter[n.Role] {
			continue
		}
		if cfg.FilterByVisibility && !n.Visible {
			continue
		}
		if len(backendFilter) > 0 && !backendFilter[n.BackendNodeID] {
			continue
		}
		out = append(out, n)
	}
	return out
}

// MergeAXFrames merges AX trees from multiple frames into a single
// tree (spec L4180: multi-frame merge). Nodes from child frames are
// appended after the parent frame's nodes.
func MergeAXFrames(frames [][]AXNode) []AXNode {
	total := 0
	for _, f := range frames {
		total += len(f)
	}
	out := make([]AXNode, 0, total)
	for _, f := range frames {
		out = append(out, f...)
	}
	return out
}

// DedupKey returns the dedup key for an AX node
// (spec L4180: dedup via (role:name:nodeId) key).
func DedupKey(n AXNode) string {
	return n.Role + ":" + n.Name + ":" + n.ID
}

// MyersDiff returns edit script length between two node slices (Myers-like LCS distance).
// Fast-path short-circuit: when before and after are identical (same length and
// pairwise equal), returns 0 without allocating the DP table (spec L4258).
func MyersDiff(before, after []AXNode) int {
	if len(before) == len(after) {
		identical := true
		for i := range before {
			if before[i].ID != after[i].ID || before[i].Role != after[i].Role {
				identical = false
				break
			}
		}
		if identical {
			return 0
		}
	}
	n := len(before)
	m := len(after)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if before[i-1].ID == after[j-1].ID && before[i-1].Role == after[j-1].Role {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	lcs := dp[n][m]
	return n + m - 2*lcs
}

// DiffOpType labels a single edit-script operation (spec L4258).
type DiffOpType string

const (
	DiffOpEqual  DiffOpType = "equal"
	DiffOpAdd    DiffOpType = "add"
	DiffOpRemove DiffOpType = "remove"
	DiffOpChange DiffOpType = "change"
)

// DiffOp is a single operation in the unified edit script
// (spec L4258: additions/removals/unchanged/changed).
type DiffOp struct {
	Type   DiffOpType `json:"type"`
	Before *AXNode    `json:"before,omitempty"` // for remove/change
	After  *AXNode    `json:"after,omitempty"`  // for add/change
}

// MyersEditScript returns the full edit script (sequence of equal/add/remove/
// change ops) between before and after, plus a summary of additions, removals,
// unchanged, and changed counts (spec L4258).
//
// The edit script is derived from the LCS backtrace of the DP table. Nodes
// present in both sequences with the same (ID, Role) but differing Value/
// Focused/Disabled are emitted as DiffOpChange; nodes only in `before` are
// DiffOpRemove; nodes only in `after` are DiffOpAdd; matched LCS nodes are
// DiffOpEqual.
func MyersEditScript(before, after []AXNode) ([]DiffOp, MyersDiffSummary) {
	n := len(before)
	m := len(after)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if before[i-1].ID == after[j-1].ID && before[i-1].Role == after[j-1].Role {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrace from (n, m) to (0, 0), collecting ops in reverse.
	var ops []DiffOp
	i, j := n, m
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && before[i-1].ID == after[j-1].ID && before[i-1].Role == after[j-1].Role:
			// LCS match: equal or changed.
			bn := before[i-1]
			an := after[j-1]
			if bn.Value != an.Value || bn.Focused != an.Focused || bn.Disabled != an.Disabled {
				bc := bn
				ac := an
				ops = append(ops, DiffOp{Type: DiffOpChange, Before: &bc, After: &ac})
			} else {
				ops = append(ops, DiffOp{Type: DiffOpEqual, Before: &bn, After: &an})
			}
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			ac := after[j-1]
			ops = append(ops, DiffOp{Type: DiffOpAdd, After: &ac})
			j--
		default:
			bc := before[i-1]
			ops = append(ops, DiffOp{Type: DiffOpRemove, Before: &bc})
			i--
		}
	}

	// Reverse ops to get forward order.
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}

	summary := MyersDiffSummary{}
	for _, op := range ops {
		switch op.Type {
		case DiffOpEqual:
			summary.Unchanged++
		case DiffOpAdd:
			summary.Additions++
		case DiffOpRemove:
			summary.Removals++
		case DiffOpChange:
			summary.Changed++
		}
	}
	return ops, summary
}

// MyersDiffSummary counts the four operation kinds in an edit script
// (spec L4258: additions/removals/unchanged/changed).
type MyersDiffSummary struct {
	Additions int `json:"additions"`
	Removals  int `json:"removals"`
	Unchanged int `json:"unchanged"`
	Changed   int `json:"changed"`
}

// FormatNode returns a compact single-line representation of an AXNode for
// unified diff output (spec L4258: unified diff with 3-line context).
func FormatNode(n AXNode) string {
	if n.Name != "" {
		return n.Role + ":" + n.Name + " [" + n.ID + "]"
	}
	return n.Role + " [" + n.ID + "]"
}

// UnifiedDiff produces a unified-diff-style string with the given context
// line count around each change hunk (spec L4258: unified diff with 3-line
// context). Lines are prefixed with ' ' (equal), '+' (add), '-' (remove),
// '~' (change before), '|' (change after). Hunk headers "@@ -a,b +c,d @@"
// separate non-adjacent change regions.
func UnifiedDiff(before, after []AXNode, contextLines int) string {
	if contextLines < 0 {
		contextLines = 3
	}
	ops, _ := MyersEditScript(before, after)
	if len(ops) == 0 {
		return ""
	}

	// Identify indices of non-equal ops.
	changeIdx := make([]int, 0)
	for i, op := range ops {
		if op.Type != DiffOpEqual {
			changeIdx = append(changeIdx, i)
		}
	}
	if len(changeIdx) == 0 {
		return ""
	}

	// Build hunk ranges: group consecutive change indices that are within
	// 2*contextLines of each other.
	type hunkRange struct{ start, end int }
	var hunks []hunkRange
	cur := hunkRange{start: changeIdx[0], end: changeIdx[0]}
	for _, idx := range changeIdx[1:] {
		if idx-cur.end <= 2*contextLines {
			cur.end = idx
		} else {
			hunks = append(hunks, cur)
			cur = hunkRange{start: idx, end: idx}
		}
	}
	hunks = append(hunks, cur)

	var b strings.Builder
	for h, hunk := range hunks {
		// Expand hunk by contextLines on each side, clamped to [0, len(ops)).
		hStart := hunk.start - contextLines
		if hStart < 0 {
			hStart = 0
		}
		hEnd := hunk.end + contextLines
		if hEnd >= len(ops) {
			hEnd = len(ops) - 1
		}
		if h > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", hStart+1, hEnd-hStart+1, hStart+1, hEnd-hStart+1))
		for i := hStart; i <= hEnd; i++ {
			op := ops[i]
			switch op.Type {
			case DiffOpEqual:
				b.WriteString(" " + FormatNode(*op.After) + "\n")
			case DiffOpAdd:
				b.WriteString("+" + FormatNode(*op.After) + "\n")
			case DiffOpRemove:
				b.WriteString("-" + FormatNode(*op.Before) + "\n")
			case DiffOpChange:
				b.WriteString("~" + FormatNode(*op.Before) + "\n")
				b.WriteString("|" + FormatNode(*op.After) + "\n")
			}
		}
	}
	return b.String()
}

// DedupRoleSnapshot removes duplicate role/name pairs preserving order.
func DedupRoleSnapshot(nodes []AXNode) []AXNode {
	seen := make(map[string]struct{}, len(nodes))
	out := make([]AXNode, 0, len(nodes))
	for _, n := range nodes {
		key := n.Role + "|" + n.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, n)
	}
	return out
}

// AXDiffSnapshot is the result of comparing two AX snapshots
// (spec L4181: DiffSnapshot: added, changed, removed).
type AXDiffSnapshot struct {
	Added   []AXNode `json:"added"`
	Changed []AXNode `json:"changed"`
	Removed []AXNode `json:"removed"`
}

// DiffAXSnapshots compares prev/curr by (role:name:nodeId) and
// returns added, changed (value/focus/disabled), and removed nodes
// (spec L4181: Compare prev/curr, return added/changed/removed).
// Agent processes only CHANGES.
func DiffAXSnapshots(prev, curr []AXNode) AXDiffSnapshot {
	prevMap := make(map[string]AXNode, len(prev))
	for _, n := range prev {
		prevMap[DedupKey(n)] = n
	}
	currMap := make(map[string]AXNode, len(curr))
	for _, n := range curr {
		currMap[DedupKey(n)] = n
	}

	var added, changed, removed []AXNode

	// Added: in curr but not prev
	for _, n := range curr {
		key := DedupKey(n)
		if _, ok := prevMap[key]; !ok {
			added = append(added, n)
		}
	}

	// Removed: in prev but not curr
	for _, n := range prev {
		key := DedupKey(n)
		if _, ok := currMap[key]; !ok {
			removed = append(removed, n)
		}
	}

	// Changed: in both but value/focus/disabled differs
	for _, n := range curr {
		key := DedupKey(n)
		if old, ok := prevMap[key]; ok {
			if old.Value != n.Value || old.Focused != n.Focused || old.Disabled != n.Disabled {
				changed = append(changed, n)
			}
		}
	}

	return AXDiffSnapshot{
		Added:   added,
		Changed: changed,
		Removed: removed,
	}
}

// HasChanges reports whether the diff contains any changes.
func (d AXDiffSnapshot) HasChanges() bool {
	return len(d.Added) > 0 || len(d.Changed) > 0 || len(d.Removed) > 0
}

// TotalChanges returns the total number of changed nodes.
func (d AXDiffSnapshot) TotalChanges() int {
	return len(d.Added) + len(d.Changed) + len(d.Removed)
}
