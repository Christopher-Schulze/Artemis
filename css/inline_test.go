package css

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseInline(t *testing.T) {
	got := ParseInline(" color: red ; font-size: 14px;background: blue;;")
	want := map[string]string{"color": "red", "font-size": "14px", "background": "blue"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
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
		"webkitTransform": "webkit-transform",
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
