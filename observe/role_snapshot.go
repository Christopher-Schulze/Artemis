package observe

import (
	"strings"
)

// RoleName is the accessibility role of an AX node, classified for
// role-based snapshot filtering (spec ss28.9, pw-role-snapshot).
type RoleName string

const (
	RoleButton     RoleName = "button"
	RoleLink       RoleName = "link"
	RoleTextbox    RoleName = "textbox"
	RoleCheckbox   RoleName = "checkbox"
	RoleRadio      RoleName = "radio"
	RoleCombobox   RoleName = "combobox"
	RoleTab        RoleName = "tab"
	RoleMenuitem   RoleName = "menuitem"
	RoleHeading    RoleName = "heading"
	RoleImg        RoleName = "img"
	RoleList       RoleName = "list"
	RoleListItem   RoleName = "listitem"
	RoleParagraph  RoleName = "paragraph"
	RoleNavigation RoleName = "navigation"
	RoleMain       RoleName = "main"
	RoleArticle    RoleName = "article"
	RoleSection    RoleName = "section"
	RoleForm       RoleName = "form"
	RoleDialog     RoleName = "dialog"
	RoleAlert      RoleName = "alert"
	RoleGeneric    RoleName = "generic"
	RoleUnknown    RoleName = "unknown"
)

// InteractiveRoles is the set of roles that represent interactive elements.
var InteractiveRoles = map[RoleName]bool{
	RoleButton:   true,
	RoleLink:     true,
	RoleTextbox:  true,
	RoleCheckbox: true,
	RoleRadio:    true,
	RoleCombobox: true,
	RoleTab:      true,
	RoleMenuitem: true,
}

// StructuralRoles is the set of roles that represent structural containers.
var StructuralRoles = map[RoleName]bool{
	RoleList:       true,
	RoleNavigation: true,
	RoleMain:       true,
	RoleArticle:    true,
	RoleSection:    true,
	RoleForm:       true,
	RoleDialog:     true,
}

// ContentRoles is the set of roles that represent content elements.
var ContentRoles = map[RoleName]bool{
	RoleHeading:   true,
	RoleImg:       true,
	RoleListItem:  true,
	RoleParagraph: true,
	RoleAlert:     true,
}

// RoleNameTracker tracks role+name occurrences to detect duplicates
// and assign nth indices when role+name pairs repeat.
type RoleNameTracker struct {
	counts    map[string]int
	refsByKey map[string][]string
}

// NewRoleNameTracker creates a tracker for role-based snapshot ref tracking.
func NewRoleNameTracker() *RoleNameTracker {
	return &RoleNameTracker{
		counts:    make(map[string]int),
		refsByKey: make(map[string][]string),
	}
}

// getKey builds a composite key from role and name.
func (t *RoleNameTracker) getKey(role string, name string) string {
	return strings.ToLower(role) + "|" + name
}

// GetNextIndex returns the next 1-based index for a role+name pair.
func (t *RoleNameTracker) GetNextIndex(role string, name string) int {
	key := t.getKey(role, name)
	t.counts[key]++
	return t.counts[key]
}

// TrackRef records a ref string for a role+name pair.
func (t *RoleNameTracker) TrackRef(role string, name string, ref string) {
	key := t.getKey(role, name)
	t.refsByKey[key] = append(t.refsByKey[key], ref)
}

// GetDuplicateKeys returns keys that have more than one ref (duplicates).
func (t *RoleNameTracker) GetDuplicateKeys() map[string]bool {
	dups := make(map[string]bool)
	for key, refs := range t.refsByKey {
		if len(refs) > 1 {
			dups[key] = true
		}
	}
	return dups
}

// ClassifyRole maps an AXNode's Role field to a RoleName.
// Falls back to tagName-based inference when the ARIA role is empty.
func ClassifyRole(node AXNode) RoleName {
	role := strings.ToLower(strings.TrimSpace(node.Role))
	if role == "" {
		return RoleUnknown
	}
	// Direct match against known roles
	rn := RoleName(role)
	if isValidRole(rn) {
		return rn
	}
	// Handle compound roles like "heading, level=1"
	if strings.Contains(role, "heading") {
		return RoleHeading
	}
	if strings.Contains(role, "list item") || strings.Contains(role, "listitem") {
		return RoleListItem
	}
	if strings.Contains(role, "menu item") || strings.Contains(role, "menuitem") {
		return RoleMenuitem
	}
	if strings.Contains(role, "combo box") || strings.Contains(role, "combobox") {
		return RoleCombobox
	}
	if strings.Contains(role, "text box") || strings.Contains(role, "textbox") {
		return RoleTextbox
	}
	return RoleGeneric
}

func isValidRole(rn RoleName) bool {
	switch rn {
	case RoleButton, RoleLink, RoleTextbox, RoleCheckbox, RoleRadio,
		RoleCombobox, RoleTab, RoleMenuitem, RoleHeading, RoleImg,
		RoleList, RoleListItem, RoleParagraph, RoleNavigation, RoleMain,
		RoleArticle, RoleSection, RoleForm, RoleDialog, RoleAlert,
		RoleGeneric:
		return true
	}
	return false
}

// SnapshotByRole filters an AX tree, returning only nodes matching the
// specified role. This produces role-filtered snapshots for efficient
// element targeting (e.g., "all buttons", "all links").
func SnapshotByRole(tree []AXNode, role RoleName) []AXNode {
	var out []AXNode
	for _, node := range tree {
		if ClassifyRole(node) == role {
			out = append(out, node)
		}
	}
	return out
}

// SnapshotAllRoles groups all nodes by their classified role, producing
// a map of RoleName -> []AXNode. This is the full role-grouped snapshot.
func SnapshotAllRoles(tree []AXNode) map[RoleName][]AXNode {
	out := make(map[RoleName][]AXNode)
	for _, node := range tree {
		rn := ClassifyRole(node)
		out[rn] = append(out[rn], node)
	}
	return out
}

// SnapshotInteractive returns only interactive elements (buttons, links,
// textboxes, checkboxes, radios, comboboxes, tabs, menuitems).
func SnapshotInteractive(tree []AXNode) []AXNode {
	var out []AXNode
	for _, node := range tree {
		rn := ClassifyRole(node)
		if InteractiveRoles[rn] {
			out = append(out, node)
		}
	}
	return out
}

// SnapshotStats describes a role snapshot's statistics.
type SnapshotStats struct {
	Lines       int
	Chars       int
	Refs        int
	Interactive int
}

// GetSnapshotStats computes statistics for a role snapshot and its ref map.
func GetSnapshotStats(snapshot string, refs map[string]string) SnapshotStats {
	interactive := 0
	for _, ref := range refs {
		// Refs are keyed by role+name; check if the ref starts with an interactive role
		for r := range InteractiveRoles {
			if strings.HasPrefix(ref, string(r)+":") {
				interactive++
				break
			}
		}
	}
	return SnapshotStats{
		Lines:       strings.Count(snapshot, "\n") + 1,
		Chars:       len(snapshot),
		Refs:        len(refs),
		Interactive: interactive,
	}
}

// BuildRoleRefMap builds a map of refKey -> refString for all nodes,
// assigning nth indices when role+name pairs duplicate.
func BuildRoleRefMap(tree []AXNode) map[string]string {
	// First pass: count occurrences to detect duplicates
	counter := NewRoleNameTracker()
	for _, node := range tree {
		rn := ClassifyRole(node)
		counter.GetNextIndex(string(rn), node.Name)
		counter.TrackRef(string(rn), node.Name, "")
	}
	dups := counter.GetDuplicateKeys()

	// Second pass: build refs with nth indices for duplicates
	tracker := NewRoleNameTracker()
	refs := make(map[string]string)
	for _, node := range tree {
		rn := ClassifyRole(node)
		key := tracker.getKey(string(rn), node.Name)
		idx := tracker.GetNextIndex(string(rn), node.Name)

		var ref string
		if dups[key] {
			ref = string(rn) + ":" + node.Name + ":" + itoa(idx)
		} else {
			ref = string(rn) + ":" + node.Name
		}
		refKey := node.ID
		if refKey == "" {
			refKey = ref
		}
		refs[refKey] = ref
		tracker.TrackRef(string(rn), node.Name, ref)
	}

	return refs
}

// itoa converts an int to its string representation without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
