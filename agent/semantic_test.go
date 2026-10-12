package agent

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

func TestSemanticAbsentDocumentReturnsCanonicalEmptyRoot(t *testing.T) {
	tests := []struct {
		name     string
		document *webapi.Document
	}{
		{name: "nil document"},
		{name: "empty document", document: webapi.NewDocument(nil, "")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var root *SemanticNode
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Fatalf("Semantic panicked: %v", recovered)
					}
				}()
				root = Semantic(test.document)
			}()
			if root == nil || root.Kind != SemSection || root.Level != 0 || root.Text != "" || len(root.Children) != 0 {
				t.Fatalf("empty semantic root = %+v", root)
			}
			if rendered := SemanticString(root); rendered != "" {
				t.Fatalf("empty semantic rendering = %q", rendered)
			}
		})
	}
	if rendered := SemanticString(nil); rendered != "" {
		t.Fatalf("nil semantic rendering = %q", rendered)
	}
}

func TestSemanticHeadingsNest(t *testing.T) {
	src := `<html><body>
		<h1>Top</h1>
		<p>p1</p>
		<h2>Sub A</h2>
		<p>aa</p>
		<h2>Sub B</h2>
		<p>bb</p>
		<h1>Top2</h1>
		<p>p2</p>
	</body></html>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "")
	root := Semantic(doc)
	if root == nil {
		t.Fatal("nil root")
	}
	rendered := SemanticString(root)
	for _, want := range []string{"# Top", "## Sub A", "## Sub B", "# Top2", "p1", "aa", "bb", "p2"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing %q in:\n%s", want, rendered)
		}
	}
}

func TestSemanticSkipsChrome(t *testing.T) {
	src := `<html><body>
		<nav><a href="/">Home</a></nav>
		<main><h1>Article</h1><p>Body</p></main>
		<footer>Footer text</footer>
		<script>console.log('x')</script>
	</body></html>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "")
	rendered := SemanticString(Semantic(doc))
	if strings.Contains(rendered, "Home") {
		t.Errorf("nav leaked into semantic: %s", rendered)
	}
	if strings.Contains(rendered, "Footer text") {
		t.Errorf("footer leaked: %s", rendered)
	}
	if !strings.Contains(rendered, "Article") || !strings.Contains(rendered, "Body") {
		t.Errorf("missing main content: %s", rendered)
	}
}

func TestSemanticListsAndImages(t *testing.T) {
	src := `<html><body>
		<h1>T</h1>
		<ul><li>one</li><li>two</li></ul>
		<img src="https://e.test/i.png" alt="alt">
	</body></html>`
	doc, _ := parser.ParseHTML(strings.NewReader(src), "")
	rendered := SemanticString(Semantic(doc))
	if !strings.Contains(rendered, "- one") || !strings.Contains(rendered, "- two") {
		t.Errorf("list missing: %s", rendered)
	}
	if !strings.Contains(rendered, "![alt](https://e.test/i.png)") {
		t.Errorf("image missing: %s", rendered)
	}
}
