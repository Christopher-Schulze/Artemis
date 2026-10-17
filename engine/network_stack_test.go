package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

func TestObeyRobotsBlocksDisallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			writeTestBody(t, w, "User-agent: *\nDisallow: /secret/\n")
		case "/secret/page":
			writeTestBody(t, w, "<html>secret</html>")
		default:
			writeTestBody(t, w, "<html>ok</html>")
		}
	}))
	defer srv.Close()

	cfg := testConfig(srv)
	cfg.ObeyRobots = true
	eng, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	// Allowed
	p, err := eng.Fetch(context.Background(), srv.URL+"/", FetchOpts{})
	if err != nil {
		t.Fatalf("fetch /: %v", err)
	}
	closeTestResource(t, "allowed page close", p.Close)

	// Disallowed
	_, err = eng.Fetch(context.Background(), srv.URL+"/secret/page", FetchOpts{})
	if !errors.Is(err, ErrRobotsDisallowed) {
		t.Errorf("expected ErrRobotsDisallowed, got %v", err)
	}
}

func TestBlockPrivateIPs(t *testing.T) {
	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	_, err = eng.Fetch(context.Background(), "http://127.0.0.1/", FetchOpts{})
	if err == nil {
		t.Error("expected error for loopback")
	}
}

func TestOnRequestInterceptionMocks(t *testing.T) {
	eng, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	intercepted := false
	page, err := eng.Fetch(context.Background(), "https://example.test/", FetchOpts{
		OnRequest: func(req *RequestInfo) (*ResponseInfo, error) {
			intercepted = true
			return &ResponseInfo{
				Status:   200,
				Headers:  http.Header{"Content-Type": []string{"text/html"}},
				Body:     []byte(`<html><body><h1>Mocked</h1></body></html>`),
				FinalURL: req.URL,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer closeTestResource(t, "page close", page.Close)
	if !intercepted {
		t.Error("OnRequest not invoked")
	}
	if !strings.Contains(page.HTML(), "Mocked") {
		t.Errorf("mock body not used: %s", page.HTML())
	}
}

func TestDocumentCookieGetSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "k=v; Path=/; SameSite=Lax")
		writeTestBody(t, w, `<!doctype html><html><body><script>
			globalThis.captured = document.cookie;
			document.cookie = "extra=1; Path=/";
		</script></body></html>`)
	}))
	defer srv.Close()

	eng, err := New(testConfig(srv))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)

	page, err := eng.Fetch(context.Background(), srv.URL, FetchOpts{RunInlineScripts: true})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer closeTestResource(t, "page close", page.Close)

	v, err := page.Eval(context.Background(), `globalThis.captured`)
	if err != nil {
		t.Fatalf("eval captured: %v", err)
	}
	if !strings.Contains(v.String(), "k=v") {
		t.Errorf("captured cookie = %q", v.String())
	}

	v, err = page.Eval(context.Background(), `document.cookie`)
	if err != nil {
		t.Fatalf("eval cookie: %v", err)
	}
	if !strings.Contains(v.String(), "extra=1") {
		t.Errorf("cookie after JS set = %q", v.String())
	}
}

func TestEnginePropagatesNetworkPolicyToJavaScriptWebSocket(t *testing.T) {
	eng, err := New(Config{SessionID: "engine-ws-policy"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(context.Background(), "https://example.test/", FetchOpts{
		OnRequest: func(req *RequestInfo) (*ResponseInfo, error) {
			return &ResponseInfo{
				Status:   http.StatusOK,
				Headers:  http.Header{"Content-Type": []string{"text/html"}},
				Body:     []byte(`<html></html>`),
				FinalURL: req.URL,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page close", page.Close)
	if _, err := page.Eval(context.Background(), `
		var enginePolicyTrace = '';
		const ws = new WebSocket('ws://127.0.0.1:80/socket');
		ws.onerror = () => { enginePolicyTrace += 'error;'; };
		ws.onclose = () => { enginePolicyTrace += 'close;'; };
	`); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		value, err := page.Eval(context.Background(), `enginePolicyTrace`)
		if err != nil {
			t.Fatalf("Eval trace: %v", err)
		}
		if strings.Contains(value.String(), "close;") {
			if !strings.Contains(value.String(), "error;") {
				t.Fatalf("policy denial did not emit error: %q", value.String())
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for WebSocket denial: %q", value.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestEngineRoutesJavaScriptWebSocketThroughConfiguredProxy(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var direct atomic.Int64
	go func() {
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			direct.Add(1)
			conn.Close()
		}
	}()
	defer closeTestResource(t, "target listener close", target.Close)
	proxied := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case proxied <- r.Method + " " + r.URL.String():
		default:
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	targetPort := target.Addr().(*net.TCPAddr).Port
	proxyPort := proxy.Listener.Addr().(*net.TCPAddr).Port
	eng, err := New(Config{SessionID: "engine-ws-proxy", ProxyURL: proxy.URL,
		PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{targetPort, proxyPort}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(context.Background(), "https://example.test/", FetchOpts{
		OnRequest: func(req *RequestInfo) (*ResponseInfo, error) {
			return &ResponseInfo{Status: http.StatusOK, Headers: http.Header{"Content-Type": []string{"text/html"}}, Body: []byte(`<html></html>`), FinalURL: req.URL}, nil
		},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeTestResource(t, "page close", page.Close)
	socketURL := fmt.Sprintf("ws://127.0.0.1:%d/socket", targetPort)
	if _, err := page.Eval(context.Background(), `new WebSocket('`+socketURL+`')`); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	select {
	case request := <-proxied:
		if request != fmt.Sprintf("GET http://127.0.0.1:%d/socket", targetPort) {
			t.Fatalf("proxy saw the wrong WebSocket handshake: %q", request)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("JavaScript WebSocket bypassed the configured proxy")
	}
	if direct.Load() != 0 {
		t.Fatalf("WebSocket also connected directly to the target: %d", direct.Load())
	}
	// Spec L4148: a proxied hostname is resolved by the proxy, never locally.
	hostURL := fmt.Sprintf("ws://unresolvable-ws.invalid:%d/socket", targetPort)
	if _, err := page.Eval(context.Background(), `new WebSocket('`+hostURL+`')`); err != nil {
		t.Fatalf("Eval hostname socket: %v", err)
	}
	select {
	case request := <-proxied:
		if request != fmt.Sprintf("GET http://unresolvable-ws.invalid:%d/socket", targetPort) {
			t.Fatalf("proxy saw the wrong hostname handshake: %q", request)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("proxied WebSocket hostname was resolved locally instead of reaching the proxy")
	}
}

func TestEngineOwnsPersistentDNSCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "browser", "dns_cache.db")
	eng, err := New(Config{SessionID: "engine-dns-cache", DNSCachePath: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("engine did not open the persistent DNS cache: %v", err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("engine close did not release the DNS cache: %v", err)
	}
}

func TestEngineSignsEveryRequestHopWithWebBotAuth(t *testing.T) {
	priv, pub, _, err := network.GenerateWebBotAuthKey()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := network.NewWebBotAuth(network.WebBotAuthConfig{Enabled: true, AgentURL: "https://agent.example.com", PrivateKey: priv})
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := network.NewWebBotAuthVerifier(pub, 0)
	if err != nil {
		t.Fatal(err)
	}
	verified := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := verifier.Verify(r); err != nil {
			verified <- "rejected " + r.URL.Path + ": " + err.Error()
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		verified <- r.URL.Path
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/page", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		writeTestBody(t, w, "<html><title>signed</title></html>")
	}))
	defer server.Close()
	config := testConfig(server)
	config.WebBotAuth = signer
	eng, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "engine close", eng.Close)
	page, err := eng.Fetch(t.Context(), server.URL+"/start", FetchOpts{})
	if err != nil {
		t.Fatalf("signed fetch: %v", err)
	}
	defer closeTestResource(t, "page close", page.Close)
	hops := make([]string, 0, 2)
	for len(hops) < 2 {
		select {
		case hop := <-verified:
			hops = append(hops, hop)
		case <-time.After(5 * time.Second):
			t.Fatalf("missing request hops: %v", hops)
		}
	}
	if hops[0] != "/start" || hops[1] != "/page" {
		t.Fatalf("not every hop carried a valid fresh signature: %v", hops)
	}
}
