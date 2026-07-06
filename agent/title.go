package agent

import "github.com/Christopher-Schulze/Artemis/webapi"

// Title returns the document <title>, trimmed.
func Title(d *webapi.Document) string {
	if d == nil {
		return ""
	}
	return d.Title()
}
