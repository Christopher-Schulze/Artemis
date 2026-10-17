package js

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/css"
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

func TestInlineStyleMalformedInputCannotProducePartialReadOrMutation(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d" style="color: red; broken"></div></body></html>`, nil)
	for _, expression := range []string{
		`document.getElementById('d').style.color`,
		`document.getElementById('d').style.color = 'blue'`,
	} {
		value, err := c.Eval(context.Background(), expression)
		if err == nil || value != nil {
			t.Fatalf("malformed style became a successful partial operation: value=%v err=%v", value, err)
		}
	}
	value, err := c.Eval(context.Background(), `document.getElementById('d').getAttribute('style')`)
	if err != nil || value.String() != "color: red; broken" {
		t.Fatalf("rejected setter changed actual DOM input: %v", err)
	}
}

func TestGetComputedStyleResolvesInlineValue(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><span id="s" style="color: green"></span></body></html>`, nil)
	v, err := c.Eval(context.Background(), `getComputedStyle(document.getElementById('s')).color`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "rgb(0, 128, 0)" {
		t.Errorf("computed color = %q", v.String())
	}
}

func TestInlineStyleCaughtExceptionRetainsTypedExecutionFailure(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="d" style="color:red;broken"></div>`, nil)
	for _, expression := range []string{
		`try { document.getElementById('d').style.color } catch(e) {} ; 'hidden'`,
		`setTimeout(function() { try { document.getElementById('d').style.color = 'blue' } catch(e) {} }, 0); 'hidden'`,
	} {
		value, err := c.Eval(context.Background(), expression)
		var diagnostic css.Diagnostic
		if value != nil || !errors.As(err, &diagnostic) || diagnostic.Code != css.DiagnosticSyntax {
			t.Fatalf("JS catch erased typed CSS execution failure: %v %v", value, err)
		}
	}
	value, err := c.Eval(context.Background(), `document.getElementById('d').getAttribute('style')`)
	if err != nil || value.String() != "color:red;broken" {
		t.Fatalf("failure leaked into an unrelated read or mutated the attribute: %v", err)
	}
}
