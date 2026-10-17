package js

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/agent"
)

func TestDocumentTitleAssignmentMutatesTheOwningHTMLDOM(t *testing.T) {
	for _, test := range []struct {
		name, source, prepare string
	}{
		{"existing", "<html><head><title>Old</title></head><body>Body</body></html>", ""},
		{"missing", "<html><head></head><body>Body</body></html>", ""},
		{"duplicate", "<html><head><title>Old</title><title>Keep second</title></head><body>Body</body></html>", ""},
		{"body title without head", "<html><head></head><body><title>Old</title>Body</body></html>", "document.documentElement.removeChild(document.head);"},
		{"inline SVG title", "<html><head></head><body><svg><title>Keep SVG</title></svg><title>Old</title>Body</body></html>", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := newCtxFromHTML(t, test.source, nil)
			literal := "\tNew\n title & <b>literal</b>\r\u00a0value\u00a0 "
			encoded, err := json.Marshal(literal)
			if err != nil {
				t.Fatal(err)
			}
			value, err := c.Eval(t.Context(), test.prepare+"document.title = "+string(encoded)+"; document.title")
			want := "New title & <b>literal</b> \u00a0value\u00a0"
			if err != nil || value.String() != want || c.Document().Title() != want {
				t.Fatalf("JS/DOM title did not reflect the actual assignment: want=%q err=%v", want, err)
			}
			html := agent.HTML(c.Document())
			if !strings.Contains(html, "&lt;b&gt;literal&lt;/b&gt;") || !strings.Contains(html, "Body") || test.name == "duplicate" && !strings.Contains(html, "Keep second") || test.name == "inline SVG title" && !strings.Contains(html, "Keep SVG") {
				t.Fatal("title assignment interpreted markup or lost unrelated DOM")
			}
		})
	}
}

func TestDocumentTitleAssignmentCoercesBeforeChangingDOM(t *testing.T) {
	for _, expression := range []string{`Symbol('not-a-DOMString')`, `{toString(){throw new Error('coercion-failed')}}`} {
		c := newCtxFromHTML(t, "<html><head></head><body>Body</body></html>", nil)
		before := agent.HTML(c.Document())
		if _, err := c.Eval(t.Context(), "document.title = "+expression); err == nil {
			t.Fatal("invalid title coercion was silently accepted")
		}
		if after := agent.HTML(c.Document()); after != before {
			t.Fatal("failed title coercion changed the DOM")
		}
	}
	c := newCtxFromHTML(t, "<html><head></head><body>Body</body></html>", nil)
	for _, test := range []struct{ expression, want string }{{"null", "null"}, {"undefined", "undefined"}, {"42", "42"}, {"''", ""}} {
		value, err := c.Eval(t.Context(), "document.title = "+test.expression+"; document.title")
		if err != nil || value.String() != test.want || c.Document().Title() != test.want {
			t.Fatalf("title coercion %s: err=%v", test.expression, err)
		}
	}
}

func TestDocumentTitleAssignmentWithoutHeadOrTitleDoesNotCreateStructure(t *testing.T) {
	c := newCtxFromHTML(t, "<html><head></head><body>Body</body></html>", nil)
	if _, err := c.Eval(t.Context(), "document.documentElement.removeChild(document.head)"); err != nil {
		t.Fatal(err)
	}
	before := agent.HTML(c.Document())
	if _, err := c.Eval(t.Context(), "document.title = 'No synthetic head'"); err != nil {
		t.Fatal(err)
	}
	if after := agent.HTML(c.Document()); after != before || c.Document().Title() != "" {
		t.Fatal("missing head/title assignment fabricated document structure")
	}
}

func TestDocumentTitleUsesChildTextAndTheExistingMutationObserver(t *testing.T) {
	c := newCtxFromHTML(t, "<html><head><title>Old</title></head><body>Body</body></html>", nil)
	if _, err := c.Eval(t.Context(), `
globalThis.titleMutations = 0;
const title = document.querySelector('title');
new MutationObserver(records => { for (const record of records) {
  if (record.type === 'childList' && record.target === title) titleMutations++;
}}).observe(title, {childList: true});
document.title = 'Changed';
const nested = document.createElement('span');
nested.textContent = 'Not direct child text';
title.appendChild(nested);
title.appendChild(document.createTextNode(' visible'));
`); err != nil {
		t.Fatal(err)
	}
	value, err := c.Eval(t.Context(), "titleMutations > 0 && document.title === 'Changed visible'")
	if err != nil || !value.Bool() || c.Document().Title() != "Changed visible" {
		t.Fatalf("title mutation bypassed actual DOM/observer semantics: %v", err)
	}
}
