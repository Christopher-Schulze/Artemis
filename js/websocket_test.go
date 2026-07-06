package js

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func startEchoWS(t *testing.T) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			typ, data, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), typ, data); err != nil {
				return
			}
		}
	}))
	url := strings.Replace(srv.URL, "http://", "ws://", 1)
	return url, func() { srv.Close() }
}

func TestWebSocketEcho(t *testing.T) {
	url, stop := startEchoWS(t)
	defer stop()

	doc, _ := parser.ParseHTML(strings.NewReader(`<html></html>`), "http://e.test/")
	rt := NewRuntime()
	defer rt.Close()
	c, _ := rt.NewContext(doc, ContextOpts{AsyncFetch: true})
	defer c.Close()

	if _, err := c.Eval(context.Background(), `
		var trace = '';
		const ws = new WebSocket(`+jsStringLit(url)+`);
		ws.onopen = () => { trace += 'open;'; ws.send('hi'); };
		ws.onmessage = (ev) => { trace += 'msg:' + ev.data + ';'; ws.close(); };
		ws.onclose = () => { trace += 'close;'; };
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}

	deadline, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		if err := c.WaitIdle(deadline); err != nil {
			t.Fatalf("WaitIdle: %v", err)
		}
		// async + ws share the same Inflight=0 stop condition; for ws we
		// need to wait until close fires. Poll the trace.
		v, _ := c.Eval(context.Background(), `trace`)
		if strings.Contains(v.String(), "close;") {
			if !strings.Contains(v.String(), "open;") || !strings.Contains(v.String(), "msg:hi;") {
				t.Errorf("trace = %q (missing open or msg)", v.String())
			}
			return
		}
		if deadline.Err() != nil {
			t.Fatalf("timed out before close; trace = %q", v.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWebSocketReadyStateTransitions(t *testing.T) {
	url, stop := startEchoWS(t)
	defer stop()

	doc, _ := parser.ParseHTML(strings.NewReader(`<html></html>`), "http://e.test/")
	rt := NewRuntime()
	defer rt.Close()
	c, _ := rt.NewContext(doc, ContextOpts{AsyncFetch: true})
	defer c.Close()

	if _, err := c.Eval(context.Background(), `
		var states = [];
		const ws = new WebSocket(`+jsStringLit(url)+`);
		states.push(ws.readyState);
		ws.onopen = () => { states.push(ws.readyState); ws.close(); };
		ws.onclose = () => { states.push(ws.readyState); };
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}

	deadline, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_ = c.WaitIdle(deadline)
		v, _ := c.Eval(context.Background(), `states.join(',')`)
		if strings.Contains(v.String(), "3") {
			if !strings.HasPrefix(v.String(), "0,") {
				t.Errorf("first state should be 0 (CONNECTING): %q", v.String())
			}
			if !strings.Contains(v.String(), "1") {
				t.Errorf("missing OPEN(1): %q", v.String())
			}
			return
		}
		if deadline.Err() != nil {
			t.Fatalf("timeout, states=%q", v.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWebSocketSendAfterClose(t *testing.T) {
	url, stop := startEchoWS(t)
	defer stop()

	doc, _ := parser.ParseHTML(strings.NewReader(`<html></html>`), "http://e.test/")
	rt := NewRuntime()
	defer rt.Close()
	c, _ := rt.NewContext(doc, ContextOpts{AsyncFetch: true})
	defer c.Close()

	// open + close + try send → should throw InvalidStateError
	if _, err := c.Eval(context.Background(), `
		var threw = false;
		const ws = new WebSocket(`+jsStringLit(url)+`);
		ws.onopen = () => {
			ws.close();
			try { ws.send('after'); } catch (e) { threw = (e.name === 'InvalidStateError'); }
		};
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	deadline, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i := 0; i < 100; i++ {
		_ = c.WaitIdle(deadline)
		v, _ := c.Eval(context.Background(), `threw`)
		if v.Bool() {
			return
		}
		if deadline.Err() != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("send after close did not throw InvalidStateError")
}

// silence unused
var _ sync.Mutex
