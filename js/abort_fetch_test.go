package js

import (
	"context"
	"strings"
	"testing"
)

func TestFetchHonorsAlreadyAbortedSignal(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: staticFetcher(t, "ok", 200),
	})
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		const ac = new AbortController();
		ac.abort();
		fetch('https://e.test/', {signal: ac.signal})
			.then(() => { captured = 'resolved'; })
			.catch(e => { captured = 'rejected:' + (e && e.message || e); });
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if !strings.HasPrefix(v.String(), "rejected:") || !strings.Contains(v.String(), "Abort") {
		t.Errorf("got %q, want rejected with AbortError", v.String())
	}
}

func TestFetchSignalNotAbortedProceeds(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: staticFetcher(t, "ok-body", 200),
	})
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		const ac = new AbortController();
		fetch('https://e.test/', {signal: ac.signal}).then(r => r.text()).then(t => { captured = t; });
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "ok-body" {
		t.Errorf("got %q", v.String())
	}
}
