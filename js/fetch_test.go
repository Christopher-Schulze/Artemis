package js

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func staticFetcher(t *testing.T, body string, status int) FetchFunc {
	t.Helper()
	return func(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
		return &FetchResponse{
			Status:     status,
			StatusText: "OK",
			Headers:    map[string][]string{"Content-Type": {"text/plain"}},
			Body:       []byte(body),
			URL:        req.URL,
		}, nil
	}
}

// Microtasks run at the end of each top-level script (V8 kAuto policy),
// so promise continuations are not visible inside the same Eval. We use
// two evals: the first registers the chain into a global, the second
// reads the result back.

func TestFetchTextThen(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: staticFetcher(t, "hello", 200),
	})
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		fetch('https://example.test/x').then(r => r.text()).then(t => { captured = t; });
	`); err != nil {
		t.Fatalf("eval1: %v", err)
	}
	v, err := c.Eval(context.Background(), `captured`)
	if err != nil {
		t.Fatalf("eval2: %v", err)
	}
	if v.String() != "hello" {
		t.Errorf("captured = %q, want hello", v.String())
	}
}

func TestFetchAwait(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: staticFetcher(t, `{"x":42}`, 200),
	})
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			const r = await fetch('https://example.test/api');
			const j = await r.json();
			captured = r.status + ":" + r.ok + ":" + j.x;
		})();
	`); err != nil {
		t.Fatalf("eval1: %v", err)
	}
	v, err := c.Eval(context.Background(), `captured`)
	if err != nil {
		t.Fatalf("eval2: %v", err)
	}
	if v.String() != "200:true:42" {
		t.Errorf("captured = %q, want 200:true:42", v.String())
	}
}

func TestFetch404HasOKFalse(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: staticFetcher(t, "nope", 404),
	})
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		fetch('https://example.test/missing').then(r => { captured = r.status + ":" + r.ok; });
	`); err != nil {
		t.Fatalf("eval1: %v", err)
	}
	v, err := c.Eval(context.Background(), `captured`)
	if err != nil {
		t.Fatalf("eval2: %v", err)
	}
	if v.String() != "404:false" {
		t.Errorf("got %q, want 404:false", v.String())
	}
}

func TestFetchTransportErrorRejects(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: func(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
			return nil, errors.New("connection refused")
		},
	})
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		fetch('https://example.test/x').then(() => { captured = 'ok'; }, e => { captured = 'rejected:' + e; });
	`); err != nil {
		t.Fatalf("eval1: %v", err)
	}
	v, err := c.Eval(context.Background(), `captured`)
	if err != nil {
		t.Fatalf("eval2: %v", err)
	}
	if !strings.Contains(v.String(), "rejected") || !strings.Contains(v.String(), "refused") {
		t.Errorf("captured = %q, want rejected:...connection refused", v.String())
	}
}

func TestFetchSendsHeadersAndBody(t *testing.T) {
	var got FetchRequest
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: func(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
			got = req
			return &FetchResponse{Status: 200, Body: []byte("ok"), URL: req.URL}, nil
		},
	})
	if _, err := c.Eval(context.Background(), `
		fetch('https://example.test/api', {
			method: 'POST',
			headers: {'Content-Type': 'application/json', 'X-Api-Key': 'secret'},
			body: '{"x":1}'
		});
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got.Method != "POST" {
		t.Errorf("method = %q, want POST", got.Method)
	}
	if string(got.Body) != `{"x":1}` {
		t.Errorf("body = %q", string(got.Body))
	}
	if got.Headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type = %q", got.Headers["Content-Type"])
	}
	if got.Headers["X-Api-Key"] != "secret" {
		t.Errorf("X-Api-Key = %q", got.Headers["X-Api-Key"])
	}
}

func TestFetchDisabledThrows(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{}) // no Fetch
	_, err := c.Eval(context.Background(), `fetch('https://example.test/')`)
	if err == nil {
		t.Fatal("expected error when fetch is not configured")
	}
}
