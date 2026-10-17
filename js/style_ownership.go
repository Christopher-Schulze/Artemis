package js

import (
	"crypto/sha256"
	"slices"
	"strconv"
	"strings"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/webapi"
	"golang.org/x/net/html"
)

// Fingerprints bind admitted sheets to actual DOM inputs. Dynamic sheet/CSSOM
// loading needs its own lifecycle proof; stale initial sheets cannot prove it.
func stylesheetFingerprint(doc *webapi.Document) ([32]byte, []*html.Node, error) {
	nodes, err := doc.QuerySelectorAll("style, link, base")
	if err != nil {
		return [32]byte{}, nil, err
	}
	if len(nodes) > 4096 {
		return [32]byte{}, nil, &css.Diagnostic{Code: css.DiagnosticBudget, Reason: "stylesheet input node limit exceeded"}
	}
	var source strings.Builder
	identities := make([]*html.Node, 0, len(nodes))
	for _, node := range nodes {
		identities = append(identities, node.Raw())
		fields := []string{node.Raw().Data, node.Raw().Namespace, node.Text()}
		for _, attribute := range node.Raw().Attr {
			fields = append(fields, attribute.Namespace, attribute.Key, attribute.Val)
		}
		for _, field := range fields {
			prefix := strconv.Itoa(len(field)) + ":"
			if len(prefix)+len(field)+source.Len()+1 > css.MaxStylesheetBytes {
				return [32]byte{}, nil, &css.Diagnostic{Code: css.DiagnosticBudget, Reason: "stylesheet input fingerprint byte limit exceeded"}
			}
			source.WriteString(prefix)
			source.WriteString(field)
		}
		source.WriteByte(';')
	}
	return sha256.Sum256([]byte(source.String())), identities, nil
}

func (m *styleManager) checkStylesheetOwnership(n *webapi.Node) error {
	root := n.Raw()
	for root.Parent != nil {
		root = root.Parent
	}
	if m.document == nil || root != m.document.RawRoot() {
		return &css.Diagnostic{Code: css.DiagnosticUnsupported, Reason: "computed style lacks owning document admission"}
	}
	fingerprint, nodes, err := stylesheetFingerprint(m.document)
	if err != nil {
		return err
	}
	if fingerprint != m.sourceFingerprint || !slices.Equal(nodes, m.sourceNodes) {
		return &css.Diagnostic{Code: css.DiagnosticUnsupported, Reason: "stylesheet mutation requires browser lifecycle semantics"}
	}
	return nil
}
