package css

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

func assertCSSSelectorChromiumParity(t *testing.T, ctx context.Context, page *bridge.Page) {
	t.Helper()
	fixtures := []struct {
		Selector string `json:"selector"`
		Markup   string `json:"markup"`
	}{
		{`div`, `<div id="target" class="card"></div>`},
		{`.card`, `<div id="target" class="card"></div>`},
		{`#target`, `<div id="target" class="card"></div>`},
		{`[data-label="VaLue"]`, `<div id="target" class="card" data-label="VaLue"></div>`},
		{`[data-label="value" i]`, `<div id="target" class="card" data-label="VaLue"></div>`},
		{`input[type="TEXT" i]`, `<input id="target" class="card" type="text">`},
		{`main > div`, `<main><div id="target" class="card"></div></main>`},
		{`main div`, `<main><section><div id="target" class="card"></div></section></main>`},
		{`span + div`, `<main><span></span><div id="target" class="card"></div></main>`},
		{`span ~ div`, `<main><span></span><p></p><div id="target" class="card"></div></main>`},
		{`:not(#missing)`, `<div id="target" class="card"></div>`},
		{`:not(.missing,#missing)`, `<div id="target" class="card"></div>`},
		{`:has(.inside)`, `<div id="target" class="card"><span class="inside"></span></div>`},
		{`:has(.outside .inside)`, `<main class="outside"><div id="target" class="card"><span class="inside"></span></div></main>`},
		{`:nth-child(2n+1)`, `<main><p></p><span></span><div id="target" class="card"></div></main>`},
		{`:nth-last-child(2)`, `<main><div id="target" class="card"></div><p></p></main>`},
		{`:only-of-type`, `<main><p></p><div id="target" class="card"></div></main>`},
		{`:empty`, `<div id="target" class="card"> </div>`},
		{`:empty`, `<div id="target" class="card"><!--comment--></div>`},
	}
	encoded, err := json.Marshal(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	parameters := struct {
		Expression    string `json:"expression"`
		ReturnByValue bool   `json:"returnByValue"`
	}{
		Expression: `(` + string(encoded) + `).map(fixture=>{document.open();document.write('<!doctype html><style>.card.card{color:blue}'+fixture.selector+'{color:red}</style>'+fixture.markup);document.close();const target=document.getElementById('target');return {matches:target.matches(fixture.selector),color:getComputedStyle(target).color};})`, ReturnByValue: true,
	}
	var result struct {
		Result struct {
			Value []struct {
				Matches bool   `json:"matches"`
				Color   string `json:"color"`
			} `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", parameters, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ExceptionDetails) != 0 || len(result.Result.Value) != len(fixtures) {
		t.Fatalf("native selector observation failed: %+v", result)
	}
	for index, fixture := range fixtures {
		source := `.card.card{color:blue}` + fixture.Selector + `{color:red}`
		sheet := ParseStylesheet(source)
		if fixture.Selector == `:has(.inside)` || fixture.Selector == `:has(.outside .inside)` {
			if !IsDiagnostic(sheet.Err()) {
				t.Fatalf("unbounded relational selector acquired support: %s", fixture.Selector)
			}
			if result.Result.Value[index].Matches != (fixture.Selector == `:has(.inside)`) {
				t.Fatalf("native relational-selector anchor oracle changed: %s %+v", fixture.Selector, result.Result.Value[index])
			}
			continue
		}
		if err := sheet.Err(); err != nil {
			t.Fatalf("selector fixture not admitted: %s %v", fixture.Selector, err)
		}
		node := parseAndFind(t, `<!doctype html>`+fixture.Markup, "#target")
		style, err := Cascade([]*Stylesheet{sheet}, node, "")
		if err != nil {
			t.Fatal(err)
		}
		color, err := style.Value("color")
		matches := sheet.Rules[1].Selectors[0].Match(node)
		if err != nil || color != result.Result.Value[index].Color || matches != result.Result.Value[index].Matches {
			t.Errorf("%s renderless match=%v color=%q Chromium=%+v err=%v", fixture.Selector, matches, color, result.Result.Value[index], err)
		}
	}
}
