package js

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// resolveFetchURL runs on the owning V8 thread before either transport path.
// The canonical network client still admits the resolved subresource target.
func (c *Context) resolveFetchURL(raw string) (string, error) {
	return resolveDocumentURL(c.doc, raw)
}

// resolveDocumentURL shares the owning-document/first-base semantics between
// JavaScript fetch and stylesheet loading; network policy still admits targets.
func resolveDocumentURL(doc *webapi.Document, raw string) (string, error) {
	reference, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("fetch: invalid URL")
	}
	if reference.IsAbs() {
		return raw, nil
	}
	if doc == nil {
		return "", errors.New("fetch: relative URL requires an owning document")
	}
	base, err := url.Parse(doc.URL())
	if err != nil {
		return "", errors.New("fetch: invalid document URL")
	}
	element, err := doc.QuerySelector("base[href]")
	if err != nil {
		return "", fmt.Errorf("fetch: document base: %w", err)
	}
	if element != nil {
		candidate, err := url.Parse(strings.Trim(element.AttrOrEmpty("href"), " \t\n\r\f"))
		if err == nil {
			candidate = base.ResolveReference(candidate)
			if candidate.Scheme != "data" && candidate.Scheme != "javascript" {
				base = candidate
			}
		}
	}
	if !base.IsAbs() || base.Opaque != "" {
		return "", errors.New("fetch: relative URL requires a hierarchical document base")
	}
	return base.ResolveReference(reference).String(), nil
}
