package observe

// snapshot.go (spec L4024: observe/snapshot.go - AX tree extraction +
// diff).
//
// This file is the spec-mandated facade for AX tree extraction and
// diff. The implementation lives in role_snapshot.go and ax_diff.go;
// this file re-exports the key types and functions under the
// spec-mandated file name.
//
// Page observation: AX tree extraction + diff.

// AXTreeNode is the spec-mandated name for AXNode
// (spec L4024: snapshot.go - AX tree extraction + diff).
type AXTreeNode = AXNode

// DiffAXTrees computes the diff between two AX trees
// (spec L4024: snapshot.go - AX tree extraction + diff).
// Returns the Myers diff distance.
func DiffAXTrees(before, after []AXTreeNode) int {
	return MyersDiff(before, after)
}

// DiffAXSnapshotTrees computes the full diff between two AX snapshots
// and returns added/changed/removed nodes
// (spec L4181: DiffSnapshot with added/changed/removed).
func DiffAXSnapshotTrees(prev, curr []AXTreeNode) AXDiffSnapshot {
	return DiffAXSnapshots(prev, curr)
}

// DedupAXSnapshot removes duplicate nodes from an AX snapshot
// (spec L4024: snapshot.go - AX tree extraction + diff).
func DedupAXSnapshot(nodes []AXTreeNode) []AXTreeNode {
	return DedupRoleSnapshot(nodes)
}

// NewAXSnapshotTracker creates a new RoleNameTracker for AX snapshot
// ref tracking (spec L4024: snapshot.go - AX tree extraction + diff).
func NewAXSnapshotTracker() *RoleNameTracker {
	return NewRoleNameTracker()
}
