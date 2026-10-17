package js

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestDocumentLoadingUsesOwnedDispatcherAndCompletesOnce(t *testing.T) {
	c := newCtxFromHTML(t, "<html><body></body></html>", nil)
	if _, err := c.Eval(t.Context(), `
		globalThis.trace = [];
		document.addEventListener('readystatechange', () => trace.push(document.readyState));
		document.addEventListener('DOMContentLoaded', () => trace.push('dom'));
		window.addEventListener('load', () => trace.push('load'));
		globalThis.Event = function() { throw Error('replaced public constructor'); };
		document.dispatchEvent = window.dispatchEvent = function() { throw Error('replaced public dispatcher'); };
		document.readyState = 'forged';
	`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := c.CompleteDocumentLoading(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	value, err := c.Eval(t.Context(), "trace.join('|') + ':' + document.readyState")
	if err != nil || value.String() != "interactive|dom|complete|load:complete" {
		t.Fatalf("owned/idempotent lifecycle mismatch: got=%q err=%v", value.String(), err)
	}
	c.Close()
	if err := c.CompleteDocumentLoading(t.Context()); err == nil {
		t.Fatal("closed Context acknowledged document loading")
	}
}

func TestDocumentLoadingCancellationPreventsLaterStages(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: func(context.Context, FetchRequest) (*FetchResponse, error) {
		cancel()
		return &FetchResponse{Status: 200, Body: []byte("cancelled by actual synchronous fetch callback")}, nil
	}})
	if _, err := c.Eval(t.Context(), `
		globalThis.loaded = false;
		document.addEventListener('DOMContentLoaded', () => fetch('/cancel'));
		window.addEventListener('load', () => { loaded = true; });
	`); err != nil {
		t.Fatal(err)
	}
	if err := c.CompleteDocumentLoading(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("loading ignored actual cancellation: %v", err)
	}
	value, err := c.Eval(t.Context(), "loaded + ':' + document.readyState")
	if err != nil || value.String() != "false:interactive" {
		t.Fatalf("cancellation executed a later loading stage: got=%q err=%v", value.String(), err)
	}
}

func TestDocumentEventFailureRemainsObservableAfterOtherListenersRun(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	_, err := c.Eval(t.Context(), `
		globalThis.otherListenerRan = false;
		document.addEventListener('probe', () => { throw Error('actual-event-error'); });
		document.addEventListener('probe', () => { otherListenerRan = true; });
		document.dispatchEvent(new Event('probe'));
	`)
	var listenerError *EventListenerError
	if !errors.As(err, &listenerError) || !strings.Contains(err.Error(), "actual-event-error") {
		t.Fatalf("listener exception was not observable: %v", err)
	}
	value, err := c.Eval(t.Context(), "otherListenerRan")
	if err != nil || !value.Bool() {
		t.Fatalf("listener exception prevented the next listener or leaked into later Eval: %v", err)
	}
}

func TestPoolRetiresExecutedDocumentAndWindowEventOwner(t *testing.T) {
	rt := NewRuntimeWithPool(1)
	defer rt.Close()
	var previousContext *Context
	for round := range 2 {
		doc, err := parser.ParseHTML(strings.NewReader("<html><body>actual new owner</body></html>"), "https://loading.test/")
		if err != nil {
			t.Fatal(err)
		}
		c, err := rt.NewContext(doc, ContextOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if round == 1 && c.v8ctx == previousContext.v8ctx {
			t.Fatal("pool reused a realm exposed to page callbacks")
		}
		if _, err := c.Eval(t.Context(), `
			if (document.readyState !== 'loading' || document.body.parentNode.parentNode !== document) throw Error('stale document');
			globalThis.events = 0;
			document.addEventListener('DOMContentLoaded', () => { events++; });
			window.onload = () => { events++; };
		`); err != nil {
			c.Close()
			t.Fatal(err)
		}
		if err := c.CompleteDocumentLoading(t.Context()); err != nil {
			c.Close()
			t.Fatal(err)
		}
		value, err := c.Eval(t.Context(), "events")
		if err != nil || value.Int64() != 2 {
			c.Close()
			t.Fatalf("event state crossed pooled owners: round=%d got=%v err=%v", round, value, err)
		}
		previousContext = c
		c.Close()
	}
}
