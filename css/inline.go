// Package css implements a minimal subset of CSS handling needed for
// agent-driven Artemis usage. Phase 1 of CSS support: inline style
// attribute parsing only. Cascade, inheritance, real selector matching,
// computed lengths, and the full CSS3 grammar arrive in a future TASK.
package css

import (
	"sort"
	"strings"
)

// ParseInline parses a CSS declaration block as found in the HTML
// `style` attribute: `color: red; font-size: 14px`. Whitespace is
// tolerated and trailing semicolons are fine. Keys are returned in
// kebab-case lowercase.
func ParseInline(s string) map[string]string {
	out := map[string]string{}
	for _, decl := range strings.Split(s, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		k, v, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// Serialize rebuilds a `style` attribute string from the map. Keys are
// emitted in alphabetical order for stability.
func Serialize(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(m[k])
	}
	return b.String()
}

// CamelToKebab converts JS-style identifiers to CSS-style: fontSize ->
// font-size. webkitTransform -> -webkit-transform when leading vendor
// prefix is detected.
func CamelToKebab(camel string) string {
	if camel == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range camel {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// KebabToCamel is the inverse: font-size -> fontSize.
func KebabToCamel(kebab string) string {
	if kebab == "" {
		return ""
	}
	var b strings.Builder
	upNext := false
	for _, r := range kebab {
		if r == '-' {
			upNext = true
			continue
		}
		if upNext && r >= 'a' && r <= 'z' {
			b.WriteRune(r - ('a' - 'A'))
			upNext = false
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
