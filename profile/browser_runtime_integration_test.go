package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestBrowserRuntimePersistentCookieAndStorageAcrossRestart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.SetCookie(w, &http.Cookie{Name: "auth", Value: "retained", Path: "/", MaxAge: 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if cookie, err := r.Cookie("auth"); err == nil && cookie.Value == "retained" {
			_, _ = w.Write([]byte(`<!doctype html><title>profile fixture</title><main>authenticated</main>`))
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><title>profile fixture</title><form method="post"><input name="username"><input name="password" type="password"><button>Sign in</button></form>`))
	}))
	defer server.Close()
	root := t.TempDir()
	manager, err := NewRuntimeManager(root)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewBrowserRuntime(manager)
	if err != nil {
		t.Fatal(err)
	}
	request := OpenSessionRequest{ProfileID: "persistent-fixture", OwnerUserRef: "owner", Class: ProfilePersistent, Lifetime: time.Hour}
	launch := browserprocess.LaunchConfig{
		Headless: true, StartupTimeout: 15 * time.Second, ShutdownTimeout: 5 * time.Second,
		AllowPrivateNetworks: true, AllowedPorts: []int{profileTestURLPort(t, server.URL)},
	}
	first, err := runtime.Open(context.Background(), request, launch)
	if err != nil {
		t.Fatalf("launch Chromium: %v", err)
	}
	_, page, err := runtime.NewPage(context.Background(), first.ID, "owner", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	waitDocumentReady(t, page, server.URL)
	var set struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
	}
	if err := page.Call(context.Background(), "Runtime.evaluate", map[string]any{"expression": `fetch("/",{method:"POST"}).then(()=>{localStorage.setItem("token","retained");return true})`, "returnByValue": true, "awaitPromise": true}, &set); err != nil {
		t.Fatal(err)
	}
	assertStoredState(t, page)
	time.Sleep(500 * time.Millisecond)
	if err := runtime.Close(context.Background(), first.ID, "owner"); err != nil {
		t.Fatal(err)
	}

	second, err := runtime.Open(context.Background(), request, launch)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background(), second.ID, "owner")
	_, restored, err := runtime.NewPage(context.Background(), second.ID, "owner", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	waitDocumentReady(t, restored, server.URL)
	var result struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := restored.Call(context.Background(), "Runtime.evaluate", map[string]any{"expression": `document.body.textContent.trim()+"|"+localStorage.getItem("token")`, "returnByValue": true}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Result.Value != "authenticated|retained" {
		t.Fatalf("persistent state lost: %q", result.Result.Value)
	}
}

func profileTestURLPort(t *testing.T, rawURL string) int {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestMeasureEnvironmentUsesChromiumValues(t *testing.T) {
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	browser, err := browserprocess.Launch(ctx, browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	transportBrowser, err := bridge.ConnectChromium(ctx, browser.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	defer transportBrowser.Close()
	browserContext, err := transportBrowser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer browserContext.Close()
	facts, err := measureEnvironment(ctx, browserContext)
	if err != nil {
		t.Fatal(err)
	}
	if !facts.Measured || facts.UserAgent == "" || facts.Platform == "" || facts.Locale == "" || facts.Timezone == "" || facts.ViewportWidth <= 0 || facts.ViewportHeight <= 0 || facts.DevicePixelRatio <= 0 || facts.HardwareConcurrency <= 0 {
		t.Fatalf("incomplete measured environment: %+v", facts)
	}
}

type cdpPageCaller interface {
	Call(context.Context, string, any, any) error
}

func waitDocumentReady(t *testing.T, page cdpPageCaller, expectedURL string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var result struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err := page.Call(context.Background(), "Runtime.evaluate", map[string]any{"expression": `document.readyState+"|"+location.href`, "returnByValue": true}, &result); err == nil && result.Result.Value == "complete|"+expectedURL+"/" {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("document did not become ready")
}

func assertStoredState(t *testing.T, page cdpPageCaller) {
	t.Helper()
	var result struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := page.Call(context.Background(), "Runtime.evaluate", map[string]any{"expression": `localStorage.getItem("token")`, "returnByValue": true}, &result); err != nil {
		t.Fatal(err)
	}
	if result.Result.Value != "retained" {
		t.Fatalf("state was not written: %q", result.Result.Value)
	}
}
