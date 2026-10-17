package js

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestFetchURLUsesOwningDocumentAndFirstBase(t *testing.T) {
	for _, test := range []struct{ name, document, html, input, want string }{
		{"relative", "https://example.test/dir/page", "", "api", "https://example.test/dir/api"},
		{"parent", "https://example.test/dir/page", "", "../api?q=1#part", "https://example.test/api?q=1#part"},
		{"root", "https://example.test/dir/page", "", "/api", "https://example.test/api"},
		{"empty", "https://example.test/dir/page?q=1", "", "", "https://example.test/dir/page?q=1"},
		{"query", "https://example.test/dir/page?q=1", "", "?q=2", "https://example.test/dir/page?q=2"},
		{"scheme relative", "https://example.test/page", "", "//other.test/api", "https://other.test/api"},
		{"first base", "https://example.test/dir/page", `<base target="_blank"><base href=" /assets/ "><base href="/ignored/">`, "api", "https://example.test/assets/api"},
		{"invalid first base", "https://example.test/dir/page", `<base href="http://["><base href="/ignored/">`, "api", "https://example.test/dir/api"},
		{"data base fallback", "https://example.test/dir/page", `<base href="data:text/html,test">`, "api", "https://example.test/dir/api"},
		{"javascript base fallback", "https://example.test/dir/page", `<base href="javascript:alert(1)">`, "api", "https://example.test/dir/api"},
		{"cross origin base", "https://example.test/page", `<base href="https://other.test/assets/">`, "api", "https://other.test/assets/api"},
		{"absolute unchanged", "https://example.test/page", `<base href="https://other.test/">`, "https://third.test/api?q=1#part", "https://third.test/api?q=1#part"},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, err := parser.ParseHTML(strings.NewReader(test.html), test.document)
			if err != nil {
				t.Fatal(err)
			}
			context := &Context{doc: document}
			got, err := context.resolveFetchURL(test.input)
			if err != nil || got != test.want {
				t.Fatalf("resolved actual document URL = %q, want %q: %v", got, test.want, err)
			}
		})
	}
}

func TestFetchURLRejectsMalformedReferenceAndUnusableBase(t *testing.T) {
	for _, test := range []struct{ document, input string }{
		{"https://example.test/page", "%zz"}, {"https://example.test/page", "http://["},
		{"", "api"}, {"about:blank", "api"},
	} {
		document, err := parser.ParseHTML(strings.NewReader(""), test.document)
		if err != nil {
			t.Fatal(err)
		}
		if resolved, err := (&Context{doc: document}).resolveFetchURL(test.input); err == nil || resolved != "" {
			t.Fatalf("unusable input admitted: %q %v", resolved, err)
		}
	}
}
