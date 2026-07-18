package observe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestChromiumObservationFixture(t *testing.T) {
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium unavailable: %v", err)
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><button id="real">Real geometry</button><button disabled>Disabled</button><button aria-label="Hidden action" style="display:none">Hidden action</button><div style="position:relative"><button style="position:absolute;left:0;top:0">Covered action</button><div style="position:absolute;left:0;top:0;width:180px;height:50px;z-index:10">overlay</div></div><input type="password" value="super-secret"><div id="host"></div><iframe srcdoc="<button>Frame button</button>"></iframe><script>host.attachShadow({mode:'open'}).innerHTML='<button aria-label="Shadow action">shadow</button>'</script>`))
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, AllowPrivateNetworks: true,
		AllowedPorts: []int{observationTestURLPort(t, fixture.URL)},
	})
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
	if _, _, err = page.Navigate(ctx, fixture.URL); err != nil {
		t.Fatal(err)
	}
	waitReady(t, ctx, page)
	collector, err := bridgeobserve.NewCollector(page, bridgeobserve.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := collector.Capture(ctx, bridgeobserve.ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	var real, disabled, hidden, covered, password, shadow *bridgeobserve.Node
	frames := map[string]bool{}
	for i := range snapshot.Nodes {
		node := &snapshot.Nodes[i]
		frames[node.FrameID] = true
		switch {
		case node.Name == "Real geometry":
			real = node
		case node.Name == "Disabled" && node.Role == "button":
			disabled = node
		case node.Name == "Hidden action" && node.Role == "button":
			hidden = node
		case node.Name == "Covered action" && node.Role == "button":
			covered = node
		case node.Tag == "input":
			password = node
		case node.Name == "Shadow action":
			shadow = node
		}
	}
	if real == nil || real.Box == nil || real.Box.Width <= 0 || real.Box.Width == 100 && real.Box.Height == 100 {
		t.Fatalf("real geometry missing or synthetic: %#v", real)
	}
	if disabled == nil || !disabled.Disabled || disabled.Interactable {
		t.Fatalf("disabled state missing: %#v", disabled)
	}
	if hidden == nil || hidden.Visible || hidden.Interactable {
		t.Fatalf("hidden state missing: %#v", hidden)
	}
	if covered == nil || covered.Hit != bridgeobserve.HitCovered || covered.Interactable {
		t.Fatalf("covered state missing: %#v warnings=%v", covered, snapshot.Warnings)
	}
	if password == nil || password.Value != "[REDACTED]" {
		t.Fatalf("password leaked: %#v", password)
	}
	if shadow == nil || len(shadow.ShadowPath) == 0 {
		t.Fatalf("shadow node missing: %#v", shadow)
	}
	if len(frames) < 2 {
		t.Fatalf("frame ownership missing: %#v", frames)
	}
}

func observationTestURLPort(t *testing.T, rawURL string) int {
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

func waitReady(t *testing.T, ctx context.Context, page *bridge.Page) {
	t.Helper()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var result struct {
			Result struct {
				Value any `json:"value"`
			} `json:"result"`
		}
		if err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.readyState", "returnByValue": true}, &result); err == nil && result.Result.Value == "complete" {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
