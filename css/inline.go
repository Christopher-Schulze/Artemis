// Package css implements the bounded CSS parser, selector cascade,
// inheritance and inline-style handling used by the Artemis JS runtime.
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
	vendorPrefix := ""
	switch {
	case strings.HasPrefix(camel, "webkit") && len(camel) > len("webkit"):
		vendorPrefix = "-"
	case strings.HasPrefix(camel, "ms") && len(camel) > len("ms"):
		vendorPrefix = "-"
	case strings.HasPrefix(camel, "Moz") && len(camel) > len("Moz"):
		camel = "moz" + camel[len("Moz"):]
		vendorPrefix = "-"
	case strings.HasPrefix(camel, "O") && len(camel) > 1:
		camel = "o" + camel[1:]
		vendorPrefix = "-"
	}
	var b strings.Builder
	b.WriteString(vendorPrefix)
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
	prefix := ""
	switch {
	case strings.HasPrefix(kebab, "-webkit-"):
		prefix, kebab = "webkit", kebab[len("-webkit-"):]
	case strings.HasPrefix(kebab, "-ms-"):
		prefix, kebab = "ms", kebab[len("-ms-"):]
	case strings.HasPrefix(kebab, "-moz-"):
		prefix, kebab = "Moz", kebab[len("-moz-"):]
	case strings.HasPrefix(kebab, "-o-"):
		prefix, kebab = "O", kebab[len("-o-"):]
	}
	var b strings.Builder
	b.WriteString(prefix)
	if prefix != "" && kebab != "" {
		first := kebab[0]
		if first >= 'a' && first <= 'z' {
			b.WriteByte(first - ('a' - 'A'))
			kebab = kebab[1:]
		}
	}
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
