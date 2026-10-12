package js

import (
	"context"
	"testing"
)

func TestWrapperIdentitySameID(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		document.getElementById('d') === document.getElementById('d')
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !v.Bool() {
		t.Error("two getElementById calls should return identical wrapper")
	}
}

func TestWrapperIdentityViaWalk(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"><p>a</p></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const d = document.getElementById('d');
			const pViaQS = d.querySelector('p');
			const pViaWalk = d.firstChild;
			return pViaQS === pViaWalk;
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !v.Bool() {
		t.Error("p via querySelector and firstChild should be the same wrapper")
	}
}

func TestWrapperIdentityInSet(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><p id="a"></p><p id="b"></p></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const set = new Set();
			set.add(document.getElementById('a'));
			set.add(document.getElementById('a')); // dedup via Set
			set.add(document.getElementById('b'));
			return set.size + ':' + set.has(document.getElementById('a'));
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "2:true" {
		t.Errorf("got %q, want 2:true (Set dedups by reference identity)", v.String())
	}
}
