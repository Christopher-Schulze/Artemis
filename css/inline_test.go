package css

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseInline(t *testing.T) {
	got, err := ParseInline(" color: red ; font-size: 14px;background: blue;;")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"color": "red", "font-size": "14px", "background": "blue"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseInlineRejectsPartialInputAndRetainsActualPriority(t *testing.T) {
	values, err := ParseInline(`content: "a;b{}"; color: red !important; color: blue; --Mixed: value;`)
	if err != nil || values["content"] != `"a;b{}"` || values["color"] != "red !important" || values["--Mixed"] != "value" {
		t.Fatalf("inline declaration projection changed admitted semantics: %v %v", values, err)
	}
	for _, source := range []string{`color: red; broken`, `content: "unterminated`, `color: rgb(1,2,3;`} {
		if values, err := ParseInline(source); err == nil || values != nil {
			t.Fatalf("malformed inline CSS returned a partial map: %q %v %v", source, values, err)
		}
	}
}

func TestSerializeStable(t *testing.T) {
	got := Serialize(map[string]string{"font-size": "14px", "color": "red"})
	if !strings.Contains(got, "color: red") || !strings.Contains(got, "font-size: 14px") {
		t.Errorf("got %q", got)
	}
}

func TestCamelKebab(t *testing.T) {
	cases := map[string]string{
		"fontSize":        "font-size",
		"backgroundColor": "background-color",
		"color":           "color",
		"webkitTransform": "-webkit-transform",
		"msTransform":     "-ms-transform",
		"MozTransform":    "-moz-transform",
		"OTransform":      "-o-transform",
		"--Mixed":         "--Mixed",
		"":                "",
	}
	for in, want := range cases {
		if got := CamelToKebab(in); got != want {
			t.Errorf("CamelToKebab(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range cases {
		if got := KebabToCamel(want); got != in {
			t.Errorf("KebabToCamel(%q) = %q, want %q", want, got, in)
		}
	}
}
