package actions

import "testing"

func TestSelectorKindSemantic(t *testing.T) {
	if SelectorKindSemantic != "semantic" {
		t.Errorf("SelectorKindSemantic = %s, want semantic", SelectorKindSemantic)
	}
}

func TestResolveSelectorSemantic(t *testing.T) {
	resolved, err := ResolveSelector("find:login button")
	if err != nil {
		t.Fatalf("ResolveSelector: %v", err)
	}
	if resolved.Kind != SelectorKindSemantic {
		t.Errorf("Kind = %s, want semantic", resolved.Kind)
	}
	if resolved.Canonical != "login button" {
		t.Errorf("Canonical = %s, want 'login button'", resolved.Canonical)
	}
}

func TestResolveSelectorCSSPrefix(t *testing.T) {
	resolved, err := ResolveSelector("css:#submit-button")
	if err != nil {
		t.Fatalf("ResolveSelector: %v", err)
	}
	if resolved.Kind != SelectorKindCSS {
		t.Errorf("Kind = %s, want css", resolved.Kind)
	}
}

func TestResolveSelectorXPathPrefix(t *testing.T) {
	resolved, err := ResolveSelector("xpath://button[@type='submit']")
	if err != nil {
		t.Fatalf("ResolveSelector: %v", err)
	}
	if resolved.Kind != SelectorKindXPath {
		t.Errorf("Kind = %s, want xpath", resolved.Kind)
	}
}

func TestResolveSelectorTextPrefix(t *testing.T) {
	resolved, err := ResolveSelector("text:Submit")
	if err != nil {
		t.Fatalf("ResolveSelector: %v", err)
	}
	if resolved.Kind != SelectorKindText {
		t.Errorf("Kind = %s, want text", resolved.Kind)
	}
}

func TestResolveSelectorRef(t *testing.T) {
	resolved, err := ResolveSelector("e123")
	if err != nil {
		t.Fatalf("ResolveSelector: %v", err)
	}
	if resolved.Kind != SelectorKindRef {
		t.Errorf("Kind = %s, want ref", resolved.Kind)
	}
}

func TestIsValidSelectorKindWithSemantic(t *testing.T) {
	if !IsValidSelectorKind(SelectorKindSemantic) {
		t.Error("IsValidSelectorKind(semantic) should be true")
	}
}

func TestSelectorPriority(t *testing.T) {
	cases := []struct {
		kind SelectorKind
		want int
	}{
		{SelectorKindRef, 0},
		{SelectorKindCSS, 1},
		{SelectorKindXPath, 2},
		{SelectorKindText, 3},
		{SelectorKindSemantic, 4},
	}
	for _, c := range cases {
		got := SelectorPriority(c.kind)
		if got != c.want {
			t.Errorf("SelectorPriority(%s) = %d, want %d", c.kind, got, c.want)
		}
	}
}

func TestCompareSelectorPriority(t *testing.T) {
	if CompareSelectorPriority(SelectorKindRef, SelectorKindCSS) != -1 {
		t.Error("Ref should have higher priority than CSS")
	}
	if CompareSelectorPriority(SelectorKindCSS, SelectorKindRef) != 1 {
		t.Error("CSS should have lower priority than Ref")
	}
	if CompareSelectorPriority(SelectorKindRef, SelectorKindRef) != 0 {
		t.Error("Same kind should have equal priority")
	}
}

func TestSortSelectorsByPriority(t *testing.T) {
	selectors := []ResolvedSelector{
		{Kind: SelectorKindSemantic, Original: "find:login"},
		{Kind: SelectorKindRef, Original: "e5"},
		{Kind: SelectorKindText, Original: "text:Submit"},
		{Kind: SelectorKindCSS, Original: "css:#btn"},
		{Kind: SelectorKindXPath, Original: "xpath://button"},
	}
	sorted := SortSelectorsByPriority(selectors)
	expected := []SelectorKind{
		SelectorKindRef, SelectorKindCSS, SelectorKindXPath, SelectorKindText, SelectorKindSemantic,
	}
	for i, want := range expected {
		if sorted[i].Kind != want {
			t.Errorf("sorted[%d].Kind = %s, want %s", i, sorted[i].Kind, want)
		}
	}
}

func TestResolvedSelectorIsSemantic(t *testing.T) {
	r := ResolvedSelector{Kind: SelectorKindSemantic}
	if !r.IsSemantic() {
		t.Error("IsSemantic should be true")
	}
}
