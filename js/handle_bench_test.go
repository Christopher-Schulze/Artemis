package js

import (
	"testing"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// BenchmarkTASK2344_NodeTableHandleNew measures the Handle hot path
// for a node not yet in the table (insert path — the common case
// during DOM traversal).
func BenchmarkTASK2344_NodeTableHandleNew(b *testing.B) {
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

// BenchmarkTASK2344_NodeTableHandleExisting measures the Handle hot
// path for a node already in the table (lookup path — the common case
// during repeated DOM access from JS).
func BenchmarkTASK2344_NodeTableHandleExisting(b *testing.B) {
	t := newNodeTable()
	n := webapi.Wrap(&html.Node{Type: html.ElementNode, Data: "div"})
	t.Handle(n) // pre-register
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t.Handle(n)
	}
}

// BenchmarkTASK2344_NodeTableGet measures the Get hot path (JS→Go
// node handle resolution — called on every DOM API call from JS).
func BenchmarkTASK2344_NodeTableGet(b *testing.B) {
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

// BenchmarkTASK2344_NodeTableGetMiss measures the Get hot path for a
// handle that doesn't exist (miss — returns nil).
func BenchmarkTASK2344_NodeTableGetMiss(b *testing.B) {
	t := newNodeTable()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := t.Get(99999); got != nil {
			b.Fatal("expected nil")
		}
	}
}

// BenchmarkTASK2344_JsonStringLiteral measures the jsonStringLiteral
// hot path (called per pooled context reset with the page URL).
func BenchmarkTASK2344_JsonStringLiteral(b *testing.B) {
	url := "https://www.example.com/path/to/page?query=value&foo=bar#fragment"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = jsonStringLiteral(url)
	}
}

// BenchmarkTASK2344_Itoa measures the itoa hot path (called per
// integer-to-string conversion in the JS bridge).
func BenchmarkTASK2344_Itoa(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = itoa(123456789)
	}
}

// BenchmarkTASK2344_JsStringLit measures the jsStringLit hot path
// (called for JS string literal escaping in the DOM bridge).
func BenchmarkTASK2344_JsStringLit(b *testing.B) {
	s := "hello \"world\"\n with \\ backslash"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = jsStringLit(s)
	}
}
