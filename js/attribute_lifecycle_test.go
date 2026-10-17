package js

import (
	"errors"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestPristineReuseInstallsOnlyCurrentDocumentAttributes(t *testing.T) {
	owner := NewRuntimeWithPool(1)
	t.Cleanup(owner.Close)
	var previous *Context
	for _, name := range []string{"previous", "current"} {
		doc, err := parser.ParseHTML(strings.NewReader(`<button id="actual" onclick="globalThis.actualHandlers+='`+name+`'">actual</button>`), "https://attribute.test/")
		if err != nil {
			t.Fatal(err)
		}
		page, err := owner.NewContext(doc, ContextOpts{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(page.Close)
		if previous == nil {
			previous = page
			page.Close()
			continue
		}
		if page.v8ctx != previous.v8ctx {
			t.Fatal("control did not reuse the actual pristine realm")
		}
		got, err := page.EvalString(t.Context(), `globalThis.actualHandlers='';document.getElementById('actual').click();actualHandlers`)
		if err != nil || got != "current" {
			t.Fatalf("current handler missing or previous document handler retained: %q %v", got, err)
		}
	}
}

func TestAttributeCompilationFailureSurvivesPristineReuseUntilLoading(t *testing.T) {
	owner, err := NewRuntimeWithWarmPool(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	doc, err := parser.ParseHTML(strings.NewReader(`<button onclick="if (">actual</button>`), "https://attribute.test/")
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.NewContext(doc, ContextOpts{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(page.Close)
	var listener *EventListenerError
	if err := page.CompleteDocumentLoading(t.Context()); !errors.As(err, &listener) || !strings.Contains(err.Error(), "SyntaxError") {
		t.Fatalf("malformed current handler acknowledged loading: %v", err)
	}
	if err := page.CompleteDocumentLoading(t.Context()); !errors.As(err, &listener) {
		t.Fatalf("retry erased failed loading admission: %v", err)
	}
}
