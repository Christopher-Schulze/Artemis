package js

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIFrameCallbacksRetireNativeHandlesAndDeliverActualMessages(t *testing.T) {
	loads := 0
	c := newCtxFromHTMLOpts(t, `<iframe src="/child"></iframe>`, ContextOpts{LoadIFrame: func(ctx context.Context, target string) (*IFrameResponse, error) {
		loads++
		return &IFrameResponse{URL: target, Body: []byte(`<title>before</title><script>addEventListener('message', event => { document.title = event.data.value; })</script>`)}, nil
	}})
	const exercise = `(() => {
		const frame = document.querySelector('iframe');
		for (let index = 0; index < 50; index++) {
			if (!frame.contentDocument) throw Error('actual frame missing');
			frame.contentWindow.postMessage({value: 'delivered'});
			if (frame.contentDocument.title !== 'delivered') throw Error('actual message lost');
		}
	})()`
	if err := c.EvalDiscard(t.Context(), exercise); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	for range 3 {
		if err := c.EvalDiscard(t.Context(), exercise); err != nil {
			t.Fatal(err)
		}
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("iframe callbacks retained native handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
	if loads != 1 {
		t.Fatalf("cached frame loaded %d times", loads)
	}
}

func TestIFrameLoaderFailuresRemainObservableWhenCaught(t *testing.T) {
	for _, test := range []struct {
		name     string
		response *IFrameResponse
		failure  error
		message  string
	}{
		{"transport", nil, errors.New("actual frame transport failure"), "actual frame transport failure"},
		{"nil response", nil, nil, "response"},
		{"missing URL", &IFrameResponse{Body: []byte("actual")}, nil, "URL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			loads := 0
			c := newCtxFromHTMLOpts(t, `<iframe src="/child"></iframe>`, ContextOpts{LoadIFrame: func(context.Context, string) (*IFrameResponse, error) {
				loads++
				return test.response, test.failure
			}})
			for range 2 {
				err := c.EvalDiscard(t.Context(), `try { document.querySelector('iframe').contentDocument; } catch {}`)
				if err == nil || !strings.Contains(err.Error(), test.message) {
					t.Fatalf("iframe failure acknowledged success: %v", err)
				}
			}
			if loads != 1 {
				t.Fatalf("failed frame repeated transport: %d", loads)
			}
		})
	}
}

func TestIFrameMessageOriginUsesActualRedirectedDocument(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<iframe src="/redirect"></iframe>`, ContextOpts{LoadIFrame: func(context.Context, string) (*IFrameResponse, error) {
		return &IFrameResponse{URL: "https://actual-child.test/final", Body: []byte(`<title>before</title><script>addEventListener('message', event => { document.title = event.data + ':' + event.origin; })</script>`)}, nil
	}})
	if err := c.EvalDiscard(t.Context(), `(() => {
		const child = document.querySelector('iframe').contentWindow;
		child.postMessage('default');
		child.postMessage('wrong', 'https://example.test');
	})()`); err != nil {
		t.Fatal(err)
	}
	c.iframes.mu.Lock()
	var child *Context
	for _, owner := range c.iframes.ctxs {
		child = owner
	}
	c.iframes.mu.Unlock()
	if child == nil {
		t.Fatal("actual redirected child missing")
	}
	if child.doc.Title() != "before" {
		t.Fatalf("cross-origin message escaped target restriction: %q", child.doc.Title())
	}
	for _, test := range []struct{ source, title string }{
		{`'explicit', 'https://actual-child.test/path'`, "explicit:https://example.test"},
		{`'wildcard', '*'`, "wildcard:https://example.test"},
		{`'options', {targetOrigin:'https://actual-child.test'}`, "options:https://example.test"},
	} {
		if err := c.EvalDiscard(t.Context(), `document.querySelector('iframe').contentWindow.postMessage(`+test.source+`)`); err != nil {
			t.Fatal(err)
		}
		if child.doc.Title() != test.title {
			t.Fatalf("actual message origin mismatch: %q want %q", child.doc.Title(), test.title)
		}
	}
}

func TestIFrameMessageValidationAndQueueBudgetsPreserveActualOwner(t *testing.T) {
	c := newCtxFromHTMLOpts(t, `<iframe src="/child"></iframe>`, ContextOpts{LoadIFrame: func(ctx context.Context, target string) (*IFrameResponse, error) {
		return &IFrameResponse{URL: target, Body: []byte(`<title>before</title><script>addEventListener('message', event => { document.title = event.data; })</script>`)}, nil
	}})
	if err := c.EvalDiscard(t.Context(), `(() => {
		const frame = document.querySelector('iframe');
		const child = frame.contentWindow;
		const saved = globalThis.JSON;
		globalThis.JSON = null;
		try { child.postMessage('actual'); } finally { globalThis.JSON = saved; }
		if (frame.contentDocument.title !== 'actual') throw Error('captured serialization lost actual message');
		for (const send of [
			() => child.postMessage('x'.repeat(1048576)),
			() => child.postMessage('bad', 'relative'),
			() => child.postMessage('bad', '*', [new ArrayBuffer(1)]),
			() => child.postMessage(undefined),
			() => __iframe_postmessage(999999, '"unknown"'),
			() => __iframe_postmessage(__doc_call('querySelector', 'iframe'), 'invalid JSON'),
			() => __iframe_postmessage(__doc_call('querySelector', 'iframe'), {}),
			() => __iframe_load({valueOf(){ throw Error('must not coerce'); }}),
			() => __iframe_root(1.5),
		]) {
			let rejected = false;
			try { send(); } catch { rejected = true; }
			if (!rejected) throw Error('invalid frame input accepted');
		}
		if (frame.contentDocument.title !== 'actual') throw Error('rejected message delivered');
	})()`); err != nil {
		t.Fatal(err)
	}
	var handle uint32
	for id := range c.iframes.ctxs {
		handle = id
	}
	for range maxIFrameMessages {
		if err := c.iframes.queueMessage(handle, `"queued"`); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.iframes.queueMessage(handle, `"overflow"`); err == nil {
		t.Fatal("message count budget accepted overflow")
	}
	if got := c.iframes.drainMessagesFor(handle); len(got) != maxIFrameMessages {
		t.Fatalf("overflow changed admitted messages: %d", len(got))
	}
	if err := c.iframes.queueMessage(handle, strings.Repeat("x", maxIFrameMessageBytes)); err != nil {
		t.Fatal(err)
	}
	if err := c.iframes.queueMessage(handle, "x"); err == nil {
		t.Fatal("message byte budget accepted overflow")
	}
	c.iframes.drainMessagesFor(handle)
	if c.iframes.messageBytes != 0 || c.iframes.messageCount != 0 || len(c.iframes.pendingMsgs) != 0 {
		t.Fatal("drained queue retained payload accounting or target indexes")
	}
	if err := c.iframes.queueMessage(handle, `"retained until close"`); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err := c.iframes.queueMessage(handle, `"after close"`); err == nil {
		t.Fatal("closed child admitted another message")
	}
	if c.iframes.messageBytes != 0 || c.iframes.messageCount != 0 || len(c.iframes.pendingMsgs) != 0 || len(c.iframes.ctxs) != 0 || len(c.iframes.docs) != 0 || len(c.iframes.failures) != 0 || c.iframes.sourceBytes != 0 {
		t.Fatal("closed registry retained child documents/messages/accounting")
	}
}

func TestIFrameAdmissionBoundsRealChildrenAndSourceBytes(t *testing.T) {
	loads := 0
	c := newCtxFromHTMLOpts(t, `<div></div>`, ContextOpts{LoadIFrame: func(ctx context.Context, target string) (*IFrameResponse, error) {
		loads++
		return &IFrameResponse{URL: target}, nil
	}})
	if err := c.EvalDiscard(t.Context(), fmt.Sprintf(`for(let index=0;index<%d;index++){ const frame=document.createElement('iframe'); frame.src='/child'; document.body.appendChild(frame); if(!frame.contentDocument)throw Error('empty actual document rejected'); }`, maxIFrameContexts)); err != nil {
		t.Fatal(err)
	}
	if loads != maxIFrameContexts || len(c.iframes.ctxs) != maxIFrameContexts {
		t.Fatal("actual child ownership missing")
	}
	if err := c.EvalDiscard(t.Context(), `const overflow=document.createElement('iframe');overflow.src='/overflow';document.body.appendChild(overflow);overflow.contentDocument;`); err == nil || !strings.Contains(err.Error(), "admission") {
		t.Fatalf("child overflow accepted: %v", err)
	}
	if loads != maxIFrameContexts {
		t.Fatal("over-capacity child reached transport")
	}
	large := newCtxFromHTMLOpts(t, `<iframe src="/large"></iframe>`, ContextOpts{LoadIFrame: func(ctx context.Context, target string) (*IFrameResponse, error) {
		return &IFrameResponse{URL: target, Body: make([]byte, maxIFrameSourceBytes+1)}, nil
	}})
	if err := large.EvalDiscard(t.Context(), `document.querySelector('iframe').contentDocument`); err == nil || !strings.Contains(err.Error(), "source byte") {
		t.Fatalf("oversized frame accepted: %v", err)
	}
	if len(large.iframes.ctxs) != 0 || len(large.iframes.docs) != 0 || large.iframes.sourceBytes != 0 {
		t.Fatal("rejected body retained parsed document/child ownership")
	}
}

func TestIFrameMessageOriginSerializesCanonicalHTTPOrigins(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"https://EXAMPLE.test:443/path", "https://example.test"},
		{"http://example.test:80/path", "http://example.test"},
		{"https://example.test:8443/path", "https://example.test:8443"},
		{"http://[::1]:80/path", "http://[::1]"},
		{"https://[::1]:8443/path", "https://[::1]:8443"},
		{"https://bücher.test/path", "https://xn--bcher-kva.test"},
		{"about:blank", "null"},
	} {
		t.Run(test.input, func(t *testing.T) {
			if got := iframeMessageOrigin(test.input); got != test.want {
				t.Fatalf("origin=%q want %q", got, test.want)
			}
		})
	}
}
