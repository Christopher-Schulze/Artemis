package js

import (
	"context"
	"strings"
	"testing"
)

func TestJSInnerHTMLSetterMutatesDOM(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><p>old</p></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `document.body.innerHTML = '<span id="s">new</span>';`); err != nil {
		t.Fatalf("set innerHTML: %v", err)
	}
	v, err := c.Eval(context.Background(), `document.getElementById('s').textContent`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "new" {
		t.Errorf("textContent = %q, want new", v.String())
	}
	v, err = c.Eval(context.Background(), `document.body.innerHTML`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !strings.Contains(v.String(), `<span id="s">new</span>`) {
		t.Errorf("body.innerHTML = %q", v.String())
	}
}

func TestJSSetAttributeRoundtrip(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><a id="x">link</a></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `document.getElementById('x').setAttribute('href','https://e.test/')`); err != nil {
		t.Fatalf("setAttribute: %v", err)
	}
	v, err := c.Eval(context.Background(), `document.getElementById('x').getAttribute('href')`)
	if err != nil {
		t.Fatalf("getAttribute: %v", err)
	}
	if v.String() != "https://e.test/" {
		t.Errorf("href = %q", v.String())
	}
}

func TestJSCreateAppendRemove(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><ul id="l"></ul></body></html>`, nil)
	script := `
		(() => {
			const ul = document.getElementById('l');
			const li = document.createElement('li');
			li.textContent = 'item';
			ul.appendChild(li);
			return ul.children.length;
		})()
	`
	v, err := c.Eval(context.Background(), script)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 1 {
		t.Errorf("ul.children.length = %d, want 1", v.Int64())
	}

	v, err = c.Eval(context.Background(), `document.getElementById('l').innerHTML`)
	if err != nil {
		t.Fatalf("innerHTML: %v", err)
	}
	if !strings.Contains(v.String(), "<li>item</li>") {
		t.Errorf("ul.innerHTML = %q", v.String())
	}

	if _, err := c.Eval(context.Background(), `{ const ul2 = document.getElementById('l'); ul2.removeChild(ul2.firstChild); }`); err != nil {
		t.Fatalf("removeChild: %v", err)
	}
	v, _ = c.Eval(context.Background(), `document.getElementById('l').children.length`)
	if v.Int64() != 0 {
		t.Errorf("after remove children.length = %d, want 0", v.Int64())
	}
}

func TestJSGetElementsByTagAndClass(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><p class="a">1</p><p class="b">2</p><span class="a">3</span></body></html>`, nil)
	v, err := c.Eval(context.Background(), `document.getElementsByTagName('p').length`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 2 {
		t.Errorf("getElementsByTagName p = %d", v.Int64())
	}
	v, err = c.Eval(context.Background(), `document.getElementsByClassName('a').length`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 2 {
		t.Errorf("getElementsByClassName a = %d", v.Int64())
	}
}

func TestJSTreeTraversal(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"><p>a</p><p>b</p></div></body></html>`, nil)
	cases := []struct {
		expr string
		want string
	}{
		{`document.getElementById('d').firstChild.tagName`, "P"},
		{`document.getElementById('d').firstChild.nextSibling.tagName`, "P"},
		{`document.getElementById('d').lastChild.textContent`, "b"},
		{`document.getElementById('d').firstChild.parentNode.tagName`, "DIV"},
		{`document.getElementById('d').children.length`, "2"},
	}
	for _, tc := range cases {
		v, err := c.Eval(context.Background(), tc.expr)
		if err != nil {
			t.Errorf("%s: %v", tc.expr, err)
			continue
		}
		if v.String() != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, v.String(), tc.want)
		}
	}
}

func TestJSTextContentSetter(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><p id="p">before</p></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `document.getElementById('p').textContent = 'after'`); err != nil {
		t.Fatalf("set textContent: %v", err)
	}
	v, _ := c.Eval(context.Background(), `document.getElementById('p').textContent`)
	if v.String() != "after" {
		t.Errorf("textContent = %q", v.String())
	}
}
