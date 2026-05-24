package observe

// AXNode is a simplified accessibility tree node for diffing.
type AXNode struct {
	ID   string
	Role string
	Name string
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
