package js

import (
	"context"
	"strings"
	"testing"
)

func TestCanvasGetContext(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><canvas id="c" width="200" height="100"></canvas></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const c = document.getElementById('c');
			const ctx = c.getContext('2d');
			ctx.fillStyle = 'red';
			ctx.fillRect(0,0,10,10);
			return ctx.fillStyle + ':' + c.width + 'x' + c.height + ':' + (ctx instanceof CanvasRenderingContext2D);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "red:200x100:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestElementAnimateReturnsAnimation(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const a = document.getElementById('d').animate([{opacity:0},{opacity:1}], 100);
			return a instanceof Animation;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !v.Bool() {
		t.Error("animate did not return Animation")
	}
}

func TestBroadcastChannelDelivers(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		const a = new BroadcastChannel('chan');
		const b = new BroadcastChannel('chan');
		b.onmessage = (ev) => { captured = ev.data; };
		a.postMessage('hello');
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "hello" {
		t.Errorf("got %q", v.String())
	}
}

func TestReadableStreamReader(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = [];
		const stream = new ReadableStream({
			start(c) { c.enqueue('a'); c.enqueue('b'); c.close(); }
		});
		const r = stream.getReader();
		(async () => {
			while (true) {
				const {value, done} = await r.read();
				if (done) break;
				captured.push(value);
			}
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured.join(',')`)
	if v.String() != "a,b" {
		t.Errorf("got %q", v.String())
	}
}

func TestStreamPipeTo(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = [];
		const r = new ReadableStream({
			start(ctrl) { ctrl.enqueue(1); ctrl.enqueue(2); ctrl.enqueue(3); ctrl.close(); }
		});
		const w = new WritableStream({
			write(chunk) { captured.push(chunk); }
		});
		r.pipeTo(w);
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	c.Eval(context.Background(), `0`) // drain microtasks
	v, _ := c.Eval(context.Background(), `captured.join(',')`)
	if v.String() != "1,2,3" {
		t.Errorf("got %q", v.String())
	}
}

func TestStreamPipeThroughTransform(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = [];
		const src = new ReadableStream({
			start(ctrl) { ctrl.enqueue(2); ctrl.enqueue(3); ctrl.close(); }
		});
		const upper = new TransformStream({
			transform(chunk, ctrl) { ctrl.enqueue(chunk * 10); }
		});
		const sink = new WritableStream({
			write(chunk) { captured.push(chunk); }
		});
		src.pipeThrough(upper).pipeTo(sink);
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	c.Eval(context.Background(), `0`)
	v, _ := c.Eval(context.Background(), `captured.join(',')`)
	if v.String() != "20,30" {
		t.Errorf("got %q", v.String())
	}
}

func TestStreamTee(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var a = [], b = [];
		const src = new ReadableStream({
			start(ctrl) { ctrl.enqueue('x'); ctrl.enqueue('y'); ctrl.close(); }
		});
		const [s1, s2] = src.tee();
		(async () => {
			const r1 = s1.getReader();
			while (true) { const {value, done} = await r1.read(); if (done) break; a.push(value); }
		})();
		(async () => {
			const r2 = s2.getReader();
			while (true) { const {value, done} = await r2.read(); if (done) break; b.push(value); }
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	c.Eval(context.Background(), `0`)
	v, _ := c.Eval(context.Background(), `a.join(',') + '|' + b.join(',')`)
	if v.String() != "x,y|x,y" {
		t.Errorf("got %q", v.String())
	}
}

func TestAttachShadow(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		const sh = document.getElementById('d').attachShadow({mode: 'open'});
		sh.innerHTML = '<span>shadowed</span>';
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `document.getElementById('d').innerHTML`)
	if !strings.Contains(v.String(), "<span>shadowed</span>") {
		t.Errorf("got %q", v.String())
	}
}

func TestFormValidity(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><input id="a" required value=""><input id="b" required value="x"></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const a = document.getElementById('a').validity;
			const b = document.getElementById('b').validity;
			return a.valueMissing + ':' + a.valid + ':' + b.valid;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:false:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestRangeCreate(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d">x</div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const d = document.getElementById('d');
			const r = document.createRange();
			r.selectNodeContents(d);
			// Element with one child -> start=0, end=1 -> NOT collapsed.
			const empty = document.createElement('span');
			const r2 = document.createRange();
			r2.selectNodeContents(empty);
			return r.collapsed + ':' + r.startOffset + ':' + r.endOffset + ':' + r2.collapsed;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "false:0:1:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestRangeTextSlicing(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><p id="p">Hello world</p></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const p = document.getElementById('p');
			const t = p.firstChild;
			const r = document.createRange();
			r.setStart(t, 0); r.setEnd(t, 5);
			return r.toString() + ':' + r.collapsed + ':' + r.startOffset + ':' + r.endOffset;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "Hello:false:0:5" {
		t.Errorf("got %q", v.String())
	}
}

func TestRangeSetStartBeforeAfter(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"><a></a><b></b><i></i></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const d = document.getElementById('d');
			const b = d.children[1];
			const r1 = document.createRange();
			r1.setStartBefore(b);
			const r2 = document.createRange();
			r2.setStartAfter(b);
			return r1.startOffset + ':' + r2.startOffset;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	// b is the second child (index 1) — before=1, after=2.
	if v.String() != "1:2" {
		t.Errorf("got %q", v.String())
	}
}

func TestSelectionExtendAndContains(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><p id="p">Hello world</p></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const p = document.getElementById('p');
			const t = p.firstChild;
			const sel = getSelection();
			sel.removeAllRanges();
			sel.collapse(t, 0);
			sel.extend(t, 5);
			return sel.toString() + ':' + sel.isCollapsed + ':' + sel.containsNode(p, true);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "Hello:false:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestHTMLElementInstanceof(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><input id="i"><a id="a">x</a><img id="m"></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const i = document.getElementById('i');
			const a = document.getElementById('a');
			const m = document.getElementById('m');
			return (i instanceof HTMLInputElement) + ':' +
			       (a instanceof HTMLAnchorElement) + ':' +
			       (m instanceof HTMLImageElement) + ':' +
			       (i instanceof HTMLElement) + ':' +
			       (i instanceof HTMLAnchorElement);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:true:true:true:false" {
		t.Errorf("got %q", v.String())
	}
}

func TestHTMLHeadingMultiTagInstanceof(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><h1 id="a">x</h1><h2 id="b">y</h2><h6 id="c">z</h6><div id="d">d</div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const a = document.getElementById('a');
			const b = document.getElementById('b');
			const c = document.getElementById('c');
			const d = document.getElementById('d');
			return (a instanceof HTMLHeadingElement) + ':' +
			       (b instanceof HTMLHeadingElement) + ':' +
			       (c instanceof HTMLHeadingElement) + ':' +
			       (d instanceof HTMLHeadingElement);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:true:true:false" {
		t.Errorf("got %q", v.String())
	}
}

func TestHTMLQuoteAndModInstanceof(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><q id="q">q</q><blockquote id="bq">bq</blockquote><ins id="i">i</ins><del id="d">d</del></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			return (document.getElementById('q')  instanceof HTMLQuoteElement) + ':' +
			       (document.getElementById('bq') instanceof HTMLQuoteElement) + ':' +
			       (document.getElementById('i')  instanceof HTMLModElement) + ':' +
			       (document.getElementById('d')  instanceof HTMLModElement);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:true:true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestHTMLTableSectionsInstanceof(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><table><thead id="h"><tr></tr></thead><tbody id="b"><tr></tr></tbody><tfoot id="f"><tr></tr></tfoot><caption id="c">cap</caption></table></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			return (document.getElementById('h') instanceof HTMLTableSectionElement) + ':' +
			       (document.getElementById('b') instanceof HTMLTableSectionElement) + ':' +
			       (document.getElementById('f') instanceof HTMLTableSectionElement) + ':' +
			       (document.getElementById('c') instanceof HTMLTableCaptionElement);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:true:true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestHTMLUnknownElement(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><my-thing id="x">x</my-thing><div id="d">d</div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const x = document.getElementById('x');
			const d = document.getElementById('d');
			return (x instanceof HTMLUnknownElement) + ':' +
			       (d instanceof HTMLUnknownElement) + ':' +
			       (x instanceof HTMLElement);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:false:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestCollectionInstanceof(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div class="x"></div><div class="x"></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const list = document.querySelectorAll('.x');
			const cls  = document.body.classList || {contains: () => false, length: 0};
			return (list instanceof NodeList) + ':' +
			       (list instanceof HTMLCollection) + ':' +
			       (cls instanceof DOMTokenList);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestVideoStubAPI(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><video id="v"></video></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const v = document.getElementById('v');
			return v.paused + ':' + v.readyState + ':' + (typeof v.play);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:0:function" {
		t.Errorf("got %q", v.String())
	}
}
