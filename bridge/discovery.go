package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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
	if strings.TrimSpace(host) == "" || port < 1 || port > 65535 {
		return nil, fmt.Errorf("chrome discovery: valid host and port required")
	}
	results := make([]DiscoveryResult, 0, 3)
	var failures error

	if r, err := d.tryJSONVersion(host, port); err == nil {
		results = append(results, r)
		if r.Found {
			return results, nil
		}
	} else {
		failures = errors.Join(failures, fmt.Errorf("json/version: %w", err))
	}

	if r, err := d.tryJSONList(host, port); err == nil {
		results = append(results, r)
		if r.Found {
			return results, nil
		}
	} else {
		failures = errors.Join(failures, fmt.Errorf("json/list: %w", err))
	}

	if r, err := d.tryDevToolsBrowser(host, port); err == nil {
		results = append(results, r)
		if r.Found {
			return results, nil
		}
	} else {
		failures = errors.Join(failures, fmt.Errorf("devtools/browser: %w", err))
	}

	return results, fmt.Errorf("chrome discovery failed for %s:%d: %w", host, port, failures)
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
	webSocketURL := rewriteWebSocketEndpoint(v.WebSocketDebuggerURL, host, port)
	return DiscoveryResult{
		URL:            url,
		Method:         DiscoveryJSONVersion,
		BrowserVersion: v.Browser,
		WebSocketURL:   webSocketURL,
		Found:          validWebSocketEndpoint(webSocketURL),
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
	for _, target := range targets {
		if target.Type == "browser" && target.WebSocketDebuggerURL != "" {
			ws = target.WebSocketDebuggerURL
			break
		}
	}
	if ws == "" {
		for _, target := range targets {
			if target.WebSocketDebuggerURL != "" {
				ws = target.WebSocketDebuggerURL
				break
			}
		}
	}
	webSocketURL := rewriteWebSocketEndpoint(ws, host, port)
	return DiscoveryResult{
		URL:          url,
		Method:       DiscoveryJSONList,
		WebSocketURL: webSocketURL,
		Found:        validWebSocketEndpoint(webSocketURL),
	}, nil
}

func (d *ChromeDiscovery) tryDevToolsBrowser(host string, port int) (result DiscoveryResult, resultErr error) {
	wsURL := FormatWebSocketURL(host, port, "/devtools/browser")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	transport, err := DialCDPTransport(ctx, CDPTransportConfig{URL: wsURL, HTTPClient: d.client})
	if err != nil {
		return DiscoveryResult{URL: wsURL, Method: DiscoveryDevToolsBrowser, Found: false}, err
	}
	defer func() {
		if closeErr := transport.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close DevTools transport: %w", closeErr))
		}
	}()
	var version BrowserVersion
	if err := transport.Call(ctx, "Browser.getVersion", nil, &version); err != nil {
		return DiscoveryResult{URL: wsURL, Method: DiscoveryDevToolsBrowser, Found: false}, err
	}
	if version.Product == "" || version.ProtocolVersion == "" {
		return DiscoveryResult{URL: wsURL, Method: DiscoveryDevToolsBrowser, Found: false}, fmt.Errorf("incomplete Browser.getVersion response")
	}
	return DiscoveryResult{
		URL: wsURL, Method: DiscoveryDevToolsBrowser, BrowserVersion: version.Product, WebSocketURL: wsURL, Found: true,
	}, nil
}

func (d *ChromeDiscovery) fetch(url string) (data []byte, resultErr error) {
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
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close discovery response: %w", closeErr))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d for %s", resp.StatusCode, url)
	}
	const maxDiscoveryResponse = 1024 * 1024
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxDiscoveryResponse+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDiscoveryResponse {
		return nil, fmt.Errorf("response exceeds %d bytes", maxDiscoveryResponse)
	}
	return data, nil
}

func rewriteWebSocketEndpoint(endpoint, host string, port int) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
		return endpoint
	}
	parsed.Host = net.JoinHostPort(host, fmt.Sprintf("%d", port))
	return parsed.String()
}

func validWebSocketEndpoint(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	return err == nil && (parsed.Scheme == "ws" || parsed.Scheme == "wss") && parsed.Host != ""
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
