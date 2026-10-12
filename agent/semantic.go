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
	arena := &nodeArena{}
	root := arena.newNode()
	root.Kind = SemSection
	if d == nil || d.RawRoot() == nil {
		return root
	}
	root.Text = d.Title()
	body := d.Body()
	if body == nil {
		return root
	}
	stack := make([]*SemanticNode, 0, 16)
	stack = append(stack, root)
	visit(body.Raw(), &stack, arena)
	return root
}

// nodeArena allocates SemanticNodes in blocks so a document with hundreds of
// nodes costs a handful of mallocs instead of one per node. Nodes returned
// to the caller are owned by the caller; blocks are never reused.
type nodeArena struct {
	blocks [][]SemanticNode
	used   int
}

const nodeArenaBlock = 64

func (a *nodeArena) newNode() *SemanticNode {
	if len(a.blocks) == 0 || a.used == len(a.blocks[len(a.blocks)-1]) {
		a.blocks = append(a.blocks, make([]SemanticNode, nodeArenaBlock))
		a.used = 0
	}
	node := &a.blocks[len(a.blocks)-1][a.used]
	a.used++
	return node
}

func visit(n *html.Node, stack *[]*SemanticNode, arena *nodeArena) {
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
			parent := unwindStack(stack, level)
			heading := arena.newNode()
			heading.Kind, heading.Level, heading.Text = SemHeading, level, text
			parent.Children = append(parent.Children, heading)
			section := arena.newNode()
			section.Kind, section.Level, section.Text = SemSection, level, text
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
			node := arena.newNode()
			node.Kind, node.Text = SemParagraph, text
			parent.Children = append(parent.Children, node)
			return
		case "ul", "ol":
			parent := top(*stack)
			list := arena.newNode()
			list.Kind = SemList
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != html.ElementNode || c.Data != "li" {
					continue
				}
				text := strings.TrimSpace(collapseInline(rawText(c)))
				if text == "" {
					continue
				}
				item := arena.newNode()
				item.Kind, item.Text = SemListItem, text
				list.Children = append(list.Children, item)
			}
			if len(list.Children) > 0 {
				parent.Children = append(parent.Children, list)
			}
			return
		case "blockquote":
			text := strings.TrimSpace(collapseInline(rawText(n)))
			if text != "" {
				node := arena.newNode()
				node.Kind, node.Text = SemQuote, text
				top(*stack).Children = append(top(*stack).Children, node)
			}
			return
		case "pre":
			text := rawText(n)
			if strings.TrimSpace(text) != "" {
				node := arena.newNode()
				node.Kind, node.Text = SemCode, strings.TrimRight(text, "\n")
				top(*stack).Children = append(top(*stack).Children, node)
			}
			return
		case "img":
			src := attrOf(n, "src")
			alt := attrOf(n, "alt")
			if src != "" {
				node := arena.newNode()
				node.Kind, node.URL, node.Text = SemImage, src, alt
				top(*stack).Children = append(top(*stack).Children, node)
			}
			return
		case "a":
			href := attrOf(n, "href")
			text := strings.TrimSpace(collapseInline(rawText(n)))
			if href != "" && text != "" {
				node := arena.newNode()
				node.Kind, node.URL, node.Text = SemLink, href, text
				top(*stack).Children = append(top(*stack).Children, node)
				return
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		visit(c, stack, arena)
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
