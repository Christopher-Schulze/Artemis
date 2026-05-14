package js

import (
	"context"
	"testing"
)

func TestScreenAndVisualViewport(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		screen.width + "x" + screen.height + ":" +
		visualViewport.width + "x" + visualViewport.height + ":" +
		window.innerWidth + ":" + window.devicePixelRatio
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "1920x1080:1920x1080:1920:1" {
		t.Errorf("got %q", v.String())
	}
}

func TestMatchMedia(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const a = matchMedia('(prefers-color-scheme: light)');
			const b = matchMedia('(max-width: 600px)');
			return a.matches + ":" + b.matches + ":" + (typeof a.addEventListener);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:false:function" {
		t.Errorf("got %q", v.String())
	}
}

func TestNavigatorStorage(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const est = await navigator.storage.estimate();
			const persisted = await navigator.storage.persisted();
			captured = est.quota + ":" + est.usage + ":" + persisted;
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "1073741824:0:false" {
		t.Errorf("got %q", v.String())
	}
}

func TestDOMImplementation(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const di = document.implementation;
			const dt = di.createDocumentType('html', '', '');
			const doc = di.createHTMLDocument('hello');
			return di.hasFeature() + ":" + dt.nodeType + ":" + dt.name +
			       ":" + (typeof doc) + ":" + (doc !== null);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:10:html:object:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestMessageChannelRoundTrip(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		const ch = new MessageChannel();
		ch.port2.onmessage = (e) => { captured = 'got:' + e.data; };
		ch.port1.postMessage('hello');
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	// Force microtask drain via a no-op eval.
	c.Eval(context.Background(), `0`)
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "got:hello" {
		t.Errorf("got %q", v.String())
	}
}

func TestWorkerStubNoCrash(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const w = new Worker('non-existent.js');
			w.postMessage({});
			w.terminate();
			const sw = new SharedWorker('also.js');
			return w.scriptURL + ":" + (typeof w.addEventListener) +
			       ":" + (typeof sw);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "non-existent.js:function:object" {
		t.Errorf("got %q", v.String())
	}
}

func TestIntersectionObserverFires(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		const io = new IntersectionObserver((entries) => {
			captured = entries.length + ":" + entries[0].isIntersecting +
			           ":" + entries[0].intersectionRatio;
		});
		io.observe(document.getElementById('d'));
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	c.Eval(context.Background(), `0`) // drain microtask
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "1:true:1" {
		t.Errorf("got %q", v.String())
	}
}

func TestResizeObserverFires(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		const ro = new ResizeObserver((entries) => {
			captured = entries.length + ":" + entries[0].contentRect.width +
			           ":" + entries[0].borderBoxSize[0].inlineSize;
		});
		ro.observe(document.getElementById('d'));
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	c.Eval(context.Background(), `0`)
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "1:0:0" {
		t.Errorf("got %q", v.String())
	}
}

func TestPerformanceMarkAndMeasure(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			performance.mark('start');
			performance.mark('end');
			performance.measure('span', 'start', 'end');
			const marks = performance.getEntriesByType('mark');
			const measures = performance.getEntriesByType('measure');
			return marks.length + ":" + measures.length + ":" + measures[0].name;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "2:1:span" {
		t.Errorf("got %q", v.String())
	}
}

func TestStaticRange(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const r = new StaticRange({startContainer: null, endContainer: null,
			                           startOffset: 0, endOffset: 0});
			return (r instanceof AbstractRange) + ":" + r.collapsed;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestXMLSerializer(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"><span>hi</span></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const s = new XMLSerializer();
			return s.serializeToString(document.getElementById('d'));
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != `<div id="d"><span>hi</span></div>` {
		t.Errorf("got %q", v.String())
	}
}

func TestNavigatorPluginsAndOnLine(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		navigator.plugins.length + ":" + navigator.mimeTypes.length + ":" +
		navigator.onLine + ":" + navigator.cookieEnabled + ":" +
		navigator.webdriver + ":" + (typeof navigator.plugins.item)
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "0:0:true:true:false:function" {
		t.Errorf("got %q", v.String())
	}
}

func TestDocumentFragmentClass(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const f = new DocumentFragment();
			return f.nodeType + ":" + f.nodeName + ":" + (f instanceof DocumentFragment);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "11:#document-fragment:true" {
		t.Errorf("got %q", v.String())
	}
}
