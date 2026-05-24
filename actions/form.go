package actions

import (
	"fmt"
	"strings"
)

// FormField is one input in a form intent.
type FormField struct {
	Name  string
	Value string
	Attrs map[string]string
}

// FormIntent captures a batched form submission plan (spec artemis/actions/form.go).
type FormIntent struct {
	ActionURL string
	Method    string
	Fields    []FormField
	Prefetch  bool
}

func (f FormIntent) Validate() error {
	if strings.TrimSpace(f.ActionURL) == "" {
		return fmt.Errorf("form intent: action url required")
	}
	if len(f.Fields) == 0 {
		return fmt.Errorf("form intent: at least one field required")
	}
	return nil
}

// PrefetchKey returns a stable cache key for navigation prefetch.
func (f FormIntent) PrefetchKey() string {
	var b strings.Builder
	b.WriteString(f.ActionURL)
	b.WriteByte('|')
	b.WriteString(strings.ToUpper(f.Method))
	for _, field := range f.Fields {
		b.WriteByte('|')
		b.WriteString(field.Name)
		b.WriteByte('=')
		b.WriteString(field.Value)
	}
	return b.String()
}
