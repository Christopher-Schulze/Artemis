package js

import (
	"context"
	"strings"
	"testing"
)

func TestMutationObserverChildListAdd(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var trace = '';
		const obs = new MutationObserver((records) => {
			for (const r of records) trace += r.type + '+' + (r.addedNodes.length) + ';';
		});
		obs.observe(document.getElementById('d'), {childList: true});
		const li = document.createElement('p');
		li.textContent = 'x';
		document.getElementById('d').appendChild(li);
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `trace`)
	if !strings.Contains(v.String(), "childList+1;") {
		t.Errorf("trace = %q, want childList+1;", v.String())
	}
}

func TestMutationObserverAttributesChange(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		new MutationObserver((records) => {
			for (const r of records) captured += r.type + ':' + r.attributeName + ';';
		}).observe(document.getElementById('d'), {attributes: true});
		document.getElementById('d').setAttribute('class', 'active');
		document.getElementById('d').setAttribute('data-x', '1');
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if !strings.Contains(v.String(), "attributes:class;") || !strings.Contains(v.String(), "attributes:data-x;") {
		t.Errorf("captured = %q", v.String())
	}
}

func TestMutationObserverSubtree(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="root"><span id="inner"></span></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var fired = 0;
		new MutationObserver(() => { fired++; }).observe(document.getElementById('root'), {childList: true, subtree: true});
		const t = document.createTextNode('hi');
		document.getElementById('inner').appendChild(t);
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `fired`)
	if v.Int64() < 1 {
		t.Errorf("subtree observer should have fired at least once: fired=%d", v.Int64())
	}
}

func TestMutationObserverDisconnect(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var fired = 0;
		const obs = new MutationObserver(() => { fired++; });
		obs.observe(document.getElementById('d'), {attributes: true});
		document.getElementById('d').setAttribute('a', '1');
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if _, err := c.Eval(context.Background(), `obs.disconnect();`); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if _, err := c.Eval(context.Background(), `document.getElementById('d').setAttribute('b', '2');`); err != nil {
		t.Fatalf("post-disconnect mutate: %v", err)
	}
	v, _ := c.Eval(context.Background(), `fired`)
	if v.Int64() != 1 {
		t.Errorf("fired = %d, want 1 (only first mutation should have fired)", v.Int64())
	}
}

func TestMutationObserverInnerHTMLFires(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"><p>old</p></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var fired = 0;
		new MutationObserver(() => { fired++; }).observe(document.getElementById('d'), {childList: true});
		document.getElementById('d').innerHTML = '<span>new</span>';
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `fired`)
	if v.Int64() < 1 {
		t.Errorf("innerHTML setter did not fire MutationObserver")
	}
}
