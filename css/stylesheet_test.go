package css

import (
	"strings"
	"testing"
)

func TestParseStylesheetSimple(t *testing.T) {
	sh := ParseStylesheet(`
		body { color: red; font-size: 14px; }
		.x { background: blue; }
	`)
	if len(sh.Rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(sh.Rules))
	}
	if sh.Rules[0].Decls["color"] != "red" {
		t.Errorf("color = %q", sh.Rules[0].Decls["color"])
	}
}

func TestParseStylesheetSelectorList(t *testing.T) {
	sh := ParseStylesheet(`h1, h2, .title { font-weight: bold; }`)
	if len(sh.Rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(sh.Rules))
	}
	if len(sh.Rules[0].Selectors) != 3 {
		t.Errorf("selectors compiled = %d, want 3", len(sh.Rules[0].Selectors))
	}
}

func TestParseStylesheetImportant(t *testing.T) {
	sh := ParseStylesheet(`p { color: red !important; font-size: 14px; }`)
	if !sh.Rules[0].Important["color"] {
		t.Error("color !important not detected")
	}
	if sh.Rules[0].Important["font-size"] {
		t.Error("font-size should NOT be important")
	}
	if sh.Rules[0].Decls["color"] != "red" {
		t.Errorf("color value should be 'red' (without !important), got %q", sh.Rules[0].Decls["color"])
	}
}

func TestParseStylesheetSkipsAtRules(t *testing.T) {
	sh := ParseStylesheet(`
		@media (min-width: 600px) {
			.big { font-size: 20px; }
		}
		@import url("x.css");
		body { color: red; }
	`)
	// At-rules with bodies are skipped wholesale; one rule (body)
	// remains.
	if len(sh.Rules) != 1 {
		t.Fatalf("got %d rules: %+v", len(sh.Rules), sh.Rules)
	}
	if !strings.Contains(sh.Rules[0].SelectorRaw, "body") {
		t.Errorf("kept rule = %q, want body", sh.Rules[0].SelectorRaw)
	}
}

func TestParseStylesheetIgnoresComments(t *testing.T) {
	sh := ParseStylesheet(`/* outside */ .a { /* inside */ color: red; } /* trailing */`)
	if len(sh.Rules) != 1 {
		t.Fatalf("got %d", len(sh.Rules))
	}
	if sh.Rules[0].Decls["color"] != "red" {
		t.Errorf("color = %q", sh.Rules[0].Decls["color"])
	}
}
