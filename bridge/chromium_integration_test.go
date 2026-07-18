package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/coder/websocket"
)

func TestChromiumLifecycleIntegration(t *testing.T) {
	binary := requireChromium(t)
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>Artemis Fixture</title><main id='ready'>ready</main>"))
	}))
	defer fixture.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second, ShutdownTimeout: 3 * time.Second,
		AllowPrivateNetworks: true, AllowedPorts: []int{testURLPort(t, fixture.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := browser.ProfileDir()
	defer browser.Close()
	if !browser.Owned() || !browser.Healthy() {
		t.Fatalf("owned=%v healthy=%v", browser.Owned(), browser.Healthy())
	}
	if browser.ReconnectPolicy() != ReconnectExplicitRelaunch {
		t.Fatalf("reconnect policy=%q", browser.ReconnectPolicy())
	}
	if browser.Version().Product == "" || browser.Version().ProtocolVersion == "" {
		t.Fatalf("version=%+v", browser.Version())
	}
	if err := browser.SetMaxPages(0); !IsCDPError(err, CDPErrorInvalidConfig) {
		t.Fatalf("invalid page limit error=%v", err)
	}
	if err := browser.SetMaxPages(2); err != nil {
		t.Fatal(err)
	}

	events, err := browser.Transport().Subscribe(32)
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	browserContext, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := browserContext.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	if page.SessionID() == "" || browserContext.ID() == "" {
		t.Fatalf("missing ownership identity: context=%q session=%q", browserContext.ID(), page.SessionID())
	}
	if err := browser.SetMaxPages(0); !IsCDPError(err, CDPErrorInvalidConfig) {
		t.Fatalf("invalid admitted page limit error=%v", err)
	}
	secondPage, err := browserContext.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	if err := browser.SetMaxPages(1); !IsCDPError(err, CDPErrorOverloaded) {
		t.Fatalf("lowered admitted page limit error=%v", err)
	}
	if err := secondPage.Close(); err != nil {
		t.Fatal(err)
	}
	if err := browser.SetMaxPages(1); err != nil {
		t.Fatalf("page limit did not accept released slot: %v", err)
	}
	waitForTargetCreated(t, ctx, events, page.TargetID())
	frameID, _, err := page.Navigate(ctx, fixture.URL)
	if err != nil || frameID == "" {
		t.Fatalf("navigate frame=%q err=%v", frameID, err)
	}
	waitForDocumentTitle(t, ctx, page, "Artemis Fixture")
	if _, err := browserContext.NewPage(ctx, "about:blank"); !IsCDPError(err, CDPErrorOverloaded) {
		t.Fatalf("page limit error=%v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := browserContext.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatalf("page slot was not released: %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	if err := browserContext.Close(); err != nil {
		t.Fatal(err)
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
	if browser.Healthy() {
		t.Fatal("browser remained healthy after close")
	}
	if _, err := os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned profile survived shutdown: %v", err)
	}
}

func testURLPort(t *testing.T, rawURL string) int {
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

func TestChromiumExternalAttachDoesNotTerminateBrowser(t *testing.T) {
	binary := requireChromium(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	processOwner, err := browserprocess.Launch(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second, ShutdownTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer processOwner.Close()
	browser, err := ConnectChromium(ctx, processOwner.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	if browser.Owned() {
		t.Fatal("external attachment claimed process ownership")
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-processOwner.Done():
		t.Fatalf("external close terminated Chromium: %v", processOwner.WaitError())
	case <-time.After(200 * time.Millisecond):
	}
}

func TestChromiumParallelLaunchesIsolateEndpointAndProfile(t *testing.T) {
	binary := requireChromium(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	first, err := browserprocess.Launch(ctx, browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := browserprocess.Launch(ctx, browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.Endpoint() == second.Endpoint() || first.ProfileDir() == second.ProfileDir() {
		t.Fatalf("parallel launch collision: endpoint %q/%q profile %q/%q", first.Endpoint(), second.Endpoint(), first.ProfileDir(), second.ProfileDir())
	}
}

func TestChromiumTargetCrashTransitionsState(t *testing.T) {
	binary := requireChromium(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := LaunchChromium(ctx, browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	browserContext, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer browserContext.Close()
	page, err := browserContext.NewPage(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	crashCtx, cancelCrash := context.WithTimeout(ctx, time.Second)
	_ = page.Call(crashCtx, "Page.crash", nil, nil)
	cancelCrash()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for page.State() == TargetStateAttached {
		select {
		case <-deadline.C:
			t.Fatalf("crashed target remained attached")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "1"}, nil); err == nil {
		t.Fatal("crashed target accepted a CDP call")
	} else {
		var targetErr *TargetError
		if !errors.As(err, &targetErr) {
			t.Fatalf("crashed target error=%v", err)
		}
	}
}

func TestChromiumRejectsIncompleteBrowserIdentity(t *testing.T) {
	endpoint := cdpTestServer(t, func(ctx context.Context, conn *websocket.Conn) error {
		command, err := readCDPCommand(ctx, conn)
		if err != nil {
			return err
		}
		return writeCDPMessage(ctx, conn, map[string]any{"id": command.ID, "result": map[string]any{}})
	})
	_, err := ConnectChromium(context.Background(), endpoint)
	if !IsCDPError(err, CDPErrorProtocol) {
		t.Fatalf("identity error=%v", err)
	}
}

func requireChromium(t *testing.T) browserprocess.Binary {
	t.Helper()
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium integration unavailable: %v", err)
	}
	return binary
}

func waitForDocumentTitle(t *testing.T, ctx context.Context, page *Page, want string) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var evaluation struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.title", "returnByValue": true}, &evaluation)
		if err == nil && evaluation.Result.Value == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("document title did not become %q: last=%q err=%v browser=%v", want, evaluation.Result.Value, err, page.owner.browser.Err())
		case <-ticker.C:
		}
	}
}

func waitForTargetCreated(t *testing.T, ctx context.Context, subscription *CDPSubscription, targetID string) {
	t.Helper()
	for {
		select {
		case event := <-subscription.Events:
			if event.Method != "Target.targetCreated" {
				continue
			}
			var payload struct {
				TargetInfo struct {
					ID string `json:"targetId"`
				} `json:"targetInfo"`
			}
			if json.Unmarshal(event.Params, &payload) == nil && payload.TargetInfo.ID == targetID {
				return
			}
		case err := <-subscription.Errors:
			t.Fatalf("target event subscription: %v", err)
		case <-ctx.Done():
			t.Fatalf("targetCreated not observed for %s", targetID)
		}
	}
}
