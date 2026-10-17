package js

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestFetchResponseBodiesConsumeOnceAndCloneIndependently(t *testing.T) {
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: staticFetcher(t, "actual body", 200)})
	if err := c.EvalDiscard(t.Context(), `
		globalThis.trace = [];
		fetch('/body').then(async response => {
			globalThis.response = response;
			const clone = response.clone();
			trace.push(response.bodyUsed, clone.bodyUsed);
			trace.push(await response.text(), response.bodyUsed, clone.bodyUsed);
			try { await response.text(); trace.push('invalid-second-read'); } catch (error) { trace.push(error instanceof TypeError); }
			try { response.clone(); trace.push('invalid-used-clone'); } catch (error) { trace.push(error instanceof TypeError); }
			trace.push(await clone.text(), clone.bodyUsed);
		});
	`); err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalString(t.Context(), `trace.join('|')`)
	if err != nil || got != "false|false|actual body|true|false|true|true|actual body|true" {
		t.Fatalf("response body state or clone independence failed: %q %v", got, err)
	}
	if c.fetchBodyBytes != 0 || len(c.callbackValues) != 0 {
		t.Fatalf("consumed bodies retained bytes or callback handles: bytes=%d handles=%d", c.fetchBodyBytes, len(c.callbackValues))
	}
}

func TestFetchResponseAbortAfterHandoffPreservesReasonAcrossClones(t *testing.T) {
	for _, asynchronous := range []bool{false, true} {
		for _, method := range []string{"text", "json", "arrayBuffer", "bytes"} {
			t.Run(fmt.Sprintf("async=%t/%s", asynchronous, method), func(t *testing.T) {
				c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{AsyncFetch: asynchronous, Fetch: staticFetcher(t, `{"actual":42}`, 200)})
				if err := c.EvalDiscard(t.Context(), `globalThis.controller=new AbortController(), reason='TypeError: actual abort reason', response=null, cloned=null, observed=''; fetch('/handoff',{signal:controller.signal}).then(r=>{response=r;cloned=r.clone()});`); err != nil {
					t.Fatal(err)
				}
				if err := c.WaitIdle(t.Context()); err != nil {
					t.Fatal(err)
				}
				if err := c.EvalDiscard(t.Context(), "controller.abort(reason)"); err != nil {
					t.Fatal(err)
				}
				if c.fetchBodyBytes != 0 {
					t.Errorf("abort retained inaccessible payload bytes: %d", c.fetchBodyBytes)
				}
				if err := c.EvalDiscard(t.Context(), `(async()=>{const result=[response.bodyUsed,cloned.bodyUsed]; for(const body of [response,cloned]){try{await body.`+method+`();result.push('invalid success')}catch(error){result.push(error===reason,body.bodyUsed)}} observed=result.join(':')})()`); err != nil {
					t.Fatal(err)
				}
				got, err := c.EvalString(t.Context(), "observed")
				if err != nil || got != "false:false:true:false:true:false" || len(c.async.signals) != 0 || c.fetchBodyBytes != 0 {
					t.Fatalf("handed-off body/clone lost abort reason/state/retirement: %q %v signals=%d bytes=%d", got, err, len(c.async.signals), c.fetchBodyBytes)
				}
			})
		}
	}
}

func TestFetchResponseFailedCloneWrapDoesNotRetainInvisibleBody(t *testing.T) {
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: staticFetcher(t, "actual body", 200)})
	if err := c.EvalDiscard(t.Context(), `globalThis.observed=''; fetch('/clone',{signal:new AbortController().signal}).then(async response=>{
		const iterator=Headers.prototype[Symbol.iterator];
		Headers.prototype[Symbol.iterator]=function*(){throw Error('clone-header-failure')};
		try{response.clone();observed='invalid success'}catch(error){observed=String(error)}finally{Headers.prototype[Symbol.iterator]=iterator}
		await response.text();
	});`); err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalString(t.Context(), "observed")
	if err != nil || !strings.Contains(got, "clone-header-failure") || c.fetchBodyBytes != 0 || len(c.async.signals) != 0 {
		t.Fatalf("failed clone retained invisible body/signal or lost failure: %q %v bytes=%d signals=%d", got, err, c.fetchBodyBytes, len(c.async.signals))
	}
}

func TestFetchResponseAbortKeepsUsedAndClosedBodySemantics(t *testing.T) {
	for _, status := range []int{200, 204} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: staticFetcher(t, "", status)})
			if err := c.EvalDiscard(t.Context(), `globalThis.observed=''; const controller=new AbortController(), reason={actual:42}; fetch('/empty',{signal:controller.signal}).then(async response=>{controller.abort(reason); try{observed=(await response.text())+':'+response.bodyUsed}catch(error){observed=(error===reason)+':'+response.bodyUsed}});`); err != nil {
				t.Fatal(err)
			}
			want := "true:false"
			if status == 204 {
				want = ":false"
			}
			got, err := c.EvalString(t.Context(), "observed")
			if err != nil || got != want || len(c.async.signals) != 0 {
				t.Fatalf("closed/null body changed after abort: %q want=%q err=%v signals=%d", got, want, err, len(c.async.signals))
			}
		})
	}
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: staticFetcher(t, "actual body", 200)})
	if err := c.EvalDiscard(t.Context(), `globalThis.observed=''; const controller=new AbortController(), reason={actual:42}; fetch('/used',{signal:controller.signal}).then(async response=>{
		const clone=response.clone(); await response.text(); controller.abort(reason);
		let usedError=false, cloneReason=false;
		try{await response.text()}catch(error){usedError=error instanceof TypeError}
		try{await clone.text()}catch(error){cloneReason=error===reason}
		observed=usedError+':'+cloneReason;
	});`); err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalString(t.Context(), "observed")
	if err != nil || got != "true:true" || c.fetchBodyBytes != 0 || len(c.async.signals) != 0 {
		t.Fatalf("used body or unread clone lost distinct state/retirement: %q %v", got, err)
	}
}

func TestFetchResponseRepeatedAbortReadsRetainOnlyReasonUntilClose(t *testing.T) {
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: staticFetcher(t, "actual body", 200)})
	if err := c.EvalDiscard(t.Context(), `globalThis.controller=new AbortController(), reason={actual:42}, response=null, observed=''; fetch('/repeated',{signal:controller.signal}).then(value=>{response=value});`); err != nil {
		t.Fatal(err)
	}
	var watch *fetchSignalWatch
	for _, retained := range c.async.signals {
		watch = retained
	}
	if watch == nil {
		t.Fatal("unread response did not retain its actual signal owner")
	}
	baseline := c.rt.iso.RetainedValueCount()
	if err := c.EvalDiscard(t.Context(), `controller.abort(reason); (async()=>{
		const checks=[];
		for(let index=0;index<3;index++)for(const body of [response,response.clone()]){
			try{await body.text();checks.push('invalid success')}catch(error){checks.push(error===reason && !body.bodyUsed)}
		}
		observed=checks.join(':');
	})()`); err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalString(t.Context(), "observed")
	if err != nil || got != "true:true:true:true:true:true" || len(c.async.signals) != 0 || c.fetchBodyBytes != 0 {
		t.Fatalf("repeat aborted read/clone lost reason, disturbed state or cancellation retirement: %q %v", got, err)
	}
	if count := c.rt.iso.RetainedValueCount(); count != baseline {
		t.Fatalf("repeat rejected reads leaked internal-isolate handles: got=%d baseline=%d", count, baseline)
	}
	c.Close()
	if watch.value != nil || watch.algorithms != nil || watch.key != nil {
		t.Fatal("closed body owner retained its exact abort-reason handles")
	}
}

func TestFetchResponseBinaryAndJSONAreActualConversions(t *testing.T) {
	for _, test := range []struct {
		name, body, source, want string
	}{
		{"array buffer", "\x00\xff\x80A", `r => r.arrayBuffer().then(value => [value instanceof ArrayBuffer, value.byteLength, ...new Uint8Array(value)].join(':'))`, "true:4:0:255:128:65"},
		{"bytes", "\x00\xff\x80A", `r => r.bytes().then(value => [value instanceof Uint8Array, ...value].join(':'))`, "true:0:255:128:65"},
		{"text BOM", "\xef\xbb\xbfactual\x00text", `r => r.text()`, "actual\x00text"},
		{"invalid UTF8", "\x80\x80A", `r => r.text()`, "\ufffd\ufffdA"},
		{"JSON BOM", "\xef\xbb\xbf{\"actual\":42}", `r => r.json().then(value => String(value.actual))`, "42"},
		{"JSON NUL", "{}\x00ignored", `r => r.json().then(() => 'invalid success', error => String(error instanceof SyntaxError) + ':' + r.bodyUsed)`, "true:true"},
		{"invalid JSON", "{", `r => r.json().then(() => 'invalid success', error => String(error instanceof SyntaxError) + ':' + r.bodyUsed)`, "true:true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: staticFetcher(t, test.body, 200)})
			if err := c.EvalDiscard(t.Context(), `globalThis.observed = ''; fetch('/data').then(`+test.source+`).then(value => { observed = value; });`); err != nil {
				t.Fatal(err)
			}
			if got, err := c.EvalString(t.Context(), "observed"); err != nil || got != test.want {
				t.Fatalf("conversion changed original bytes or acknowledged malformed data: %q %v", got, err)
			}
		})
	}
}

func TestFetchResponseMetadataAndReceiverCannotBeForged(t *testing.T) {
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: func(ctx context.Context, request FetchRequest) (*FetchResponse, error) {
		return &FetchResponse{Status: 201, StatusText: "Created", URL: request.URL, Body: []byte("owned"), Headers: map[string][]string{
			"X-ACTUAL": {"first", "second"}, "Set-Cookie": {"HttpOnly=secret; HttpOnly"},
		}}, nil
	}})
	if err := c.EvalDiscard(t.Context(), `
		globalThis.observed = '';
		fetch('/actual').then(async r => {
			r.status = 500; r.ok = false; r.headers = {}; r.bodyUsed = true;
			let wrongReceiver;
			try { await r.text.call(localStorage); wrongReceiver = 'accepted'; } catch (error) { wrongReceiver = error instanceof TypeError; }
			observed = [r.status, r.ok, r.bodyUsed, r.headers.get('x-actual'), r.headers.get('SET-COOKIE'), wrongReceiver, await r.text()].join('|');
		});
	`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), "observed"); err != nil || got != "201|true|false|first, second||true|owned" {
		t.Fatalf("response metadata/receiver lost its actual owner: %q %v", got, err)
	}
}

func TestFetchNullBodyAndPayloadRetirement(t *testing.T) {
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: staticFetcher(t, "", 204)})
	if err := c.EvalDiscard(t.Context(), `globalThis.observed = ''; fetch('/empty').then(async r => { observed = [await r.text(), r.bodyUsed, (await r.arrayBuffer()).byteLength, r.bodyUsed].join(':'); });`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), "observed"); err != nil || got != ":false:0:false" {
		t.Fatalf("null-body consumption fabricated a used stream: %q %v", got, err)
	}
	c.Close()
	if c.fetchBodyBytes != 0 || len(c.fetchBodyIDs) != 0 || c.responseFactory != nil || c.responseTrue != nil || c.responseFalse != nil {
		t.Fatal("closed response owner retained payload, handles or bound functions")
	}
}

func TestFetchResponseValidationAndRetainedBytesAreBounded(t *testing.T) {
	for _, test := range []struct {
		name string
		resp FetchResponse
	}{
		{"invalid status", FetchResponse{Status: 101}},
		{"invalid header", FetchResponse{Status: 200, Headers: map[string][]string{"Bad Header": {"value"}}}},
		{"injected value", FetchResponse{Status: 200, Headers: map[string][]string{"X-Actual": {"one\r\ninjected: yes"}}}},
		{"null body bytes", FetchResponse{Status: 204, Body: []byte("forbidden")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: func(context.Context, FetchRequest) (*FetchResponse, error) { return &test.resp, nil }})
			if err := c.EvalDiscard(t.Context(), `globalThis.observed = ''; fetch('/hostile').then(() => { observed = 'invalid success'; }, error => { observed = String(error); });`); err != nil {
				t.Fatal(err)
			}
			if got, err := c.EvalString(t.Context(), "observed"); err != nil || !strings.Contains(got, "fetch:") || c.fetchBodyBytes != 0 {
				t.Fatalf("hostile response gained retained body or success: %q %v bytes=%d", got, err, c.fetchBodyBytes)
			}
		})
	}
	policy, err := network.NewPolicy(network.PolicyConfig{MaxResponseBodyBytes: 5}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: policy, Fetch: staticFetcher(t, "1234", 200)})
	if err := c.EvalDiscard(t.Context(), `globalThis.kept = null, rejected = ''; fetch('/first').then(r => { kept = r; }); fetch('/second').catch(error => { rejected = String(error); });`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), "rejected"); err != nil || !strings.Contains(got, "byte limit") || c.fetchBodyBytes != 4 {
		t.Fatalf("retained body quota was not enforced: %q %v bytes=%d", got, err, c.fetchBodyBytes)
	}
	c.Close()
	if c.fetchBodyBytes != 0 {
		t.Fatal("close did not retire retained payload bytes")
	}
}
