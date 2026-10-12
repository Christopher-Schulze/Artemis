package cdpops_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/cdpops"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestElementClientChromiumActionabilityTruth(t *testing.T) {
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium unavailable: %v", err)
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, writeErr := w.Write([]byte(`<!doctype html>
<button id="ready">Ready</button>
<button id="disabled" disabled>Disabled</button>
<button id="hidden" style="visibility:hidden">Hidden</button>
<button id="no-layout" style="display:none">No layout</button>
<div style="position:relative;width:200px;height:50px">
  <button id="covered" style="position:absolute;left:0;top:0">Covered</button>
  <div style="position:absolute;left:0;top:0;width:200px;height:50px;z-index:10">overlay</div>
</div>`)); writeErr != nil {
			t.Errorf("write element fixture response: %v", writeErr)
		}
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, AllowPrivateNetworks: true,
		AllowedPorts: []int{elementTestURLPort(t, fixture.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeCDPOpsTestResource(t, "browser", browser.Close)
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeCDPOpsTestResource(t, "browser context", owner.Close)
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	defer closeCDPOpsTestResource(t, "page", page.Close)
	if _, _, err = page.Navigate(ctx, fixture.URL); err != nil {
		t.Fatal(err)
	}
	waitForElementDocument(t, ctx, page)
	client, err := cdpops.NewElementClient(page)
	if err != nil {
		t.Fatal(err)
	}
	elements, err := client.QuerySelector(ctx, "button")
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]cdpops.ElementInfo, len(elements))
	for _, element := range elements {
		byID[element.ID] = element
	}
	assertElementState(t, byID, "ready", cdpops.ActionabilityReady, true)
	assertElementState(t, byID, "disabled", cdpops.ActionabilityDisabled, false)
	assertElementState(t, byID, "hidden", cdpops.ActionabilityHidden, false)
	assertElementState(t, byID, "no-layout", cdpops.ActionabilityNoLayout, false)
	assertElementState(t, byID, "covered", cdpops.ActionabilityCovered, false)
}

func closeCDPOpsTestResource(t *testing.T, label string, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Errorf("close %s: %v", label, err)
	}
}

func assertElementState(t *testing.T, elements map[string]cdpops.ElementInfo, id string, state cdpops.ActionabilityState, clickable bool) {
	t.Helper()
	element, ok := elements[id]
	if !ok {
		t.Fatalf("element %q missing from query: %#v", id, elements)
	}
	if element.Actionability != state || element.Clickable != clickable {
		t.Fatalf("element %q actionability=%s clickable=%v, want %s/%v: %#v", id, element.Actionability, element.Clickable, state, clickable, element)
	}
}

func elementTestURLPort(t *testing.T, rawURL string) int {
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

type elementRuntimeEvaluateParams struct {
	Expression    string `json:"expression"`
	ReturnByValue bool   `json:"returnByValue"`
}

type elementRuntimeEvaluateResult struct {
	Result struct {
		Value string `json:"value"`
	} `json:"result"`
}

func waitForElementDocument(t *testing.T, ctx context.Context, page *bridge.Page) {
	t.Helper()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var result elementRuntimeEvaluateResult
		params := elementRuntimeEvaluateParams{Expression: "document.readyState", ReturnByValue: true}
		if err := page.Call(ctx, "Runtime.evaluate", params, &result); err == nil && result.Result.Value == "complete" {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
