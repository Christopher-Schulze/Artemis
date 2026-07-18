package bridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestChromiumNetworkPolicyDeniesPrivateNavigationByDefault(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer fixture.Close()
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{})
	owner, err := browser.NewContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.NewPage(context.Background(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{fixture.URL, strings.Replace(fixture.URL, "127.0.0.1", "localhost", 1)} {
		if _, _, err := page.Navigate(context.Background(), target); !errors.Is(err, network.ErrPolicyDenied) {
			t.Fatalf("navigation %q error=%v", target, err)
		}
	}
	if !browser.Healthy() {
		t.Fatalf("expected denial kept browser healthy: %v", browser.Err())
	}
}

func TestChromiumNetworkPolicyBlocksRedirectSubframeAndWorkerEgress(t *testing.T) {
	var deniedHits atomic.Int64
	denied := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		deniedHits.Add(1)
	}))
	defer denied.Close()
	allowed := newEgressFixture(t, denied.URL)
	defer allowed.Close()
	browser := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{
		AllowPrivateNetworks: true, AllowedPorts: []int{testURLPort(t, allowed.URL)},
	})
	owner, err := browser.NewContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	page, err := owner.NewPage(context.Background(), allowed.URL)
	if err != nil {
		t.Fatal(err)
	}
	waitForEgressAttempts(t, page)
	if _, _, err := page.Navigate(context.Background(), allowed.URL+"/redirect"); err == nil {
		t.Fatal("redirect to denied port succeeded")
	}
	if got := deniedHits.Load(); got != 0 {
		t.Fatalf("denied destination received %d requests", got)
	}
	if !browser.Healthy() {
		t.Fatalf("expected denials kept browser healthy: %v", browser.Err())
	}
}

func newEgressFixture(t *testing.T, deniedURL string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(writer, `<!doctype html><title>egress</title><body><iframe src=%q onload="document.body.dataset.frameDone='true'" onerror="document.body.dataset.frameDone='true'"></iframe><script>
		const worker = new Worker('/worker.js');
		worker.onmessage = () => document.body.dataset.workerDone = 'true';
		</script></body>`, deniedURL)
	})
	mux.HandleFunc("/worker.js", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/javascript")
		_, _ = fmt.Fprintf(writer, `fetch(%q).catch(() => {}).finally(() => postMessage('done'));`, deniedURL)
	})
	mux.HandleFunc("/redirect", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, deniedURL, http.StatusFound)
	})
	return httptest.NewServer(mux)
}

func waitForEgressAttempts(t *testing.T, page *Page) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		var result struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		err := page.Call(ctx, "Runtime.evaluate", map[string]any{
			"expression": "document.body.dataset.workerDone === 'true' && document.body.dataset.frameDone === 'true' ? 'true' : ''", "returnByValue": true,
		}, &result)
		if err == nil && result.Result.Value == "true" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("worker egress attempt did not complete: %v", ctx.Err())
}

func launchPolicyTestBrowser(t *testing.T, config browserprocess.LaunchConfig) *ChromiumBrowser {
	t.Helper()
	binary := requireChromium(t)
	config.BinaryPath = binary.Path
	config.Headless = true
	config.StartupTimeout = 10 * time.Second
	browser, err := LaunchChromium(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := browser.Close(); err != nil {
			t.Errorf("close browser: %v", err)
		}
	})
	return browser
}
