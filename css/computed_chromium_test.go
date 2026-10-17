package css

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"golang.org/x/image/colornames"
)

func TestCSSComputedChromiumDifferential(t *testing.T) {
	inputs := make([]string, 0, len(colornames.Map)+280)
	for name := range colornames.Map {
		inputs = append(inputs, name)
	}
	sort.Strings(inputs)
	inputs = append(inputs, "transparent", "rebeccapurple", `r\65 d`, "#f0a", "#F0A8", "#010203", "rgb(1,2,3)", "rgb(100%,0%,50%)", "rgba(1,2,3,0.5)", "rgba(1,2,3,50%)", "rgb(-20,300,127.5)", "rgba(1,2,3,4)", "rgba(1,2,3,-1)", "RGB(1,2,3)")
	for alpha := 0; alpha <= 255; alpha++ {
		inputs = append(inputs, fmt.Sprintf("#010203%02x", alpha))
	}
	encoded, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatal(err)
	}
	// The process owner permits a 30-second startup; action and owned cleanup
	// must not be cancelled by a shorter enclosing test context.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := browser.Close(); err != nil {
			t.Errorf("close owned Chromium: %v", err)
		}
	})
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Errorf("close owned context: %v", err)
		}
	})
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := page.Close(); err != nil {
			t.Errorf("close owned page: %v", err)
		}
	})
	var identity struct {
		Product  string `json:"product"`
		Revision string `json:"revision"`
	}
	if err := page.CallBrowser(ctx, "Browser.getVersion", struct{}{}, &identity); err != nil || identity.Product == "" || identity.Revision == "" {
		t.Fatalf("native differential browser identity missing: %+v %v", identity, err)
	}
	t.Logf("native CSS conformance browser=%s revision=%s", identity.Product, identity.Revision)
	parameters := struct {
		Expression    string `json:"expression"`
		ReturnByValue bool   `json:"returnByValue"`
	}{Expression: `(() => { const element = document.createElement('div'); document.body.appendChild(element); return ` + string(encoded) + `.map(value => { element.style.color = ''; element.style.color = value; return getComputedStyle(element).color; }); })()`, ReturnByValue: true}
	var result struct {
		Result struct {
			Value []string `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", parameters, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ExceptionDetails) != 0 || len(result.Result.Value) != len(inputs) {
		t.Fatalf("Chromium color evaluation failed: count=%d exceptions=%s", len(result.Result.Value), result.ExceptionDetails)
	}
	for index, source := range inputs {
		tokens, diagnostic := tokenizeCSS(source)
		if diagnostic != nil {
			t.Fatal(diagnostic)
		}
		actual, err := parseCSSColor(tokens)
		if err != nil || actual.serialize() != result.Result.Value[index] {
			t.Errorf("input=%q renderless=%q Chromium=%q error=%v", source, actual.serialize(), result.Result.Value[index], err)
		}
	}
	assertCSSComputedChromiumParity(t, ctx, page)
	assertCSSStyleDeclarationChromiumParity(t, ctx, page)
	assertCSSSelectorChromiumParity(t, ctx, page)
}

func assertCSSComputedChromiumParity(t *testing.T, ctx context.Context, page *bridge.Page) {
	t.Helper()
	source := `html {font-size:16px;color:red;--base:red;--alias:var(--base)} body {font-size:20px} #parent {font-size:2em;color:blue} #child {--base:green;--number:10;--dimension:var(--number)px;color:var(--alias);background-color:currentColor;font-size:150%;opacity:80%;visibility:hidden} #invalid {color:blue;color:var(--missing);font-size:40px;font-size:var(--dimension)} #point {font-size:1pt} #fraction {font-size:12.3456789px} #centimeter {font-size:1cm} #relative {font-size:112.345%} #large {font-size:1000000px} #tiny {font-size:0.0000001px} #zero {font-size:-0px}`
	source += ` #box {--space:1px 2px 3px 4px;padding:var(--space);padding-right:9px !important;padding:2px;margin:1px 2px 3px 4px;margin-left:8px;background:blue;background-color:red} #pending {padding:5px;padding:var(--missing);padding-left:8px} #negative {--space:-1px 2px 3px 4px;padding:5px;padding:var(--space)} #reset-background {background-color:red;background:navy}`
	source += ` #fraction {opacity:0.123456789} #relative {opacity:12.3456789%} #tiny {opacity:0.0000000123456789} #zero {opacity:-0} #large {opacity:0.9999995} #point {opacity:0.1000005} #centimeter {opacity:0.9999985}`
	document := `<!doctype html><html><head><style>` + source + `</style></head><body><div id="parent"><p id="child"><span id="invalid"></span></p></div><p id="point"></p><p id="fraction"></p><p id="centimeter"><span id="relative"></span></p><p id="large"></p><p id="tiny"></p><p id="zero"></p><div id="box"></div><div id="pending"></div><div id="negative"></div><div id="reset-background"></div></body></html>`
	queries := []struct {
		ID       string `json:"id"`
		Property string `json:"property"`
	}{
		{"child", "color"}, {"child", "background-color"}, {"child", "font-size"},
		{"child", "opacity"}, {"child", "visibility"}, {"child", "--alias"}, {"child", "--dimension"},
		{"invalid", "color"}, {"invalid", "font-size"}, {"invalid", "visibility"},
		{"point", "font-size"}, {"fraction", "font-size"}, {"centimeter", "font-size"},
		{"relative", "font-size"}, {"large", "font-size"}, {"tiny", "font-size"}, {"zero", "font-size"},
		{"fraction", "opacity"}, {"relative", "opacity"}, {"tiny", "opacity"}, {"zero", "opacity"},
		{"large", "opacity"}, {"point", "opacity"}, {"centimeter", "opacity"},
		{"box", "padding"}, {"box", "padding-right"}, {"box", "margin"},
		{"box", "background"}, {"box", "background-color"},
		{"pending", "padding"}, {"negative", "padding"}, {"reset-background", "background"},
	}
	encodedDocument, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	encodedQueries, err := json.Marshal(queries)
	if err != nil {
		t.Fatal(err)
	}
	parameters := struct {
		Expression    string `json:"expression"`
		ReturnByValue bool   `json:"returnByValue"`
	}{Expression: `(() => {document.open();document.write(` + string(encodedDocument) + `);document.close(); return ` + string(encodedQueries) + `.map(query => getComputedStyle(document.getElementById(query.id)).getPropertyValue(query.property));})()`, ReturnByValue: true}
	var result struct {
		Result struct {
			Value []string `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", parameters, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ExceptionDetails) != 0 || len(result.Result.Value) != len(queries) {
		t.Fatalf("Chromium computed-style evaluation failed: %+v", result)
	}
	sheet := ParseStylesheet(source)
	for index, query := range queries {
		node := parseAndFind(t, document, "#"+query.ID)
		style, err := Cascade([]*Stylesheet{sheet}, node, "")
		if err != nil {
			t.Fatal(err)
		}
		actual, err := style.Value(query.Property)
		if err != nil || actual != result.Result.Value[index] {
			t.Errorf("%s/%s renderless=%q Chromium=%q error=%v", query.ID, query.Property, actual, result.Result.Value[index], err)
		}
	}
}
