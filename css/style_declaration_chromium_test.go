package css

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

type cssInlineOperation struct {
	Kind     string `json:"kind"`
	Property string `json:"property"`
	Value    string `json:"value"`
	Priority string `json:"priority"`
}

type cssInlineFixture struct {
	Name       string               `json:"name"`
	Source     string               `json:"source"`
	Operations []cssInlineOperation `json:"operations"`
	Properties []string             `json:"properties"`
	Attribute  bool                 `json:"attribute"`
}

type cssInlineObservation struct {
	Text       string   `json:"text"`
	Items      []string `json:"items"`
	Values     []string `json:"values"`
	Priorities []string `json:"priorities"`
	Removed    []string `json:"removed"`
}

func cssInlineFixtures() []cssInlineFixture {
	fixtures := []cssInlineFixture{
		{Name: "attribute important then normal", Source: `color:red !important;width:12px`, Properties: []string{"color", "width"}, Attribute: true},
		{Name: "attribute normal then important", Source: `width:12px;color:red !important`, Properties: []string{"color", "width"}, Attribute: true},
		{Name: "attribute duplicate filtering", Source: `color:red !important;width:12px;color:blue`, Properties: []string{"color", "width"}, Attribute: true},
		{Name: "duplicates and empty custom", Source: `width:12PX;color:red !important;width:20px;color:blue;--Mixed: /* comment */ Value;--empty:;padding:1px 2px;padding-left:3px`, Properties: []string{"color", "width", "padding", "padding-left", "--Mixed", "--empty"}},
		{Name: "ordered mutation", Source: `padding-left:1px;color:red !important;padding:2px`, Operations: []cssInlineOperation{{Kind: "set", Property: "padding-left", Value: "3px"}, {Kind: "set", Property: "color", Value: "blue"}}, Properties: []string{"padding", "padding-left", "color"}},
		{Name: "priority no-op", Source: `color:red !important`, Operations: []cssInlineOperation{{Kind: "set", Property: "color", Value: "blue", Priority: "bad"}}, Properties: []string{"color"}},
		{Name: "empty removal", Source: `color:red !important`, Operations: []cssInlineOperation{{Kind: "set", Property: "color", Value: "", Priority: "bad"}}, Properties: []string{"color"}},
		{Name: "pending shorthand", Source: `--space:2px;padding:var(--space) !important;color:red`, Properties: []string{"padding", "padding-left", "color", "--space"}},
		{Name: "pending removal", Source: `--space:2px;padding:var(--space) !important;color:red`, Operations: []cssInlineOperation{{Kind: "remove", Property: "padding"}}, Properties: []string{"padding", "color"}},
		{Name: "normal shorthand removal", Source: `padding:2px;color:red!important`, Operations: []cssInlineOperation{{Kind: "set", Property: "padding-left", Value: "3px"}, {Kind: "remove", Property: "padding"}}, Properties: []string{"padding", "color"}},
		{Name: "background", Source: `background:blue;background-color:red`, Properties: []string{"background", "background-color", "background-image"}},
		{Name: "escaped custom name", Source: `--a\;b: Original;--Case:Value`, Operations: []cssInlineOperation{{Kind: "set", Property: "--a;b", Value: "Changed", Priority: "IMPORTANT"}}, Properties: []string{"--a;b", "--Case", "--case"}},
	}
	for _, value := range []struct{ property, source string }{
		{"width", "12PX"}, {"width", "50%"}, {"width", "2em"}, {"width", "0"},
		{"font-size", "12.3456789px"}, {"font-size", "medium"}, {"font-weight", "700"},
		{"line-height", "1.5"}, {"line-height", "120%"}, {"opacity", "2"}, {"opacity", "-1"}, {"opacity", "120%"},
		{"display", "inline-block"}, {"z-index", "+001"}, {"color", "#f00"}, {"color", "currentColor"},
	} {
		fixtures = append(fixtures, cssInlineFixture{Name: value.property + "/" + value.source, Source: value.property + ":" + value.source, Properties: []string{value.property}})
	}
	return fixtures
}

func assertCSSStyleDeclarationChromiumParity(t *testing.T, ctx context.Context, page *bridge.Page) {
	t.Helper()
	fixtures := cssInlineFixtures()
	encoded, err := json.Marshal(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	parameters := struct {
		Expression    string `json:"expression"`
		ReturnByValue bool   `json:"returnByValue"`
	}{Expression: `(() => { return ` + string(encoded) + `.map(fixture => { const element = document.createElement('div'), style = element.style; if (fixture.attribute) element.setAttribute('style',fixture.source); else style.cssText = fixture.source; const removed = (fixture.operations || []).map(operation => { if (operation.kind === 'set') {style.setProperty(operation.property,operation.value,operation.priority);return '';} return style.removeProperty(operation.property); }); return {text:style.cssText,items:Array.from({length:style.length},(_,index)=>style.item(index)),values:fixture.properties.map(property=>style.getPropertyValue(property)),priorities:fixture.properties.map(property=>style.getPropertyPriority(property)),removed}; }); })()`, ReturnByValue: true}
	var result struct {
		Result struct {
			Value []cssInlineObservation `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", parameters, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ExceptionDetails) != 0 || len(result.Result.Value) != len(fixtures) {
		t.Fatalf("native inline-style observation failed: %+v", result)
	}
	for index, fixture := range fixtures {
		actual := observeCSSInlineFixture(t, fixture)
		if !reflect.DeepEqual(actual, result.Result.Value[index]) {
			t.Errorf("%s\nrenderless=%+v\nChromium=%+v", fixture.Name, actual, result.Result.Value[index])
		}
	}
	assertCSSAttributeParsingBoundary(t, ctx, page)
}

func assertCSSAttributeParsingBoundary(t *testing.T, ctx context.Context, page *bridge.Page) {
	t.Helper()
	parameters := struct {
		Expression    string `json:"expression"`
		ReturnByValue bool   `json:"returnByValue"`
	}{Expression: `['<!doctype html>', ''].map(prefix => {document.open();document.write(prefix+'<div id="probe" style="color:red !important;width:1px"></div>');document.close();const style=document.getElementById('probe').style, attribute=Array.from(style).join(',');style.cssText='color:red !important;width:1px';return document.compatMode+':'+attribute+'|'+Array.from(style).join(',');})`, ReturnByValue: true}
	var result struct {
		Result struct {
			Value []string `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", parameters, &result); err != nil {
		t.Fatal(err)
	}
	want := []string{"CSS1Compat:color,width|width,color", "BackCompat:color,width|width,color"}
	if len(result.ExceptionDetails) != 0 || !reflect.DeepEqual(result.Result.Value, want) {
		t.Fatalf("native attribute/mutable parsing boundary changed: %+v want=%v", result, want)
	}
}

func observeCSSInlineFixture(t *testing.T, fixture cssInlineFixture) cssInlineObservation {
	t.Helper()
	style := requireStyleDeclaration(t, fixture.Source)
	if fixture.Attribute {
		var err error
		style, err = ParseStyleAttribute(fixture.Source)
		if err != nil {
			t.Fatal(err)
		}
	}
	observation := cssInlineObservation{Removed: make([]string, 0, len(fixture.Operations))}
	for _, operation := range fixture.Operations {
		if operation.Kind == "set" {
			if _, err := style.SetProperty(operation.Property, operation.Value, operation.Priority); err != nil {
				t.Fatal(err)
			}
			observation.Removed = append(observation.Removed, "")
		} else {
			previous, _, err := style.RemoveProperty(operation.Property)
			if err != nil {
				t.Fatal(err)
			}
			observation.Removed = append(observation.Removed, previous)
		}
	}
	text, err := style.CSSString()
	if err != nil {
		t.Fatal(err)
	}
	observation.Text = text
	observation.Items = make([]string, style.Length())
	for index := range observation.Items {
		observation.Items[index] = style.Item(index)
	}
	for _, property := range fixture.Properties {
		value, err := style.GetPropertyValue(property)
		if err != nil {
			t.Fatal(err)
		}
		priority, err := style.GetPropertyPriority(property)
		if err != nil {
			t.Fatal(err)
		}
		observation.Values = append(observation.Values, value)
		observation.Priorities = append(observation.Priorities, priority)
	}
	return observation
}
