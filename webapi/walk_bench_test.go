package webapi

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// benchDOM builds a document with `rows` repeated blocks, each carrying a
// span, a paragraph and text, plus one deep anchor with a unique id/class.
// Matches are far rarer than visited nodes, which is the regime where the
// value-walk (zero alloc per non-matching visit) pays off over the *Node walk.
func benchDOM(b *testing.B, rows int) *Node {
	b.Helper()
	var sb strings.Builder
	sb.WriteString("<html><body>")
	for i := 0; i < rows; i++ {
		sb.WriteString(`<div class="row item"><span>some text content</span><p>paragraph body</p></div>`)
	}
	sb.WriteString(`<a id="target" class="link" href="/deep">deep link</a></body></html>`)
	root, err := html.Parse(strings.NewReader(sb.String()))
	if err != nil {
		b.Fatalf("parse: %v", err)
	}
	return NewDocument(root, "").Root()
}

func BenchmarkGetElementsByTagName(b *testing.B) {
	root := benchDOM(b, 300)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := GetElementsByTagName(root, "a"); len(got) != 1 {
			b.Fatalf("want 1 anchor, got %d", len(got))
		}
	}
}

func BenchmarkGetElementById(b *testing.B) {
	root := benchDOM(b, 300)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := GetElementById(root, "target"); got == nil {
			b.Fatal("target not found")
		}
	}
}

func BenchmarkGetElementsByClassName(b *testing.B) {
	root := benchDOM(b, 300)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := GetElementsByClassName(root, "item"); len(got) != 300 {
			b.Fatalf("want 300 items, got %d", len(got))
		}
	}
}
