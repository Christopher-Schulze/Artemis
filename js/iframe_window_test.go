package js

import (
	"context"
	"testing"
)

func TestWindowParentTopSelf(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(window.parent === window) + ':' +
		(window.top === window) + ':' +
		(window.self === window) + ':' +
		(window.opener === null)
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:true:true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestWindowFramesLive(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><iframe></iframe><iframe></iframe></body></html>`, nil)
	v, err := c.Eval(context.Background(), `window.frames.length`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 2 {
		t.Errorf("got %d, want 2", v.Int64())
	}
}
