package observe

// snapshot.go (observe/snapshot.go - AX tree extraction +
// diff).
//
// This file is the spec-mandated facade for AX tree extraction and
// diff. The implementation lives in role_snapshot.go and ax_diff.go;
// this file re-exports the key types and functions under the
// spec-mandated file name.
//
// Page observation: AX tree extraction + diff.

// AXTreeNode is the spec-mandated name for AXNode
// (snapshot.go - AX tree extraction + diff).
type AXTreeNode = AXNode

// DiffAXTrees computes the diff between two AX trees
// (snapshot.go - AX tree extraction + diff).
// Returns the Myers diff distance.
func DiffAXTrees(before, after []AXTreeNode) int {
	return MyersDiff(before, after)
}

// DiffAXSnapshotTrees computes the full diff between two AX snapshots
// and returns added/changed/removed nodes
// (DiffSnapshot with added/changed/removed).
func DiffAXSnapshotTrees(prev, curr []AXTreeNode) AXDiffSnapshot {
	return DiffAXSnapshots(prev, curr)
}

// DedupAXSnapshot removes duplicate nodes from an AX snapshot
// (snapshot.go - AX tree extraction + diff).
func DedupAXSnapshot(nodes []AXTreeNode) []AXTreeNode {
	return DedupRoleSnapshot(nodes)
}

// NewAXSnapshotTracker creates a new RoleNameTracker for AX snapshot
// ref tracking (snapshot.go - AX tree extraction + diff).
func NewAXSnapshotTracker() *RoleNameTracker {
	return NewRoleNameTracker()
}
