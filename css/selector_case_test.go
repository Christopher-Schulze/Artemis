package css

import "testing"

func TestCSSCascadeRejectsCaseDependentDocumentModeMatching(t *testing.T) {
	for _, test := range []struct {
		source, markup string
		unsupported    bool
	}{
		{`.Card{color:red}`, `<div id="x" class="card"></div>`, true},
		{`#X{color:red}`, `<div id="x"></div>`, true},
		{`:not(.Card){color:red}`, `<div id="x" class="card"></div>`, true},
		{`.Card span{color:red}`, `<div class="card"><span id="x"></span></div>`, true},
		{`.\43 ard{color:red}`, `<div id="x" class="card"></div>`, true},
		{`.Card{color:red}`, `<div id="x" class="Card"></div>`, false},
		{`.card{color:red}`, `<div id="x" class="other"></div>`, false},
		{`[data-name="#X.Card"]{color:red}`, `<div id="x" class="card"></div>`, false},
	} {
		node := parseAndFind(t, test.markup, "#x")
		sheet := ParseStylesheet(test.source)
		if err := sheet.Err(); err != nil {
			t.Fatalf("case control did not reach actual matching admission: %v", err)
		}
		_, err := Cascade([]*Stylesheet{sheet}, node, "")
		if test.unsupported != IsDiagnostic(err) {
			t.Fatalf("document-mode admission for %s: %v", test.source, err)
		}
	}
}
