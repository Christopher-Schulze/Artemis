package js

import (
	"errors"
	"math"
	"strings"

	v8 "github.com/tommie/v8go"
	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// installDocument installs the native trampolines on the global object
// and runs the bootstrap script that defines the JS-side `document`,
// `Element` prototype, and `__wrap` helper. Every JS reference to a Go
// node is an integer handle resolved through the Context's nodeTable.
func installDocument(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	if err := installNodeTrampolines(iso, v8ctx, c); err != nil {
		return err
	}
	if err := installDocTrampolines(iso, v8ctx, c); err != nil {
		return err
	}
	if err := installDocSetCookie(iso, v8ctx, c); err != nil {
		return err
	}
	c.registerBootstrap("artemis-dom-bootstrap", domBootstrap)
	return nil
}

// domBridgeTemplates caches the 6 native trampolines installed by
// installDocument (__node_get/set/call + __doc_get/call/set_cookie). Each callback
// resolves *Context via Runtime.contextFor(info.Context()) so the
// templates can be shared across all Contexts in the Isolate.
type domBridgeTemplates struct {
	nodeGet   *v8.FunctionTemplate
	nodeSet   *v8.FunctionTemplate
	nodeCall  *v8.FunctionTemplate
	docGet    *v8.FunctionTemplate
	docCall   *v8.FunctionTemplate
	setCookie *v8.FunctionTemplate
}

func (r *Runtime) ensureDOMBridgeTemplates() *domBridgeTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.domBridgeTemplates != nil {
		return r.domBridgeTemplates
	}
	iso := r.iso
	r.domBridgeTemplates = &domBridgeTemplates{
		nodeGet: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 2 {
				return v8.Null(iso)
			}
			nodeID, ok := checkedInt64ToUint32(args[0].Integer())
			if !ok {
				return v8.Null(iso)
			}
			n := c.nodes.Get(nodeID)
			if n == nil {
				return v8.Null(iso)
			}
			return domCallbackResult(iso, nodeGetProp(iso, c.v8ctx, c, n, args[1].String()))
		}),
		nodeSet: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 3 {
				return v8.Null(iso)
			}
			nodeID, ok := checkedInt64ToUint32(args[0].Integer())
			if !ok {
				return v8.Null(iso)
			}
			n := c.nodes.Get(nodeID)
			if n == nil {
				return v8.Null(iso)
			}
			if err := nodeSetProp(c, n, args[1].String(), args[2].String()); err != nil {
				return domCallbackResult(iso, domCallbackError(iso, err))
			}
			return v8.Null(iso)
		}),
		nodeCall: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 2 {
				return v8.Null(iso)
			}
			nodeID, err := mutationInteger(args[0], math.MaxUint32)
			if err != nil {
				return domCallbackResult(iso, domCallbackError(iso, err))
			}
			n := c.nodes.Get(uint32(nodeID))
			if n == nil {
				return domCallbackResult(iso, domCallbackError(iso, errors.New("node call: valid target required")))
			}
			return domCallbackResult(iso, nodeCall(iso, c.v8ctx, c, n, args[1].String(), args[2:]))
		}),
		docGet: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return v8.Null(iso)
			}
			return domCallbackResult(iso, docGetProp(iso, c, args[0].String()))
		}),
		docCall: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return v8.Null(iso)
			}
			return domCallbackResult(iso, docCall(iso, c.v8ctx, c, args[0].String(), args[1:]))
		}),
		setCookie: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) == 0 || c.setCookie == nil {
				return v8.Null(iso)
			}
			value, err := args[0].StringChecked()
			if err != nil {
				return domCallbackResult(iso, domCallbackError(iso, err))
			}
			c.setCookie(value)
			return v8.Null(iso)
		}),
	}
	return r.domBridgeTemplates
}

// installNodeTrampolines installs `__node_get`, `__node_set`,
// `__node_call` on the global. They take an integer handle as first
// arg, dispatch by name string.
func installNodeTrampolines(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureDOMBridgeTemplates()
	g := v8ctx.Global()
	defer g.Release()
	get, set, call := t.nodeGet.GetFunction(v8ctx), t.nodeSet.GetFunction(v8ctx), t.nodeCall.GetFunction(v8ctx)
	defer get.Release()
	defer set.Release()
	defer call.Release()
	if err := g.Set("__node_get", get); err != nil {
		return err
	}
	if err := g.Set("__node_set", set); err != nil {
		return err
	}
	return g.Set("__node_call", call)
}

// installDocTrampolines installs `__doc_get` and `__doc_call`.
func installDocTrampolines(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureDOMBridgeTemplates()
	g := v8ctx.Global()
	defer g.Release()
	get, call := t.docGet.GetFunction(v8ctx), t.docCall.GetFunction(v8ctx)
	defer get.Release()
	defer call.Release()
	if err := g.Set("__doc_get", get); err != nil {
		return err
	}
	return g.Set("__doc_call", call)
}

func nodeGetProp(iso *v8.Isolate, v8ctx *v8.Context, c *Context, n *webapi.Node, prop string) *v8.Value {
	switch prop {
	case "tagName":
		return mustValue(iso, strings.ToUpper(n.Tag()))
	case "nodeType":
		// Standard DOM nodeType values (https://dom.spec.whatwg.org/#node).
		switch n.Type() {
		case webapi.NodeElement:
			return mustValue(iso, int32(1))
		case webapi.NodeText:
			return mustValue(iso, int32(3))
		case webapi.NodeComment:
			return mustValue(iso, int32(8))
		case webapi.NodeDocument:
			return mustValue(iso, int32(9))
		case webapi.NodeDoctype:
			return mustValue(iso, int32(10))
		default:
			return mustValue(iso, int32(0))
		}
	case "nodeName":
		switch n.Type() {
		case webapi.NodeElement:
			return mustValue(iso, strings.ToUpper(n.Tag()))
		case webapi.NodeText:
			return mustValue(iso, "#text")
		case webapi.NodeComment:
			return mustValue(iso, "#comment")
		case webapi.NodeDocument:
			return mustValue(iso, "#document")
		default:
			return mustValue(iso, "")
		}
	case "textContent":
		return mustValue(iso, n.Text())
	case "innerHTML":
		return mustValue(iso, innerHTMLOf(n))
	case "outerHTML":
		return mustValue(iso, outerHTMLOf(n))
	case "parentId":
		return nodeHandleValue(iso, c.nodes.Handle(n.Parent()))
	case "firstChildId":
		return nodeHandleValue(iso, c.nodes.Handle(n.FirstChild()))
	case "lastChildId":
		return nodeHandleValue(iso, c.nodes.Handle(n.LastChild()))
	case "nextSiblingId":
		return nodeHandleValue(iso, c.nodes.Handle(n.NextSibling()))
	case "prevSiblingId":
		return nodeHandleValue(iso, c.nodes.Handle(n.PrevSibling()))
	case "childIds":
		kids := n.Children()
		ids := make([]any, len(kids))
		for i, k := range kids {
			ids[i] = c.nodes.Handle(k)
		}
		return idsToArray(v8ctx, iso, ids)
	}
	return v8.Null(iso)
}

func nodeSetProp(c *Context, n *webapi.Node, prop, val string) error {
	switch prop {
	case "innerHTML":
		// Capture old children for the mutation record before replacement.
		oldKids := n.Children()
		if err := webapi.SetInnerHTML(n, val); err != nil {
			return err
		}
		newKids := n.Children()
		oldIDs := make([]uint32, 0, len(oldKids))
		for _, k := range oldKids {
			oldIDs = append(oldIDs, c.nodes.Handle(k))
		}
		newIDs := make([]uint32, 0, len(newKids))
		for _, k := range newKids {
			newIDs = append(newIDs, c.nodes.Handle(k))
		}
		return c.observers.recordMutation(c, mutationRecord{
			Type:       MutationChildList,
			TargetID:   c.nodes.Handle(n),
			AddedIDs:   newIDs,
			RemovedIDs: oldIDs,
		}, n)
	case "textContent":
		if n.Type() == webapi.NodeText || n.Type() == webapi.NodeComment || n.Type() == webapi.NodeRaw {
			oldValue := n.Data()
			webapi.SetTextContent(n, val)
			return c.observers.recordMutation(c, mutationRecord{Type: MutationCharacterData, TargetID: c.nodes.Handle(n), OldValue: &oldValue}, n)
		}
		oldKids := n.Children()
		oldIDs := make([]uint32, 0, len(oldKids))
		for _, k := range oldKids {
			oldIDs = append(oldIDs, c.nodes.Handle(k))
		}
		webapi.SetTextContent(n, val)
		newKids := n.Children()
		newIDs := make([]uint32, 0, len(newKids))
		for _, k := range newKids {
			newIDs = append(newIDs, c.nodes.Handle(k))
		}
		return c.observers.recordMutation(c, mutationRecord{
			Type:       MutationChildList,
			TargetID:   c.nodes.Handle(n),
			AddedIDs:   newIDs,
			RemovedIDs: oldIDs,
		}, n)
	}
	return nil
}

func nodeCall(iso *v8.Isolate, v8ctx *v8.Context, c *Context, n *webapi.Node, method string, args []*v8.Value) *v8.Value {
	switch method {
	case "setAttribute":
		if len(args) < 2 {
			return v8.Null(iso)
		}
		name := args[0].String()
		old, existed := n.Attr(name)
		var oldValue *string
		if existed {
			oldValue = &old
		}
		webapi.SetAttribute(n, name, args[1].String())
		if strings.EqualFold(name, "style") {
			c.styleMgr.invalidateInlineStyle(n)
		}
		if err := c.observers.recordMutation(c, mutationRecord{
			Type:          MutationAttributes,
			TargetID:      c.nodes.Handle(n),
			AttributeName: name,
			OldValue:      oldValue,
		}, n); err != nil {
			return domCallbackError(iso, err)
		}
		return v8.Null(iso)
	case "removeAttribute":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		name := args[0].String()
		old, existed := n.Attr(name)
		if !existed {
			return v8.Null(iso)
		}
		webapi.RemoveAttribute(n, name)
		if strings.EqualFold(name, "style") {
			c.styleMgr.invalidateInlineStyle(n)
		}
		if err := c.observers.recordMutation(c, mutationRecord{
			Type:          MutationAttributes,
			TargetID:      c.nodes.Handle(n),
			AttributeName: name,
			OldValue:      &old,
		}, n); err != nil {
			return domCallbackError(iso, err)
		}
		return v8.Null(iso)
	case "hasAttribute":
		if len(args) < 1 {
			return mustValue(iso, false)
		}
		return mustValue(iso, webapi.HasAttribute(n, args[0].String()))
	case "getAttribute":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		v, ok := n.Attr(args[0].String())
		if !ok {
			return v8.Null(iso)
		}
		return mustValue(iso, v)
	case "appendChild":
		if len(args) < 1 {
			return domCallbackError(iso, errors.New("appendChild: child required"))
		}
		childID, err := mutationInteger(args[0], math.MaxUint32)
		if err != nil {
			return domCallbackError(iso, err)
		}
		child := c.nodes.Get(uint32(childID))
		if child == nil {
			return domCallbackError(iso, errors.New("appendChild: valid child required"))
		}
		if err := c.insertObservedChild(n, child, nil); err != nil {
			return domCallbackError(iso, err)
		}
		return nodeHandleValue(iso, c.nodes.Handle(child))
	case "removeChild":
		if len(args) < 1 {
			return domCallbackError(iso, errors.New("removeChild: child required"))
		}
		childID, err := mutationInteger(args[0], math.MaxUint32)
		if err != nil {
			return domCallbackError(iso, err)
		}
		child := c.nodes.Get(uint32(childID))
		if child == nil {
			return domCallbackError(iso, errors.New("removeChild: valid child required"))
		}
		if err := c.removeObservedChild(n, child); err != nil {
			return domCallbackError(iso, err)
		}
		return nodeHandleValue(iso, c.nodes.Handle(child))
	case "insertBefore":
		if len(args) < 2 {
			return domCallbackError(iso, errors.New("insertBefore: child and reference required"))
		}
		newChildID, err := mutationInteger(args[0], math.MaxUint32)
		if err != nil {
			return domCallbackError(iso, err)
		}
		newChild := c.nodes.Get(uint32(newChildID))
		var ref *webapi.Node
		if !args[1].IsNullOrUndefined() {
			refID, err := mutationInteger(args[1], math.MaxUint32)
			if err != nil {
				return domCallbackError(iso, err)
			}
			ref = c.nodes.Get(uint32(refID))
			if ref == nil {
				return domCallbackError(iso, errors.New("insertBefore: valid reference required"))
			}
		}
		if newChild == nil {
			return domCallbackError(iso, errors.New("insertBefore: valid child required"))
		}
		if err := c.insertObservedChild(n, newChild, ref); err != nil {
			return domCallbackError(iso, err)
		}
		return nodeHandleValue(iso, c.nodes.Handle(newChild))
	case "cloneNode":
		deep := false
		if len(args) >= 1 {
			deep = args[0].Boolean()
		}
		clone := webapi.CloneNode(n, deep)
		return nodeHandleValue(iso, c.nodes.Handle(clone))
	case "querySelector":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		m, err := n.QuerySelector(args[0].String())
		if err != nil || m == nil {
			return v8.Null(iso)
		}
		return nodeHandleValue(iso, c.nodes.Handle(m))
	case "querySelectorAll":
		if len(args) < 1 {
			return idsToArray(v8ctx, iso, nil)
		}
		matches, err := n.QuerySelectorAll(args[0].String())
		if err != nil {
			return idsToArray(v8ctx, iso, nil)
		}
		ids := make([]any, len(matches))
		for i, m := range matches {
			ids[i] = c.nodes.Handle(m)
		}
		return idsToArray(v8ctx, iso, ids)
	case "styleGet", "stylePriority", "styleSet", "styleRemove", "styleLength", "styleItem", "styleText", "styleTextSet":
		value, err := c.callInlineStyle(n, method, args)
		if err != nil {
			c.styleMgr.recordFailure(err)
			return domCallbackError(iso, err)
		}
		if value == nil {
			return v8.Undefined(iso)
		}
		return mustValue(iso, value)
	}
	return v8.Null(iso)
}

func docGetProp(iso *v8.Isolate, c *Context, prop string) *v8.Value {
	switch prop {
	case "title":
		return mustValue(iso, c.doc.Title())
	case "readyState":
		return mustValue(iso, c.documentReadyState())
	case "htmlTitleId":
		return nodeHandleValue(iso, c.nodes.Handle(c.doc.HTMLTitleElement()))
	case "URL":
		return mustValue(iso, c.doc.URL())
	case "bodyId":
		return nodeHandleValue(iso, c.nodes.Handle(c.doc.Body()))
	case "rootId":
		return nodeHandleValue(iso, c.nodes.Handle(c.doc.Root()))
	case "documentElementId":
		return nodeHandleValue(iso, c.nodes.Handle(c.doc.HTMLElement()))
	case "headId":
		return nodeHandleValue(iso, c.nodes.Handle(c.doc.Head()))
	case "cookie":
		if c.getCookie != nil {
			return mustValue(iso, c.getCookie())
		}
		return mustValue(iso, "")
	}
	return v8.Null(iso)
}

func installDocSetCookie(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	fn := c.rt.ensureDOMBridgeTemplates().setCookie
	g, callback := v8ctx.Global(), fn.GetFunction(v8ctx)
	defer g.Release()
	defer callback.Release()
	return g.Set("__doc_set_cookie", callback)
}

func docCall(iso *v8.Isolate, v8ctx *v8.Context, c *Context, method string, args []*v8.Value) *v8.Value {
	switch method {
	case "querySelector":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		m, err := c.doc.QuerySelector(args[0].String())
		if err != nil || m == nil {
			return v8.Null(iso)
		}
		return nodeHandleValue(iso, c.nodes.Handle(m))
	case "querySelectorAll":
		if len(args) < 1 {
			return idsToArray(v8ctx, iso, nil)
		}
		matches, err := c.doc.QuerySelectorAll(args[0].String())
		if err != nil {
			return idsToArray(v8ctx, iso, nil)
		}
		ids := make([]any, len(matches))
		for i, m := range matches {
			ids[i] = c.nodes.Handle(m)
		}
		return idsToArray(v8ctx, iso, ids)
	case "getElementById":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		m := webapi.GetElementById(c.doc.Root(), args[0].String())
		if m == nil {
			return v8.Null(iso)
		}
		return nodeHandleValue(iso, c.nodes.Handle(m))
	case "getElementsByTagName":
		if len(args) < 1 {
			return idsToArray(v8ctx, iso, nil)
		}
		matches := webapi.GetElementsByTagName(c.doc.Root(), args[0].String())
		ids := make([]any, len(matches))
		for i, m := range matches {
			ids[i] = c.nodes.Handle(m)
		}
		return idsToArray(v8ctx, iso, ids)
	case "getElementsByClassName":
		if len(args) < 1 {
			return idsToArray(v8ctx, iso, nil)
		}
		matches := webapi.GetElementsByClassName(c.doc.Root(), args[0].String())
		ids := make([]any, len(matches))
		for i, m := range matches {
			ids[i] = c.nodes.Handle(m)
		}
		return idsToArray(v8ctx, iso, ids)
	case "createElement":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		el := webapi.CreateElement(args[0].String())
		return nodeHandleValue(iso, c.nodes.Handle(el))
	case "createTextNode":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		t := webapi.CreateTextNode(args[0].String())
		return nodeHandleValue(iso, c.nodes.Handle(t))
	}
	return v8.Null(iso)
}

// DOM helpers return owned handles or isolate-owned null/undefined constants.
// Transfer owned handles after the native callback has copied the result.
func domCallbackResult(iso *v8.Isolate, value *v8.Value) *v8.Value {
	if value == v8.Null(iso) || value == v8.Undefined(iso) {
		return value
	}
	return value.ReturnFromCallback()
}

func domCallbackError(iso *v8.Isolate, cause error) *v8.Value {
	message, err := v8.NewValue(iso, cause.Error())
	if err != nil {
		return iso.ThrowException(v8.Null(iso))
	}
	defer message.Release()
	return iso.ThrowException(message)
}

func mustValue(iso *v8.Isolate, v any) *v8.Value {
	out, err := v8.NewValue(iso, v)
	if err != nil {
		return v8.Null(iso)
	}
	return out
}

func nodeHandleValue(iso *v8.Isolate, id uint32) *v8.Value {
	jsID, ok := checkedUint32ToInt32(id)
	if !ok {
		return v8.Null(iso)
	}
	return mustValue(iso, jsID)
}

// idsToArray converts native values, including checked node handles, to a JS array.
func idsToArray(v8ctx *v8.Context, iso *v8.Isolate, ids []any) *v8.Value {
	obj, err := v8ctx.NewNullPrototypeObject()
	if err != nil {
		return domCallbackError(iso, err)
	}
	for i, id := range ids {
		if nativeID, ok := id.(uint32); ok {
			jsID, valid := checkedUint32ToInt32(nativeID)
			if !valid {
				obj.Release()
				return domCallbackError(iso, errors.New("DOM collection node handle exceeds JavaScript range"))
			}
			id = jsID
		}
		if err := obj.SetIdx(uint32(i), id); err != nil {
			obj.Release()
			return domCallbackError(iso, err)
		}
	}
	length, ok := checkedIntToInt32(len(ids))
	if !ok {
		obj.Release()
		return domCallbackError(iso, errors.New("DOM collection length exceeds JavaScript range"))
	}
	if err := obj.Set("length", length); err != nil {
		obj.Release()
		return domCallbackError(iso, err)
	}
	return obj.Value
}

func innerHTMLOf(n *webapi.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	for c := n.Raw().FirstChild; c != nil; c = c.NextSibling {
		_ = html.Render(&b, c)
	}
	return b.String()
}

func outerHTMLOf(n *webapi.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	_ = html.Render(&b, n.Raw())
	return b.String()
}

// domBootstrap is the JS-side definition of the document object and the
// Element prototype. It runs once per Context, after native trampolines
// are installed on the global.
const domBootstrap = `
(() => {
  const ELEM_PROTO = {
    get tagName()      { return __node_get(this.__id, 'tagName'); },
    get nodeType()     { return __node_get(this.__id, 'nodeType'); },
    get nodeName()     { return __node_get(this.__id, 'nodeName'); },
    get id()           { return this.getAttribute('id') || ''; },
    set id(v)          { this.setAttribute('id', String(v)); },
    get className()    { return this.getAttribute('class') || ''; },
    set className(v)   { this.setAttribute('class', String(v)); },
    get name()         { return this.getAttribute('name') || ''; },
    set name(v)        { this.setAttribute('name', String(v)); },
    get href()         { return this.getAttribute('href') || ''; },
    set href(v)        { this.setAttribute('href', String(v)); },
    get src()          { return this.getAttribute('src') || ''; },
    set src(v)         { this.setAttribute('src', String(v)); },
    get value()        { return this.getAttribute('value') || ''; },
    set value(v)       { this.setAttribute('value', String(v)); },
    get type()         { return this.getAttribute('type') || ''; },
    set type(v)        { this.setAttribute('type', String(v)); },
    get textContent()  { return __node_get(this.__id, 'textContent'); },
    set textContent(v) { __node_set(this.__id, 'textContent', String(v)); },
    // CharacterData accessors for text/comment/cdata nodes. Element nodes
    // intentionally return undefined for .data (matching DOM spec) but
    // accept .nodeValue == null for symmetry with browser behaviour.
    get data() {
      const t = this.nodeType;
      return (t === 3 || t === 4 || t === 8) ? __node_get(this.__id, 'textContent') : undefined;
    },
    set data(v) {
      const t = this.nodeType;
      if (t === 3 || t === 4 || t === 8) __node_set(this.__id, 'textContent', v === null ? '' : String(v));
    },
    get nodeValue() {
      const t = this.nodeType;
      return (t === 3 || t === 4 || t === 8) ? __node_get(this.__id, 'textContent') : null;
    },
    set nodeValue(v) {
      const t = this.nodeType;
      if (t === 3 || t === 4 || t === 8) __node_set(this.__id, 'textContent', v === null ? '' : String(v));
    },
    get length() {
      const t = this.nodeType;
      if (t === 3 || t === 4 || t === 8) {
        const s = __node_get(this.__id, 'textContent');
        return s ? s.length : 0;
      }
      return undefined;
    },
    get innerHTML()    { return __node_get(this.__id, 'innerHTML'); },
    set innerHTML(v)   { __node_set(this.__id, 'innerHTML', String(v)); },
    get outerHTML()    { return __node_get(this.__id, 'outerHTML'); },
    get parentNode()   { return __wrap(__node_get(this.__id, 'parentId')); },
    get parentElement(){ return this.parentNode; },
    get firstChild()   { return __wrap(__node_get(this.__id, 'firstChildId')); },
    get lastChild()    { return __wrap(__node_get(this.__id, 'lastChildId')); },
    get nextSibling()  { return __wrap(__node_get(this.__id, 'nextSiblingId')); },
    get previousSibling(){ return __wrap(__node_get(this.__id, 'prevSiblingId')); },
    get childNodes()   {
      const ids = __node_get(this.__id, 'childIds') || [];
      const out = [];
      for (let i = 0; i < ids.length; i++) out.push(__wrap(ids[i]));
      return out;
    },
    get children()     { return this.childNodes.filter(n => n && n.nodeType === 1); },
    setAttribute(name, value) { __node_call(this.__id, 'setAttribute', String(name), String(value)); },
    getAttribute(name)        { return __node_call(this.__id, 'getAttribute', String(name)); },
    removeAttribute(name)     { __node_call(this.__id, 'removeAttribute', String(name)); },
    hasAttribute(name)        { return __node_call(this.__id, 'hasAttribute', String(name)); },
    appendChild(child) {
      const parentID = __nodeIdentity(this), childID = __nodeIdentity(child);
      if (!parentID || !childID) throw new TypeError('appendChild requires actual Nodes');
      __node_call(parentID, 'appendChild', childID); return child;
    },
    removeChild(child) {
      const parentID = __nodeIdentity(this), childID = __nodeIdentity(child);
      if (!parentID || !childID) throw new TypeError('removeChild requires actual Nodes');
      __node_call(parentID, 'removeChild', childID); return child;
    },
    insertBefore(child, ref)  {
      if (arguments.length < 2) throw new TypeError('insertBefore requires a reference Node or null');
      const parentID = __nodeIdentity(this), childID = __nodeIdentity(child), referenceID = ref == null ? null : __nodeIdentity(ref);
      if (!parentID || !childID || referenceID === 0) throw new TypeError('insertBefore requires actual Nodes');
      __node_call(parentID, 'insertBefore', childID, referenceID);
      return child;
    },
    cloneNode(deep)           { return __wrap(__node_call(this.__id, 'cloneNode', !!deep)); },
    querySelector(sel)        { return __wrap(__node_call(this.__id, 'querySelector', String(sel))); },
    querySelectorAll(sel)     {
      const arr = __node_call(this.__id, 'querySelectorAll', String(sel)) || [];
      const out = [];
      for (let i = 0; i < arr.length; i++) out.push(__wrap(arr[i]));
      return out;
    },
    addEventListener(type, fn, options) {
      addListener(this.__id, String(type), fn, options);
    },
    removeEventListener(type, fn, options) {
      removeListener(this.__id, String(type), fn, options);
    },
    dispatchEvent(event) {
      return dispatch(this, event);
    },
    click() {
      const ev = new globalThis.Event('click', {bubbles: true});
      this.dispatchEvent(ev);
    },
    get style() {
      let style = _styleCache.get(this);
      if (!style) { style = inlineStyle(this.__id); _styleCache.set(this, style); }
      return style;
    },
    set style(value) { this.style.cssText = value; },
  };
  globalThis.__ELEM_PROTO = ELEM_PROTO;
  globalThis.getComputedStyle = function(el) { return el ? el.style : {}; };

  // One registry owns elements, Document and Window. Symbols cannot collide
  // with native node handles. Records preserve registration/removal identity.
  const documentKey = Symbol('document'), windowKey = Symbol('window');
  const owner = globalThis, apply = Reflect.apply, stringify = String;
  const eventErrors = [], propertyHandlers = [];
  let eventErrorOverflow = 0;
  const __LISTENERS = new Map();
  let weakListeners = new WeakMap();
  const listenerStore = id => id && typeof id === 'object' ? weakListeners : __LISTENERS;
  const captureOption = options => typeof options === 'boolean' ? options : !!(options && options.capture);
  const targetKey = target => target === DOC ? documentKey : target === owner ? windowKey : target.__id;
  function _getOrCreate(id, type) {
    const store = listenerStore(id);
    let m = store.get(id);
    if (!m) { m = new Map(); store.set(id, m); }
    let buckets = m.get(type);
    if (!buckets) { buckets = {bubble: new Map(), capture: new Map()}; m.set(type, buckets); }
    return buckets;
  }
  function addListener(id, type, fn, options) {
    if (typeof fn !== 'function' && (!fn || typeof fn !== 'object')) return;
    const b = _getOrCreate(id, type);
    const listeners = captureOption(options) ? b.capture : b.bubble;
    if (!listeners.has(fn)) listeners.set(fn, {fn, once: !!(options && options.once), passive: !!(options && options.passive)});
  }
  function removeListener(id, type, fn, options) {
    const m = listenerStore(id).get(id);
    if (!m) return;
    const b = m.get(type);
    if (!b) return;
    (captureOption(options) ? b.capture : b.bubble).delete(fn);
  }
  function getListeners(id, type, useCapture) {
    const m = listenerStore(id).get(id);
    if (!m) return null;
    const b = m.get(type);
    if (!b) return null;
    return useCapture ? b.capture : b.bubble;
  }
  globalThis.__addListener = addListener;
  globalThis.__removeListener = removeListener;
  globalThis.__getListeners = getListeners;
  function recordEventError(error) {
    if (eventErrors.length >= 16) { eventErrorOverflow++; return; }
    let text;
    try { text = stringify(error && error.stack || error); }
    catch (_) { text = 'event listener threw an unprintable value'; }
    eventErrors.push('callback: ' + text.slice(0, 2048));
  }
  function invoke(target, event, capture, phase) {
    const listeners = getListeners(targetKey(target), event.type, capture);
    if (!listeners) return;
    event.currentTarget = target;
    event.eventPhase = phase;
    for (const record of Array.from(listeners.values())) {
      if (event.__stopImmediate) break;
      if (listeners.get(record.fn) !== record) continue;
      if (record.once) listeners.delete(record.fn);
      event.__passive = record.passive;
      try {
        if (typeof record.fn === 'function') apply(record.fn, target, [event]);
        else if (typeof record.fn.handleEvent === 'function') apply(record.fn.handleEvent, record.fn, [event]);
      } catch (error) { recordEventError(error); }
      finally { event.__passive = false; }
    }
  }
  function dispatch(target, event, override) {
    if (!event || typeof event !== 'object' || !event.type) throw new TypeError('event is required');
    if (event.__dispatching) throw new Error('event is already being dispatched');
    const path = [target];
    for (let node = target; node !== owner;) {
      node = node === DOC ? (event.type === 'load' ? null : owner) : node.parentNode;
      if (!node) break;
      path.push(node);
    }
    event.target = override || target;
    event.__dispatching = true;
    try {
      for (let i = path.length - 1; i > 0 && !event.__stopProp; i--) invoke(path[i], event, true, 1);
      if (!event.__stopProp) {
        invoke(target, event, true, 2);
        invoke(target, event, false, 2);
      }
      if (event.bubbles) for (let i = 1; i < path.length && !event.__stopProp; i++) invoke(path[i], event, false, 3);
    } finally {
      event.currentTarget = null;
      event.eventPhase = 0;
      event.__stopProp = event.__stopImmediate = event.__dispatching = false;
    }
    return !event.defaultPrevented;
  }
  // Per-Context wrapper cache so that two calls to __wrap(sameId) return
  // the same Object reference. JS-side identity (===) for Element wrappers
  // matters to libraries that put nodes into Sets/WeakSets, compare via
  // === in event listeners, etc.
  const _wrapCache = new Map();
  let _styleCache = new WeakMap();
  const cssName = name => name.startsWith('--') ? name : name === 'cssFloat' ? 'float' :
    name.replace(/^(webkit|ms)(?=[A-Z])/, '-$1').replace(/[A-Z]/g, letter => '-' + letter.toLowerCase());
  function inlineStyle(id) {
    const string = value => {
      if (typeof value === 'symbol') throw new TypeError('CSSOM requires a DOMString');
      return String(value);
    };
    const methods = {
      getPropertyValue(property) {
        if (!arguments.length) throw new TypeError('getPropertyValue requires a property');
        return __node_call(id, 'styleGet', string(property));
      },
      getPropertyPriority(property) {
        if (!arguments.length) throw new TypeError('getPropertyPriority requires a property');
        return __node_call(id, 'stylePriority', string(property));
      },
      setProperty(property, value, priority = '') {
        if (arguments.length < 2) throw new TypeError('setProperty requires property and value');
        __node_call(id, 'styleSet', string(property), value === null ? '' : string(value), priority === null ? '' : string(priority));
      },
      removeProperty(property) {
        if (!arguments.length) throw new TypeError('removeProperty requires a property');
        return __node_call(id, 'styleRemove', string(property));
      },
      item(index) {
        if (!arguments.length) throw new TypeError('item requires an index');
        return __node_call(id, 'styleItem', index >>> 0);
      },
      get length() { return __node_call(id, 'styleLength'); },
      get cssText() { return __node_call(id, 'styleText'); },
      set cssText(value) { __node_call(id, 'styleTextSet', value === null ? '' : string(value)); },
      get parentRule() { return null; },
      [Symbol.iterator]: function*() { for (let i = 0; i < this.length; i++) yield this.item(i); },
      [Symbol.toStringTag]: 'CSSStyleDeclaration',
    };
    return new Proxy(methods, {
      get(target, property, receiver) {
        if (Reflect.has(target, property)) return Reflect.get(target, property, receiver);
        if (typeof property !== 'string') return undefined;
        if (/^(0|[1-9][0-9]*)$/.test(property)) return methods.item(Number(property));
        return __node_call(id, 'styleGet', cssName(property));
      },
      set(target, property, value, receiver) {
        if (Reflect.has(target, property)) return Reflect.set(target, property, value, receiver);
        if (typeof property !== 'string') return Reflect.set(target, property, value, receiver);
        __node_call(id, 'styleSet', cssName(property), value === null ? '' : string(value), '');
        return true;
      },
    });
  }
  globalThis.__wrap = (id) => {
    if (!id) return null;
    if (id === __doc_get('rootId')) return DOC;
    let w = _wrapCache.get(id);
    if (w) return w;
    w = Object.create(ELEM_PROTO);
    w.__id = id;
    _wrapCache.set(id, w);
    return w;
  };

  const DOC = {
    get readyState() { return __doc_get('readyState'); },
    get nodeType() { return 9; },
    get parentNode() { return null; },
    get defaultView() { return owner; },
    get title() { return __doc_get('title'); },
    set title(value) {
      if (typeof value === 'symbol') throw new TypeError('document.title requires a DOMString');
      const text = String(value);
      let title = __wrap(__doc_get('htmlTitleId'));
      if (!title) {
        const head = this.head;
        if (!head) return;
        title = this.createElement('title');
        head.appendChild(title);
      }
      title.textContent = text;
    },
    get URL()   { return __doc_get('URL'); },
    get body()  { return __wrap(__doc_get('bodyId')); },
    get head()  { return __wrap(__doc_get('headId')); },
    get documentElement() { return __wrap(__doc_get('documentElementId')); },
    querySelector(sel)    { return __wrap(__doc_call('querySelector', String(sel))); },
    querySelectorAll(sel) {
      const arr = __doc_call('querySelectorAll', String(sel)) || [];
      const out = [];
      for (let i = 0; i < arr.length; i++) out.push(__wrap(arr[i]));
      return out;
    },
    getElementById(id)              { return __wrap(__doc_call('getElementById', String(id))); },
    getElementsByTagName(tag)       {
      const arr = __doc_call('getElementsByTagName', String(tag)) || [];
      const out = [];
      for (let i = 0; i < arr.length; i++) out.push(__wrap(arr[i]));
      return out;
    },
    getElementsByClassName(cls)     {
      const arr = __doc_call('getElementsByClassName', String(cls)) || [];
      const out = [];
      for (let i = 0; i < arr.length; i++) out.push(__wrap(arr[i]));
      return out;
    },
    createElement(tag)              { return __wrap(__doc_call('createElement', String(tag))); },
    createTextNode(text)            { return __wrap(__doc_call('createTextNode', String(text))); },
    get cookie()                    { return __doc_get('cookie'); },
    set cookie(v)                   { __doc_set_cookie(String(v)); },
  };
  globalThis.document = DOC;
  const wrapCacheGet = Map.prototype.get;
  globalThis.__nodeIdentity = value => {
    if (value === DOC) return __doc_get('rootId');
    if (!value || (typeof value !== 'object' && typeof value !== 'function')) return 0;
    const id = value.__id;
    return apply(wrapCacheGet, _wrapCache, [id]) === value ? id : 0;
  };

  class Event {
    constructor(type, init) {
      init = init || {};
      this.type = String(type);
      this.bubbles = !!init.bubbles;
      this.cancelable = !!init.cancelable;
      this.defaultPrevented = false;
      this.target = null;
      this.currentTarget = null;
      this.eventPhase = 0;
      this.__stopProp = false;
      this.__stopImmediate = false;
      this.__dispatching = false;
    }
    preventDefault() { if (this.cancelable && !this.__passive) this.defaultPrevented = true; }
    stopPropagation() { this.__stopProp = true; }
    stopImmediatePropagation() { this.__stopImmediate = this.__stopProp = true; }
  }
  globalThis.Event = Event;

  for (const target of [DOC, owner]) {
    target.addEventListener = function(type, fn, options) { addListener(targetKey(this), String(type), fn, options); };
    target.removeEventListener = function(type, fn, options) { removeListener(targetKey(this), String(type), fn, options); };
    target.dispatchEvent = function(event) { return dispatch(this, event); };
  }
  function installPropertyHandler(target, type) {
    let callback = null;
    const listener = function(event) { if (apply(callback, this, [event]) === false) event.preventDefault(); };
    Object.defineProperty(target, 'on' + type, {
      configurable: false, enumerable: true, get: () => callback,
      set(value) {
        const next = typeof value === 'function' ? value : null;
        if (next && !callback) addListener(targetKey(target), type, listener);
        if (!next && callback) removeListener(targetKey(target), type, listener);
        callback = next;
      }
    });
    propertyHandlers.push(() => { callback = null; });
  }
  installPropertyHandler(DOC, 'readystatechange');
  installPropertyHandler(owner, 'load');
  Object.defineProperty(owner, '__artemisEventTarget', {value: function(operation, target, type, callback, options) {
    if (operation === 0) return addListener(targetKey(target), String(type), callback, options);
    if (operation === 1) return removeListener(targetKey(target), String(type), callback, options);
    if (operation === 2) return dispatch(target, type);
    if (operation === 3) return dispatch(target, new Event(type));
    throw new TypeError('Invalid event target operation');
  }});
  Object.defineProperty(owner, '__artemisDispatchDocumentEvent', {value: function(operation, error) {
    if (operation === 5) return recordEventError(error);
    if (operation === 0) {
      let errors = eventErrors.join('\n'); eventErrors.length = 0;
      if (eventErrorOverflow) errors += (errors ? '\n' : '') + eventErrorOverflow + ' additional callback failures omitted';
      eventErrorOverflow = 0;
      return errors;
    }
    if (operation === 4) {
      __LISTENERS.clear(); _wrapCache.clear(); _styleCache = new WeakMap(); eventErrors.length = 0; eventErrorOverflow = 0;
      weakListeners = new WeakMap();
      for (const reset of propertyHandlers) reset();
      return;
    }
    if (operation === 3) return dispatch(owner, new Event('load'), DOC);
    return dispatch(DOC, new Event(operation === 2 ? 'DOMContentLoaded' : 'readystatechange', {bubbles: operation === 2}));
  }});
})();
`
