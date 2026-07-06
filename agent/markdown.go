package agent

import (
	"net/url"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// mdConverterPool reuses mdConverter instances across nested link/list/
// blockquote processing to avoid per-element allocations. Each Acquire
// resets the builder and clears list state; Release returns the converter
// to the pool.
var mdConverterPool = sync.Pool{
	New: func() interface{} { return &mdConverter{} },
}

func acquireMDConverter(base string) *mdConverter {
	mc := mdConverterPool.Get().(*mdConverter)
	mc.base = base
	mc.b.Reset()
	mc.listKind = mc.listKind[:0]
	mc.olIndex = mc.olIndex[:0]
	mc.inPre = false
	return mc
}

func releaseMDConverter(mc *mdConverter) {
	mdConverterPool.Put(mc)
}

// builderPool reuses strings.Builder instances for short-lived text
// accumulation (table cells, code blocks, raw text collection).
var builderPool = sync.Pool{
	New: func() interface{} { return &strings.Builder{} },
}

func acquireBuilder() *strings.Builder {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	return b
}

func releaseBuilder(b *strings.Builder) {
	builderPool.Put(b)
}

// Markdown renders the document body as CommonMark-flavored Markdown.
// Script, style, template, head, and noscript subtrees are skipped.
// Conversion is best-effort: not every HTML construct has a Markdown
// equivalent, in which case the inner text is emitted unwrapped.
func Markdown(d *webapi.Document) string {
	if d == nil || d.RawRoot() == nil {
		return ""
	}
	body := bodyOrRoot(d.RawRoot())
	mc := acquireMDConverter(d.URL())
	mc.walk(body)
	result := strings.TrimSpace(mc.b.String())
	releaseMDConverter(mc)
	return result
}

func bodyOrRoot(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "html" {
			for cc := c.FirstChild; cc != nil; cc = cc.NextSibling {
				if cc.Type == html.ElementNode && cc.Data == "body" {
					return cc
				}
			}
		}
	}
	return n
}

type mdConverter struct {
	b        strings.Builder
	base     string
	listKind []byte
	olIndex  []int
	inPre    bool
}

func (m *mdConverter) walk(n *html.Node) {
	if n == nil {
		return
	}
	switch n.Type {
	case html.TextNode:
		m.writeText(n.Data)
		return
	case html.CommentNode:
		return
	case html.ElementNode:
		// fall through to per-tag handling
	default:
		m.walkChildren(n)
		return
	}

	switch n.Data {
	case "script", "style", "noscript", "template", "head":
		return
	case "h1", "h2", "h3", "h4", "h5", "h6":
		m.heading(n, int(n.Data[1]-'0'))
	case "p":
		m.paragraph(n)
	case "br":
		m.b.WriteString("  \n")
	case "hr":
		m.ensureBlankLine()
		m.b.WriteString("---\n\n")
	case "strong", "b":
		m.wrap(n, "**", "**")
	case "em", "i":
		m.wrap(n, "*", "*")
	case "code":
		if isInsidePre(n) {
			m.walkChildren(n)
		} else {
			m.wrap(n, "`", "`")
		}
	case "pre":
		m.codeBlock(n)
	case "a":
		m.link(n)
	case "img":
		m.image(n)
	case "ul":
		m.list(n, 'u')
	case "ol":
		m.list(n, 'o')
	case "li":
		m.walkChildren(n)
	case "blockquote":
		m.blockquote(n)
	case "table":
		m.table(n)
	case "tr", "td", "th", "thead", "tbody", "tfoot", "caption", "colgroup", "col":
		m.walkChildren(n)
	case "div", "section", "article", "main", "header", "footer", "aside", "nav", "figure", "figcaption":
		m.block(n)
	default:
		m.walkChildren(n)
	}
}

func (m *mdConverter) writeText(s string) {
	if m.inPre {
		m.b.WriteString(s)
		return
	}
	m.b.WriteString(collapseInline(s))
}

func (m *mdConverter) walkChildren(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		m.walk(c)
	}
}

func (m *mdConverter) ensureBlankLine() {
	s := m.b.String()
	if len(s) == 0 {
		return
	}
	if strings.HasSuffix(s, "\n\n") {
		return
	}
	if strings.HasSuffix(s, "\n") {
		m.b.WriteByte('\n')
		return
	}
	m.b.WriteString("\n\n")
}

func (m *mdConverter) heading(n *html.Node, level int) {
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	m.ensureBlankLine()
	m.b.WriteString(strings.Repeat("#", level))
	m.b.WriteByte(' ')
	var inner mdConverter
	inner.base = m.base
	inner.walkChildren(n)
	m.b.WriteString(strings.TrimSpace(inner.b.String()))
	m.b.WriteString("\n\n")
}

func (m *mdConverter) paragraph(n *html.Node) {
	m.ensureBlankLine()
	m.walkChildren(n)
	m.b.WriteString("\n\n")
}

func (m *mdConverter) block(n *html.Node) {
	m.walkChildren(n)
}

func (m *mdConverter) wrap(n *html.Node, lhs, rhs string) {
	m.b.WriteString(lhs)
	m.walkChildren(n)
	m.b.WriteString(rhs)
}

func (m *mdConverter) link(n *html.Node) {
	href := attrOf(n, "href")
	if href == "" {
		m.walkChildren(n)
		return
	}
	href = m.resolveURL(href)
	inner := acquireMDConverter(m.base)
	inner.walkChildren(n)
	text := strings.TrimSpace(inner.b.String())
	releaseMDConverter(inner)
	if text == "" {
		text = href
	}
	m.b.WriteByte('[')
	m.b.WriteString(text)
	m.b.WriteString("](")
	m.b.WriteString(href)
	m.b.WriteByte(')')
}

func (m *mdConverter) image(n *html.Node) {
	src := attrOf(n, "src")
	alt := attrOf(n, "alt")
	if src == "" {
		return
	}
	src = m.resolveURL(src)
	m.b.WriteString("![")
	m.b.WriteString(alt)
	m.b.WriteString("](")
	m.b.WriteString(src)
	m.b.WriteByte(')')
}

func (m *mdConverter) resolveURL(href string) string {
	if m.base == "" {
		return href
	}
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	base, err := url.Parse(m.base)
	if err != nil {
		return href
	}
	return base.ResolveReference(u).String()
}

func (m *mdConverter) codeBlock(n *html.Node) {
	m.ensureBlankLine()
	m.b.WriteString("```\n")
	buf := acquireBuilder()
	collectRawText(n, buf)
	body := strings.TrimRight(buf.String(), "\n")
	releaseBuilder(buf)
	m.b.WriteString(body)
	m.b.WriteString("\n```\n\n")
}

func collectRawText(n *html.Node, b *strings.Builder) {
	if n == nil {
		return
	}
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectRawText(c, b)
	}
}

func (m *mdConverter) list(n *html.Node, kind byte) {
	m.ensureBlankLine()
	m.listKind = append(m.listKind, kind)
	m.olIndex = append(m.olIndex, 0)
	depth := len(m.listKind) - 1
	indent := strings.Repeat("  ", depth)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "li" {
			continue
		}
		m.b.WriteString(indent)
		if kind == 'u' {
			m.b.WriteString("- ")
		} else {
			m.olIndex[depth]++
			m.b.WriteString(strconv.Itoa(m.olIndex[depth]))
			m.b.WriteString(". ")
		}
		inner := acquireMDConverter(m.base)
		inner.listKind = append(inner.listKind, m.listKind...)
		inner.olIndex = append(inner.olIndex, m.olIndex...)
		inner.walkChildren(c)
		text := strings.TrimSpace(inner.b.String())
		releaseMDConverter(inner)
		text = strings.ReplaceAll(text, "\n", "\n"+indent+"  ")
		m.b.WriteString(text)
		m.b.WriteByte('\n')
	}
	m.listKind = m.listKind[:len(m.listKind)-1]
	m.olIndex = m.olIndex[:len(m.olIndex)-1]
	m.b.WriteByte('\n')
}

func (m *mdConverter) blockquote(n *html.Node) {
	m.ensureBlankLine()
	inner := acquireMDConverter(m.base)
	inner.walkChildren(n)
	text := strings.TrimRight(inner.b.String(), "\n")
	releaseMDConverter(inner)
	if text == "" {
		return
	}
	for _, line := range strings.Split(text, "\n") {
		m.b.WriteString("> ")
		m.b.WriteString(line)
		m.b.WriteByte('\n')
	}
	m.b.WriteByte('\n')
}

func (m *mdConverter) table(n *html.Node) {
	rows := tableRows(n)
	if len(rows) == 0 {
		return
	}
	m.ensureBlankLine()
	header := rows[0]
	writeRow := func(cells []string) {
		m.b.WriteByte('|')
		for _, c := range cells {
			m.b.WriteByte(' ')
			m.b.WriteString(c)
			m.b.WriteString(" |")
		}
		m.b.WriteByte('\n')
	}
	writeRow(header)
	m.b.WriteByte('|')
	for range header {
		m.b.WriteString("---|")
	}
	m.b.WriteByte('\n')
	for _, row := range rows[1:] {
		// pad short rows so the table renders consistently
		if len(row) < len(header) {
			row = append(row, make([]string, len(header)-len(row))...)
		}
		writeRow(row[:len(header)])
	}
	m.b.WriteByte('\n')
}

func tableRows(n *html.Node) [][]string {
	var out [][]string
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n == nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "tr" {
			var cells []string
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
					b := acquireBuilder()
					collectRawText(c, b)
					cells = append(cells, strings.TrimSpace(collapseInline(b.String())))
					releaseBuilder(b)
				}
			}
			if len(cells) > 0 {
				out = append(out, cells)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	return out
}

// isCollapsibleWS reports whether c is one of the ASCII whitespace bytes that
// collapseInline / collapseWhitespace fold into a single space.
func isCollapsibleWS(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// collapseInline folds every run of ASCII whitespace into a single space,
// preserving a leading/trailing single space. It scans BYTES, not runes: the
// folded set is ASCII and every byte of a multi-byte UTF-8 sequence is >= 0x80,
// so it never matches and is copied verbatim — identical output to a rune scan
// for the valid UTF-8 the parser produces, without the per-char decode and
// WriteRune re-encode. An already-collapsed string is returned as-is, allocation
// free.
func collapseInline(s string) string {
	if !needsInlineCollapse(s) {
		return s
	}
	b := acquireBuilder()
	b.Grow(len(s))
	prevSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isCollapsibleWS(c) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteByte(c)
		prevSpace = false
	}
	result := b.String()
	releaseBuilder(b)
	return result
}

// needsInlineCollapse reports whether collapseInline would change s: any tab/CR/LF,
// or any run of two or more whitespace bytes.
func needsInlineCollapse(s string) bool {
	prevSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isCollapsibleWS(c) {
			prevSpace = false
			continue
		}
		if c != ' ' || prevSpace {
			return true
		}
		prevSpace = true
	}
	return false
}

func attrOf(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

func isInsidePre(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.Data == "pre" {
			return true
		}
	}
	return false
}
