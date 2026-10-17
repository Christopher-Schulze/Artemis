package js

import (
	"context"
	"testing"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/webapi"
	"golang.org/x/net/html"
)

func TestCSSGlobalPresenceDoesNotInventConstructors(t *testing.T) {
	c := newCtxFromHTML(t, `<div></div>`, nil)
	value, err := c.Eval(context.Background(), `[typeof getComputedStyle,typeof CSSStyleDeclaration,typeof CSSStyleSheet].join(',')`)
	if err != nil || value.String() != "function,undefined,undefined" {
		t.Fatalf("CSS global inventory disagrees with actual bindings: %v %v", value, err)
	}
}

func TestInlineStyleCSSOMOrderedLiveMutations(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="d" style="padding-left:1px;color:red !important;padding:2px"></div>`, nil)
	for _, test := range []struct{ name, expression, want string }{
		{"identity", `document.getElementById('d').style === document.getElementById('d').style`, "true"},
		{"priority excluded", `document.getElementById('d').style.color`, "red"},
		{"priority method", `document.getElementById('d').style.getPropertyPriority('COLOR')`, "important"},
		{"longhand overrides shorthand", `document.getElementById('d').style.paddingLeft = '3px';document.getElementById('d').style.padding`, "2px 2px 2px 3px"},
		{"write replaces priority", `document.getElementById('d').style.color = 'blue';document.getElementById('d').style.getPropertyPriority('color')`, ""},
		{"items", `Array.from(document.getElementById('d').style).join(',')`, "padding-top,padding-right,padding-bottom,padding-left,color"},
		{"numeric item", `document.getElementById('d').style[0]`, "padding-top"},
		{"removal result", `document.getElementById('d').style.removeProperty('color')`, "blue"},
		{"cssText", `document.getElementById('d').style.cssText`, "padding: 2px 2px 2px 3px;"},
		{"whole style setter", `document.getElementById('d').style = 'color:red !important;width:12PX';document.getElementById('d').style.cssText`, "width: 12px; color: red !important;"},
		{"live attribute", `document.getElementById('d').setAttribute('style','color:green');document.getElementById('d').style.color`, "green"},
		{"removed attribute", `document.getElementById('d').removeAttribute('style');document.getElementById('d').style.length`, "0"},
		{"DOMString failure", `try {document.getElementById('d').style.color=Symbol()}catch(e){e instanceof TypeError}`, "true"},
		{"index conversion failure", `try {document.getElementById('d').style.item(1n)}catch(e){e instanceof TypeError}`, "true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := c.Eval(context.Background(), test.expression)
			if err != nil || value == nil || value.String() != test.want {
				t.Fatalf("CSSOM outcome=%v want=%q error=%v", value, test.want, err)
			}
		})
	}
}

func TestInlineStyleCSSOMRejectsInjectionWithoutMutation(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="d" style="color:red"></div>`, nil)
	for _, expression := range []string{
		`document.getElementById('d').style.color='blue;opacity:0'`,
		`document.getElementById('d').style.setProperty('color','blue !important')`,
		`document.getElementById('d').style.cssText='color:blue;broken'`,
	} {
		value, err := c.Eval(context.Background(), expression)
		if value != nil || !css.IsDiagnostic(err) {
			t.Fatalf("CSSOM injection acquired success: %v %v", value, err)
		}
		value, err = c.Eval(context.Background(), `document.getElementById('d').getAttribute('style')`)
		if err != nil || value.String() != "color:red" {
			t.Fatalf("failed CSSOM write changed DOM: %v %v", value, err)
		}
	}
}

func TestComputedStyleCSSOMMethodsAreLiveAndReadOnly(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="d" style="color:red"></div>`, nil)
	for _, test := range []struct{ expression, want string }{
		{`globalThis.computed=getComputedStyle(document.getElementById('d'));computed.getPropertyValue('color')`, "rgb(255, 0, 0)"},
		{`document.getElementById('d').style.color='blue';computed.getPropertyValue('color')`, "rgb(0, 0, 255)"},
		{`computed.getPropertyPriority('color')`, ""},
		{`computed.cssText`, ""},
		{`try{computed.color='red'}catch(e){e.name+':'+e.code}`, "NoModificationAllowedError:7"},
		{`try{computed.setProperty('color','red')}catch(e){e.name+':'+e.code}`, "NoModificationAllowedError:7"},
		{`try{computed.removeProperty('color')}catch(e){e.name+':'+e.code}`, "NoModificationAllowedError:7"},
		{`try{computed.cssText='color:red'}catch(e){e.name+':'+e.code}`, "NoModificationAllowedError:7"},
		{`try{getComputedStyle({})}catch(e){e instanceof TypeError}`, "true"},
	} {
		value, err := c.Eval(context.Background(), test.expression)
		if err != nil || value == nil || value.String() != test.want {
			t.Fatalf("computed CSSOM outcome=%v want=%q error=%v", value, test.want, err)
		}
	}
}

func TestInlineStyleOwnershipCountAndMutationFailureAreAtomic(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="d" style="color:red"></div>`, nil)
	for index := 0; index < 1024; index++ {
		node := webapi.NewDocument(&html.Node{Type: html.ElementNode, Data: "div"}, "").Root()
		style, err := css.ParseStyleDeclaration("")
		if err != nil {
			t.Fatal(err)
		}
		if err := c.styleMgr.retainInlineStyle(node, &inlineStyleState{style: style}); err != nil {
			t.Fatalf("exact inline owner count rejected: %v", err)
		}
	}
	value, err := c.Eval(context.Background(), `document.getElementById('d').style.cssText='color:blue'`)
	if value != nil || !css.IsDiagnostic(err) || len(c.styleMgr.inline) != 1024 {
		t.Fatalf("excess owner count acquired mutation: %v %v", value, err)
	}
	value, err = c.Eval(context.Background(), `document.getElementById('d').getAttribute('style')`)
	if err != nil || value.String() != "color:red" {
		t.Fatalf("failed owner admission changed DOM: %v %v", value, err)
	}
	for node := range c.styleMgr.inline {
		c.styleMgr.invalidateInlineStyle(webapi.NewDocument(node, "").Root())
		break
	}
	value, err = c.Eval(context.Background(), `document.getElementById('d').style.cssText='color:blue';document.getElementById('d').style.color`)
	if err != nil || value.String() != "blue" || c.styleMgr.inlineBytes != len("color: blue;") {
		t.Fatalf("retired owner did not release admission: %v %v", value, err)
	}
}
