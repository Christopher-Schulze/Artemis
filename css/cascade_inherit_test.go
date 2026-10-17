package css

import (
	"testing"
)

func TestCascadeInheritsColor(t *testing.T) {
	sh := ParseStylesheet(`body { color: red; font-size: 16px; }`)
	doc := `<html><body><div><p id="a">x</p></div></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := requireCascade(t, []*Stylesheet{sh}, n, "")
	if value := computedValue(t, got, "color"); value != "rgb(255, 0, 0)" {
		t.Errorf("color = %q, want computed red (inherited from body)", value)
	}
	if value := computedValue(t, got, "font-size"); value != "16px" {
		t.Errorf("font-size = %q, want 16px (inherited)", value)
	}
}

func TestCascadeOwnValueBeatsInheritance(t *testing.T) {
	sh := ParseStylesheet(`body { color: red; } #a { color: blue; }`)
	doc := `<html><body><p id="a">x</p></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := requireCascade(t, []*Stylesheet{sh}, n, "")
	if value := computedValue(t, got, "color"); value != "rgb(0, 0, 255)" {
		t.Errorf("color = %q, want computed blue (own beats inherited)", value)
	}
}

func TestCascadeNonInheritedNotInherited(t *testing.T) {
	sh := ParseStylesheet(`body { padding: 10px; }`)
	doc := `<html><body><p id="a">x</p></body></html>`
	n := parseAndFind(t, doc, "#a")
	got := requireCascade(t, []*Stylesheet{sh}, n, "")
	if declaration, exists := got.Cascaded("padding"); exists {
		t.Errorf("padding declaration = %q, should NOT inherit", declaration.Value)
	}
}

func TestCascadeInheritsThroughMultipleLevels(t *testing.T) {
	sh := ParseStylesheet(`html { color: green; }`)
	doc := `<html><body><div><span><a id="deep">x</a></span></div></body></html>`
	n := parseAndFind(t, doc, "#deep")
	got := requireCascade(t, []*Stylesheet{sh}, n, "")
	if value := computedValue(t, got, "color"); value != "rgb(0, 128, 0)" {
		t.Errorf("color = %q, want computed green (inherited 4 levels up)", value)
	}
}
