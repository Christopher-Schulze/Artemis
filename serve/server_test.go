package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/network"
)

const testAuthToken = "artemis-test-token"

func testAgentConfig() artemis.AgentConfig {
	return artemis.AgentConfig{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: allTestPorts()}}
}

func allTestPorts() []int {
	ports := make([]int, 65535)
	for i := range ports {
		ports[i] = i + 1
	}
	return ports
}

func writeTestResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := fmt.Fprint(w, body); err != nil {
		t.Errorf("write fixture response: %v", err)
	}
}

func closeTestWebsocketNow(t *testing.T, c *websocket.Conn) {
	t.Helper()
	if err := c.CloseNow(); err != nil {
		t.Errorf("close websocket: %v", err)
	}
}

func closeTestWebsocket(t *testing.T, c *websocket.Conn) {
	t.Helper()
	if err := c.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Errorf("close websocket: %v", err)
	}
}

func stopTestAgent(t *testing.T, agent *artemis.Agent) {
	t.Helper()
	if err := agent.Stop(); err != nil {
		t.Errorf("stop agent: %v", err)
	}
}

func stopTestStreamingServer(t *testing.T, server *StreamingServer) {
	t.Helper()
	if err := server.Stop(); err != nil {
		t.Errorf("stop streaming server: %v", err)
	}
}

func startServer(t *testing.T) (string, func()) {
	t.Helper()
	agent, err := artemis.NewAgent(testAgentConfig())
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if startErr := agent.Start(ctx); startErr != nil {
		cancel()
		t.Fatalf("agent start: %v", startErr)
	}
	srv := New(agent, Opts{AuthToken: testAuthToken, RateLimit: testRateLimit()})
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	httpServer := &http.Server{Handler: http.HandlerFunc(srv.handleWS), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := httpServer.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Errorf("serve test server: %v", serveErr)
		}
	}()
	cleanup := func() {
		if closeErr := httpServer.Close(); closeErr != nil {
			t.Errorf("close test server: %v", closeErr)
		}
		cancel()
		stopTestAgent(t, agent)
	}
	return addr, cleanup
}

func dial(t *testing.T, addr string) *websocket.Conn {
	t.Helper()
	opts := &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + testAuthToken}}}
	c, response, err := websocket.Dial(context.Background(), "ws://"+addr+"/", opts)
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Match the server's 8MB read limit so large page dumps don't
	// hit the default 32KB client-side limit.
	c.SetReadLimit(8 << 20)
	return c
}

func testRateLimit() RateLimit {
	return RateLimit{RequestsPerSecond: 1000, Burst: 1000, ClientRequestsPerMinute: 60000, ClientBurst: 1000, ClientBucketTTL: time.Hour}
}

// startServerWithAuth mirrors startServer but enforces a bearer AuthToken on
// every connection, so the auth gate in handleWS can be exercised directly.
func startServerWithAuth(t *testing.T, token string) (string, func()) {
	t.Helper()
	agent, err := artemis.NewAgent(testAgentConfig())
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if startErr := agent.Start(ctx); startErr != nil {
		cancel()
		t.Fatalf("agent start: %v", startErr)
	}
	srv := New(agent, Opts{AuthToken: token, RateLimit: testRateLimit()})
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	httpServer := &http.Server{Handler: http.HandlerFunc(srv.handleWS), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := httpServer.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Errorf("serve test server: %v", serveErr)
		}
	}()
	cleanup := func() {
		if closeErr := httpServer.Close(); closeErr != nil {
			t.Errorf("close test server: %v", closeErr)
		}
		cancel()
		stopTestAgent(t, agent)
	}
	return addr, cleanup
}

// TestServerAuthTokenEnforced is a failable proof that the serve bearer-token
// gate rejects missing and wrong tokens and admits the correct one. If the auth
// check in handleWS is removed or weakened, the missing/wrong-token cases start
// connecting and fail the test.
func TestServerAuthTokenEnforced(t *testing.T) {
	addr, cleanup := startServerWithAuth(t, "s3cret")
	defer cleanup()

	// No Authorization header -> rejected.
	if c, response, err := websocket.Dial(context.Background(), "ws://"+addr+"/", nil); response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
		if err == nil {
			closeTestWebsocket(t, c)
			t.Fatal("connection without token was accepted, want rejected")
		}
	} else if err == nil {
		closeTestWebsocket(t, c)
		t.Fatal("connection without token was accepted, want rejected")
	}

	// Wrong token -> rejected.
	badOpts := &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer wrong"}}}
	if c, response, err := websocket.Dial(context.Background(), "ws://"+addr+"/", badOpts); response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
		if err == nil {
			closeTestWebsocket(t, c)
			t.Fatal("connection with wrong token was accepted, want rejected")
		}
	} else if err == nil {
		closeTestWebsocket(t, c)
		t.Fatal("connection with wrong token was accepted, want rejected")
	}

	// Correct token -> accepted.
	goodOpts := &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer s3cret"}}}
	c, response, err := websocket.Dial(context.Background(), "ws://"+addr+"/", goodOpts)
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatalf("connection with correct token was rejected: %v", err)
	}
	closeTestWebsocket(t, c)
}

func roundTrip(t *testing.T, c *websocket.Conn, req Request) Response {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if writeErr := c.Write(context.Background(), websocket.MessageText, body); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
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
		writeTestResponse(t, w, `<!doctype html><html><head><title>SrvTest</title></head><body><h1>Hi</h1></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()

	c := dial(t, addr)
	defer closeTestWebsocketNow(t, c)

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

	closeS, err := json.Marshal(SessionCloseParams{SessionID: sid})
	if err != nil {
		t.Fatalf("marshal session close: %v", err)
	}
	if r := roundTrip(t, c, Request{ID: "6", Cmd: "session.close", Params: closeS}); !r.OK {
		t.Errorf("session.close: %+v", r)
	}
}

func TestUnknownCommand(t *testing.T) {
	addr, cleanup := startServer(t)
	defer cleanup()
	c := dial(t, addr)
	defer closeTestWebsocketNow(t, c)
	resp := roundTrip(t, c, Request{ID: "1", Cmd: "garbage"})
	if resp.OK || resp.Error == nil || resp.Error.Code != "unknown_cmd" {
		t.Errorf("got %+v, want unknown_cmd", resp)
	}
}

func TestServerLifecycleListenAndShutdown(t *testing.T) {
	agent, err := artemis.NewAgent(testAgentConfig())
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("agent start: %v", err)
	}
	defer stopTestAgent(t, agent)
	srv := New(agent, Opts{AuthToken: testAuthToken, RateLimit: testRateLimit()})
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
		writeTestResponse(t, w, `<!doctype html><html><body><input id="q" type="text" value=""></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer closeTestWebsocketNow(t, c)

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
		writeTestResponse(t, w, `<!doctype html><html><body></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer closeTestWebsocketNow(t, c)

	typeP := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"selector":"#missing","text":"x"}`, sid, pid))
	resp := roundTrip(t, c, Request{ID: "t", Cmd: "page.type", Params: typeP})
	if resp.OK || resp.Error == nil || resp.Error.Code != "type_failed" {
		t.Errorf("got %+v, want type_failed", resp)
	}
}

func TestPageAssertSelectorExists(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestResponse(t, w, `<!doctype html><html><body><h1 id="hi">Hi</h1></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer closeTestWebsocketNow(t, c)

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
		writeTestResponse(t, w, `<!doctype html><html><head><title>Hello World</title></head><body></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer closeTestWebsocketNow(t, c)

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
		writeTestResponse(t, w, `<!doctype html><html><body></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer closeTestWebsocketNow(t, c)

	p := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q,"mode":"bogus"}`, sid, pid))
	resp := roundTrip(t, c, Request{ID: "a", Cmd: "page.assert", Params: p})
	if resp.OK || resp.Error == nil || resp.Error.Code != "bad_mode" {
		t.Errorf("got %+v, want bad_mode", resp)
	}
}

func TestPageWaitIdle(t *testing.T) {
	// A page with no JS context (runScripts=false) WaitIdle returns nil.
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestResponse(t, w, `<!doctype html><html><body><h1>Static</h1></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	sid, pid, c := openSessionPage(t, addr, page.URL)
	defer closeTestWebsocketNow(t, c)

	p := []byte(fmt.Sprintf(`{"sessionId":%q,"pageId":%q}`, sid, pid))
	resp := roundTrip(t, c, Request{ID: "w", Cmd: "page.wait_idle", Params: p})
	if !resp.OK {
		t.Fatalf("page.wait_idle: %+v", resp)
	}
}
