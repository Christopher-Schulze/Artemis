package js

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestFetchRequestFiltersForbiddenWireAuthority(t *testing.T) {
	for _, name := range []string{"Accept-Charset", "Accept-Encoding", "Access-Control-Request-Headers", "Access-Control-Request-Method", "Connection", "Content-Length", "Cookie", "Cookie2", "Date", "DNT", "Expect", "Host", "Keep-Alive", "Origin", "Referer", "Set-Cookie", "TE", "Trailer", "Transfer-Encoding", "Upgrade", "Via", "Proxy-Actual", "sEc-Actual"} {
		t.Run(name, func(t *testing.T) {
			var got FetchRequest
			c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: func(_ context.Context, request FetchRequest) (*FetchResponse, error) {
				got = request
				return &FetchResponse{Status: 200}, nil
			}})
			encoded, err := json.Marshal(map[string]string{name: "page-supplied", "X-Actual": "\u00ff"})
			if err != nil {
				t.Fatal(err)
			}
			if err := c.EvalDiscard(t.Context(), `fetch('/wire',{headers:`+string(encoded)+`})`); err != nil {
				t.Fatal(err)
			}
			if len(got.Headers) != 1 || got.Headers["X-Actual"] != "\xff" {
				t.Fatalf("page gained forbidden wire authority or Latin1 changed: %#v", got.Headers)
			}
		})
	}
}

func TestFetchRequestMethodOverrideUsesQuotedHeaderList(t *testing.T) {
	for _, test := range []struct {
		value     string
		forbidden bool
	}{
		{"GET, \tTrAcE ", true}, {"CONNECT", true}, {"PATCH, TRACK", true},
		{`"GET, TRACE", PATCH`, false}, {`"escaped\", TRACE", PATCH`, false}, {`"unterminated, TRACE`, false}, {"PATCH", false},
	} {
		for _, name := range []string{"X-HTTP-Method", "X-HTTP-Method-Override", "X-Method-Override"} {
			if got := forbiddenFetchHeader(name, test.value); got != test.forbidden {
				t.Errorf("%s=%q forbidden=%t want=%t", name, test.value, got, test.forbidden)
			}
		}
	}
	for _, name := range []string{"Authorization", "Permissions-Policy", "User-Agent", "X-Actual"} {
		if forbiddenFetchHeader(name, "TRACE") {
			t.Errorf("unrelated header rejected: %s", name)
		}
	}
}

func TestFetchRequestRejectsConversionFailuresBeforeTransport(t *testing.T) {
	for _, test := range []struct{ name, input, options, want string }{
		{"URL coercion", `({toString(){throw Error('URL conversion')}})`, `{}`, "URL conversion"},
		{"method getter", `'/data'`, `{get method(){throw Error('method getter')}}`, "method getter"},
		{"method coercion", `'/data'`, `{method:{toString(){throw Error('method conversion')}}}`, "method conversion"},
		{"headers getter", `'/data'`, `{get headers(){throw Error('headers getter')}}`, "headers getter"},
		{"header value", `'/data'`, `{headers:{get 'X-Actual'(){throw Error('header conversion')}}}`, "header conversion"},
		{"body getter", `'/data'`, `{method:'POST',get body(){throw Error('body getter')}}`, "body getter"},
		{"body coercion", `'/data'`, `{method:'POST',body:{toString(){throw Error('body conversion')}}}`, "body conversion"},
		{"invalid method", `'/data'`, `{method:'POST injected'}`, "method"},
		{"forbidden method", `'/data'`, `{method:'TrAcE'}`, "method"},
		{"GET body", `'/data'`, `{body:'actual'}`, "body"},
		{"HEAD body", `'/data'`, `{method:'HEAD',body:''}`, "body"},
		{"invalid headers", `'/data'`, `{headers:42}`, "headers"},
		{"signal getter", `'/data'`, `{get signal(){throw Error('signal getter')}}`, "signal getter"},
		{"forged signal", `'/data'`, `{signal:{aborted:false}}`, "AbortSignal"},
		{"primitive signal", `'/data'`, `{signal:42}`, "AbortSignal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: func(context.Context, FetchRequest) (*FetchResponse, error) {
				calls++
				return &FetchResponse{Status: 200}, nil
			}})
			if err := c.EvalDiscard(t.Context(), `globalThis.observed=''; fetch(`+test.input+`,`+test.options+`).then(()=>{observed='invalid success'},e=>{observed=String(e)});`); err != nil {
				t.Fatal(err)
			}
			got, err := c.EvalString(t.Context(), "observed")
			if err != nil || !strings.Contains(got, test.want) || calls != 0 {
				t.Fatalf("conversion reached transport or lost failure: %q %v calls=%d", got, err, calls)
			}
		})
	}
}

func TestFetchRequestCopiesExactBinaryViewAndEnforcesPolicy(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       []byte
		rejected   bool
	}{
		{"array buffer", `new Uint8Array([0,255,128,65]).buffer`, []byte{0, 255, 128, 65}, false},
		{"view offset", `new Uint8Array(new Uint8Array([9,0,255,128,8]).buffer,1,3)`, []byte{0, 255, 128}, false},
		{"data view", `new DataView(new Uint8Array([9,0,255,8]).buffer,1,2)`, []byte{0, 255}, false},
		{"string budget", `'12345'`, nil, true},
		{"buffer budget", `new Uint8Array(5)`, nil, true},
		{"shared view", `new Uint8Array(new SharedArrayBuffer(2))`, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy, err := network.NewPolicy(network.PolicyConfig{MaxRequestBodyBytes: 4}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var requests []FetchRequest
			c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: policy, Fetch: func(_ context.Context, request FetchRequest) (*FetchResponse, error) {
				requests = append(requests, request)
				return &FetchResponse{Status: 200}, nil
			}})
			if err := c.EvalDiscard(t.Context(), `globalThis.observed=''; fetch('/binary',{method:'POST',body:`+test.body+`}).then(()=>{observed='sent'},e=>{observed=String(e)});`); err != nil {
				t.Fatal(err)
			}
			got, err := c.EvalString(t.Context(), "observed")
			if err != nil {
				t.Fatal(err)
			}
			if test.rejected {
				if len(requests) != 0 || got == "sent" || got == "" {
					t.Fatalf("invalid body reached transport: %q requests=%d", got, len(requests))
				}
			} else if len(requests) != 1 || !bytes.Equal(requests[0].Body, test.want) {
				t.Fatalf("binary region changed: requests=%v want=%v result=%q", requests, test.want, got)
			}
		})
	}
}
