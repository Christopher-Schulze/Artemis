package agent

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

func benchDoc(tb testing.TB) *webapi.Document {
	tb.Helper()
	var s strings.Builder
	s.WriteString(`<!doctype html><html><head><title>B</title>
<meta property="og:title" content="bench">
<script type="application/ld+json">{"@type":"Article","headline":"x"}</script>
</head><body>`)
	for i := 0; i < 50; i++ {
		s.WriteString(`<section><h2>H</h2><p>p with <a href="/x">link</a></p><ul><li>1</li><li>2</li></ul></section>`)
	}
	s.WriteString(`</body></html>`)
	d, err := parser.ParseHTML(strings.NewReader(s.String()), "https://e.test/")
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

func BenchmarkMarkdown(b *testing.B) {
	d := benchDoc(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Markdown(d)
	}
}

func BenchmarkText(b *testing.B) {
	d := benchDoc(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Text(d)
	}
}

func BenchmarkLinks(b *testing.B) {
	d := benchDoc(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Links(d)
	}
}

func BenchmarkStructured(b *testing.B) {
	d := benchDoc(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Structured(d)
	}
}

func BenchmarkSemantic(b *testing.B) {
	d := benchDoc(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Semantic(d)
	}
}
