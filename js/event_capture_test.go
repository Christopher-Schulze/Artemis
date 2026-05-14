package js

import (
	"context"
	"testing"
)

func TestEventCapturePhase(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="outer"><span id="inner"></span></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const trace = [];
			const outer = document.getElementById('outer');
			const inner = document.getElementById('inner');
			outer.addEventListener('click', () => trace.push('outer-bubble'));
			outer.addEventListener('click', () => trace.push('outer-capture'), true);
			inner.addEventListener('click', () => trace.push('inner-bubble'));
			inner.dispatchEvent(new Event('click', {bubbles: true}));
			return trace.join(',');
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	// Capture: outer first. Then target+bubble: inner, outer.
	if v.String() != "outer-capture,inner-bubble,outer-bubble" {
		t.Errorf("got %q, want outer-capture,inner-bubble,outer-bubble", v.String())
	}
}

func TestEventCaptureOptionsObject(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			let captureFired = 0;
			const d = document.getElementById('d');
			const fn = () => { captureFired++; };
			d.addEventListener('click', fn, {capture: true});
			d.dispatchEvent(new Event('click'));
			d.removeEventListener('click', fn, {capture: true});
			d.dispatchEvent(new Event('click'));
			return captureFired;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 1 {
		t.Errorf("got %d, want 1 (only first dispatch should fire)", v.Int64())
	}
}

func TestEventCaptureStopPropagationStopsBubble(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="outer"><span id="inner"></span></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const trace = [];
			document.getElementById('outer').addEventListener('click', () => trace.push('cap'), true);
			document.getElementById('outer').addEventListener('click', () => trace.push('bub-outer'));
			document.getElementById('inner').addEventListener('click', (e) => { trace.push('inner'); e.stopPropagation(); });
			document.getElementById('outer').addEventListener('click', () => trace.push('cap-stop-skipped'), true); // still fires (capture before stopPropagation reaches target)
			document.getElementById('inner').dispatchEvent(new Event('click', {bubbles: true}));
			return trace.join(',');
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	// Both capture listeners on outer fire (capture phase runs before
	// target's stopPropagation). Then target. Then bubble is skipped.
	if v.String() != "cap,cap-stop-skipped,inner" {
		t.Errorf("got %q", v.String())
	}
}
