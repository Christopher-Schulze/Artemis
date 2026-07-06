package js

import (
	"context"
	"testing"
)

func TestInputCheckedProperty(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><input type="checkbox" id="c1" checked><input type="checkbox" id="c2"></body></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const a = document.getElementById('c1');
			const b = document.getElementById('c2');
			b.checked = true;
			a.checked = false;
			return a.checked + ':' + b.checked + ':' +
			       (a.getAttribute('checked') === null) + ':' +
			       (b.getAttribute('checked') !== null);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "false:true:true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestInputValueRoundtrip(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><input id="i" value="initial"></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `document.getElementById('i').value = 'updated'`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `document.getElementById('i').value`)
	if v.String() != "updated" {
		t.Errorf("got %q", v.String())
	}
	v, _ = c.Eval(context.Background(), `document.getElementById('i').getAttribute('value')`)
	if v.String() != "updated" {
		t.Errorf("attr got %q", v.String())
	}
}

func TestInputDisabledRequiredReadonly(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><input id="i" disabled required readonly></body></html>`, nil)
	v, _ := c.Eval(context.Background(), `
		(() => {
			const i = document.getElementById('i');
			return i.disabled + ':' + i.required + ':' + i.readOnly;
		})()
	`)
	if v.String() != "true:true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestInputForm(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><form id="f"><input id="i"></form></body></html>`, nil)
	// Wrappers are fresh per call (Object.create), so === over wrappers
	// returns false. We compare via __id which is stable per node.
	v, _ := c.Eval(context.Background(), `document.getElementById('i').form.__id === document.getElementById('f').__id`)
	if !v.Bool() {
		t.Error("input.form should be the parent form (by handle)")
	}
}

func TestSelectValueAndIndex(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><select id="s"><option value="a">A</option><option value="b" selected>B</option><option value="c">C</option></select></body></html>`, nil)
	v, _ := c.Eval(context.Background(), `document.getElementById('s').value`)
	if v.String() != "b" {
		t.Errorf("initial value = %q", v.String())
	}
	v, _ = c.Eval(context.Background(), `document.getElementById('s').selectedIndex`)
	if v.Int64() != 1 {
		t.Errorf("selectedIndex = %d", v.Int64())
	}
	if _, err := c.Eval(context.Background(), `document.getElementById('s').value = 'c'`); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, _ = c.Eval(context.Background(), `document.getElementById('s').value`)
	if v.String() != "c" {
		t.Errorf("after set value = %q", v.String())
	}
	v, _ = c.Eval(context.Background(), `document.getElementById('s').selectedIndex`)
	if v.Int64() != 2 {
		t.Errorf("after set selectedIndex = %d", v.Int64())
	}
}

func TestAnchorURLParts(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><a id="a" href="https://e.test:8443/path?x=1#frag">Link</a></body></html>`, nil)
	cases := []struct {
		expr string
		want string
	}{
		{`document.getElementById('a').protocol`, "https:"},
		{`document.getElementById('a').hostname`, "e.test"},
		{`document.getElementById('a').port`, "8443"},
		{`document.getElementById('a').host`, "e.test:8443"},
		{`document.getElementById('a').pathname`, "/path"},
		{`document.getElementById('a').search`, "?x=1"},
		{`document.getElementById('a').hash`, "#frag"},
	}
	for _, tc := range cases {
		v, _ := c.Eval(context.Background(), tc.expr)
		if v.String() != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, v.String(), tc.want)
		}
	}
}
