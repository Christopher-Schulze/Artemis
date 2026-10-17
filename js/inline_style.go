package js

import (
	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/webapi"
	v8 "github.com/tommie/v8go"
	"golang.org/x/net/html"
)

type inlineStyleState struct {
	source string
	style  *css.StyleDeclaration
}

func (m *styleManager) inlineStyle(n *webapi.Node) (*inlineStyleState, error) {
	source := n.AttrOrEmpty("style")
	if state := m.inline[n.Raw()]; state != nil && state.source == source {
		return state, nil
	}
	style, err := css.ParseStyleAttribute(source)
	if err != nil {
		return nil, err
	}
	state := &inlineStyleState{source: source, style: style}
	return state, m.retainInlineStyle(n, state)
}

func (m *styleManager) invalidateInlineStyle(n *webapi.Node) {
	if state := m.inline[n.Raw()]; state != nil {
		m.inlineBytes -= len(state.source)
		delete(m.inline, n.Raw())
	}
}

func (m *styleManager) retainInlineStyle(n *webapi.Node, state *inlineStyleState) error {
	previous := m.inline[n.Raw()]
	bytes := m.inlineBytes + len(state.source)
	if previous != nil {
		bytes -= len(previous.source)
	}
	if bytes > css.MaxStylesheetBytes || previous == nil && len(m.inline) >= 1024 {
		return &css.Diagnostic{Code: css.DiagnosticBudget, Reason: "inline CSSOM ownership limit exceeded"}
	}
	if m.inline == nil {
		m.inline = make(map[*html.Node]*inlineStyleState)
	}
	m.inline[n.Raw()], m.inlineBytes = state, bytes
	return nil
}

func (c *Context) commitInlineStyle(n *webapi.Node, style *css.StyleDeclaration) error {
	source, err := style.CSSString()
	if err != nil {
		return err
	}
	if err := c.styleMgr.retainInlineStyle(n, &inlineStyleState{source: source, style: style}); err != nil {
		return err
	}
	old, existed := n.Attr("style")
	var oldValue *string
	if existed {
		oldValue = &old
	}
	webapi.SetAttribute(n, "style", source)
	return c.observers.recordMutation(c, mutationRecord{
		Type: MutationAttributes, TargetID: c.nodes.Handle(n), AttributeName: "style",
		OldValue: oldValue,
	}, n)
}

func (c *Context) callInlineStyle(n *webapi.Node, method string, args []*v8.Value) (any, error) {
	if n.Raw().Type != html.ElementNode || n.Raw().Namespace != "" {
		return nil, &css.Diagnostic{Code: css.DiagnosticUnsupported, Reason: "inline CSSOM requires an HTML element"}
	}
	required := 1
	if method == "styleLength" || method == "styleText" {
		required = 0
	} else if method == "styleSet" {
		required = 3
	}
	if len(args) < required {
		return nil, &css.Diagnostic{Code: css.DiagnosticSyntax, Reason: "missing inline CSSOM argument"}
	}
	if method == "styleTextSet" {
		style, err := css.ParseStyleDeclaration(args[0].String())
		if err != nil {
			return nil, err
		}
		return nil, c.commitInlineStyle(n, style)
	}
	state, err := c.styleMgr.inlineStyle(n)
	if err != nil {
		return nil, err
	}
	return c.dispatchInlineStyle(n, state.style, method, args)
}

func (c *Context) dispatchInlineStyle(n *webapi.Node, style *css.StyleDeclaration, method string, args []*v8.Value) (any, error) {
	switch method {
	case "styleLength":
		return int32(style.Length()), nil
	case "styleItem":
		return style.Item(int(uint32(args[0].Integer()))), nil
	case "styleText":
		return style.CSSString()
	case "styleGet":
		return style.GetPropertyValue(args[0].String())
	case "stylePriority":
		return style.GetPropertyPriority(args[0].String())
	case "styleSet":
		candidate := style.Clone()
		changed, err := candidate.SetProperty(args[0].String(), args[1].String(), args[2].String())
		if err != nil || !changed {
			return nil, err
		}
		return nil, c.commitInlineStyle(n, candidate)
	case "styleRemove":
		candidate := style.Clone()
		value, changed, err := candidate.RemoveProperty(args[0].String())
		if err != nil || !changed {
			return value, err
		}
		return value, c.commitInlineStyle(n, candidate)
	}
	return nil, &css.Diagnostic{Code: css.DiagnosticUnsupported, Reason: "unsupported inline CSSOM operation"}
}
