package js

import (
	"strings"

	"golang.org/x/net/html"
	v8 "rogchap.com/v8go"

	"github.com/Christopher-Schulze/Artemis/css"
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

// domBridgeTemplates caches the 5 native trampolines installed by
// installDocument (__node_get/set/call + __doc_get/call). Each callback
// resolves *Context via Runtime.contextFor(info.Context()) so the
// templates can be shared across all Contexts in the Isolate.
type domBridgeTemplates struct {
	nodeGet  *v8.FunctionTemplate
	nodeSet  *v8.FunctionTemplate
	nodeCall *v8.FunctionTemplate
	docGet   *v8.FunctionTemplate
	docCall  *v8.FunctionTemplate
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
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 2 {
				return v8.Null(iso)
			}
			n := c.nodes.Get(uint32(args[0].Integer()))
			if n == nil {
				return v8.Null(iso)
			}
			return nodeGetProp(iso, c.v8ctx, c, n, args[1].String())
		}),
		nodeSet: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 3 {
				return v8.Null(iso)
			}
			n := c.nodes.Get(uint32(args[0].Integer()))
			if n == nil {
				return v8.Null(iso)
			}
			nodeSetProp(c, n, args[1].String(), args[2].String())
			return v8.Null(iso)
		}),
		nodeCall: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 2 {
				return v8.Null(iso)
			}
			n := c.nodes.Get(uint32(args[0].Integer()))
			if n == nil {
				return v8.Null(iso)
			}
			return nodeCall(iso, c.v8ctx, c, n, args[1].String(), args[2:])
		}),
		docGet: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return v8.Null(iso)
			}
			return docGetProp(iso, c, args[0].String())
		}),
		docCall: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return v8.Null(iso)
			}
			return docCall(iso, c.v8ctx, c, args[0].String(), args[1:])
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
	if err := g.Set("__node_get", t.nodeGet.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := g.Set("__node_set", t.nodeSet.GetFunction(v8ctx)); err != nil {
		return err
	}
	return g.Set("__node_call", t.nodeCall.GetFunction(v8ctx))
}

// installDocTrampolines installs `__doc_get` and `__doc_call`.
func installDocTrampolines(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureDOMBridgeTemplates()
	g := v8ctx.Global()
	if err := g.Set("__doc_get", t.docGet.GetFunction(v8ctx)); err != nil {
		return err
	}
	return g.Set("__doc_call", t.docCall.GetFunction(v8ctx))
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
		return mustValue(iso, int32(c.nodes.Handle(n.Parent())))
	case "firstChildId":
		return mustValue(iso, int32(c.nodes.Handle(n.FirstChild())))
	case "lastChildId":
		return mustValue(iso, int32(c.nodes.Handle(n.LastChild())))
	case "nextSiblingId":
		return mustValue(iso, int32(c.nodes.Handle(n.NextSibling())))
	case "prevSiblingId":
		return mustValue(iso, int32(c.nodes.Handle(n.PrevSibling())))
	case "childIds":
		kids := n.Children()
		ids := make([]any, len(kids))
		for i, k := range kids {
			ids[i] = int32(c.nodes.Handle(k))
		}
		return idsToArray(v8ctx, iso, ids)
	}
	return v8.Null(iso)
}

func nodeSetProp(c *Context, n *webapi.Node, prop, val string) {
	switch prop {
	case "innerHTML":
		// Capture old children for the mutation record before replacement.
		oldKids := n.Children()
		_ = webapi.SetInnerHTML(n, val)
		newKids := n.Children()
		oldIDs := make([]uint32, 0, len(oldKids))
		for _, k := range oldKids {
			oldIDs = append(oldIDs, c.nodes.Handle(k))
		}
		newIDs := make([]uint32, 0, len(newKids))
		for _, k := range newKids {
			newIDs = append(newIDs, c.nodes.Handle(k))
		}
		c.observers.recordMutation(c, mutationRecord{
			Type:       MutationChildList,
			TargetID:   c.nodes.Handle(n),
			AddedIDs:   newIDs,
			RemovedIDs: oldIDs,
		}, n)
	case "textContent":
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
		c.observers.recordMutation(c, mutationRecord{
			Type:       MutationChildList,
			TargetID:   c.nodes.Handle(n),
			AddedIDs:   newIDs,
			RemovedIDs: oldIDs,
		}, n)
	}
}

func nodeCall(iso *v8.Isolate, v8ctx *v8.Context, c *Context, n *webapi.Node, method string, args []*v8.Value) *v8.Value {
	switch method {
	case "setAttribute":
		if len(args) < 2 {
			return v8.Null(iso)
		}
		name := args[0].String()
		webapi.SetAttribute(n, name, args[1].String())
		c.observers.recordMutation(c, mutationRecord{
			Type:          MutationAttributes,
			TargetID:      c.nodes.Handle(n),
			AttributeName: name,
		}, n)
		return v8.Null(iso)
	case "removeAttribute":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		name := args[0].String()
		webapi.RemoveAttribute(n, name)
		c.observers.recordMutation(c, mutationRecord{
			Type:          MutationAttributes,
			TargetID:      c.nodes.Handle(n),
			AttributeName: name,
		}, n)
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
			return v8.Null(iso)
		}
		child := c.nodes.Get(uint32(args[0].Integer()))
		if child == nil {
			return v8.Null(iso)
		}
		_ = webapi.AppendChild(n, child)
		c.observers.recordMutation(c, mutationRecord{
			Type:     MutationChildList,
			TargetID: c.nodes.Handle(n),
			AddedIDs: []uint32{c.nodes.Handle(child)},
		}, n)
		return mustValue(iso, int32(c.nodes.Handle(child)))
	case "removeChild":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		child := c.nodes.Get(uint32(args[0].Integer()))
		if child == nil {
			return v8.Null(iso)
		}
		_ = webapi.RemoveChild(n, child)
		c.observers.recordMutation(c, mutationRecord{
			Type:       MutationChildList,
			TargetID:   c.nodes.Handle(n),
			RemovedIDs: []uint32{c.nodes.Handle(child)},
		}, n)
		return mustValue(iso, int32(c.nodes.Handle(child)))
	case "insertBefore":
		if len(args) < 2 {
			return v8.Null(iso)
		}
		newChild := c.nodes.Get(uint32(args[0].Integer()))
		var ref *webapi.Node
		if !args[1].IsNullOrUndefined() {
			ref = c.nodes.Get(uint32(args[1].Integer()))
		}
		if newChild == nil {
			return v8.Null(iso)
		}
		_ = webapi.InsertBefore(n, newChild, ref)
		c.observers.recordMutation(c, mutationRecord{
			Type:     MutationChildList,
			TargetID: c.nodes.Handle(n),
			AddedIDs: []uint32{c.nodes.Handle(newChild)},
		}, n)
		return mustValue(iso, int32(c.nodes.Handle(newChild)))
	case "cloneNode":
		deep := false
		if len(args) >= 1 {
			deep = args[0].Boolean()
		}
		clone := webapi.CloneNode(n, deep)
		return mustValue(iso, int32(c.nodes.Handle(clone)))
	case "querySelector":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		m, err := n.QuerySelector(args[0].String())
		if err != nil || m == nil {
			return v8.Null(iso)
		}
		return mustValue(iso, int32(c.nodes.Handle(m)))
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
			ids[i] = int32(c.nodes.Handle(m))
		}
		return idsToArray(v8ctx, iso, ids)
	case "styleGet":
		// args[0] = camelCase property name; returns the inline value
		if len(args) < 1 {
			return mustValue(iso, "")
		}
		prop := css.CamelToKebab(args[0].String())
		styles := css.ParseInline(n.AttrOrEmpty("style"))
		return mustValue(iso, styles[prop])
	case "styleSet":
		if len(args) < 2 {
			return v8.Null(iso)
		}
		prop := css.CamelToKebab(args[0].String())
		val := args[1].String()
		styles := css.ParseInline(n.AttrOrEmpty("style"))
		if val == "" {
			delete(styles, prop)
		} else {
			styles[prop] = val
		}
		webapi.SetAttribute(n, "style", css.Serialize(styles))
		return v8.Null(iso)
	}
	return v8.Null(iso)
}

func docGetProp(iso *v8.Isolate, c *Context, prop string) *v8.Value {
	switch prop {
	case "title":
		return mustValue(iso, c.doc.Title())
	case "URL":
		return mustValue(iso, c.doc.URL())
	case "bodyId":
		return mustValue(iso, int32(c.nodes.Handle(c.doc.Body())))
	case "documentElementId":
		return mustValue(iso, int32(c.nodes.Handle(c.doc.HTMLElement())))
	case "headId":
		return mustValue(iso, int32(c.nodes.Handle(c.doc.Head())))
	case "cookie":
		if c.getCookie != nil {
			return mustValue(iso, c.getCookie())
		}
		return mustValue(iso, "")
	}
	return v8.Null(iso)
}

func installDocSetCookie(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	fn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		if len(args) < 1 {
			return v8.Null(iso)
		}
		if c.setCookie != nil {
			c.setCookie(args[0].String())
		}
		return v8.Null(iso)
	})
	return v8ctx.Global().Set("__doc_set_cookie", fn.GetFunction(v8ctx))
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
		return mustValue(iso, int32(c.nodes.Handle(m)))
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
			ids[i] = int32(c.nodes.Handle(m))
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
		return mustValue(iso, int32(c.nodes.Handle(m)))
	case "getElementsByTagName":
		if len(args) < 1 {
			return idsToArray(v8ctx, iso, nil)
		}
		matches := webapi.GetElementsByTagName(c.doc.Root(), args[0].String())
		ids := make([]any, len(matches))
		for i, m := range matches {
			ids[i] = int32(c.nodes.Handle(m))
		}
		return idsToArray(v8ctx, iso, ids)
	case "getElementsByClassName":
		if len(args) < 1 {
			return idsToArray(v8ctx, iso, nil)
		}
		matches := webapi.GetElementsByClassName(c.doc.Root(), args[0].String())
		ids := make([]any, len(matches))
		for i, m := range matches {
			ids[i] = int32(c.nodes.Handle(m))
		}
		return idsToArray(v8ctx, iso, ids)
	case "createElement":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		el := webapi.CreateElement(args[0].String())
		return mustValue(iso, int32(c.nodes.Handle(el)))
	case "createTextNode":
		if len(args) < 1 {
			return v8.Null(iso)
		}
		t := webapi.CreateTextNode(args[0].String())
		return mustValue(iso, int32(c.nodes.Handle(t)))
	}
	return v8.Null(iso)
}

func mustValue(iso *v8.Isolate, v any) *v8.Value {
	out, err := v8.NewValue(iso, v)
	if err != nil {
		return v8.Null(iso)
	}
	return out
}

// idsToArray converts a slice of int32 handles to a JS array.
func idsToArray(v8ctx *v8.Context, iso *v8.Isolate, ids []any) *v8.Value {
	tmpl := v8.NewObjectTemplate(iso)
	obj, err := tmpl.NewInstance(v8ctx)
	if err != nil {
		return v8.Null(iso)
	}
	for i, id := range ids {
		_ = obj.SetIdx(uint32(i), id)
	}
	_ = obj.Set("length", int32(len(ids)))
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
      if (t === 3 || t === 4 || t === 8) __node_set(this.__id, 'textContent', String(v));
    },
    get nodeValue() {
      const t = this.nodeType;
      return (t === 3 || t === 4 || t === 8) ? __node_get(this.__id, 'textContent') : null;
    },
    set nodeValue(v) {
      const t = this.nodeType;
      if (t === 3 || t === 4 || t === 8) __node_set(this.__id, 'textContent', String(v));
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
    appendChild(child)        { __node_call(this.__id, 'appendChild', child.__id); return child; },
    removeChild(child)        { __node_call(this.__id, 'removeChild', child.__id); return child; },
    insertBefore(child, ref)  {
      __node_call(this.__id, 'insertBefore', child.__id, ref ? ref.__id : null);
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
      if (typeof fn !== 'function') return;
      let useCapture = false;
      if (typeof options === 'boolean') useCapture = options;
      else if (options && typeof options === 'object') useCapture = !!options.capture;
      __addListener(this.__id, String(type), fn, useCapture);
    },
    removeEventListener(type, fn, options) {
      if (typeof fn !== 'function') return;
      let useCapture = false;
      if (typeof options === 'boolean') useCapture = options;
      else if (options && typeof options === 'object') useCapture = !!options.capture;
      __removeListener(this.__id, String(type), fn, useCapture);
    },
    dispatchEvent(event) {
      if (!event || typeof event !== 'object') return false;
      event.target = this;
      const path = [];
      for (let n = this; n; n = n.parentNode) path.push(n);
      // Capture phase: root -> target.parentNode (target excluded)
      for (let i = path.length - 1; i > 0; i--) {
        const n = path[i];
        event.currentTarget = n;
        const set = __getListeners(n.__id, event.type, true);
        if (set) for (const fn of set) { try { fn.call(n, event); } catch (e) {} }
        if (event.__stopProp) return !event.defaultPrevented;
      }
      // AT_TARGET: fire both capture and bubble listeners on the target.
      event.currentTarget = this;
      const tCap = __getListeners(this.__id, event.type, true);
      if (tCap) for (const fn of tCap) { try { fn.call(this, event); } catch (e) {} }
      const tBub = __getListeners(this.__id, event.type, false);
      if (tBub) for (const fn of tBub) { try { fn.call(this, event); } catch (e) {} }
      if (event.__stopProp || !event.bubbles) return !event.defaultPrevented;
      // Bubble phase: target.parentNode -> root
      for (let i = 1; i < path.length; i++) {
        const n = path[i];
        event.currentTarget = n;
        const set = __getListeners(n.__id, event.type, false);
        if (set) for (const fn of set) { try { fn.call(n, event); } catch (e) {} }
        if (event.__stopProp) break;
      }
      return !event.defaultPrevented;
    },
    click() {
      const ev = new globalThis.Event('click', {bubbles: true});
      this.dispatchEvent(ev);
    },
    get style() {
      const id = this.__id;
      return new Proxy({}, {
        get(_t, prop) {
          if (typeof prop !== 'string') return undefined;
          return __node_call(id, 'styleGet', prop);
        },
        set(_t, prop, value) {
          if (typeof prop !== 'string') return true;
          __node_call(id, 'styleSet', prop, String(value));
          return true;
        },
      });
    },
  };
  globalThis.__ELEM_PROTO = ELEM_PROTO;
  globalThis.getComputedStyle = function(el) { return el ? el.style : {}; };

  // Listener registry. Keyed by handle id, then event type, then phase.
  // Each phase has its own Set so addEventListener with capture:true
  // creates a separate registration that removeEventListener with the
  // same capture flag matches.
  const __LISTENERS = new Map(); // id -> Map<type, {bubble: Set<fn>, capture: Set<fn>}>
  function _getOrCreate(id, type) {
    let m = __LISTENERS.get(id);
    if (!m) { m = new Map(); __LISTENERS.set(id, m); }
    let buckets = m.get(type);
    if (!buckets) { buckets = {bubble: new Set(), capture: new Set()}; m.set(type, buckets); }
    return buckets;
  }
  globalThis.__addListener = (id, type, fn, useCapture) => {
    const b = _getOrCreate(id, type);
    (useCapture ? b.capture : b.bubble).add(fn);
  };
  globalThis.__removeListener = (id, type, fn, useCapture) => {
    const m = __LISTENERS.get(id);
    if (!m) return;
    const b = m.get(type);
    if (!b) return;
    (useCapture ? b.capture : b.bubble).delete(fn);
  };
  globalThis.__getListeners = (id, type, useCapture) => {
    const m = __LISTENERS.get(id);
    if (!m) return null;
    const b = m.get(type);
    if (!b) return null;
    return useCapture ? b.capture : b.bubble;
  };
  // Per-Context wrapper cache so that two calls to __wrap(sameId) return
  // the same Object reference. JS-side identity (===) for Element wrappers
  // matters to libraries that put nodes into Sets/WeakSets, compare via
  // === in event listeners, etc.
  const _wrapCache = new Map();
  globalThis.__wrap = (id) => {
    if (!id) return null;
    let w = _wrapCache.get(id);
    if (w) return w;
    w = Object.create(ELEM_PROTO);
    w.__id = id;
    _wrapCache.set(id, w);
    return w;
  };

  const DOC = {
    get title() { return __doc_get('title'); },
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

  class Event {
    constructor(type, init) {
      init = init || {};
      this.type = String(type);
      this.bubbles = !!init.bubbles;
      this.cancelable = !!init.cancelable;
      this.defaultPrevented = false;
      this.target = null;
      this.currentTarget = null;
      this.__stopProp = false;
    }
    preventDefault() { if (this.cancelable) this.defaultPrevented = true; }
    stopPropagation() { this.__stopProp = true; }
  }
  globalThis.Event = Event;

  // Document is an EventTarget too: delegate to the documentElement.
  DOC.addEventListener = function(type, fn) {
    if (typeof fn !== 'function') return;
    __addListener(__doc_get('documentElementId'), String(type), fn);
  };
  DOC.removeEventListener = function(type, fn) {
    if (typeof fn !== 'function') return;
    __removeListener(__doc_get('documentElementId'), String(type), fn);
  };
  DOC.dispatchEvent = function(event) {
    const root = DOC.documentElement;
    return root ? root.dispatchEvent(event) : !event.defaultPrevented;
  };
})();
`
