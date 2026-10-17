package renderless

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestCSSAdapterUsesCanonicalDiagnosticsAndTokenBoundaries(t *testing.T) {
	for _, source := range []string{`@media all{div{color:red}}`, `div{color:red;broken}`, `.ok,:hover{color:red}`} {
		rules, err := NewCSSParser().Parse(source)
		if rules != nil || !css.IsDiagnostic(err) {
			t.Fatalf("adapter dropped canonical rejection: %v %v", rules, err)
		}
	}
	rules, err := NewCSSParser().Parse(`div{--text:"a;b}c";--color:blue;color:red!important;color:var(--color)}`)
	if err != nil || len(rules) != 1 || len(rules[0].Declarations) != 4 {
		t.Fatalf("adapter split token boundaries or lost ordered declarations: %v %v", rules, err)
	}
	doc, err := parser.ParseHTML(strings.NewReader(`<div id="x"></div>`), "https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	node, err := doc.QuerySelector("#x")
	if err != nil || node == nil {
		t.Fatalf("actual DOM node missing: %v", err)
	}
	style := NewComputedStyle(rules)
	for _, test := range []struct{ property, want string }{{"color", "rgb(255, 0, 0)"}, {"--text", `"a;b}c"`}} {
		value, err := style.Compute(node.Raw(), "", test.property)
		if err != nil || value != test.want {
			t.Fatalf("canonical adapter %s=%q want=%q error=%v", test.property, value, test.want, err)
		}
	}
	if style.RuleCount() != 1 || !strings.Contains(rules[0].String(), "declarations:4") {
		t.Fatal("adapter diagnostics lost actual rule identity")
	}
	if value, err := style.Compute(node.Raw(), "width:20px", "width"); value != "" || !css.IsDiagnostic(err) {
		t.Fatalf("adapter fabricated layout computation: %q %v", value, err)
	}
	if value, err := style.Compute(nil, "", "color"); value != "" || !css.IsDiagnostic(err) {
		t.Fatalf("adapter fabricated missing DOM computation: %q %v", value, err)
	}
}
