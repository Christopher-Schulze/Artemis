package engine

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestPageSemanticTreeHandlesNilAbsentAndClosedPages(t *testing.T) {
	var nilPage *Page
	pages := []struct {
		name string
		page *Page
	}{
		{name: "nil page", page: nilPage},
		{name: "absent document", page: &Page{}},
	}
	document, err := parser.ParseHTML(strings.NewReader(`<html><body><p>retained</p></body></html>`), "")
	if err != nil {
		t.Fatal(err)
	}
	closedPage := &Page{document: document}
	if err := closedPage.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range pages {
		t.Run(test.name, func(t *testing.T) {
			root := test.page.SemanticTree()
			if root == nil || root.Kind != agent.SemSection || root.Text != "" || len(root.Children) != 0 {
				t.Fatalf("empty semantic root = %+v", root)
			}
		})
	}
	if rendered := agent.SemanticString(closedPage.SemanticTree()); strings.TrimSpace(rendered) != "retained" {
		t.Fatalf("closed page semantic rendering = %q", rendered)
	}
}
