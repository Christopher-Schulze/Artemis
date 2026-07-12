package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/engine"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestChromiumExecutorAgainstRealChromiumFixture(t *testing.T) {
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium unavailable: %v", err)
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<!doctype html><html><head><title>Hybrid Fixture</title></head><body><h1>dynamic-ready</h1><script>document.body.dataset.ready="true"</script></body></html>`)
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	output, err := (ChromiumExecutor{Page: page}).Execute(ctx, ExecutionRequest{URL: fixture.URL})
	if err != nil {
		t.Fatalf("ChromiumExecutor.Execute: %v", err)
	}
	if !output.Verified || output.Page.Title != "Hybrid Fixture" || output.Page.HTML == "" || output.Page.Text == "" {
		t.Fatalf("output=%+v", output)
	}
}

func TestHybridRouterRealFixtureParityAcrossRenderlessAndChromium(t *testing.T) {
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium unavailable: %v", err)
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<!doctype html><html><head><title>Parity</title></head><body><h1>same-content</h1></body></html>`)
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	eng, err := engine.New(engine.Config{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
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
	defer staticResult.Close()
	chromiumResult, err := hybrid.Execute(ctx, RouteRequest{URL: fixture.URL, ForceMode: ModeChromiumCDP})
	if err != nil {
		t.Fatalf("Chromium route: %v", err)
	}
	if staticResult.Output.Title != chromiumResult.Output.Title || staticResult.Output.Text != chromiumResult.Output.Text {
		t.Fatalf("parity mismatch static=%+v chromium=%+v", staticResult.Output, chromiumResult.Output)
	}
}
