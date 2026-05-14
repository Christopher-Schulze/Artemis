package agent

import "artemis/webapi"

// Title returns the document <title>, trimmed.
func Title(d *webapi.Document) string {
	if d == nil {
		return ""
	}
	return d.Title()
}
