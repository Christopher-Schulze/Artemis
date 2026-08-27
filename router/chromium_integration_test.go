package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	artemisobserve "github.com/Christopher-Schulze/Artemis/observe"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestChromiumExecutorAgainstRealChromiumFixture(t *testing.T) {
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium unavailable: %v", err)
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprint(w, `<!doctype html><html><head><title>Hybrid Fixture</title></head><body><h1>dynamic-ready</h1><script>document.body.dataset.ready="true"</script></body></html>`); err != nil {
			t.Errorf("write Chromium fixture response: %v", err)
		}
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, AllowPrivateNetworks: true,
		AllowedPorts: []int{chromiumTestURLPort(t, fixture.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRouterTestResource(t, "browser", browser.Close)
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRouterTestResource(t, "browser context", owner.Close)
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	defer closeRouterTestResource(t, "page", page.Close)
	observer, err := artemisobserve.NewLiveCollector(page, page, artemisobserve.DefaultLiveConfig())
	if err != nil {
		t.Fatalf("live observer: %v", err)
	}
	defer closeRouterTestResource(t, "live observer", observer.Close)
	output, err := (ChromiumExecutor{Page: page, Observer: observer}).Execute(ctx, ExecutionRequest{URL: fixture.URL})
	if err != nil {
		t.Fatalf("ChromiumExecutor.Execute: %v", err)
	}
	if !output.Verified || output.Page.Title != "Hybrid Fixture" || output.Page.HTML == "" || output.Page.Text == "" {
		t.Fatalf("output=%+v", output)
	}
	if output.Observation == nil || output.Observation.Snapshot.Schema != "artemis.observation.v1" {
		t.Fatalf("observation=%+v", output.Observation)
	}
}

func TestHybridRouterRealFixtureParityAcrossRenderlessAndChromium(t *testing.T) {
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium unavailable: %v", err)
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprint(w, `<!doctype html><html><head><title>Parity</title></head><body><h1>same-content</h1></body></html>`); err != nil {
			t.Errorf("write parity fixture response: %v", err)
		}
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	eng := testEngineConfig(t, time.Second, fixture)
	defer closeRouterTestResource(t, "engine", eng.Close)
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, AllowPrivateNetworks: true,
		AllowedPorts: []int{chromiumTestURLPort(t, fixture.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeRouterTestResource(t, "browser", browser.Close)
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRouterTestResource(t, "browser context", owner.Close)
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	defer closeRouterTestResource(t, "page", page.Close)
	hybrid, err := New(Config{Executors: map[Mode]Executor{
		ModeStaticFetch: RenderlessExecutor{Engine: eng},
		ModeChromiumCDP: ChromiumExecutor{Page: page},
	}})
	if err != nil {
		t.Fatal(err)
	}
	staticResult, err := hybrid.Execute(ctx, RouteRequest{URL: fixture.URL, Signals: Signals{IsHTML: true}})
	if err != nil {
		t.Fatalf("static route: %v", err)
	}
	defer closeRouterTestResource(t, "static route result", staticResult.Close)
	chromiumResult, err := hybrid.Execute(ctx, RouteRequest{URL: fixture.URL, ForceMode: ModeChromiumCDP})
	if err != nil {
		t.Fatalf("Chromium route: %v", err)
	}
	if staticResult.Output.Title != chromiumResult.Output.Title || staticResult.Output.Text != chromiumResult.Output.Text {
		t.Fatalf("parity mismatch static=%+v chromium=%+v", staticResult.Output, chromiumResult.Output)
	}
}

func chromiumTestURLPort(t *testing.T, rawURL string) int {
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

func closeRouterTestResource(t *testing.T, label string, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Errorf("close %s: %v", label, err)
	}
}
