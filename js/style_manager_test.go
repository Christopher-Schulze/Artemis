package js

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestGetComputedStyleFromStylesheet(t *testing.T) {
	c := newCtxFromHTML(t, `<html><head>
<style>
  .card { background: blue; padding: 8px; }
  #x { color: red !important; }
  p { color: green; }
</style>
</head><body>
  <p id="x" class="card" style="margin: 4px">hello</p>
</body></html>`, nil)
	cases := []struct {
		expr string
		want string
	}{
		{`getComputedStyle(document.getElementById('x')).color`, "rgb(255, 0, 0)"},                                                             // !important wins
		{`getComputedStyle(document.getElementById('x')).background`, "rgb(0, 0, 255) none repeat scroll 0% 0% / auto padding-box border-box"}, // class
		{`getComputedStyle(document.getElementById('x')).padding`, "8px"},                                                                      // class
		{`getComputedStyle(document.getElementById('x')).margin`, "4px"},                                                                       // inline
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

func TestGetComputedStyleNoStylesheet(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d" style="color: red"></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `getComputedStyle(document.getElementById('d')).color`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "rgb(255, 0, 0)" {
		t.Errorf("got %q", v.String())
	}
}

func TestStylesheetDocumentOrderSurvivesExternalLoading(t *testing.T) {
	var loaded []string
	c := newCtxFromHTMLOpts(t, `<link rel="STYLESHEET" href="one"><style>#d { color: green }</style><link rel="notstylesheet" href="ignored"><div id="d"></div>`, ContextOpts{
		LoadStylesheet: func(ctx context.Context, href string) ([]byte, error) {
			loaded = append(loaded, href)
			return []byte(`#d { color: red }`), ctx.Err()
		},
	})
	value, err := c.Eval(context.Background(), `getComputedStyle(document.getElementById('d')).color`)
	if err != nil || value.String() != "rgb(0, 128, 0)" || len(loaded) != 1 || loaded[0] != "https://example.test/one" {
		t.Fatalf("stylesheet document order or rel token changed: value=%v loaded=%v err=%v", value, loaded, err)
	}
}

func TestStylesheetFailuresCannotConstructPartialContext(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		loader StylesheetLoader
		code   css.DiagnosticCode
	}{
		{"missing loader", `<link rel="stylesheet" href="secret">`, nil, css.DiagnosticLoad},
		{"loader error", `<link rel="stylesheet" href="secret">`, func(context.Context, string) ([]byte, error) { return nil, errors.New("sensitive URL and credentials") }, css.DiagnosticLoad},
		{"unsupported inline", `<style>@media all { div {color:red} }</style>`, nil, css.DiagnosticUnsupported},
		{"malformed inline", `<style>div {color:"broken}</style>`, nil, css.DiagnosticSyntax},
		{"conditional media", `<style media="(min-width: 1px)">div {color:red}</style>`, nil, css.DiagnosticUnsupported},
		{"alternate", `<link rel="alternate stylesheet" href="secret">`, nil, css.DiagnosticUnsupported},
		{"count limit", strings.Repeat(`<style></style>`, 129), nil, css.DiagnosticBudget},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parser.ParseHTML(strings.NewReader(test.source), "https://example.test/")
			if err != nil {
				t.Fatal(err)
			}
			runtime := NewRuntime()
			defer runtime.Close()
			actual, err := runtime.NewContext(doc, ContextOpts{LoadStylesheet: test.loader})
			if actual != nil {
				actual.Close()
				t.Fatal("partial context constructed")
			}
			var value css.Diagnostic
			var pointer *css.Diagnostic
			if errors.As(err, &value) {
				pointer = &value
			} else if !errors.As(err, &pointer) {
				t.Fatalf("typed diagnostic lost: %v", err)
			}
			if pointer.Code != test.code || strings.Contains(err.Error(), "sensitive") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("wrong or unredacted stylesheet failure: %v", err)
			}
		})
	}
}

func TestInactiveStylesheetsDoNotRequireLoader(t *testing.T) {
	c := newCtxFromHTML(t, `<link rel="stylesheet" disabled href="unused"><link rel="ſtylesheet" href="unused"><link rel="stylesheet icon" href="unused"><style type="text/less">unparsed</style><style type="text/cſs">unparsed</style><style type=" text/css ">unparsed</style><div id="d"></div>`, nil)
	value, err := c.Eval(context.Background(), `document.getElementById('d').id`)
	if err != nil || value.String() != "d" {
		t.Fatalf("inactive stylesheet interfered with document: %v", err)
	}
}

func TestEmptyStylesheetLinksDoNotConsumeActiveSheetBudget(t *testing.T) {
	c := newCtxFromHTML(t, strings.Repeat(`<link rel="stylesheet" href=" ">`, 129)+`<style>#d{color:red}</style><div id="d"></div>`, nil)
	value, err := c.Eval(t.Context(), `getComputedStyle(document.getElementById('d')).color`)
	if err != nil || value.String() != "rgb(255, 0, 0)" {
		t.Fatalf("empty links consumed active stylesheet budget: %v", err)
	}
}

func TestStylesheetHTMLWhitespacePreservesURLAndRejectsNonASCIIMedia(t *testing.T) {
	var loaded string
	c := newCtxFromHTMLOpts(t, `<link rel="stylesheet" href=" one ">`, ContextOpts{LoadStylesheet: func(ctx context.Context, url string) ([]byte, error) {
		loaded = url
		return nil, ctx.Err()
	}})
	if loaded != "https://example.test/%C2%A0one%C2%A0" || c.styleMgr == nil {
		t.Fatalf("non-HTML whitespace was stripped from stylesheet URL: %q", loaded)
	}
	doc, err := parser.ParseHTML(strings.NewReader(`<style media=" all ">p{color:red}</style>`), "https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newStyleManager(t.Context(), doc, nil); !css.IsDiagnostic(err) {
		t.Fatalf("non-ASCII media whitespace acquired supported matching: %v", err)
	}
}

func TestStylesheetLoadRetainsActualFailureIdentity(t *testing.T) {
	doc, err := parser.ParseHTML(strings.NewReader(`<link rel="stylesheet" href="failure.css">`), "https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("private transport failure")
	_, err = newStyleManager(t.Context(), doc, func(context.Context, string) ([]byte, error) { return nil, cause })
	if !css.IsDiagnostic(err) || !errors.Is(err, cause) || strings.Contains(err.Error(), "private transport") {
		t.Fatalf("stylesheet load lost original failure or exposed transport text: %v", err)
	}
}

func TestStylesheetMutationCannotReturnStaleComputedEvidence(t *testing.T) {
	for _, mutation := range []string{
		`document.querySelector('style').textContent='#d{color:blue}'`,
		`document.querySelector('style').setAttribute('media','print')`,
		`document.querySelector('link').setAttribute('href','changed.css')`,
		`document.querySelector('base').setAttribute('href','/changed/')`,
		`const style=document.createElement('style');style.textContent='#d{color:blue}';document.head.appendChild(style)`,
		`const style=document.querySelector('style');style.parentNode.removeChild(style)`,
	} {
		t.Run(mutation, func(t *testing.T) {
			c := newCtxFromHTMLOpts(t, `<base href="/"><style>#d{color:red}</style><link rel="stylesheet" href="first.css"><div id="d"></div>`, ContextOpts{
				LoadStylesheet: func(ctx context.Context, href string) ([]byte, error) { return nil, ctx.Err() },
			})
			value, err := c.Eval(context.Background(), `getComputedStyle(document.getElementById('d')).color`)
			if err != nil || value.String() != "rgb(255, 0, 0)" {
				t.Fatalf("initial owned style failed: %v %v", value, err)
			}
			value, err = c.Eval(context.Background(), mutation+`;getComputedStyle(document.getElementById('d')).color`)
			if value != nil || !css.IsDiagnostic(err) {
				t.Fatalf("stylesheet mutation returned stale evidence: %v %v", value, err)
			}
		})
	}
}

func TestComputedStyleCannotUseAnotherDocumentOrDetachedOwner(t *testing.T) {
	c := newCtxFromHTML(t, `<style>#d{color:red}</style><div id="d"></div>`, nil)
	for _, expression := range []string{
		`getComputedStyle(document.createElement('div')).color`,
		`const node=document.getElementById('d');node.parentNode.removeChild(node);getComputedStyle(node).color`,
	} {
		value, err := c.Eval(context.Background(), expression)
		if value != nil || !css.IsDiagnostic(err) {
			t.Fatalf("unowned style produced computed proof: %v %v", value, err)
		}
	}
}
