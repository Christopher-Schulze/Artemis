package js

import (
	"context"
	"testing"
)

func TestAddEventListenerAndDispatch(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><button id="b">go</button></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			let fired = 0;
			const btn = document.getElementById('b');
			btn.addEventListener('click', () => { fired++; });
			btn.dispatchEvent(new Event('click'));
			btn.dispatchEvent(new Event('click'));
			return fired;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 2 {
		t.Errorf("fired = %d, want 2", v.Int64())
	}
}

func TestRemoveEventListener(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><button id="b">go</button></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			let fired = 0;
			const fn = () => { fired++; };
			const btn = document.getElementById('b');
			btn.addEventListener('click', fn);
			btn.dispatchEvent(new Event('click'));
			btn.removeEventListener('click', fn);
			btn.dispatchEvent(new Event('click'));
			return fired;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 1 {
		t.Errorf("fired = %d, want 1", v.Int64())
	}
}

func TestEventBubbles(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="outer"><button id="b">go</button></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			let outerFired = 0;
			document.getElementById('outer').addEventListener('click', () => { outerFired++; });
			document.getElementById('b').dispatchEvent(new Event('click', {bubbles: true}));
			return outerFired;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 1 {
		t.Errorf("outer fired = %d, want 1", v.Int64())
	}
}

func TestEventDoesNotBubbleByDefault(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="outer"><button id="b">go</button></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			let outerFired = 0;
			document.getElementById('outer').addEventListener('click', () => { outerFired++; });
			document.getElementById('b').dispatchEvent(new Event('click'));
			return outerFired;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 0 {
		t.Errorf("outer fired = %d, want 0", v.Int64())
	}
}

func TestStopPropagation(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="outer"><button id="b">go</button></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			let outerFired = 0;
			document.getElementById('outer').addEventListener('click', () => { outerFired++; });
			document.getElementById('b').addEventListener('click', (e) => { e.stopPropagation(); });
			document.getElementById('b').dispatchEvent(new Event('click', {bubbles: true}));
			return outerFired;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 0 {
		t.Errorf("outer fired = %d, want 0 (stopPropagation)", v.Int64())
	}
}

func TestPreventDefaultRoundTrip(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><a id="a" href="#">go</a></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			document.getElementById('a').addEventListener('click', (e) => { e.preventDefault(); });
			const ev = new Event('click', {cancelable: true});
			const ok = document.getElementById('a').dispatchEvent(ev);
			return ev.defaultPrevented + ":" + ok;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:false" {
		t.Errorf("dispatch = %q, want true:false", v.String())
	}
}

func TestClickShortcut(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><button id="b">go</button></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			let fired = 0;
			document.getElementById('b').addEventListener('click', () => { fired++; });
			document.getElementById('b').click();
			return fired;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 1 {
		t.Errorf("click() fired = %d, want 1", v.Int64())
	}
}

func TestListenerThisIsTarget(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><button id="b">go</button></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			let captured = "";
			document.getElementById('b').addEventListener('click', function() { captured = this.tagName; });
			document.getElementById('b').click();
			return captured;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "BUTTON" {
		t.Errorf("this.tagName = %q, want BUTTON", v.String())
	}
}
