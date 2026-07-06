package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DiscoveryMethod identifies which Chrome DevTools discovery endpoint was
// used to locate a debuggable target (spec L4266).
type DiscoveryMethod string

const (
	DiscoveryJSONVersion     DiscoveryMethod = "json/version"
	DiscoveryJSONList        DiscoveryMethod = "json/list"
	DiscoveryDevToolsBrowser DiscoveryMethod = "devtools/browser"
)

// DiscoveryResult is a single discovered Chrome debuggable surface.
type DiscoveryResult struct {
	URL            string
	Method         DiscoveryMethod
	BrowserVersion string
	WebSocketURL   string
	Found          bool
}

// jsonVersionResponse mirrors the /json/version payload.
type jsonVersionResponse struct {
	Browser              string `json:"Browser"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// jsonListTarget mirrors a single entry of /json/list.
type jsonListTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// ChromeDiscovery locates Chrome DevTools endpoints by trying three
// fallback methods in order: /json/version, /json/list, /devtools/browser.
type ChromeDiscovery struct {
	client *http.Client
}

// NewChromeDiscovery builds a ChromeDiscovery with a 2s per-request timeout.
func NewChromeDiscovery() *ChromeDiscovery {
	return &ChromeDiscovery{
		client: &http.Client{Timeout: 2 * time.Second},
	}
}

// NewChromeDiscoveryWithClient allows injecting an http.Client (useful for
// tests that want custom transport).
func NewChromeDiscoveryWithClient(client *http.Client) *ChromeDiscovery {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return &ChromeDiscovery{client: client}
}

// Discover tries each fallback method against host:port and returns the
// results in attempt order. At least one Found result indicates success.
func (d *ChromeDiscovery) Discover(host string, port int) ([]DiscoveryResult, error) {
	results := make([]DiscoveryResult, 0, 3)

	if r, err := d.tryJSONVersion(host, port); err == nil {
		results = append(results, r)
		if r.Found {
			return results, nil
		}
	}

	if r, err := d.tryJSONList(host, port); err == nil {
		results = append(results, r)
		if r.Found {
			return results, nil
		}
	}

	if r, err := d.tryDevToolsBrowser(host, port); err == nil {
		results = append(results, r)
		if r.Found {
			return results, nil
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("chrome discovery failed for %s:%d: no endpoint reachable", host, port)
	}
	return results, nil
}

func (d *ChromeDiscovery) tryJSONVersion(host string, port int) (DiscoveryResult, error) {
	url := fmt.Sprintf("http://%s/json/version", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	body, err := d.fetch(url)
	if err != nil {
		return DiscoveryResult{URL: url, Method: DiscoveryJSONVersion, Found: false}, err
	}
	var v jsonVersionResponse
	if err := json.Unmarshal(body, &v); err != nil {
		return DiscoveryResult{URL: url, Method: DiscoveryJSONVersion, Found: false}, err
	}
	return DiscoveryResult{
		URL:            url,
		Method:         DiscoveryJSONVersion,
		BrowserVersion: v.Browser,
		WebSocketURL:   v.WebSocketDebuggerURL,
		Found:          v.WebSocketDebuggerURL != "",
	}, nil
}

func (d *ChromeDiscovery) tryJSONList(host string, port int) (DiscoveryResult, error) {
	url := fmt.Sprintf("http://%s/json/list", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	body, err := d.fetch(url)
	if err != nil {
		return DiscoveryResult{URL: url, Method: DiscoveryJSONList, Found: false}, err
	}
	var targets []jsonListTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return DiscoveryResult{URL: url, Method: DiscoveryJSONList, Found: false}, err
	}
	ws := ""
	if len(targets) > 0 {
		ws = targets[0].WebSocketDebuggerURL
	}
	return DiscoveryResult{
		URL:          url,
		Method:       DiscoveryJSONList,
		WebSocketURL: ws,
		Found:        ws != "",
	}, nil
}

func (d *ChromeDiscovery) tryDevToolsBrowser(host string, port int) (DiscoveryResult, error) {
	wsURL := FormatWebSocketURL(host, port, "/devtools/browser")
	probeURL := fmt.Sprintf("http://%s/devtools/browser", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	// Chrome responds to a plain HTTP GET on the browser WS endpoint with a
	// 400/426 upgrade-required status when the endpoint is live. A 404 or
	// connection error means the browser endpoint is not exposed.
	resp, err := d.client.Get(probeURL)
	if err != nil {
		return DiscoveryResult{URL: wsURL, Method: DiscoveryDevToolsBrowser, Found: false}, err
	}
	defer resp.Body.Close()
	// Drain the body so the underlying connection can be reused; the read
	// result is irrelevant since we only inspect the status code.
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUpgradeRequired || resp.StatusCode == http.StatusSwitchingProtocols {
		return DiscoveryResult{URL: wsURL, Method: DiscoveryDevToolsBrowser, WebSocketURL: wsURL, Found: true}, nil
	}
	return DiscoveryResult{URL: wsURL, Method: DiscoveryDevToolsBrowser, Found: false}, fmt.Errorf("devtools/browser endpoint returned status %d", resp.StatusCode)
}

func (d *ChromeDiscovery) fetch(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// FormatWebSocketURL builds a ws:// URL with correct IPv6 bracketing.
func FormatWebSocketURL(host string, port int, path string) string {
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("ws://%s%s", net.JoinHostPort(host, fmt.Sprintf("%d", port)), path)
}
