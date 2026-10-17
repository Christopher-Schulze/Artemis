package js

import (
	"context"
	"errors"
	"math"
	"strings"

	v8 "github.com/tommie/v8go"
	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// styleManager collects CSS stylesheets that apply to the document:
// inline `<style>` tags AND external `<link rel=stylesheet href=...>`
// fetched via the engine's StylesheetLoader.
type styleManager struct {
	sheets            []*css.Stylesheet
	failure           error
	failures          int
	inline            map[*html.Node]*inlineStyleState
	inlineBytes       int
	document          *webapi.Document
	sourceFingerprint [32]byte
	sourceNodes       []*html.Node
}

// StylesheetLoader is what the styleManager uses to fetch external
// stylesheets. The engine provides one wrapping its HTTP client.
type StylesheetLoader func(ctx context.Context, url string) ([]byte, error)

// newStyleManager harvests every <style> element under the document and
// every active <link rel=stylesheet href=...> in document order. Missing
// loaders and unsupported media semantics cannot produce partial CSS evidence.
func newStyleManager(ctx context.Context, doc *webapi.Document, loader StylesheetLoader) (*styleManager, error) {
	if doc == nil || doc.Root() == nil {
		return &styleManager{}, nil
	}
	nodes, err := doc.QuerySelectorAll("style, link")
	if err != nil {
		return nil, err
	}
	m := &styleManager{}
	m.document = doc
	m.sourceFingerprint, m.sourceNodes, err = stylesheetFingerprint(doc)
	if err != nil {
		return nil, err
	}
	bytes := 0
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		active, err := admitStylesheetNode(node)
		if err != nil {
			return nil, err
		}
		if !active {
			continue
		}
		if len(m.sheets) >= 128 {
			return nil, &css.Diagnostic{Code: css.DiagnosticBudget, Reason: "document stylesheet count limit exceeded"}
		}
		text, err := stylesheetSource(ctx, doc, node, loader)
		if err != nil {
			return nil, err
		}
		bytes += len(text)
		if err := m.appendStylesheet(text, bytes); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *styleManager) appendStylesheet(text string, aggregateBytes int) error {
	if aggregateBytes > css.MaxStylesheetBytes {
		return &css.Diagnostic{Code: css.DiagnosticBudget, Reason: "document stylesheet byte limit exceeded"}
	}
	sheet := css.ParseStylesheet(text)
	if err := sheet.Err(); err != nil {
		return err
	}
	m.sheets = append(m.sheets, sheet)
	return nil
}

func admitStylesheetNode(node *webapi.Node) (bool, error) {
	if node.Raw().Namespace != "" {
		return false, &css.Diagnostic{Code: css.DiagnosticUnsupported, Reason: "foreign stylesheet namespace requires browser semantics"}
	}
	link := node.Raw().Data == "link"
	if link && !cssRelToken(node.AttrOrEmpty("rel"), "stylesheet") {
		return false, nil
	}
	if _, disabled := node.Attr("disabled"); disabled && link {
		return false, nil
	}
	kind := strings.Trim(node.AttrOrEmpty("type"), " \t\r\n\f")
	if kind != "" && strings.ToLower(kind) != "text/css" {
		return false, nil
	}
	media := strings.ToLower(strings.Trim(node.AttrOrEmpty("media"), " \t\r\n\f"))
	if media != "" && media != "all" && media != "screen" {
		return false, &css.Diagnostic{Code: css.DiagnosticUnsupported, Reason: "stylesheet media requires browser evaluation"}
	}
	if link && cssRelToken(node.AttrOrEmpty("rel"), "alternate") {
		return false, &css.Diagnostic{Code: css.DiagnosticUnsupported, Reason: "alternate stylesheet selection requires browser semantics"}
	}
	if link && strings.Trim(node.AttrOrEmpty("href"), " \t\r\n\f") == "" {
		return false, nil
	}
	return true, nil
}

func stylesheetSource(ctx context.Context, doc *webapi.Document, node *webapi.Node, loader StylesheetLoader) (string, error) {
	if node.Raw().Data != "link" {
		return node.Text(), nil
	}
	href := strings.Trim(node.AttrOrEmpty("href"), " \t\r\n\f")
	if href == "" {
		return "", nil
	}
	if loader == nil {
		return "", &css.Diagnostic{Code: css.DiagnosticLoad, Reason: "stylesheet loader required"}
	}
	href, err := resolveDocumentURL(doc, href)
	if err != nil {
		return "", &css.Diagnostic{Code: css.DiagnosticLoad, Reason: "invalid stylesheet resource URL", Cause: err}
	}
	body, err := loader(ctx, href)
	if errors.Is(err, network.ErrAdBlocked) {
		// A blocked stylesheet contributes no rules, as in a browser.
		return "", nil
	}
	if err != nil {
		return "", &css.Diagnostic{Code: css.DiagnosticLoad, Reason: "external stylesheet load failed", Cause: errors.Join(err, ctx.Err())}
	}
	return string(body), nil
}

func cssRelToken(value, want string) bool {
	for _, token := range strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune(" \t\r\n\f", r) }) {
		if strings.ToLower(token) == want {
			return true
		}
	}
	return false
}

// Native callbacks retain the original typed failure even if JS catches its
// exception. The enclosing execution consumes this bounded evidence once.
func (m *styleManager) recordFailure(err error) {
	if err == nil || m.failures >= 64 {
		return
	}
	m.failure = errors.Join(m.failure, err)
	m.failures++
}

func (m *styleManager) takeFailure() error {
	if m == nil {
		return nil
	}
	err := m.failure
	m.failure, m.failures = nil, 0
	if err == nil {
		return nil
	}
	return &css.ExecutionFailure{Cause: err}
}

// computed returns the cascaded styles for n, merged with the inline
// style attribute. !important from stylesheets wins over inline; inline
// wins over non-important sheet declarations.
func (m *styleManager) computed(n *webapi.Node, property string) (string, error) {
	if m == nil || n == nil {
		return "", &css.Diagnostic{Code: css.DiagnosticUnsupported, Reason: "computed style requires an element"}
	}
	if err := m.checkStylesheetOwnership(n); err != nil {
		return "", err
	}
	inline := n.AttrOrEmpty("style")
	style, err := css.Cascade(m.sheets, n.Raw(), inline)
	if err != nil {
		return "", err
	}
	return style.Value(property)
}

func (r *Runtime) ensureCascadeStyleTemplate() *v8.FunctionTemplate {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cascadeStyleTemplate != nil {
		return r.cascadeStyleTemplate
	}
	iso := r.iso
	r.cascadeStyleTemplate = v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		defer info.Release()
		c := r.contextFor(info.Context())
		args := info.Args()
		if c == nil || len(args) < 2 {
			return r.callbackPrimitive(info, "")
		}
		nodeID, err := mutationInteger(args[0], math.MaxUint32)
		if err != nil {
			return domCallbackResult(iso, domCallbackError(iso, err))
		}
		n := c.nodes.Get(uint32(nodeID))
		if n == nil {
			return r.callbackPrimitive(info, "")
		}
		property, err := args[1].StringChecked()
		if err != nil {
			return domCallbackResult(iso, domCallbackError(iso, err))
		}
		value, err := c.styleMgr.computed(n, css.CamelToKebab(property))
		if err != nil {
			c.styleMgr.recordFailure(err)
			return domCallbackResult(iso, domCallbackError(iso, err))
		}
		return r.callbackPrimitive(info, value)
	})
	return r.cascadeStyleTemplate
}

// installStyleManagerBridge exposes a `__cascade_style(id, prop)` native
// trampoline. The live JS getComputedStyle proxy uses this canonical owner
// for every computed property read.
func installStyleManagerBridge(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureCascadeStyleTemplate()
	global, function := v8ctx.Global(), t.GetFunction(v8ctx)
	defer global.Release()
	defer function.Release()
	if err := global.Set("__cascade_style", function); err != nil {
		return err
	}
	c.registerBootstrap("artemis-style-manager", inlineGetComputedStyleBootstrap)
	return nil
}
