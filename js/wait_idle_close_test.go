package js

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestWaitIdleCloseWakesBeforeTransportCleanupCanPublishAResult(t *testing.T) {
	for _, test := range []struct {
		name  string
		frame bool
	}{{"parent only", false}, {"owned frame", true}} {
		t.Run(test.name, func(t *testing.T) { assertWaitIdleCloseWake(t, test.frame) })
	}
}

func assertWaitIdleCloseWake(t *testing.T, ownedFrame bool) {
	t.Helper()
	entered, retired, allowReturn := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var allow sync.Once
	defer allow.Do(func() { close(allowReturn) })
	c := newCtxFromHTMLOpts(t, "<html><body><iframe id='owned' src='/child'></iframe></body></html>", ContextOpts{AsyncFetch: true,
		LoadIFrame: func(ctx context.Context, target string) (*IFrameResponse, error) {
			return &IFrameResponse{URL: target, Body: []byte("<html><body>actual owned child</body></html>")}, nil
		},
		Fetch: func(ctx context.Context, request FetchRequest) (*FetchResponse, error) {
			close(entered)
			<-ctx.Done()
			// Keep cancellation cleanup behind a barrier so no HTTP-result
			// event can accidentally wake the waiter instead of owner close.
			<-allowReturn
			defer close(retired)
			return nil, ctx.Err()
		},
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if ownedFrame {
		value, err := c.Eval(ctx, "document.getElementById('owned').contentDocument.body.textContent")
		if err != nil || value.String() != "actual owned child" {
			t.Fatalf("actual owned frame was not created: %v", err)
		}
		c.iframes.mu.Lock()
		linked := len(c.iframes.ctxs) == 1
		for _, child := range c.iframes.ctxs {
			linked = linked && child != nil && child.rt == c.rt
		}
		c.iframes.mu.Unlock()
		if !linked {
			t.Fatal("frame did not own a real Context sharing the isolate owner")
		}
	}
	if _, err := c.Eval(ctx, "fetch('/pending')"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("actual fetch callback never entered")
	}
	waited := make(chan error, 1)
	go func() { waited <- c.WaitIdle(ctx) }()
	for {
		if !c.rt.ctxMu.TryLock() {
			break
		}
		c.rt.ctxMu.Unlock()
		select {
		case <-time.After(time.Millisecond):
		case <-ctx.Done():
			t.Fatal("waiter never acquired isolate ownership")
		}
	}
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	select {
	case err := <-waited:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter ignored owner close: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("owner close could not wake its pending-fetch waiter")
	}
	allow.Do(func() { close(allowReturn) })
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("owner close retained isolate ownership")
	}
	select {
	case <-retired:
	case <-ctx.Done():
		t.Fatal("controlled fetch cleanup did not retire")
	}
}
