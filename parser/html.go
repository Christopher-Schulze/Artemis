// Package parser exposes HTML parsing as a thin facade over
// golang.org/x/net/html. The parser hands ownership of the resulting
// node tree to the webapi package, which wraps it for the rest of
// Artemis.
package parser

import (
	"fmt"
	"io"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// ParseHTML parses an HTML document from r. The url is stored on the
// returned Document for resolving relative references.
func ParseHTML(r io.Reader, url string) (*webapi.Document, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	return webapi.NewDocument(root, url), nil
}
