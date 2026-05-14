package css

import (
	"strings"

	"github.com/andybalholm/cascadia"
)

// Rule is one CSS rule: a (possibly comma-separated) selector list plus
// the declarations parsed from the rule body.
type Rule struct {
	// SelectorRaw is the un-trimmed selector text as it appeared.
	SelectorRaw string
	// Selectors is the cascadia-compiled selector list. Specificity is
	// per individual selector; we keep them split so we can match each
	// against an element and pick the highest specificity.
	Selectors []cascadia.Sel
	// Decls is the parsed declaration block. Each value may carry a
	// trailing " !important" sentinel; cascade uses Important to detect.
	Decls map[string]string
	// Important records which decls were declared `!important`.
	Important map[string]bool
}

// Stylesheet is a parsed CSS document.
type Stylesheet struct {
	Rules []Rule
}

// ParseStylesheet parses a CSS source text into a Stylesheet. The
// parser tokenises rule blocks by balancing braces, ignores @-rules
// (their bodies are not applied), and skips selectors that fail to
// compile through cascadia.
func ParseStylesheet(src string) *Stylesheet {
	out := &Stylesheet{}
	src = stripComments(src)
	i := 0
	n := len(src)
	for i < n {
		// Skip whitespace
		for i < n && isSpace(src[i]) {
			i++
		}
		if i >= n {
			break
		}
		if src[i] == '@' {
			// skip @-rule: until matching `;` for short forms or
			// balanced `{...}` for block forms.
			i = skipAtRule(src, i)
			continue
		}
		// selector list runs until the next `{`.
		selStart := i
		for i < n && src[i] != '{' {
			i++
		}
		if i >= n {
			return out
		}
		selectorRaw := strings.TrimSpace(src[selStart:i])
		i++ // past '{'
		blockStart := i
		depth := 1
		for i < n && depth > 0 {
			switch src[i] {
			case '{':
				depth++
			case '}':
				depth--
			}
			i++
		}
		blockEnd := i - 1
		if blockEnd < blockStart {
			continue
		}
		body := src[blockStart:blockEnd]
		decls, important := parseDeclBlock(body)
		if len(decls) == 0 || selectorRaw == "" {
			continue
		}
		// Compile each comma-separated selector via cascadia.
		var sels []cascadia.Sel
		for _, part := range splitSelectors(selectorRaw) {
			s, err := cascadia.Parse(part)
			if err != nil || s == nil {
				continue
			}
			sels = append(sels, s)
		}
		if len(sels) == 0 {
			continue
		}
		out.Rules = append(out.Rules, Rule{
			SelectorRaw: selectorRaw,
			Selectors:   sels,
			Decls:       decls,
			Important:   important,
		})
	}
	return out
}

// parseDeclBlock parses a rule body (the text between `{` and `}`).
// Returns the property map plus a parallel `important` map.
func parseDeclBlock(body string) (map[string]string, map[string]bool) {
	decls := map[string]string{}
	important := map[string]bool{}
	for _, decl := range strings.Split(body, ";") {
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
		// strip "!important" suffix
		if idx := strings.LastIndex(strings.ToLower(v), "!important"); idx >= 0 {
			before := strings.TrimSpace(v[:idx])
			decls[k] = before
			important[k] = true
			continue
		}
		decls[k] = v
	}
	return decls, important
}

// splitSelectors splits a selector list at top-level commas (cascadia
// handles complex selectors but not lists).
func splitSelectors(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if t := strings.TrimSpace(cur.String()); t != "" {
					out = append(out, t)
				}
				cur.Reset()
				continue
			}
		}
		cur.WriteRune(r)
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		out = append(out, t)
	}
	return out
}

// stripComments removes /* ... */ blocks from src.
func stripComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	i := 0
	for i < len(src) {
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '*' {
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return b.String()
			}
			i = i + 2 + j + 2
			continue
		}
		b.WriteByte(src[i])
		i++
	}
	return b.String()
}

// skipAtRule advances past an @-rule starting at position i. Returns
// the index of the first character after the rule.
func skipAtRule(src string, i int) int {
	n := len(src)
	for i < n {
		switch src[i] {
		case ';':
			return i + 1
		case '{':
			depth := 1
			i++
			for i < n && depth > 0 {
				switch src[i] {
				case '{':
					depth++
				case '}':
					depth--
				}
				i++
			}
			return i
		}
		i++
	}
	return n
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }
