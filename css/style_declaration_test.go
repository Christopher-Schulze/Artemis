package css

import (
	"fmt"
	"strings"
	"testing"
)

func requireStyleDeclaration(t *testing.T, source string) *StyleDeclaration {
	t.Helper()
	style, err := ParseStyleDeclaration(source)
	if err != nil {
		t.Fatal(err)
	}
	return style
}

func TestCSSStyleAttributePreservesImmutableParsingOrder(t *testing.T) {
	for _, source := range []string{"color:red !important;width:1px", "--a:value !important;opacity:1"} {
		style, err := ParseStyleAttribute(source)
		if err != nil || style == nil {
			t.Fatalf("immutable attribute rejected: %v %v", style, err)
		}
		mutable := requireStyleDeclaration(t, source)
		if style.Item(0) == mutable.Item(0) {
			t.Fatalf("attribute and mutable parsing lost their actual order distinction: %s", source)
		}
	}
	for _, source := range []string{"color:red", "width:1px;color:red !important", "color:red !important;color:blue", "color:red !important;width:1px;opacity:1"} {
		if style, err := ParseStyleAttribute(source); err != nil || style == nil {
			t.Fatalf("mode-independent attribute rejected: %v %v", style, err)
		}
	}
}

func stylePriority(t *testing.T, style *StyleDeclaration, property string) string {
	t.Helper()
	value, err := style.GetPropertyPriority(property)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCSSStyleDeclarationOrderDuplicatesPriorityAndSpecifiedValues(t *testing.T) {
	style := requireStyleDeclaration(t, `width:12PX;color:red !important;width:20px;color:blue;--Mixed: /* comment */ Value;--empty:;padding:1px 2px;padding-left:3px`)
	for _, test := range []struct{ property, value, priority string }{
		{"color", "red", "important"}, {"COLOR", "red", "important"}, {"width", "20px", ""},
		{"--Mixed", "Value", ""}, {"--mixed", "", ""}, {"--empty", "", ""},
		{"padding", "1px 2px 1px 3px", ""}, {"padding-left", "3px", ""},
	} {
		value, err := style.GetPropertyValue(test.property)
		if err != nil || value != test.value || stylePriority(t, style, test.property) != test.priority {
			t.Fatalf("CSSOM %s=%q priority=%q err=%v", test.property, value, stylePriority(t, style, test.property), err)
		}
	}
	if style.Length() != 8 || style.Item(0) != "width" || style.Item(1) != "--Mixed" || style.Item(-1) != "" || style.Item(8) != "" {
		t.Fatalf("declaration order, count or bounds changed: len=%d first=%q second=%q", style.Length(), style.Item(0), style.Item(1))
	}
	serialized, err := style.CSSString()
	if err != nil || serialized != "width: 20px; --Mixed: Value; --empty: ; padding: 1px 2px 1px 3px; color: red !important;" {
		t.Fatalf("inline serializer reordered or lost declarations: %q %v", serialized, err)
	}
}

func TestCSSStyleDeclarationMutationOverridesPriorityWithoutMovingOrInjecting(t *testing.T) {
	style := requireStyleDeclaration(t, `padding-left:1px;color:red !important;padding:2px`)
	if changed, err := style.SetProperty("padding-left", "3px", ""); err != nil || !changed {
		t.Fatalf("longhand mutation failed: %v %v", changed, err)
	}
	if changed, err := style.SetProperty("color", "blue", ""); err != nil || !changed || stylePriority(t, style, "color") != "" {
		t.Fatalf("explicit CSSOM write retained old important priority: %v %v", changed, err)
	}
	if style.Item(0) != "padding-top" || style.Item(style.Length()-1) != "color" {
		t.Fatal("setting existing declarations changed their order")
	}
	value, err := style.GetPropertyValue("padding")
	if err != nil || value != "2px 2px 2px 3px" {
		t.Fatalf("alphabetic rewrite or earlier shorthand masked the write: %q %v", value, err)
	}
	before, err := style.CSSString()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"green; opacity:0", "green !important", `"unterminated`, "var(color)"} {
		if changed, err := style.SetProperty("color", value, ""); changed || !IsDiagnostic(err) {
			t.Fatalf("invalid CSSOM value acquired mutation: %q changed=%v err=%v", value, changed, err)
		}
		actual, err := style.CSSString()
		if err != nil || actual != before {
			t.Fatal("failed candidate modified the retained declarations")
		}
	}
	if changed, err := style.SetProperty("color", "blue", ""); err != nil || changed {
		t.Fatalf("identical write became a DOM mutation: %v %v", changed, err)
	}
	if changed, err := style.SetProperty("color", "green", "bad"); err != nil || changed {
		t.Fatalf("invalid priority was not a CSSOM no-op: %v %v", changed, err)
	}
	if changed, err := style.SetProperty("color", "", "bad"); err != nil || changed {
		t.Fatalf("invalid priority mutated an empty-value write: %v %v", changed, err)
	}
	if changed, err := style.SetProperty("color", "", ""); err != nil || !changed {
		t.Fatalf("valid empty write did not remove its declaration: %v %v", changed, err)
	}
}

func TestCSSStyleDeclarationPendingShorthandsAndLosslessRemoval(t *testing.T) {
	style := requireStyleDeclaration(t, `--space:2px;padding:var(--space) !important;color:red`)
	value, err := style.GetPropertyValue("padding")
	if err != nil || value != "var(--space)" || stylePriority(t, style, "padding") != "important" {
		t.Fatalf("pending shorthand was resolved as an authored longhand: %q %v", value, err)
	}
	if value, err := style.GetPropertyValue("padding-left"); err != nil || value != "" {
		t.Fatalf("pending longhand exposed a fabricated value: %q %v", value, err)
	}
	before, err := style.CSSString()
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := style.RemoveProperty("padding-left"); changed || !IsDiagnostic(err) {
		t.Fatalf("partial pending removal silently lost values: %v %v", changed, err)
	}
	if after, err := style.CSSString(); err != nil || after != before {
		t.Fatal("rejected pending removal changed the state")
	}
	previous, changed, err := style.RemoveProperty("padding")
	if err != nil || !changed || previous != "" || style.Length() != 2 {
		t.Fatalf("whole shorthand removal failed: %q %v %v", previous, changed, err)
	}
	separate := requireStyleDeclaration(t, `padding-top:var(--space);padding-right:var(--space);padding-bottom:var(--space);padding-left:var(--space)`)
	if value, err := separate.GetPropertyValue("padding"); err != nil || value != "" {
		t.Fatalf("independent longhand variables became a semantically different shorthand: %q %v", value, err)
	}
	wide := requireStyleDeclaration(t, `padding:inherit;padding-left:inherit`)
	if value, err := wide.GetPropertyValue("padding"); err != nil || value != "inherit" {
		t.Fatalf("identical CSS-wide values lost legal shorthand serialization: %q %v", value, err)
	}
}

func TestCSSStyleDeclarationEscapedNamesAndRetainedCustomBudget(t *testing.T) {
	style := requireStyleDeclaration(t, `--a\;b: Original; --Case: Value`)
	if changed, err := style.SetProperty("--a;b", "Changed", "IMPORTANT"); err != nil || !changed {
		t.Fatalf("literal custom name lost its escaped identity: %v %v", changed, err)
	}
	serialized, err := style.CSSString()
	if err != nil {
		t.Fatal(err)
	}
	parsed := requireStyleDeclaration(t, serialized)
	if value, err := parsed.GetPropertyValue("--a;b"); err != nil || value != "Changed" || stylePriority(t, parsed, "--a;b") != "important" {
		t.Fatalf("custom name serialization changed property identity: %q %v", value, err)
	}
	var source strings.Builder
	for index := 0; index < maxCSSVariables; index++ {
		fmt.Fprintf(&source, "--v%d:value;", index)
	}
	bounded := requireStyleDeclaration(t, source.String())
	if changed, err := bounded.SetProperty("--excess", "value", ""); changed || !IsDiagnostic(err) || bounded.Length() != maxCSSVariables {
		t.Fatalf("custom property retention grew past its quota: %v %v", changed, err)
	}
}
