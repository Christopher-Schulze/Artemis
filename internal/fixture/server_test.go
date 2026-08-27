package fixture

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestServerIndex(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.BaseURL(), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "Artemis Fixture Server") {
		t.Errorf("index missing server title")
	}
	if !strings.Contains(string(body), "html-001") {
		t.Errorf("index missing scenario html-001")
	}
}

func TestServerStaticHTML(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/html-001"), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET html-001: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "HTML Fixture") {
		t.Errorf("body missing title")
	}
}

func TestServerRedirectChain(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	client := &http.Client{CheckRedirect: nil}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/redirect-001"), nil)
	if err != nil {
		t.Fatalf("new redirect request: %v", err)
	}
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET redirect-001: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Request.URL.Path != "/redirect-final" {
		t.Errorf("final path = %q, want /redirect-final", resp.Request.URL.Path)
	}
}

func TestServerCookiePersistence(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	firstRequest, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/cookie-001"), nil)
	if requestErr != nil {
		t.Fatalf("new cookie-001 request: %v", requestErr)
	}
	firstResponse, requestErr := client.Do(firstRequest)
	if requestErr != nil {
		t.Fatalf("GET cookie-001: %v", requestErr)
	}
	if closeErr := firstResponse.Body.Close(); closeErr != nil {
		t.Fatalf("close cookie-001 response: %v", closeErr)
	}
	secondRequest, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/cookie-002"), nil)
	if err != nil {
		t.Fatalf("new cookie-002 request: %v", err)
	}
	resp, err := client.Do(secondRequest)
	if err != nil {
		t.Fatalf("GET cookie-002: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "fixture-test=1") {
		t.Errorf("cookie not echoed: %q", string(body))
	}
}

func TestServerBasicAuth(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/auth-basic-001"), nil)
	if err != nil {
		t.Fatalf("new auth request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET auth: %v", err)
	}
	if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
		t.Fatalf("discard unauthorized response: %v", copyErr)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("close unauthorized response: %v", closeErr)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/auth-basic-001"), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.SetBasicAuth("fixture", "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET auth with credentials: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "Authorized") {
		t.Errorf("body missing Authorized: %q", string(body))
	}
}

func TestServerFormPOST(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	form := url.Values{
		"name":  {"Alice"},
		"email": {"alice@fixture.test"},
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL("/form-submit"), strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new form request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST form: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "Alice") || !strings.Contains(string(body), "alice@fixture.test") {
		t.Errorf("body missing form values: %q", string(body))
	}
}

func TestServerFileUpload(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	var b strings.Builder
	mw := multipart.NewWriter(&b)
	fw, err := mw.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, writeErr := fw.Write([]byte("hello fixture")); writeErr != nil {
		t.Fatalf("write form file: %v", writeErr)
	}
	if closeErr := mw.Close(); closeErr != nil {
		t.Fatalf("close multipart writer: %v", closeErr)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL("/file-upload"), strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST file: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "13") || !strings.Contains(string(body), "hello fixture") {
		t.Errorf("body missing upload result: %q", string(body))
	}
}

func TestServerWebSocketEcho(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	wsURL := strings.Replace(s.URL("/ws-001"), "http://", "ws://", 1)
	ctx := context.Background()
	c, response, err := websocket.Dial(ctx, wsURL, nil)
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer closeTestResource(t, "websocket close", c.CloseNow)

	if writeErr := c.Write(ctx, websocket.MessageText, []byte("hello")); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}
	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText || string(data) != "hello" {
		t.Errorf("echo = %q (%d), want hello", string(data), typ)
	}
}

func TestServerSlow(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	start := time.Now()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/slow-001"), nil)
	if err != nil {
		t.Fatalf("new slow request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET slow: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if time.Since(start) < 400*time.Millisecond {
		t.Errorf("slow response returned too quickly")
	}
}

func TestServerCrash(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/crash-001"), nil)
	if err != nil {
		t.Fatalf("new crash request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET crash: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestServerMalformed(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/malformed-001"), nil)
	if err != nil {
		t.Fatalf("new malformed request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET malformed: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "Start") {
		t.Errorf("body missing Start: %q", string(body))
	}
}

func TestServerScenarioByPath(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	sc := s.ScenarioByPath("/html-001")
	if sc == nil {
		t.Fatal("ScenarioByPath returned nil")
	}
	if sc.ID != "html-001" {
		t.Errorf("ID = %q, want html-001", sc.ID)
	}
}

func TestServerPolicyConfigAllowsPort(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	cfg := s.PolicyConfig()
	if !cfg.AllowPrivateNetworks {
		t.Error("AllowPrivateNetworks not set")
	}
	if len(cfg.AllowedPorts) == 0 {
		t.Fatal("AllowedPorts empty")
	}
	found := false
	for _, p := range cfg.AllowedPorts {
		if p == 80 || p == 443 {
			continue
		}
		found = true
	}
	if !found {
		t.Error("fixture port not in AllowedPorts")
	}
}

func TestServerChallenge(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/challenge-001"), nil)
	if err != nil {
		t.Fatalf("new challenge request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET challenge: %v", err)
	}
	if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
		t.Fatalf("discard forbidden response: %v", copyErr)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("close forbidden response: %v", closeErr)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/challenge-001"), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Answer", "2")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET challenge with answer: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "Challenge passed") {
		t.Errorf("body missing success: %q", string(body))
	}
}

func TestServerLarge(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/large-001"), nil)
	if err != nil {
		t.Fatalf("new large request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET large: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "End") {
		t.Errorf("body missing End")
	}
}

func TestServerURL(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	if s.URL("/foo") != s.BaseURL()+"/foo" {
		t.Errorf("URL mismatch")
	}
	if s.URL("foo") != s.BaseURL()+"/foo" {
		t.Errorf("URL missing leading slash")
	}
	if s.URL("") != s.BaseURL() {
		t.Errorf("empty URL mismatch")
	}
}

func TestServerFormGET(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/form-search?q=fixture"), nil)
	if err != nil {
		t.Fatalf("new form search request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET search: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "fixture") {
		t.Errorf("body missing query: %q", string(body))
	}
}

func TestServerSPAJSON(t *testing.T) {
	s := NewServerWithDefaults()
	defer s.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL("/api/spa-data"), nil)
	if err != nil {
		t.Fatalf("new API request: %v", err)
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET api: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("response body close: %v", closeErr)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "Hello SPA") {
		t.Errorf("body missing Hello SPA: %q", string(body))
	}
}
