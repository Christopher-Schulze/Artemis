package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Christopher-Schulze/Artemis/engine"
)

func startServer(t *testing.T) (string, func()) {
	t.Helper()
	eng, err := engine.New(engine.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	srv := New(eng, Opts{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	go func() {
		_ = (&http.Server{Handler: http.HandlerFunc(srv.handleWS)}).Serve(ln)
	}()
	cleanup := func() {
		_ = ln.Close()
		_ = eng.Close()
	}
	return addr, cleanup
}

func dial(t *testing.T, addr string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(context.Background(), "ws://"+addr+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Match the server's 8MB read limit so large page dumps don't
	// hit the default 32KB client-side limit.
	c.SetReadLimit(8 << 20)
	return c
}

func roundTrip(t *testing.T, c *websocket.Conn, req Request) Response {
	t.Helper()
	body, _ := json.Marshal(req)
	if err := c.Write(context.Background(), websocket.MessageText, body); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, data, err := c.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return resp
}

func TestSessionOpenEvalDump(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>SrvTest</title></head><body><h1>Hi</h1></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()

	c := dial(t, addr)
	defer c.CloseNow()

	resp := roundTrip(t, c, Request{ID: "1", Cmd: "session.new"})
	if !resp.OK {
		t.Fatalf("session.new: %+v", resp)
	}
	sid := resp.Value.(map[string]any)["sessionId"].(string)

	open := []byte(fmt.Sprintf(`{"sessionId":%q,"url":%q}`, sid, page.URL))
	resp = roundTrip(t, c, Request{ID: "2", Cmd: "page.open", Params: open})
	if !resp.OK {
		t.Fatalf("page.open: %+v", resp)
	}
	pid := resp.Value.(map[string]any)["pageId"].(string)
	title := resp.Value.(map[string]any)["title"].(string)
	if title != "SrvTest" {
		t.Errorf("title = %q", title)
	}

	evalP := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"expr":%q}`, sid, pid, "document.title"))
	resp = roundTrip(t, c, Request{ID: "3", Cmd: "page.eval", Params: evalP})
	if !resp.OK {
		t.Fatalf("page.eval: %+v", resp)
	}
	got := resp.Value.(map[string]any)["value"].(string)
	if got != "SrvTest" {
		t.Errorf("eval value = %q", got)
	}

	dumpP := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"format":"markdown"}`, sid, pid))
	resp = roundTrip(t, c, Request{ID: "4", Cmd: "page.dump", Params: dumpP})
	if !resp.OK {
		t.Fatalf("page.dump: %+v", resp)
	}
	md := resp.Value.(map[string]any)["data"].(string)
	if !strings.Contains(md, "# Hi") {
		t.Errorf("markdown = %q", md)
	}

	closeP := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q}`, sid, pid))
	if r := roundTrip(t, c, Request{ID: "5", Cmd: "page.close", Params: closeP}); !r.OK {
		t.Errorf("page.close: %+v", r)
	}

	closeS := []byte(fmt.Sprintf(`{"sessionId":%q}`, sid))
	if r := roundTrip(t, c, Request{ID: "6", Cmd: "session.close", Params: closeS}); !r.OK {
		t.Errorf("session.close: %+v", r)
	}
}

func TestUnknownCommand(t *testing.T) {
	addr, cleanup := startServer(t)
	defer cleanup()
	c := dial(t, addr)
	defer c.CloseNow()
	resp := roundTrip(t, c, Request{ID: "1", Cmd: "garbage"})
	if resp.OK || resp.Error == nil || resp.Error.Code != "unknown_cmd" {
		t.Errorf("got %+v, want unknown_cmd", resp)
	}
}

func TestServerLifecycleListenAndShutdown(t *testing.T) {
	eng, _ := engine.New(engine.Config{})
	defer eng.Close()
	srv := New(eng, Opts{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if err := srv.ListenAndServe(ctx, "127.0.0.1:0"); err != nil && err != context.Canceled {
		t.Errorf("serve err: %v", err)
	}
}

// helper: open a session + page against an httptest server and return
// sessionId, pageId, and the connected websocket.
func openSessionPage(t *testing.T, addr string, pageURL string) (sid, pid string, c *websocket.Conn) {
	t.Helper()
	c = dial(t, addr)
	resp := roundTrip(t, c, Request{ID: "s", Cmd: "session.new"})
	if !resp.OK {
		t.Fatalf("session.new: %+v", resp)
	}
	sid = resp.Value.(map[string]any)["sessionId"].(string)
	open := []byte(fmt.Sprintf(`{"sessionId":%q,"url":%q}`, sid, pageURL))
	resp = roundTrip(t, c, Request{ID: "p", Cmd: "page.open", Params: open})
	if !resp.OK {
		t.Fatalf("page.open: %+v", resp)
	}
	pid = resp.Value.(map[string]any)["pageId"].(string)
	return sid, pid, c
}

func TestPageType(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body><input id="q" type="text" value=""></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer c.CloseNow()

	typeP := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"selector":"#q","text":"hello"}`, sid, pid))
	resp := roundTrip(t, c, Request{ID: "t", Cmd: "page.type", Params: typeP})
	if !resp.OK {
		t.Fatalf("page.type: %+v", resp)
	}

	// Verify the value was set via page.eval.
	evalP := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"expr":"document.getElementById('q').value"}`, sid, pid))
	resp = roundTrip(t, c, Request{ID: "v", Cmd: "page.eval", Params: evalP})
	if !resp.OK {
		t.Fatalf("page.eval: %+v", resp)
	}
	got := resp.Value.(map[string]any)["value"].(string)
	if got != "hello" {
		t.Errorf("input value = %q, want hello", got)
	}
}

func TestPageTypeMissingSelectorErrors(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer c.CloseNow()

	typeP := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"selector":"#missing","text":"x"}`, sid, pid))
	resp := roundTrip(t, c, Request{ID: "t", Cmd: "page.type", Params: typeP})
	if resp.OK || resp.Error == nil || resp.Error.Code != "type_failed" {
		t.Errorf("got %+v, want type_failed", resp)
	}
}

func TestPageAssertSelectorExists(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body><h1 id="hi">Hi</h1></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer c.CloseNow()

	// Positive: selector exists.
	p1 := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"mode":"selector_exists","selector":"#hi"}`, sid, pid))
	resp := roundTrip(t, c, Request{ID: "a1", Cmd: "page.assert", Params: p1})
	if !resp.OK {
		t.Fatalf("page.assert: %+v", resp)
	}
	v := resp.Value.(map[string]any)
	if v["pass"] != true {
		t.Errorf("pass = %v, want true", v["pass"])
	}

	// Negative: selector missing, want=false.
	p2 := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"mode":"selector_exists","selector":"#nope","want":false}`, sid, pid))
	resp = roundTrip(t, c, Request{ID: "a2", Cmd: "page.assert", Params: p2})
	if !resp.OK {
		t.Fatalf("page.assert: %+v", resp)
	}
	v = resp.Value.(map[string]any)
	if v["pass"] != true {
		t.Errorf("pass = %v, want true (selector missing, want=false)", v["pass"])
	}

	// Negative assertion that should fail: selector missing, want=true (default).
	p3 := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"mode":"selector_exists","selector":"#nope"}`, sid, pid))
	resp = roundTrip(t, c, Request{ID: "a3", Cmd: "page.assert", Params: p3})
	if !resp.OK {
		t.Fatalf("page.assert: %+v", resp)
	}
	v = resp.Value.(map[string]any)
	if v["pass"] != false {
		t.Errorf("pass = %v, want false (selector missing, want=true default)", v["pass"])
	}
}

func TestPageAssertTitleContains(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>Hello World</title></head><body></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer c.CloseNow()

	p := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"mode":"title_contains","substring":"Hello"}`, sid, pid))
	resp := roundTrip(t, c, Request{ID: "a", Cmd: "page.assert", Params: p})
	if !resp.OK {
		t.Fatalf("page.assert: %+v", resp)
	}
	v := resp.Value.(map[string]any)
	if v["pass"] != true {
		t.Errorf("pass = %v, want true", v["pass"])
	}

	p2 := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"mode":"title_contains","substring":"Nope"}`, sid, pid))
	resp = roundTrip(t, c, Request{ID: "a2", Cmd: "page.assert", Params: p2})
	if !resp.OK {
		t.Fatalf("page.assert: %+v", resp)
	}
	v = resp.Value.(map[string]any)
	if v["pass"] != false {
		t.Errorf("pass = %v, want false", v["pass"])
	}
}

func TestPageAssertBadMode(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer c.CloseNow()

	p := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"mode":"bogus"}`, sid, pid))
	resp := roundTrip(t, c, Request{ID: "a", Cmd: "page.assert", Params: p})
	if resp.OK || resp.Error == nil || resp.Error.Code != "bad_mode" {
		t.Errorf("got %+v, want bad_mode", resp)
	}
}

func TestPageWaitIdle(t *testing.T) {
	// A page with no JS context (runScripts=false) WaitIdle returns nil.
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><body><h1>Static</h1></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer c.CloseNow()

	p := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q}`, sid, pid))
	resp := roundTrip(t, c, Request{ID: "w", Cmd: "page.wait_idle", Params: p})
	if !resp.OK {
		t.Fatalf("page.wait_idle: %+v", resp)
	}
}
