package agent

import (
	"encoding/json"
	"strings"

	"artemis/webapi"
)

// StructuredData aggregates structured information found in a document.
type StructuredData struct {
	JSONLD    []map[string]any
	OpenGraph map[string]string
	Twitter   map[string]string
	Meta      map[string]string
	Title     string
}

// Structured extracts JSON-LD scripts and meta-tag conventions from the
// document. Empty fields mean "nothing found", never nil for the maps so
// callers can index them safely.
func Structured(d *webapi.Document) StructuredData {
	out := StructuredData{
		OpenGraph: map[string]string{},
		Twitter:   map[string]string{},
		Meta:      map[string]string{},
	}
	if d == nil {
		return out
	}
	out.Title = d.Title()

	if scripts, err := d.QuerySelectorAll(`script[type="application/ld+json"]`); err == nil {
		for _, s := range scripts {
			body := strings.TrimSpace(s.Text())
			if body == "" {
				continue
			}
			out.JSONLD = append(out.JSONLD, parseJSONLD(body)...)
		}
	}

	metas, _ := d.QuerySelectorAll("meta")
	for _, m := range metas {
		content, hasContent := m.Attr("content")
		if !hasContent {
			continue
		}
		if prop, ok := m.Attr("property"); ok && prop != "" {
			out.Meta[prop] = content
			if strings.HasPrefix(prop, "og:") {
				out.OpenGraph[strings.TrimPrefix(prop, "og:")] = content
			}
			if strings.HasPrefix(prop, "twitter:") {
				out.Twitter[strings.TrimPrefix(prop, "twitter:")] = content
			}
		}
		if name, ok := m.Attr("name"); ok && name != "" {
			out.Meta[name] = content
			if strings.HasPrefix(name, "twitter:") {
				out.Twitter[strings.TrimPrefix(name, "twitter:")] = content
			}
			if strings.HasPrefix(name, "og:") {
				out.OpenGraph[strings.TrimPrefix(name, "og:")] = content
			}
		}
		if hv, ok := m.Attr("http-equiv"); ok && hv != "" {
			out.Meta[hv] = content
		}
	}
	return out
}

// parseJSONLD parses a single <script type="application/ld+json"> body.
// JSON-LD allows the body to be either an object or an array of objects.
func parseJSONLD(body string) []map[string]any {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	if strings.HasPrefix(body, "[") {
		var arr []map[string]any
		if err := json.Unmarshal([]byte(body), &arr); err == nil {
			return arr
		}
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(body), &obj); err == nil && obj != nil {
		return []map[string]any{obj}
	}
	return nil
}
