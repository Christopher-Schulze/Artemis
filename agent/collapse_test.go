package agent

import (
	"strings"
	"testing"
)

func TestCollapseInline(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"clean", "abc", "abc"},
		{"single spaces", "a b c", "a b c"},
		{"double space", "a  b", "a b"},
		{"tabs and newlines", "a\t\nb", "a b"},
		{"run of mixed ws", "a\t\t \n b", "a b"},
		{"leading single kept", " a", " a"},
		{"trailing single kept", "a ", "a "},
		{"leading run folded", "   a", " a"},
		{"trailing run folded", "a   ", "a "},
		{"both ends", "  a  b  ", " a b "},
		{"utf8 multibyte preserved", "héllo  wörld", "héllo wörld"},
		{"cjk preserved", "中文  测试", "中文 测试"},
		{"emoji preserved", "a😀  b", "a😀 b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := collapseInline(c.in); got != c.want {
				t.Fatalf("collapseInline(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestCollapseWhitespace(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"clean", "abc", "abc"},
		{"single spaces", "a b c", "a b c"},
		{"leading and trailing trimmed", "  a  b  ", "a b"},
		{"tabs and newlines", "\t a \n", "a"},
		{"run of mixed ws", "a\t\t \n b", "a b"},
		{"utf8 multibyte preserved", "  héllo  wörld  ", "héllo wörld"},
		{"cjk preserved", " 中文  测试 ", "中文 测试"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := collapseWhitespace(c.in); got != c.want {
				t.Fatalf("collapseWhitespace(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Mix of already-clean strings (fast-path, no alloc) and dirty strings
// (builder path), the realistic ratio for inline text and link labels.
var collapseInputs = []string{
	"Already clean single spaced label",
	"Read more about our products",
	"  messy   text\twith\nlots   of   whitespace  ",
	"héllo  wörld with a double  space",
	"NoWhitespaceAtAll",
}

func BenchmarkCollapseInline(b *testing.B) {
	b.ReportAllocs()
	var sink int
	for i := 0; i < b.N; i++ {
		sink += len(collapseInline(collapseInputs[i%len(collapseInputs)]))
	}
	_ = sink
}

func BenchmarkCollapseInlineClean(b *testing.B) {
	const clean = "A perfectly clean already single spaced sentence of text"
	b.ReportAllocs()
	var sink int
	for i := 0; i < b.N; i++ {
		sink += len(collapseInline(clean))
	}
	if strings.Contains(clean, "  ") {
		b.Fatal("fixture not clean")
	}
	_ = sink
}
