package network

import "strings"

// FilterEasyListRules normalizes easylist-style entries.
func FilterEasyListRules(rules []string) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		r = strings.TrimSpace(r)
		if r == "" || strings.HasPrefix(r, "!") {
			continue
		}
		out = append(out, r)
	}
	return out
}
