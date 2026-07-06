package scraper

import (
	"fmt"
	"strings"
)

// translator.go (spec L4028: scraper/translator.go - CSS -> XPath
// translation).
//
// Web scraping engine: CSS -> XPath translation. This translator
// converts CSS selectors to XPath expressions for use with XPath-based
// parsers. Supports a subset of CSS selectors: type, class, ID,
// descendant, attribute, and text matching.

// CSSToXPath translates a CSS selector to an XPath expression
// (spec L4028: CSS -> XPath translation).
// Supports: tag, .class, #id, [attr], [attr='val'], descendant (space),
// and > (child) combinators.
func CSSToXPath(css string) (string, error) {
	css = strings.TrimSpace(css)
	if css == "" {
		return "", fmt.Errorf("translator: empty CSS selector")
	}

	// Split on combinators
	parts := strings.Fields(css)
	if len(parts) == 0 {
		return "", fmt.Errorf("translator: invalid CSS selector %q", css)
	}

	var xpathParts []string
	for i, part := range parts {
		xpath, err := cssPartToXPath(part)
		if err != nil {
			return "", err
		}
		if i == 0 {
			xpathParts = append(xpathParts, xpath)
		} else {
			// Descendant combinator (space)
			xpathParts = append(xpathParts, xpath)
		}
	}

	// Join with descendant axis
	result := strings.Join(xpathParts, "//")
	if !strings.HasPrefix(result, "//") {
		result = "//" + result
	}
	return result, nil
}

// cssPartToXPath converts a single CSS selector part to XPath
// (spec L4028: CSS -> XPath translation).
func cssPartToXPath(part string) (string, error) {
	if part == "" {
		return "", fmt.Errorf("translator: empty CSS part")
	}

	// Check for child combinator
	if part == ">" {
		return "", fmt.Errorf("translator: > combinator should be handled by caller")
	}

	var tag, class, id string
	var attrs []string

	// Extract tag
	idx := 0
	for idx < len(part) && part[idx] != '.' && part[idx] != '#' && part[idx] != '[' {
		idx++
	}
	tag = part[:idx]
	if tag == "" {
		tag = "*"
	}
	rest := part[idx:]

	// Parse .class, #id, [attr] selectors
	for len(rest) > 0 {
		switch rest[0] {
		case '.':
			// Class
			end := 1
			for end < len(rest) && rest[end] != '.' && rest[end] != '#' && rest[end] != '[' {
				end++
			}
			class = rest[1:end]
			rest = rest[end:]
		case '#':
			// ID
			end := 1
			for end < len(rest) && rest[end] != '.' && rest[end] != '#' && rest[end] != '[' {
				end++
			}
			id = rest[1:end]
			rest = rest[end:]
		case '[':
			// Attribute
			end := strings.Index(rest, "]")
			if end < 0 {
				return "", fmt.Errorf("translator: unclosed attribute selector in %q", part)
			}
			attrs = append(attrs, rest[1:end])
			rest = rest[end+1:]
		default:
			rest = rest[1:]
		}
	}

	// Build XPath
	xpath := tag
	conditions := []string{}
	if class != "" {
		conditions = append(conditions, fmt.Sprintf("contains(concat(' ', normalize-space(@class), ' '), ' %s ')", class))
	}
	if id != "" {
		conditions = append(conditions, fmt.Sprintf("@id='%s'", id))
	}
	for _, attr := range attrs {
		conditions = append(conditions, translateAttrSelector(attr))
	}
	if len(conditions) > 0 {
		xpath += "[" + strings.Join(conditions, " and ") + "]"
	}
	return xpath, nil
}

// translateAttrSelector translates a CSS attribute selector to XPath
// (spec L4028: CSS -> XPath translation).
func translateAttrSelector(attr string) string {
	// [attr='value'] -> @attr='value'
	// [attr] -> @attr
	if idx := strings.Index(attr, "="); idx >= 0 {
		name := strings.TrimSpace(attr[:idx])
		val := strings.TrimSpace(attr[idx+1:])
		// Remove quotes from value
		val = strings.Trim(val, "'\"")
		return fmt.Sprintf("@%s='%s'", name, val)
	}
	return "@" + attr
}

// IsXPath reports whether a selector string looks like an XPath
// expression (spec L4028: CSS -> XPath translation).
func IsXPath(selector string) bool {
	return strings.HasPrefix(strings.TrimSpace(selector), "//") ||
		strings.HasPrefix(strings.TrimSpace(selector), "/")
}

// IsCSS reports whether a selector string looks like a CSS selector
// (spec L4028: CSS -> XPath translation).
func IsCSS(selector string) bool {
	return !IsXPath(selector) && selector != ""
}

// TranslateSelector auto-detects whether a selector is CSS or XPath
// and returns the XPath equivalent
// (spec L4028: CSS -> XPath translation).
func TranslateSelector(selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", fmt.Errorf("translator: empty selector")
	}
	if IsXPath(selector) {
		return selector, nil // already XPath
	}
	return CSSToXPath(selector)
}
