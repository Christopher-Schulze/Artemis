package js

import (
	"context"
	"strings"
	"testing"
)

func TestXHRBasicGET(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: staticFetcher(t, "hello-xhr", 200),
	})
	if _, err := c.Eval(context.Background(), `
		var xhr = new XMLHttpRequest();
		var captured = '';
		xhr.onload = function() { captured = xhr.status + ":" + xhr.responseText; };
		xhr.open('GET', 'https://example.test/x');
		xhr.send();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "200:hello-xhr" {
		t.Errorf("captured = %q", v.String())
	}
}

func TestXHRReadyStateTransitions(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: staticFetcher(t, "ok", 200),
	})
	if _, err := c.Eval(context.Background(), `
		var xhr = new XMLHttpRequest();
		var trace = '';
		xhr.onreadystatechange = function() { trace += xhr.readyState + ','; };
		xhr.open('GET', 'https://example.test/x');
		xhr.send();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `trace`)
	if !strings.Contains(v.String(), "1,") || !strings.Contains(v.String(), "4,") {
		t.Errorf("trace = %q", v.String())
	}
}

func TestXHRPOSTWithBody(t *testing.T) {
	var got FetchRequest
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: func(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
			got = req
			return &FetchResponse{Status: 201, Body: []byte("created")}, nil
		},
	})
	if _, err := c.Eval(context.Background(), `
		var xhr = new XMLHttpRequest();
		xhr.open('POST', 'https://example.test/x');
		xhr.setRequestHeader('Content-Type', 'application/json');
		xhr.send('{"a":1}');
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got.Method != "POST" || string(got.Body) != `{"a":1}` {
		t.Errorf("captured = %+v", got)
	}
	if got.Headers["Content-Type"] != "application/json" {
		t.Errorf("content-type = %q", got.Headers["Content-Type"])
	}
}

func TestXHRErrorFiresOnError(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: func(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
			return nil, context.DeadlineExceeded
		},
	})
	if _, err := c.Eval(context.Background(), `
		var xhr = new XMLHttpRequest();
		var called = false;
		xhr.onerror = function() { called = true; };
		xhr.open('GET', 'https://example.test/x');
		xhr.send();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `called`)
	if !v.Bool() {
		t.Error("onerror not fired")
	}
}
