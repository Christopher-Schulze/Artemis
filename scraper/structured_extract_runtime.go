package scraper

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

var structuredNumberPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)$`)

// ExtractStructuredHTML executes a validated declarative schema against an
// HTML document without making an inference call. Every selector is compiled
// before extraction and every required/coercion failure is returned.
func ExtractStructuredHTML(htmlDoc string, schema *StructuredSchema, baseURL string) (*StructuredExtractResult, error) {
	if err := ValidateSchema(schema); err != nil {
		return nil, fmt.Errorf("structured extract: validate schema: %w", err)
	}
	doc, err := html.Parse(strings.NewReader(htmlDoc))
	if err != nil {
		return nil, fmt.Errorf("structured extract: parse html: %w", err)
	}
	result := NewStructuredExtractResult(schema, nil, 0)
	result.AddEvidenceRef("schema:" + schema.Hash())
	value, matched, extractErr := extractStructuredNode(schema, doc, baseURL, "schema")
	result.MatchedCount = matched
	result.ExtractedValue = value
	if extractErr != nil {
		result.AddError(extractErr.Error())
		return result, extractErr
	}
	return result, nil
}

// ExtractStructured is the short facade for callers that already have a
// complete HTML document string.
func ExtractStructured(htmlDoc string, schema *StructuredSchema) (*StructuredExtractResult, error) {
	return ExtractStructuredHTML(htmlDoc, schema, "")
}

func validateStructuredSelector(selector string) error {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return fmt.Errorf("selector is empty")
	}
	if strings.Contains(selector, "::") {
		return fmt.Errorf("pseudo-elements are not extractable: %q", selector)
	}
	if strings.ContainsAny(selector, "{};") || strings.Contains(strings.ToLower(selector), "javascript:") {
		return fmt.Errorf("selector contains forbidden syntax: %q", selector)
	}
	if strings.HasPrefix(selector, ">") || strings.HasPrefix(selector, "+") || strings.HasPrefix(selector, "~") ||
		strings.HasSuffix(selector, ">") || strings.HasSuffix(selector, "+") || strings.HasSuffix(selector, "~") {
		return fmt.Errorf("selector has an incomplete combinator: %q", selector)
	}
	if strings.Contains(selector, ">>") || strings.Contains(selector, "+ +") || strings.Contains(selector, "~ ~") {
		return fmt.Errorf("selector has an invalid combinator: %q", selector)
	}
	if _, err := cascadia.Compile(selector); err != nil {
		return fmt.Errorf("invalid CSS selector %q: %w", selector, err)
	}
	return nil
}

func selectStructuredNodes(parent *html.Node, selector string) ([]*html.Node, error) {
	compiled, err := cascadia.Compile(selector)
	if err != nil {
		return nil, fmt.Errorf("compile selector %q: %w", selector, err)
	}
	var matches []*html.Node
	if parent != nil && compiled.Match(parent) {
		matches = append(matches, parent)
	}
	matches = append(matches, cascadia.QueryAll(parent, compiled)...)
	return matches, nil
}

func extractStructuredNode(schema *StructuredSchema, parent *html.Node, baseURL, path string) (interface{}, int, error) {
	if schema == nil {
		return nil, 0, fmt.Errorf("%s: schema is nil", path)
	}
	matches, err := selectStructuredNodes(parent, schema.Selector)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", path, err)
	}
	if len(matches) == 0 {
		if schema.IsRequired {
			return nil, 0, fmt.Errorf("%s: required selector %q not found", path, schema.Selector)
		}
		return nil, 0, nil
	}

	switch schema.Kind {
	case KindObject:
		value, err := extractStructuredObject(schema, matches[0], baseURL, path)
		return value, len(matches), err
	case KindList:
		values := make([]interface{}, 0, len(matches))
		for i, match := range matches {
			value, _, itemErr := extractStructuredNode(schema.Item, match, baseURL, fmt.Sprintf("%s[%d]", path, i))
			if itemErr != nil {
				return nil, len(matches), itemErr
			}
			if value != nil {
				values = append(values, value)
			}
		}
		return values, len(matches), nil
	default:
		value, scalarErr := extractStructuredScalar(schema, matches, baseURL, path)
		return value, len(matches), scalarErr
	}
}

func extractStructuredObject(schema *StructuredSchema, parent *html.Node, baseURL, path string) (map[string]interface{}, error) {
	names := make([]string, 0, len(schema.FieldsMap))
	for name := range schema.FieldsMap {
		names = append(names, name)
	}
	sort.Strings(names)
	value := make(map[string]interface{}, len(names))
	for _, name := range names {
		field := schema.FieldsMap[name]
		fieldValue, _, err := extractStructuredNode(field, parent, baseURL, path+"."+name)
		if err != nil {
			return nil, err
		}
		if fieldValue != nil {
			value[name] = fieldValue
		}
	}
	return value, nil
}

func extractStructuredScalar(schema *StructuredSchema, matches []*html.Node, baseURL, path string) (interface{}, error) {
	values := make([]interface{}, 0, len(matches))
	for i, node := range matches {
		value, err := extractStructuredScalarValue(schema, node, baseURL)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", path, i, err)
		}
		if schema.Kind != KindNumber && strings.TrimSpace(fmt.Sprint(value)) == "" {
			if schema.IsRequired {
				return nil, fmt.Errorf("%s: required value is empty", path)
			}
			continue
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		if schema.IsRequired {
			return nil, fmt.Errorf("%s: required value is empty", path)
		}
		return nil, nil
	}
	if schema.Join != "" && schema.Kind != KindNumber && schema.Coerce != "number" {
		parts := make([]string, 0, len(values))
		for _, value := range values {
			parts = append(parts, fmt.Sprint(value))
		}
		return strings.Join(parts, schema.Join), nil
	}
	return values[0], nil
}

func extractStructuredScalarValue(schema *StructuredSchema, node *html.Node, baseURL string) (interface{}, error) {
	coerce := strings.ToLower(strings.TrimSpace(schema.Coerce))
	if schema.Kind == KindNumber || coerce == "number" {
		return parseStructuredNumber(nodeText(node))
	}
	if schema.Kind == KindURL || coerce == "url" {
		attribute := schema.Attr
		if attribute == "" {
			attribute = "href"
		}
		value := nodeAttr(node, attribute)
		if value == "" {
			value = nodeText(node)
		}
		return resolveStructuredURL(value, baseURL)
	}
	if schema.Kind == KindAttr {
		value := nodeAttr(node, schema.Attr)
		if schema.Trim {
			value = strings.TrimSpace(value)
		}
		return value, nil
	}
	if schema.Kind == KindHTML {
		value := renderStructuredHTML(node)
		if schema.Trim {
			value = strings.TrimSpace(value)
		}
		return value, nil
	}
	value := nodeText(node)
	if schema.Trim {
		value = strings.TrimSpace(value)
	}
	return value, nil
}

func parseStructuredNumber(raw string) (float64, error) {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, "$€£¥")
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, ",", "")
	if !structuredNumberPattern.MatchString(value) {
		return 0, fmt.Errorf("invalid number %q", raw)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("invalid number %q", raw)
	}
	return parsed, nil
}

func resolveStructuredURL(raw, baseURL string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("empty URL")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "javascript" || parsed.Scheme == "data" {
		return "", fmt.Errorf("invalid URL %q", raw)
	}
	if baseURL != "" {
		base, baseErr := url.Parse(baseURL)
		if baseErr != nil {
			return "", fmt.Errorf("invalid base URL %q: %w", baseURL, baseErr)
		}
		parsed = base.ResolveReference(parsed)
	}
	if parsed.String() == "" {
		return "", fmt.Errorf("invalid URL %q", raw)
	}
	return parsed.String(), nil
}

func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func nodeAttr(node *html.Node, name string) string {
	if node == nil {
		return ""
	}
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, name) {
			return attribute.Val
		}
	}
	return ""
}

func renderStructuredHTML(node *html.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&builder, child); err != nil {
			return ""
		}
	}
	return builder.String()
}
