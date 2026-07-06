package parser

import (
	"strings"
	"testing"
)

const smallHTML = `<!doctype html><html><head><title>X</title></head><body><h1>Hi</h1><p>Hello <b>world</b>.</p></body></html>`

func mediumHTML() string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><title>M</title></head><body><main>`)
	for i := 0; i < 200; i++ {
		b.WriteString(`<section><h2>Heading</h2><p>Some <a href="/x">link</a> text and <b>bold</b> bits.</p><ul><li>a</li><li>b</li><li>c</li></ul></section>`)
	}
	b.WriteString(`</main></body></html>`)
	return b.String()
}

// largeHTML5000 builds a ~5000-element HTML document to match the
// Scrapling research floor benchmark (5000 nested elements, 2.02ms
// text extraction on Scrapling's Python parser).
func largeHTML5000() string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><title>L</title></head><body><main>`)
	for i := 0; i < 1000; i++ {
		b.WriteString(`<section><h2>Heading</h2><p>Some <a href="/x">link</a> text and <b>bold</b> bits.</p><ul><li>a</li><li>b</li><li>c</li></ul></section>`)
	}
	b.WriteString(`</main></body></html>`)
	return b.String()
}

func BenchmarkParseSmall(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := ParseHTML(strings.NewReader(smallHTML), ""); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseMedium(b *testing.B) {
	src := mediumHTML()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParseHTML(strings.NewReader(src), ""); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTASK2344_ParseLarge5000 measures parsing a ~5000-element
// HTML document for direct comparison against the Scrapling research
// floor (2.02ms text extraction, 5000 nested elements).
func BenchmarkTASK2344_ParseLarge5000(b *testing.B) {
	src := largeHTML5000()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParseHTML(strings.NewReader(src), ""); err != nil {
			b.Fatal(err)
		}
	}
}
