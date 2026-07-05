package agent

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// SemanticKind classifies a node in a SemanticTree.
type SemanticKind int

const (
	SemSection SemanticKind = iota
	SemHeading
	SemParagraph
	SemList
	SemListItem
	SemLink
	SemQuote
	SemCode
	SemImage
)

// String returns a lowercase name for the kind.
func (k SemanticKind) String() string {
	switch k {
	case SemHeading:
		return "heading"
	case SemParagraph:
		return "paragraph"
	case SemList:
		return "list"
	case SemListItem:
		return "listItem"
	case SemLink:
		return "link"
	case SemQuote:
		return "quote"
	case SemCode:
		return "code"
	case SemImage:
		return "image"
	default:
		return "section"
	}
}

// SemanticNode is a single node in the SemanticTree. Section nodes
// nest other nodes; leaves have empty Children.
type SemanticNode struct {
	Kind     SemanticKind
	Level    int    // headings: 1-6; section: 0; otherwise unused
	Text     string // content text (collapsed whitespace)
	URL      string // for SemLink and SemImage
	Children []*SemanticNode
}

// Semantic returns a hierarchical agent-friendly view of the document
// body. Nav, footer, aside, script, style, and template are skipped.
func Semantic(d *webapi.Document) *SemanticNode {
	root := &SemanticNode{Kind: SemSection, Level: 0, Text: d.Title()}
	if d == nil {
		return root
	}
	body := d.Body()
	if body == nil {
		return root
	}
	stack := []*SemanticNode{root}
	visit(body.Raw(), &stack)
	return root
}

func visit(n *html.Node, stack *[]*SemanticNode) {
	if n == nil {
		return
	}
	if n.Type == html.ElementNode {
		switch n.Data {
		case "script", "style", "noscript", "template", "head",
			"nav", "footer", "aside":
			return
		}
		if level, ok := headingLevel(n.Data); ok {
			text := strings.TrimSpace(collapseInline(rawText(n)))
			heading := &SemanticNode{Kind: SemHeading, Level: level, Text: text}
			parent := unwindStack(stack, level)
			parent.Children = append(parent.Children, heading)
			section := &SemanticNode{Kind: SemSection, Level: level, Text: text}
			parent.Children = append(parent.Children, section)
			*stack = append(*stack, section)
			return
		}
		switch n.Data {
		case "p":
			text := strings.TrimSpace(collapseInline(rawText(n)))
			if text == "" {
				return
			}
			parent := top(*stack)
			parent.Children = append(parent.Children, &SemanticNode{Kind: SemParagraph, Text: text})
			return
		case "ul", "ol":
			parent := top(*stack)
			list := &SemanticNode{Kind: SemList}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != html.ElementNode || c.Data != "li" {
					continue
				}
				text := strings.TrimSpace(collapseInline(rawText(c)))
				if text == "" {
					continue
				}
				list.Children = append(list.Children, &SemanticNode{Kind: SemListItem, Text: text})
			}
			if len(list.Children) > 0 {
				parent.Children = append(parent.Children, list)
			}
			return
		case "blockquote":
			text := strings.TrimSpace(collapseInline(rawText(n)))
			if text != "" {
				top(*stack).Children = append(top(*stack).Children, &SemanticNode{Kind: SemQuote, Text: text})
			}
			return
		case "pre":
			text := rawText(n)
			if strings.TrimSpace(text) != "" {
				top(*stack).Children = append(top(*stack).Children, &SemanticNode{Kind: SemCode, Text: strings.TrimRight(text, "\n")})
			}
			return
		case "img":
			src := attrOf(n, "src")
			alt := attrOf(n, "alt")
			if src != "" {
				top(*stack).Children = append(top(*stack).Children, &SemanticNode{Kind: SemImage, URL: src, Text: alt})
			}
			return
		case "a":
			href := attrOf(n, "href")
			text := strings.TrimSpace(collapseInline(rawText(n)))
			if href != "" && text != "" {
				top(*stack).Children = append(top(*stack).Children, &SemanticNode{Kind: SemLink, URL: href, Text: text})
				return
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		visit(c, stack)
	}
}

func headingLevel(tag string) (int, bool) {
	if len(tag) != 2 || tag[0] != 'h' {
		return 0, false
	}
	if tag[1] < '1' || tag[1] > '6' {
		return 0, false
	}
	return int(tag[1] - '0'), true
}

// unwindStack pops sections until the top has Level < target.
func unwindStack(stack *[]*SemanticNode, level int) *SemanticNode {
	for len(*stack) > 1 && (*stack)[len(*stack)-1].Level >= level {
		*stack = (*stack)[:len(*stack)-1]
	}
	return (*stack)[len(*stack)-1]
}

func top(stack []*SemanticNode) *SemanticNode {
	return stack[len(stack)-1]
}

func rawText(n *html.Node) string {
	var b strings.Builder
	collectRawText(n, &b)
	return b.String()
}

// SemanticString renders a SemanticTree as an indented Markdown-ish view
// suitable for piping into an LLM.
func SemanticString(n *SemanticNode) string {
	var b strings.Builder
	renderSemantic(&b, n, 0)
	return strings.TrimRight(b.String(), "\n")
}

func renderSemantic(b *strings.Builder, n *SemanticNode, depth int) {
	if n == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	switch n.Kind {
	case SemSection:
		if depth > 0 && n.Text != "" {
			b.WriteString(indent)
			b.WriteString(strings.Repeat("#", n.Level))
			b.WriteByte(' ')
			b.WriteString(n.Text)
			b.WriteByte('\n')
		}
	case SemHeading:
		// Section was already emitted above in tree order; skip.
		return
	case SemParagraph:
		b.WriteString(indent)
		b.WriteString(n.Text)
		b.WriteString("\n")
	case SemList:
		for _, c := range n.Children {
			b.WriteString(indent)
			b.WriteString("- ")
			b.WriteString(c.Text)
			b.WriteByte('\n')
		}
		return
	case SemQuote:
		b.WriteString(indent)
		b.WriteString("> ")
		b.WriteString(n.Text)
		b.WriteByte('\n')
	case SemCode:
		b.WriteString(indent)
		b.WriteString("```\n")
		for _, line := range strings.Split(n.Text, "\n") {
			b.WriteString(indent)
			b.WriteString(line)
			b.WriteByte('\n')
		}
		b.WriteString(indent)
		b.WriteString("```\n")
	case SemLink:
		b.WriteString(indent)
		b.WriteString("[")
		b.WriteString(n.Text)
		b.WriteString("](")
		b.WriteString(n.URL)
		b.WriteString(")\n")
	case SemImage:
		b.WriteString(indent)
		b.WriteString("![")
		b.WriteString(n.Text)
		b.WriteString("](")
		b.WriteString(n.URL)
		b.WriteString(")\n")
	}
	for _, c := range n.Children {
		nextDepth := depth
		if n.Kind == SemSection && depth > 0 {
			nextDepth = depth + 1
		} else if n.Kind == SemSection && depth == 0 {
			nextDepth = 1
		}
		renderSemantic(b, c, nextDepth)
	}
}
