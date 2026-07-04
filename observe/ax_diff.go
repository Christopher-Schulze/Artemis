package observe

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
func MyersDiff(before, after []AXNode) int {
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
