package scraper

import "strings"

// ResolvePriority picks the first selector candidate present in query.
func (f *Finder) ResolvePriority(query string, priority []string) string {
	_ = f
	q := strings.TrimSpace(strings.ToLower(query))
	for _, sel := range priority {
		if strings.Contains(q, strings.ToLower(sel)) || strings.Contains(strings.ToLower(sel), q) {
			return sel
		}
	}
	if len(priority) > 0 {
		return priority[0]
	}
	return query
}
