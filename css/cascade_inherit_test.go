package css

import (
	"testing"
)

func TestCascadeInheritsColor(t *testing.T) {
	sh := ParseStylesheet(`body { color: red; font-size: 16px; }`)
	doc := `<html><body><div><p id="a">x</p></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := Cascade([]*Stylesheet{sh}, n, "")
	if got["color"] != "red" {
		t.Errorf("color = %q, want red (inherited from body)", got["color"])
	}
	if got["font-size"] != "16px" {
		t.Errorf("font-size = %q, want 16px (inherited)", got["font-size"])
	}
}

func TestCascadeOwnValueBeatsInheritance(t *testing.T) {
	sh := ParseStylesheet(`body { color: red; } #a { color: blue; }`)
	doc := `<html><body><p id="a">x</p></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := Cascade([]*Stylesheet{sh}, n, "")
	if got["color"] != "blue" {
		t.Errorf("color = %q, want blue (own beats inherited)", got["color"])
	}
}

func TestCascadeNonInheritedNotInherited(t *testing.T) {
	sh := ParseStylesheet(`body { padding: 10px; }`)
	doc := `<html><body><p id="a">x</p></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := Cascade([]*Stylesheet{sh}, n, "")
	if got["padding"] != "" {
		t.Errorf("padding = %q, should NOT inherit", got["padding"])
	}
}

func TestCascadeInheritsThroughMultipleLevels(t *testing.T) {
	sh := ParseStylesheet(`html { color: green; }`)
	doc := `<html><body><div><span><a id="deep">x</a></span></div></body></html>`
	n := parseAndFind(t, doc, "#deep")
	got := Cascade([]*Stylesheet{sh}, n, "")
	if got["color"] != "green" {
		t.Errorf("color = %q, want green (inherited 4 levels up)", got["color"])
	}
}
