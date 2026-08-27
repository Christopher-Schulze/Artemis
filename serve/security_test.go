package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	artemis "github.com/Christopher-Schulze/Artemis"
)

func startSecurityServer(t *testing.T, opts Opts) (string, *Server, func()) {
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
	server := New(agent, opts)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatalf("listen: %v", err)
	}
	httpServer := &http.Server{Handler: http.HandlerFunc(server.handleWS)}
	go func() {
		if serveErr := httpServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Errorf("serve security test server: %v", serveErr)
		}
	}()
	cleanup := func() {
		if closeErr := httpServer.Close(); closeErr != nil {
			t.Errorf("close security test server: %v", closeErr)
		}
		cancel()
		stopTestAgent(t, agent)
	}
	return listener.Addr().String(), server, cleanup
}

func dialSecurityClient(t *testing.T, addr, token, clientID, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{"Authorization": []string{"Bearer " + token}}
	if clientID != "" {
		header.Set(ClientIDHeader, clientID)
	}
	if origin != "" {
		header.Set("Origin", origin)
	}
	return websocket.Dial(context.Background(), "ws://"+addr+"/", &websocket.DialOptions{HTTPHeader: header})
}

func TestServeFailsClosedWithoutAuthenticationToken(t *testing.T) {
	agent, err := artemis.NewAgent(testAgentConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := New(agent, Opts{})
	if err := server.ListenAndServe(context.Background(), "127.0.0.1:0"); err == nil || !strings.Contains(err.Error(), "authentication token required") {
		t.Fatalf("ListenAndServe error = %v, want authentication requirement", err)
	}
}

func TestServeRejectsNonLoopbackBindAndHost(t *testing.T) {
	for _, address := range []string{"0.0.0.0:9333", "[::]:9333", "example.com:9333", ":9333"} {
		if err := validateLoopbackAddress(address); err == nil {
			t.Errorf("validateLoopbackAddress(%q) accepted external bind", address)
		}
	}
	for _, address := range []string{"127.0.0.1:9333", "127.0.0.2:0", "[::1]:9333"} {
		if err := validateLoopbackAddress(address); err != nil {
			t.Errorf("validateLoopbackAddress(%q): %v", address, err)
		}
	}

	_, server, cleanup := startSecurityServer(t, Opts{AuthToken: testAuthToken, RateLimit: testRateLimit()})
	defer cleanup()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://evil.example/", nil)
	request.Host = "evil.example"
	recorder := httptest.NewRecorder()
	server.handleWS(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("host rejection status = %d, want 403", recorder.Code)
	}
}

func TestServeRejectsQueryCredentials(t *testing.T) {
	_, server, cleanup := startSecurityServer(t, Opts{AuthToken: testAuthToken, RateLimit: testRateLimit()})
	defer cleanup()
	for _, key := range []string{"token", "auth_token", "access_token", "session_token", "sessionToken", "csrf_token", "csrfToken"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1/?"+key+"=secret", nil)
		recorder := httptest.NewRecorder()
		server.handleWS(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("query credential %q status = %d, want 400", key, recorder.Code)
		}
	}
}

func TestServeOriginRestriction(t *testing.T) {
	addr, _, cleanup := startSecurityServer(t, Opts{AuthToken: testAuthToken, RateLimit: testRateLimit()})
	defer cleanup()

	connection, response, err := dialSecurityClient(t, addr, testAuthToken, "", "https://evil.example")
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err == nil {
		closeTestWebsocketNow(t, connection)
		t.Fatal("disallowed browser origin connected")
	} else if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("disallowed origin status = %v, err = %v", response, err)
	}

	connection, response, err = dialSecurityClient(t, addr, testAuthToken, "", "http://"+addr)
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatalf("loopback origin rejected: %v", err)
	}
	closeTestWebsocketNow(t, connection)
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split server address: %v", err)
	}
	connection, response, err = dialSecurityClient(t, addr, testAuthToken, "", "http://localhost:"+port)
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatalf("configured localhost origin rejected: %v", err)
	}
	closeTestWebsocketNow(t, connection)
}

func TestServeSessionOwnershipAndReconnectCapability(t *testing.T) {
	addr, server, cleanup := startSecurityServer(t, Opts{AuthToken: testAuthToken, RateLimit: testRateLimit()})
	defer cleanup()

	owner, ownerResponse, err := dialSecurityClient(t, addr, testAuthToken, "", "")
	if ownerResponse != nil && ownerResponse.Body != nil {
		if closeErr := ownerResponse.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	clientID := ownerResponse.Header.Get(ClientIDHeader)
	if !validateClientID(clientID, server.clientSigningKey) {
		t.Fatalf("server returned invalid client capability %q", clientID)
	}
	replacement := "0"
	if strings.HasSuffix(clientID, replacement) {
		replacement = "1"
	}
	tampered := clientID[:len(clientID)-1] + replacement
	invalid, response, authErr := dialSecurityClient(t, addr, testAuthToken, tampered, "")
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if authErr == nil {
		closeTestWebsocketNow(t, invalid)
		t.Fatal("tampered client capability connected")
	} else if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered client response = %+v, err = %v", response, authErr)
	}
	created := roundTrip(t, owner, Request{ID: "new", Cmd: string(CmdSessionNew)})
	if !created.OK {
		t.Fatalf("session.new: %+v", created.Error)
	}
	var session SessionNewResult
	if decodeErr := DecodeTypedResult(&created, &session); decodeErr != nil {
		t.Fatal(decodeErr)
	}

	other, otherResponse, err := dialSecurityClient(t, addr, testAuthToken, "", "")
	if otherResponse != nil && otherResponse.Body != nil {
		if closeErr := otherResponse.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	params, err := json.Marshal(PageDumpParams{SessionID: session.SessionID, PageID: "unknown", Format: string(DumpText)})
	if err != nil {
		t.Fatalf("marshal page.dump params: %v", err)
	}
	denied := roundTrip(t, other, Request{ID: "cross", Cmd: string(CmdPageDump), Params: params})
	if denied.OK || denied.Error == nil || denied.Error.Code != string(ErrOwnershipDenied) {
		t.Fatalf("cross-client session access = %+v, want ownership_denied", denied)
	}
	listed := roundTrip(t, other, Request{ID: "list", Cmd: string(CmdSessionList)})
	var list SessionListResult
	if decodeErr := DecodeTypedResult(&listed, &list); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(list.Sessions) != 0 {
		t.Fatalf("other client listed owned sessions: %+v", list.Sessions)
	}
	closeTestWebsocketNow(t, other)
	closeTestWebsocketNow(t, owner)

	resumed, resumedResponse, err := dialSecurityClient(t, addr, testAuthToken, clientID, "")
	if resumedResponse != nil && resumedResponse.Body != nil {
		if closeErr := resumedResponse.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatalf("resume with issued client capability: %v", err)
	}
	defer closeTestWebsocketNow(t, resumed)
	listed = roundTrip(t, resumed, Request{ID: "resume-list", Cmd: string(CmdSessionList)})
	if err := DecodeTypedResult(&listed, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 1 || list.Sessions[0].SessionID != session.SessionID {
		t.Fatalf("resumed client sessions = %+v", list.Sessions)
	}
}

func TestServeRateLimitUsesNormalizedClientAcrossReconnects(t *testing.T) {
	config := RateLimit{
		RequestsPerSecond: 100, Burst: 100,
		ClientRequestsPerMinute: 1, ClientBurst: 1, ClientBucketTTL: time.Hour,
	}
	addr, _, cleanup := startSecurityServer(t, Opts{AuthToken: testAuthToken, RateLimit: config})
	defer cleanup()

	first, firstResponse, err := dialSecurityClient(t, addr, testAuthToken, "", "")
	if firstResponse != nil && firstResponse.Body != nil {
		if closeErr := firstResponse.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatalf("first connection: %v", err)
	}
	defer closeTestWebsocketNow(t, first)
	second, response, err := dialSecurityClient(t, addr, testAuthToken, "", "")
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err == nil {
		closeTestWebsocketNow(t, second)
		t.Fatal("reconnect bypassed normalized-client rate limit")
	}
	if response == nil || response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") == "" {
		t.Fatalf("rate response = %+v, err = %v", response, err)
	}
}

func TestTokenRotationRevokesConnectionsAndOwnedSessions(t *testing.T) {
	addr, _, cleanup := startSecurityServer(t, Opts{AuthToken: testAuthToken, RateLimit: testRateLimit()})
	defer cleanup()
	connection, connectionResponse, err := dialSecurityClient(t, addr, testAuthToken, "", "")
	if connectionResponse != nil && connectionResponse.Body != nil {
		if closeErr := connectionResponse.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	created := roundTrip(t, connection, Request{ID: "new", Cmd: string(CmdSessionNew)})
	if !created.OK {
		t.Fatalf("session.new: %+v", created.Error)
	}
	rotated := roundTrip(t, connection, Request{ID: "rotate", Cmd: string(CmdTokenRotate)})
	var result TokenRotateResult
	if decodeErr := DecodeTypedResult(&rotated, &result); decodeErr != nil {
		t.Fatalf("token.rotate: %v", decodeErr)
	}
	if result.Token == "" || result.Token == testAuthToken {
		t.Fatalf("rotated token = %q", result.Token)
	}
	closeTestWebsocketNow(t, connection)

	oldConnection, response, authErr := dialSecurityClient(t, addr, testAuthToken, "", "")
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if authErr == nil {
		closeTestWebsocketNow(t, oldConnection)
		t.Fatal("old token remained valid after rotation")
	} else if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old token response = %+v, err = %v", response, authErr)
	}
	newConnection, newResponse, err := dialSecurityClient(t, addr, result.Token, "", "")
	if newResponse != nil && newResponse.Body != nil {
		if closeErr := newResponse.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatalf("new token rejected: %v", err)
	}
	defer closeTestWebsocketNow(t, newConnection)
	listed := roundTrip(t, newConnection, Request{ID: "list", Cmd: string(CmdSessionList)})
	var sessions SessionListResult
	if err := DecodeTypedResult(&listed, &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions.Sessions) != 0 {
		t.Fatalf("rotation retained prior authenticated sessions: %+v", sessions.Sessions)
	}
}

func TestNormalizeClientAddressUnmapsAndStripsPort(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:51000":       "127.0.0.1",
		"127.0.0.1:51001":       "127.0.0.1",
		"[::ffff:127.0.0.1]:80": "127.0.0.1",
		"[::1]:9333":            "::1",
	}
	for input, want := range cases {
		if got := normalizeClientAddress(input); got != want {
			t.Errorf("normalizeClientAddress(%q) = %q, want %q", input, got, want)
		}
	}
}
