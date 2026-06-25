// Package parser exposes HTML parsing as a thin facade over
// golang.org/x/net/html. This file implements the Scrapling CSS-to-XPath
// translator (spec L4393) supporting the ::text and ::attr() pseudo
// elements that Scrapling adds on top of standard CSS.
//
// The translator mirrors the behaviour of
// research/webstack/Scrapling-main/scrapling/core/translator.py: class
// selectors use the contains(concat(' ', normalize-space(@class), ' '),
// ' name ') form, and the ::text / ::attr(name) pseudo-elements append
// /text() and /@name respectively.
//
// Prefix handling: Scrapling's Python HTMLTranslator defaults its prefix
// to "descendant-or-self::" (so css_to_xpath("div") returns
// "descendant-or-self::div"). The spec examples for this Go port use the
// cssselect-standard "//" form ("div" -> "//div"), so CSSToXPath
// defaults to the "//" prefix to match those examples. Callers that need
// the literal Scrapling prefix pass it via CSSToXPathWithPrefix.
package parser

import (
	"fmt"
	"strings"
)

// ParseError is returned by CSSToXPath when the input CSS cannot be
// translated. The error message describes the offending fragment.
type ParseError struct {
	CSS    string
	Reason string
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	return fmt.Sprintf("parser: css2xpath: %s: %q", e.Reason, e.CSS)
}

// defaultXPathPrefix is the prefix prepended to every translated
// selector by CSSToXPath. It is "//" to match the spec examples
// ("div" -> "//div"). Use CSSToXPathWithPrefix to select the Scrapling
// "descendant-or-self::" prefix or an empty prefix.
const defaultXPathPrefix = "//"

// scraplingPrefix is the prefix Scrapling's Python HTMLTranslator uses
// by default. It is exposed for callers and tests that want to reproduce
// Scrapling's exact output.
const scraplingPrefix = "descendant-or-self::"

// CSSToXPath translates a CSS selector (with Scrapling ::text / ::attr()
// extensions) into an XPath expression using the default "//" prefix.
func CSSToXPath(css string) (string, error) {
	return CSSToXPathWithPrefix(css, defaultXPathPrefix)
}

// CSSToXPathWithPrefix translates a CSS selector into an XPath
// expression using the given prefix. The prefix is prepended once to the
// start of the expression (replacing the leading "//"). An empty prefix
// produces a bare expression with no leading axis. Multiple
// comma-separated selectors are joined with " | ".
func CSSToXPathWithPrefix(css string, prefix string) (string, error) {
	trimmed := strings.TrimSpace(css)
	if trimmed == "" {
		return "", &ParseError{CSS: css, Reason: "empty selector"}
	}

	parts := splitSelectors(trimmed)
	if len(parts) == 0 {
		return "", &ParseError{CSS: css, Reason: "no selectors after split"}
	}

	translated := make([]string, 0, len(parts))
	for _, p := range parts {
		xp, err := translateOne(p, prefix)
		if err != nil {
			return "", err
		}
		translated = append(translated, xp)
	}
	return strings.Join(translated, " | "), nil
}

// splitSelectors splits a CSS group selector on top-level commas. CSS
// commas cannot appear inside attribute values for the subset we
// support, so a simple state machine that tracks quote context is
// sufficient.
func splitSelectors(s string) []string {
	var parts []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			b.WriteByte(c)
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			b.WriteByte(c)
			if c == '"' {
				inDouble = false
			}
		case c == '\'':
			inSingle = true
			b.WriteByte(c)
		case c == '"':
			inDouble = true
			b.WriteByte(c)
		case c == ',':
			if t := strings.TrimSpace(b.String()); t != "" {
				parts = append(parts, t)
			}
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if t := strings.TrimSpace(b.String()); t != "" {
		parts = append(parts, t)
	}
	return parts
}

// translateOne translates a single (non-comma) CSS selector into XPath.
func translateOne(selector string, prefix string) (string, error) {
	sel := strings.TrimSpace(selector)
	if sel == "" {
		return "", &ParseError{CSS: selector, Reason: "empty selector component"}
	}

	// Extract trailing pseudo-elements (::text or ::attr(name)). Only one
	// trailing pseudo-element is supported per selector, matching Scrapling.
	textPseudo := false
	attrName := ""
	if idx := strings.Index(sel, "::"); idx >= 0 {
		pe := strings.TrimSpace(sel[idx+2:])
		sel = strings.TrimSpace(sel[:idx])
		if sel == "" {
			return "", &ParseError{CSS: selector, Reason: "pseudo-element without preceding selector"}
		}
		switch {
		case pe == "text":
			textPseudo = true
		case strings.HasPrefix(pe, "attr(") && strings.HasSuffix(pe, ")"):
			attrName = pe[len("attr(") : len(pe)-1]
			if attrName == "" {
				return "", &ParseError{CSS: selector, Reason: "::attr() requires an attribute name"}
			}
		default:
			return "", &ParseError{CSS: selector, Reason: "unsupported pseudo-element ::" + pe}
		}
	}

	if sel == "" {
		return "", &ParseError{CSS: selector, Reason: "empty selector after pseudo-element extraction"}
	}

	compounds, err := splitCompounds(sel)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for i, c := range compounds {
		if i == 0 {
			b.WriteString(prefix)
		} else if c.combinator == ">" {
			b.WriteByte('/')
		} else {
			b.WriteString("//")
		}
		step := translateCompound(c.text)
		if strings.HasPrefix(step, "INVALID:") {
			return "", &ParseError{CSS: c.text, Reason: "invalid compound selector"}
		}
		b.WriteString(step)
	}

	if textPseudo {
		b.WriteString("/text()")
	}
	if attrName != "" {
		b.WriteString("/@")
		b.WriteString(attrName)
	}
	return b.String(), nil
}

// compound represents a single compound selector plus the combinator
// that precedes it ("" for the first compound or descendant, ">" for
// child).
type compound struct {
	combinator string
	text       string
}

// splitCompounds splits a selector into compound selectors on
// whitespace (descendant) and ">" (child) combinators while preserving
// the combinator that joins each compound to the previous one.
func splitCompounds(sel string) ([]compound, error) {
	var out []compound
	i := 0
	n := len(sel)
	for i < n {
		// Skip leading whitespace before the next compound.
		for i < n && isSpace(sel[i]) {
			i++
		}
		if i >= n {
			break
		}
		// Detect a child combinator '>'.
		combinator := ""
		if sel[i] == '>' {
			combinator = ">"
			i++
			for i < n && isSpace(sel[i]) {
				i++
			}
			if i >= n {
				return nil, &ParseError{CSS: sel, Reason: "child combinator '>' at end of selector"}
			}
		}
		// Read one compound until the next top-level combinator.
		start := i
		depth := 0
		inSingle := false
		inDouble := false
		for i < n {
			c := sel[i]
			switch {
			case inSingle:
				if c == '\'' {
					inSingle = false
				}
			case inDouble:
				if c == '"' {
					inDouble = false
				}
			case c == '\'':
				inSingle = true
			case c == '"':
				inDouble = true
			case c == '[':
				depth++
			case c == ']':
				if depth > 0 {
					depth--
				}
			case isSpace(c) || c == '>':
				if depth == 0 {
					goto done
				}
			}
			i++
		}
	done:
		comp := strings.TrimSpace(sel[start:i])
		if comp == "" {
			return nil, &ParseError{CSS: sel, Reason: "empty compound selector"}
		}
		out = append(out, compound{combinator: combinator, text: comp})
	}
	if len(out) == 0 {
		return nil, &ParseError{CSS: sel, Reason: "no compound selectors parsed"}
	}
	return out, nil
}

// translateCompound translates a single compound selector (e.g.
// "div.foo#bar[href]") into an XPath step. The element name defaults to
// "*". An invalid compound returns a sentinel beginning with "INVALID:"
// which the caller converts into a ParseError.
func translateCompound(comp string) string {
	element := "*"
	conditions := []string{}

	i := 0
	n := len(comp)
	// Leading element name (optional, may be "*").
	if i < n && (isIdentStart(comp[i]) || comp[i] == '*') {
		start := i
		if comp[i] == '*' {
			i++
		} else {
			for i < n && isIdentPart(comp[i]) {
				i++
			}
		}
		element = comp[start:i]
	}

	for i < n {
		c := comp[i]
		switch c {
		case '.':
			i++
			start := i
			for i < n && isIdentPart(comp[i]) {
				i++
			}
			name := comp[start:i]
			if name == "" {
				return invalidCompound(comp)
			}
			conditions = append(conditions, classCondition(name))
		case '#':
			i++
			start := i
			for i < n && isIdentPart(comp[i]) {
				i++
			}
			name := comp[start:i]
			if name == "" {
				return invalidCompound(comp)
			}
			conditions = append(conditions, fmt.Sprintf("@id=%s", xpathStringLiteral(name)))
		case '[':
			i++
			start := i
			depth := 1
			inSingle := false
			inDouble := false
			for i < n && depth > 0 {
				ch := comp[i]
				switch {
				case inSingle:
					if ch == '\'' {
						inSingle = false
					}
				case inDouble:
					if ch == '"' {
						inDouble = false
					}
				case ch == '\'':
					inSingle = true
				case ch == '"':
					inDouble = true
				case ch == '[':
					depth++
				case ch == ']':
					depth--
					if depth == 0 {
						goto attrdone
					}
				}
				i++
			}
		attrdone:
			inner := strings.TrimSpace(comp[start:i])
			i++ // skip ']'
			cond, ok := translateAttribute(inner)
			if !ok {
				return invalidCompound(comp)
			}
			conditions = append(conditions, cond)
		default:
			return invalidCompound(comp)
		}
	}

	if len(conditions) == 0 {
		return element
	}
	return fmt.Sprintf("%s[%s]", element, strings.Join(conditions, " and "))
}

// invalidCompound returns a sentinel that the caller treats as a parse
// error. The "INVALID:" prefix cannot appear in valid XPath.
func invalidCompound(comp string) string {
	return "INVALID:" + comp
}

// translateAttribute translates the inside of an attribute selector
// (e.g. "href", "type='text'", "class~='foo'") into an XPath predicate.
func translateAttribute(inner string) (string, bool) {
	if inner == "" {
		return "", false
	}
	// Operators we support: =, ~=, ^=, $=, *=. Check longer operators
	// first so "=" does not shadow "^=".
	for _, op := range []string{"~=", "^=", "$=", "*=", "="} {
		if idx := strings.Index(inner, op); idx > 0 {
			name := strings.TrimSpace(inner[:idx])
			val := strings.TrimSpace(inner[idx+len(op):])
			if name == "" {
				return "", false
			}
			value, ok := unquote(val)
			if !ok {
				return "", false
			}
			return attributeCondition(name, op, value), true
		}
	}
	// No operator: presence selector "[name]".
	name := strings.TrimSpace(inner)
	if name == "" {
		return "", false
	}
	return "@" + name, true
}

// attributeCondition builds the XPath predicate for an attribute
// selector with the given operator.
func attributeCondition(name, op, value string) string {
	switch op {
	case "=":
		return fmt.Sprintf("@%s=%s", name, xpathStringLiteral(value))
	case "~=":
		return fmt.Sprintf("contains(concat(' ', normalize-space(@%s), ' '), %s)", name, xpathStringLiteral(" "+value+" "))
	case "^=":
		return fmt.Sprintf("starts-with(@%s, %s)", name, xpathStringLiteral(value))
	case "$=":
		return fmt.Sprintf("substring(@%s, string-length(@%s) - string-length(%s) + 1) = %s",
			name, name, xpathStringLiteral(value), xpathStringLiteral(value))
	case "*=":
		return fmt.Sprintf("contains(@%s, %s)", name, xpathStringLiteral(value))
	}
	return fmt.Sprintf("@%s=%s", name, xpathStringLiteral(value))
}

// classCondition builds the XPath predicate for a ".name" class
// selector, matching Scrapling/cssselect's HTMLTranslator output.
func classCondition(name string) string {
	return fmt.Sprintf("contains(concat(' ', normalize-space(@class), ' '), %s)", xpathStringLiteral(" "+name+" "))
}

// unquote removes surrounding single or double quotes from a CSS string
// literal. It reports whether the value was a usable literal.
func unquote(s string) (string, bool) {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1], true
		}
	}
	if s == "" {
		return "", false
	}
	// Unquoted values are permitted in CSS attribute selectors.
	return s, true
}

// xpathStringLiteral returns an XPath string literal for the given Go
// string. If the value contains both single and double quotes, the
// concat() form from the XPath 1.0 spec is used.
func xpathStringLiteral(s string) string {
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	if !strings.Contains(s, "\"") {
		return "\"" + s + "\""
	}
	var parts []string
	var b strings.Builder
	useSingle := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' && useSingle {
			flush(&b, &parts, useSingle)
			useSingle = false
			parts = append(parts, `"`+string(c)+`"`)
		} else if c == '"' && !useSingle {
			flush(&b, &parts, useSingle)
			useSingle = true
			parts = append(parts, `'`+string(c)+`'`)
		} else {
			b.WriteByte(c)
		}
	}
	flush(&b, &parts, useSingle)
	return "concat(" + strings.Join(parts, ", ") + ")"
}

// flush appends the buffered string as a quoted XPath literal.
func flush(b *strings.Builder, parts *[]string, useSingle bool) {
	if b.Len() == 0 {
		return
	}
	if useSingle {
		*parts = append(*parts, "'"+b.String()+"'")
	} else {
		*parts = append(*parts, "\""+b.String()+"\"")
	}
	b.Reset()
}

// isSpace reports whether c is an ASCII whitespace character.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// isIdentStart reports whether c can start a CSS identifier.
func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '-'
}

// isIdentPart reports whether c can appear in a CSS identifier after
// the first character.
func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
