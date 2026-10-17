package scraper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/security"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

const selectorEvidenceBytes = 32768
const selectorEvidenceNodes = 512

type selectorElementEvidence struct {
	Tag        string            `json:"tag"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Text       string            `json:"text,omitempty"`
}

type selectorDocumentEvidence struct {
	Schema    string                    `json:"schema"`
	Nodes     []selectorElementEvidence `json:"nodes"`
	Truncated bool                      `json:"truncated"`
}

// Scope includes the actual origin and static intent, never DOM field values.
func selectorCachePattern(domain, pattern, pageURL, intent string) (string, error) {
	parsed, err := url.Parse(pageURL)
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || !strings.EqualFold(parsed.Hostname(), domain) {
		return "", fmt.Errorf("ai finder: cache domain must match the actual HTTP page origin")
	}
	if len(pattern) > 4096 || len(intent) > 1024 || !utf8.ValidString(pattern) || !utf8.ValidString(intent) {
		return "", fmt.Errorf("ai finder: structural cache context exceeds bounds")
	}
	raw, err := json.Marshal([]string{parsed.Scheme + "://" + strings.ToLower(parsed.Host), pattern, strings.TrimSpace(intent)})
	if err != nil {
		return "", fmt.Errorf("ai finder: cache context: %w", err)
	}
	hash := sha256.Sum256(raw)
	return "selector-context-v1:" + hex.EncodeToString(hash[:]), nil
}

func selectorPageEvidence(ctx context.Context, doc *webapi.Document) (string, error) {
	if ctx == nil || doc == nil || doc.Root() == nil {
		return "", fmt.Errorf("ai finder: context and document required for evidence")
	}
	redactor := security.NewRedactor(security.DefaultRedactionConfig())
	evidence := selectorDocumentEvidence{Schema: "artemis.selector-context.v1", Nodes: make([]selectorElementEvidence, 0)}
	webapi.WalkValue(doc.Root(), func(node webapi.Node) webapi.WalkAction {
		if ctx.Err() != nil {
			return webapi.WalkStop
		}
		if node.Type() != webapi.NodeElement {
			return webapi.WalkContinue
		}
		switch node.Tag() {
		case "script", "style", "textarea":
			return webapi.WalkSkip
		}
		if len(evidence.Nodes) >= selectorEvidenceNodes {
			evidence.Truncated = true
			return webapi.WalkStop
		}
		element := selectorElementEvidence{Tag: node.Tag(), Attributes: make(map[string]string)}
		for _, name := range []string{"id", "class", "role", "name", "type", "aria-label", "alt", "title", "disabled", "hidden", "inert"} {
			if value, exists := node.Attr(name); exists {
				evidence.Truncated = evidence.Truncated || len(value) > 256
				element.Attributes[name] = redactor.RedactSnapshot(selectorBoundedText(value)).Redacted
			}
		}
		// Only direct observed text; input/textarea values and scripts never enter context.
		for child := node.Raw().FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				evidence.Truncated = evidence.Truncated || len(child.Data) > 256 || len(element.Text)+len(child.Data)+1 > 256
				element.Text += " " + selectorBoundedText(child.Data)
				if len(element.Text) >= 256 {
					evidence.Truncated = true
					break
				}
			}
		}
		element.Text = redactor.RedactSnapshot(selectorBoundedText(strings.TrimSpace(element.Text))).Redacted
		evidence.Nodes = append(evidence.Nodes, element)
		return webapi.WalkContinue
	})
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for {
		raw, err := json.Marshal(evidence)
		if err != nil {
			return "", fmt.Errorf("ai finder: page evidence: %w", err)
		}
		if len(raw) <= selectorEvidenceBytes {
			return string(raw), nil
		}
		fit := len(evidence.Nodes) * selectorEvidenceBytes / len(raw)
		if fit >= len(evidence.Nodes) {
			fit = len(evidence.Nodes) - 1
		}
		evidence.Nodes = evidence.Nodes[:fit]
		evidence.Truncated = true
	}
}

func selectorBoundedText(value string) string {
	if len(value) <= 256 {
		return value
	}
	limit := 256
	for !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}
