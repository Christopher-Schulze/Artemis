package js

import (
	"context"
	"strings"
	"testing"
)

func TestStyleReadInline(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d" style="color: red; font-size: 14px"></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `document.getElementById('d').style.color`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "red" {
		t.Errorf("color = %q", v.String())
	}
	v, _ = c.Eval(context.Background(), `document.getElementById('d').style.fontSize`)
	if v.String() != "14px" {
		t.Errorf("fontSize = %q", v.String())
	}
}

func TestStyleWriteUpdatesAttribute(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `document.getElementById('d').style.color = 'blue'`); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, err := c.Eval(context.Background(), `document.getElementById('d').getAttribute('style')`)
	if err != nil {
		t.Fatalf("attr: %v", err)
	}
	if !strings.Contains(v.String(), "color: blue") {
		t.Errorf("attr = %q", v.String())
	}
}

func TestGetComputedStyleEqualsInline(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><span id="s" style="color: green"></span></body></html>`, nil)
	v, err := c.Eval(context.Background(), `getComputedStyle(document.getElementById('s')).color`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "green" {
		t.Errorf("computed color = %q", v.String())
	}
}
