package css

import (
	"strings"

	csssyntax "github.com/tdewolff/parse/v2/css"
	"golang.org/x/net/html"
)

type cssSelectorIdentifier struct {
	attribute string
	value     string
}

type cssSelectorSpelling struct {
	value     string
	ambiguous bool
}

func cssSelectorIdentifiers(tokens []cssToken) []cssSelectorIdentifier {
	var names []cssSelectorIdentifier
	brackets := 0
	var previous cssToken
	for _, token := range tokens {
		if token.kind == csssyntax.CommentToken {
			continue
		}
		if token.kind == csssyntax.LeftBracketToken {
			brackets++
		}
		if token.kind == csssyntax.RightBracketToken {
			brackets--
		}
		if brackets == 0 {
			if token.kind == csssyntax.HashToken {
				names = append(names, cssSelectorIdentifier{attribute: "id", value: decodeCSSIdentifier(token.text[1:])})
			} else if token.kind == csssyntax.IdentToken && previous.kind == csssyntax.DelimToken && previous.text == "." {
				names = append(names, cssSelectorIdentifier{attribute: "class", value: decodeCSSIdentifier(token.text)})
			}
		}
		previous = token
	}
	return names
}

// Without document-mode provenance, case-fold-equivalent IDs/classes cannot
// establish that exact matching is browser-equivalent. The check is once per
// Cascade and bounded independently of selector/ancestor matching work.
func checkCSSSelectorCase(sheets []*Stylesheet, root *html.Node) error {
	names := cssSelectorSpellings(sheets)
	if len(names) == 0 {
		return nil
	}
	for root.Parent != nil {
		root = root.Parent
	}
	work := 0
	for node := root; node != nil; {
		work++
		if work > maxCSSCascadeSelectorChecks {
			return cssDiagnostic(DiagnosticBudget, 0, "selector document-mode proof work limit exceeded")
		}
		if err := checkCSSNodeSelectorCase(node, names); err != nil {
			return err
		}
		if node.FirstChild != nil {
			node = node.FirstChild
			continue
		}
		for node != nil && node.NextSibling == nil {
			work++
			if work > maxCSSCascadeSelectorChecks {
				return cssDiagnostic(DiagnosticBudget, 0, "selector document-mode proof work limit exceeded")
			}
			node = node.Parent
		}
		if node != nil {
			node = node.NextSibling
		}
	}
	return nil
}

func cssSelectorSpellings(sheets []*Stylesheet) map[cssSelectorIdentifier]cssSelectorSpelling {
	names := make(map[cssSelectorIdentifier]cssSelectorSpelling)
	for _, sheet := range sheets {
		for _, rule := range sheet.Rules {
			for _, name := range rule.identifiers {
				key := cssSelectorIdentifier{attribute: name.attribute, value: cssASCIILower(name.value)}
				previous, exists := names[key]
				names[key] = cssSelectorSpelling{value: name.value, ambiguous: previous.ambiguous || exists && previous.value != name.value}
			}
		}
	}
	return names
}

func checkCSSNodeSelectorCase(node *html.Node, names map[cssSelectorIdentifier]cssSelectorSpelling) error {
	for _, attribute := range node.Attr {
		if attribute.Namespace != "" || attribute.Key != "id" && attribute.Key != "class" {
			continue
		}
		values := []string{attribute.Val}
		if attribute.Key == "class" {
			values = strings.FieldsFunc(attribute.Val, func(r rune) bool { return strings.ContainsRune(" \t\r\n\f", r) })
		}
		for _, value := range values {
			matching, exists := names[cssSelectorIdentifier{attribute: attribute.Key, value: cssASCIILower(value)}]
			if exists && (matching.ambiguous || matching.value != value) {
				return cssDiagnostic(DiagnosticUnsupported, 0, "ID or class case matching requires document-mode provenance")
			}
		}
	}
	return nil
}
