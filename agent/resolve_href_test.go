package agent

import (
	"net/url"
	"testing"
)

func TestResolveHrefFastPaths(t *testing.T) {
	base, _ := url.Parse("https://example.com/dir/page?q=1")
	cases := []struct{ in, want string }{
		{"#section", "#section"},
		{"https://other.com/x", "https://other.com/x"},
		{"mailto:a@b.c", "mailto:a@b.c"},
		{"javascript:void(0)", "javascript:void(0)"},
		{"//cdn.example.com/lib.js", "https://cdn.example.com/lib.js"},
		{"/abs/path", "https://example.com/abs/path"},
		{"rel/page", "https://example.com/dir/rel/page"},
		{"../up", "https://example.com/up"},
		{"", ""},
	}
	for _, c := range cases {
		if got := resolveHref(base, c.in); got != c.want {
			t.Errorf("resolveHref(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := resolveHref(nil, "/x"); got != "/x" {
		t.Errorf("nil base must pass through: %q", got)
	}
}

func TestIsSchemePrefix(t *testing.T) {
	for s, want := range map[string]bool{
		"http": true, "https": true, "mailto": true, "chrome-extension": true, "a+b-c.d": true,
		"": false, "1abc": false, "-x": false, "has space": false, "a/b": false,
	} {
		if got := isSchemePrefix(s); got != want {
			t.Errorf("isSchemePrefix(%q) = %v, want %v", s, got, want)
		}
	}
}
