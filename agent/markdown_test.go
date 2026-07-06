package agent

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func mdOf(t *testing.T, src, base string) string {
	t.Helper()
	d, err := parser.ParseHTML(strings.NewReader(src), base)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Markdown(d)
}

func TestMarkdownHeadings(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`<h1>One</h1>`, "# One"},
		{`<h2>Two</h2>`, "## Two"},
		{`<h3>Three</h3>`, "### Three"},
		{`<h6>Six</h6>`, "###### Six"},
	}
	for _, c := range cases {
		got := mdOf(t, c.in, "")
		if !strings.Contains(got, c.want) {
			t.Errorf("Markdown(%q) = %q, want contains %q", c.in, got, c.want)
		}
	}
}

func TestMarkdownInlineEmphasis(t *testing.T) {
	got := mdOf(t, `<p>This is <b>bold</b> and <i>italic</i> and <code>code</code>.</p>`, "")
	for _, want := range []string{"**bold**", "*italic*", "`code`"} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown missing %q in: %s", want, got)
		}
	}
}

func TestMarkdownLinkAbsolute(t *testing.T) {
	got := mdOf(t, `<p><a href="/x">y</a></p>`, "https://example.test/foo/")
	want := "[y](https://example.test/x)"
	if !strings.Contains(got, want) {
		t.Errorf("Markdown link = %q, want contains %q", got, want)
	}
}

func TestMarkdownLinkRelativeWithoutBase(t *testing.T) {
	got := mdOf(t, `<a href="page.html">go</a>`, "")
	want := "[go](page.html)"
	if !strings.Contains(got, want) {
		t.Errorf("Markdown link = %q, want contains %q", got, want)
	}
}

func TestMarkdownImage(t *testing.T) {
	got := mdOf(t, `<img src="a.png" alt="A">`, "")
	want := "![A](a.png)"
	if !strings.Contains(got, want) {
		t.Errorf("Markdown img = %q, want contains %q", got, want)
	}
}

func TestMarkdownLists(t *testing.T) {
	got := mdOf(t, `<ul><li>one</li><li>two</li></ul>`, "")
	if !strings.Contains(got, "- one") || !strings.Contains(got, "- two") {
		t.Errorf("ul missing items: %q", got)
	}
	got = mdOf(t, `<ol><li>a</li><li>b</li></ol>`, "")
	if !strings.Contains(got, "1. a") || !strings.Contains(got, "2. b") {
		t.Errorf("ol missing items: %q", got)
	}
}

func TestMarkdownBlockquote(t *testing.T) {
	got := mdOf(t, `<blockquote><p>hi</p><p>bye</p></blockquote>`, "")
	if !strings.Contains(got, "> hi") || !strings.Contains(got, "> bye") {
		t.Errorf("blockquote: %q", got)
	}
}

func TestMarkdownHr(t *testing.T) {
	got := mdOf(t, `<p>a</p><hr><p>b</p>`, "")
	if !strings.Contains(got, "---") {
		t.Errorf("hr missing: %q", got)
	}
}

func TestMarkdownPreCode(t *testing.T) {
	got := mdOf(t, "<pre><code>line one\nline two</code></pre>", "")
	if !strings.Contains(got, "```") || !strings.Contains(got, "line one\nline two") {
		t.Errorf("pre/code missing: %q", got)
	}
}

func TestMarkdownSkipsScriptStyle(t *testing.T) {
	got := mdOf(t, `<p>seen</p><script>var x=1</script><style>p{color:red}</style>`, "")
	if strings.Contains(got, "var x=1") || strings.Contains(got, "color:red") {
		t.Errorf("leaked script/style: %q", got)
	}
	if !strings.Contains(got, "seen") {
		t.Errorf("missing visible content: %q", got)
	}
}

func TestMarkdownNilSafe(t *testing.T) {
	if Markdown(nil) != "" {
		t.Error("Markdown(nil) should be empty string")
	}
}
