package actions

import (
	"fmt"
	"strings"
)

const MaxFormIntentFields = 128

// FormField is one input in a form intent.
type FormField struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Selector string `json:"selector"`
}

// FormIntent identifies one form-scoped batch without placing field values in
// cache identity. PageID prevents two tabs with the same selectors from
// sharing prefetched references.
type FormIntent struct {
	SessionID string      `json:"sessionId"`
	PageID    string      `json:"pageId"`
	FormRoot  string      `json:"formRoot"`
	Fields    []FormField `json:"fields"`
}

func (f FormIntent) Validate() error {
	if invalidFormIdentityPart(f.SessionID) {
		return fmt.Errorf("form intent: session ID required")
	}
	if invalidFormIdentityPart(f.PageID) {
		return fmt.Errorf("form intent: page ID required")
	}
	if invalidFormIdentityPart(f.FormRoot) {
		return fmt.Errorf("form intent: form root required")
	}
	if len(f.Fields) == 0 {
		return fmt.Errorf("form intent: at least one field required")
	}
	if len(f.Fields) > MaxFormIntentFields {
		return fmt.Errorf("form intent: field count %d exceeds limit %d", len(f.Fields), MaxFormIntentFields)
	}
	selectors := make(map[string]struct{}, len(f.Fields))
	for index, field := range f.Fields {
		if invalidFormIdentityPart(field.Name) {
			return fmt.Errorf("form intent: field %d name required", index)
		}
		if invalidFormIdentityPart(field.Selector) {
			return fmt.Errorf("form intent: field %d selector required", index)
		}
		selector := strings.TrimSpace(field.Selector)
		if _, exists := selectors[selector]; exists {
			return fmt.Errorf("form intent: duplicate selector %q", selector)
		}
		selectors[selector] = struct{}{}
	}
	return nil
}

// FormIdentity is the collision-safe cache identity. It deliberately excludes
// field names and values.
type FormIdentity struct {
	SessionID string
	PageID    string
	FormRoot  string
}

func (f FormIntent) Identity() FormIdentity {
	return FormIdentity{
		SessionID: strings.TrimSpace(f.SessionID),
		PageID:    strings.TrimSpace(f.PageID),
		FormRoot:  strings.TrimSpace(f.FormRoot),
	}
}

func invalidFormIdentityPart(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || strings.ContainsRune(trimmed, '\x00')
}
