package js

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestXHRCallbackFailuresRemainObservableWithoutStoppingLaterDelivery(t *testing.T) {
	for _, asynchronous := range []bool{false, true} {
		for _, test := range []struct{ name, source, want string }{
			{"load", `xhr.onload=()=>{trace+='load';throw Error('actual-xhr-callback')}`, "4:load"},
			{"ready", `xhr.onreadystatechange=()=>{if(xhr.readyState===4){trace+='ready,';throw Error('actual-xhr-callback')}};xhr.onload=()=>trace+='load'`, "4:ready,load"},
			{"open", `xhr.onreadystatechange=()=>{if(xhr.readyState===1){trace+='open,';throw Error('actual-xhr-callback')}};xhr.onload=()=>trace+='load'`, "4:open,load"},
			{"error", `xhr.onerror=()=>{trace+='error';throw Error('actual-xhr-callback')}`, "4:error"},
		} {
			t.Run(test.name+map[bool]string{false: "/sync", true: "/async"}[asynchronous], func(t *testing.T) {
				requests := make(chan FetchRequest, 1)
				c := newCtxFromHTMLOpts(t, `<body></body>`, ContextOpts{AsyncFetch: asynchronous, Fetch: func(ctx context.Context, request FetchRequest) (*FetchResponse, error) {
					select {
					case requests <- request:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					if test.name == "error" {
						return nil, errors.New("actual transport failure")
					}
					return &FetchResponse{Status: 200, Body: []byte("actual-body")}, nil
				}})
				failure := c.EvalDiscard(t.Context(), `globalThis.trace='';globalThis.xhr=new XMLHttpRequest();`+test.source+`;xhr.open('GET','/actual');xhr.send()`)
				failure = errors.Join(failure, c.WaitIdle(t.Context()))
				var listener *EventListenerError
				if !errors.As(failure, &listener) || !strings.Contains(failure.Error(), "actual-xhr-callback") {
					t.Fatalf("XHR callback exception acknowledged success: %v", failure)
				}
				select {
				case request := <-requests:
					if request.URL != "https://example.test/actual" {
						t.Fatalf("XHR lost its real request: %+v", request)
					}
				default:
					t.Fatal("earlier callback failure prevented the request")
				}
				if got, err := c.EvalString(t.Context(), `xhr.readyState+':'+trace`); err != nil || got != test.want {
					t.Fatalf("callback failure prevented later state or delivery: %q %v", got, err)
				}
			})
		}
	}
}

func TestGeolocationDenialCallbacksCannotDiscardFailures(t *testing.T) {
	for _, method := range []string{"getCurrentPosition", "watchPosition"} {
		t.Run(method, func(t *testing.T) {
			c := newCtxFromHTML(t, `<body></body>`, nil)
			err := c.EvalDiscard(t.Context(), `globalThis.denied=false;globalThis.token=navigator.geolocation.`+method+`(null,error=>{denied=error.code===1;throw Error('actual-denial-callback')})`)
			var listener *EventListenerError
			if !errors.As(err, &listener) || !strings.Contains(err.Error(), "actual-denial-callback") {
				t.Fatalf("denial callback failure disappeared: %v", err)
			}
			want := "true:undefined"
			if method == "watchPosition" {
				want = "true:0"
			}
			if got, err := c.EvalString(t.Context(), `denied+':'+String(token)`); err != nil || got != want {
				t.Fatalf("denial behavior changed or error did not drain: %q %v", got, err)
			}
		})
	}
}
