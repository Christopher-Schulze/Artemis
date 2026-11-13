package js

import (
	"testing"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// BenchmarkNodeTableHandleNew measures the Handle hot path
// for a node not yet in the table (insert path — the common case
// during DOM traversal).
func BenchmarkNodeTableHandleNew(b *testing.B) {
	t := newNodeTable()
	nodes := make([]*webapi.Node, b.N)
	for i := range nodes {
		nodes[i] = webapi.Wrap(&html.Node{Type: html.ElementNode, Data: "div"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t.Handle(nodes[i])
	}
}

// BenchmarkNodeTableHandleExisting measures the Handle hot
// path for a node already in the table (lookup path — the common case
// during repeated DOM access from JS).
func BenchmarkNodeTableHandleExisting(b *testing.B) {
	t := newNodeTable()
	n := webapi.Wrap(&html.Node{Type: html.ElementNode, Data: "div"})
	t.Handle(n) // pre-register
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t.Handle(n)
	}
}

// BenchmarkNodeTableGet measures the Get hot path (JS→Go
// node handle resolution — called on every DOM API call from JS).
func BenchmarkNodeTableGet(b *testing.B) {
	t := newNodeTable()
	n := webapi.Wrap(&html.Node{Type: html.ElementNode, Data: "div"})
	id := t.Handle(n) // pre-register
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := t.Get(id); got != n {
			b.Fatal("node mismatch")
		}
	}
}

// BenchmarkNodeTableGetMiss measures the Get hot path for a
// handle that doesn't exist (miss — returns nil).
func BenchmarkNodeTableGetMiss(b *testing.B) {
	t := newNodeTable()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := t.Get(99999); got != nil {
			b.Fatal("expected nil")
		}
	}
}

// BenchmarkJsonStringLiteral measures the jsonStringLiteral
// hot path (called per pooled context reset with the page URL).
func BenchmarkJsonStringLiteral(b *testing.B) {
	url := "https://www.example.com/path/to/page?query=value&foo=bar#fragment"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = jsonStringLiteral(url)
	}
}

// BenchmarkItoa measures the itoa hot path (called per
// integer-to-string conversion in the JS bridge).
func BenchmarkItoa(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = itoa(123456789)
	}
}

// BenchmarkJsStringLit measures the jsStringLit hot path
// (called for JS string literal escaping in the DOM bridge).
func BenchmarkJsStringLit(b *testing.B) {
	s := "hello \"world\"\n with \\ backslash"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = jsStringLit(s)
	}
}
