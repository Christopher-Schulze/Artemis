package webapi

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestDocumentHTMLTitleTargetAndWhitespace(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"head", "<title>  A\tB\nC\rD\fE  </title>", "A B C D E"},
		{"non ASCII spaces", "<title>&nbsp; A\tB &nbsp;</title>", "\u00a0 A B \u00a0"},
		{"body", "<html><head></head><body><title>Body title</title></body></html>", "Body title"},
		{"first", "<title>First</title><title>Second</title>", "First"},
		{"inline SVG", "<html><body><svg><title>SVG</title></svg><title>HTML</title></body></html>", "HTML"},
		{"no HTML title", "<html><body><svg><title>SVG</title></svg></body></html>", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := html.Parse(strings.NewReader(test.source))
			if err != nil {
				t.Fatal(err)
			}
			document := NewDocument(root, "https://example.test/")
			if got := document.Title(); got != test.want {
				t.Fatalf("actual HTML title = %q, want %q", got, test.want)
			}
			title := document.HTMLTitleElement()
			if test.want == "" {
				if title != nil {
					t.Fatal("foreign title became the HTML title")
				}
				return
			}
			if title == nil || title.Raw().Namespace != "" || title.Tag() != "title" {
				t.Fatal("HTML title target lost its actual node identity")
			}
		})
	}
}

func TestDocumentHTMLTitleEmptyOwner(t *testing.T) {
	for _, document := range []*Document{nil, NewDocument(nil, ""), NewDocument(&html.Node{Type: html.DocumentNode}, "")} {
		if document.HTMLTitleElement() != nil || document.Title() != "" {
			t.Fatal("missing document fabricated a title")
		}
	}
}
