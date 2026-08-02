package js

import (
	"context"
	"strings"
	"testing"
)

func TestFetchWithFormDataBody(t *testing.T) {
	var got FetchRequest
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: func(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
			got = req
			return &FetchResponse{Status: 200, Body: []byte("ok")}, nil
		},
	})
	if _, err := c.Eval(context.Background(), `
		const fd = new FormData();
		fd.append('user', 'ada');
		fd.append('email', 'a@b.test');
		fetch('https://e.test/login', {method: 'POST', body: fd});
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got.Method != "POST" {
		t.Errorf("method = %q", got.Method)
	}
	body := string(got.Body)
	if !strings.Contains(body, "user=ada") || !strings.Contains(body, "email=a%40b.test") {
		t.Errorf("body = %q (expected urlencoded form data)", body)
	}
	if got.Headers["Content-Type"] != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q (expected auto-set)", got.Headers["Content-Type"])
	}
}

func TestFetchFormDataPreservesCallerContentTypeCaseInsensitively(t *testing.T) {
	var got FetchRequest
	c := newCtxFromHTMLOpts(t, `<html></html>`, ContextOpts{
		Fetch: func(_ context.Context, req FetchRequest) (*FetchResponse, error) {
			got = req
			return &FetchResponse{Status: 200, Body: []byte("ok")}, nil
		},
	})
	if _, err := c.Eval(context.Background(), `
		const fd = new FormData();
		fd.append('user', 'ada');
		fetch('https://e.test/login', {
			method: 'POST',
			headers: {'content-type': 'application/custom'},
			body: fd,
		});
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got.Headers["content-type"] != "application/custom" {
		t.Fatalf("caller content type = %q", got.Headers["content-type"])
	}
	if _, duplicate := got.Headers["Content-Type"]; duplicate {
		t.Fatalf("auto content type duplicated caller header: %#v", got.Headers)
	}
}
