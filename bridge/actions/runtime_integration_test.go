package actions

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	artemistabs "github.com/Christopher-Schulze/Artemis/bridge/tabs"
	artemisdownload "github.com/Christopher-Schulze/Artemis/download"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

type actionFixture struct {
	runtime  *Runtime
	page     *bridge.Page
	observer *bridgeobserve.Collector
	server   *httptest.Server
	close    func()
}

func newActionFixture(t *testing.T) *actionFixture {
	t.Helper()
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><title>Actions</title><style>body{height:3000px}#drag,#drop{width:100px;height:50px;margin:10px}</style><button aria-label="Counter" onclick="this.dataset.count=String(Number(this.dataset.count||0)+1)">Counter</button><input aria-label="Name"><select aria-label="Choice"><option value="a">A</option><option value="b">B</option></select><input aria-label="Agree" type="checkbox"><input aria-label="Upload" type="file"><a aria-label="Download" download="proof.txt" href="%s/download">Download</a><div id="drag" draggable="true" aria-label="Drag">Drag</div><div id="drop" aria-label="Drop">Drop</div><div id="host"></div><iframe srcdoc="<button aria-label='Frame action' onclick='this.dataset.hit=1'>Frame action</button>"></iframe><script>host.attachShadow({mode:'open'}).innerHTML='<button aria-label="Shadow click" onclick="this.dataset.hit=1">shadow</button>'</script>`, server.URL)
	})
	mux.HandleFunc("/second", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<!doctype html><title>Second</title><p>second</p>"))
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="proof.txt"`)
		_, _ = w.Write([]byte("verified-download"))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, AllowPrivateNetworks: true,
		AllowedPorts: []int{actionTestURLPort(t, server.URL)},
	})
	if err != nil {
		cancel()
		server.Close()
		t.Fatal(err)
	}
	owner, err := browser.NewContext(ctx)
	if err != nil {
		browser.Close()
		cancel()
		server.Close()
		t.Fatal(err)
	}
	page, err := owner.NewPage(ctx, "about:blank")
	if err != nil {
		owner.Close()
		browser.Close()
		cancel()
		server.Close()
		t.Fatal(err)
	}
	if _, _, err = page.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	waitRuntimeReady(t, ctx, page)
	observer, err := bridgeobserve.NewCollector(page, bridgeobserve.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	downloadPolicy, err := network.NewPolicy(network.DefaultPolicyConfig(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	downloads, err := artemisdownload.NewDownloadManager(artemisdownload.DownloadConfig{RootDir: t.TempDir(), SessionID: "integration", Policy: downloadPolicy})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntimeWithConfig(page, observer, nil, RuntimeConfig{Downloads: downloads})
	if err != nil {
		t.Fatal(err)
	}
	return &actionFixture{runtime: runtime, page: page, observer: observer, server: server, close: func() { owner.Close(); browser.Close(); cancel(); server.Close() }}
}

func TestRuntimeRealChromiumInteractionMatrix(t *testing.T) {
	f := newActionFixture(t)
	defer f.close()
	ctx := context.Background()
	snapshot, err := f.observer.Capture(ctx, bridgeobserve.ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{}
	frameID := ""
	for _, node := range snapshot.Nodes {
		if node.Ref != "" && refs[node.Name] == "" {
			refs[node.Name] = node.Ref
		}
		if node.Name == "Frame action" {
			frameID = node.FrameID
		}
	}
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindViewport, Width: 900, Height: 700}))
	frameEval := f.runtime.Execute(ctx, Request{Kind: KindFrameEvaluate, FrameID: frameID, Expression: "document.querySelector('button').textContent"})
	requireAction(t, frameEval)
	if frameEval.Value != "Frame action" {
		t.Fatalf("frame eval=%#v", frameEval.Value)
	}
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindFocus, Ref: refs["Name"]}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindFill, Ref: refs["Name"], Text: "alpha"}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindType, Ref: refs["Name"], Text: "-beta"}))
	assertValue(t, f.runtime, `document.querySelector('[aria-label="Name"]').value==="alpha-beta"`)
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindClear, Ref: refs["Name"]}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindSelect, Ref: refs["Choice"], Value: "b"}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindCheck, Ref: refs["Agree"]}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindUncheck, Ref: refs["Agree"]}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindHover, Ref: refs["Counter"]}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindClick, Ref: refs["Counter"]}))
	assertValue(t, f.runtime, `document.querySelector('[aria-label="Counter"]').dataset.count==="1"`)
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindKey, Key: "a", Text: "a"}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindScroll, DeltaY: 500}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindDrag, Ref: refs["Drag"], TargetRef: refs["Drop"]}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindClick, Ref: refs["Shadow click"]}))
	assertValue(t, f.runtime, "document.querySelector('#host').shadowRoot.querySelector('button').dataset.hit==='1'")
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindClick, Ref: refs["Frame action"]}))
	assertValue(t, f.runtime, "document.querySelector('iframe').contentDocument.querySelector('button').dataset.hit==='1'")
	upload := filepath.Join(t.TempDir(), "upload-proof.txt")
	if err = os.WriteFile(upload, []byte("upload-proof"), 0o600); err != nil {
		t.Fatal(err)
	}
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindUpload, Ref: refs["Upload"], Files: []string{upload}}))
	assertValue(t, f.runtime, `document.querySelector('[aria-label="Upload"]').files[0].text().then(v=>v==='upload-proof')`)
	download := f.runtime.Execute(ctx, Request{Kind: KindDownload, Ref: refs["Download"], Timeout: 5 * time.Second})
	requireAction(t, download)
	if download.Download == nil || download.Download.Size != int64(len("verified-download")) || download.Download.SHA256 == "" {
		t.Fatalf("download=%#v", download.Download)
	}
	raw, err := os.ReadFile(download.Download.Path)
	if err != nil || string(raw) != "verified-download" {
		t.Fatalf("download bytes=%q err=%v", raw, err)
	}
	shot := f.runtime.Execute(ctx, Request{Kind: KindScreenshot, Format: "png"})
	requireAction(t, shot)
	if shot.Width <= 0 || shot.Height <= 0 || shot.MIME != "image/png" {
		t.Fatalf("screenshot=%#v", shot)
	}
	pdf := f.runtime.Execute(ctx, Request{Kind: KindPDF})
	requireAction(t, pdf)
	if pdf.MIME != "application/pdf" || len(pdf.Bytes) < 100 {
		t.Fatalf("pdf bytes=%d mime=%s", len(pdf.Bytes), pdf.MIME)
	}
	eval := f.runtime.Execute(ctx, Request{Kind: KindEvaluate, Expression: "6*7"})
	requireAction(t, eval)
	if eval.Value != float64(42) {
		t.Fatalf("eval=%#v", eval.Value)
	}
	assertValue(t, f.runtime, "document.title==='Actions'")
	tab := f.runtime.Execute(ctx, Request{Kind: KindTabOpen, URL: f.server.URL + "/second"})
	requireAction(t, tab)
	if tab.TargetID == "" {
		t.Fatal("tab target ID missing")
	}
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindTabSwitch, TargetID: tab.TargetID}))
	active, ok := f.runtime.tabs.GetTab(tab.TargetID)
	if !ok || active.State != artemistabs.TabStateActive || active.CDPID != tab.TargetID {
		t.Fatalf("active tab projection=%#v", active)
	}
	listed := f.runtime.Execute(ctx, Request{Kind: KindTabList})
	requireAction(t, listed)
	ids, ok := listed.Value.([]string)
	if !ok || len(ids) != 2 {
		t.Fatalf("tab list=%#v", listed.Value)
	}
}

func TestRuntimeNavigationHistoryReloadDialogAndDenials(t *testing.T) {
	f := newActionFixture(t)
	defer f.close()
	ctx := context.Background()
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindNavigate, URL: f.server.URL + "/second"}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindBack}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindForward}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindReload}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindWait}))
	if err := f.page.Call(ctx, "Page.enable", map[string]any{}, &struct{}{}); err != nil {
		t.Fatal(err)
	}
	dialogEvents, err := f.page.SubscribeBrowserEvents(8)
	if err != nil {
		t.Fatal(err)
	}
	defer dialogEvents.Close()
	dialogDone := make(chan error, 1)
	go func() {
		dialogDone <- f.page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "prompt('proof','x')"}, &struct{}{})
	}()
	dialogCtx, cancelDialogWait := context.WithTimeout(ctx, 5*time.Second)
	defer cancelDialogWait()
	for {
		select {
		case event, ok := <-dialogEvents.Events:
			if !ok {
				t.Fatal("dialog event subscription closed")
			}
			if event.Method == "Page.javascriptDialogOpening" && event.SessionID == f.page.SessionID() {
				goto dialogReady
			}
		case err, ok := <-dialogEvents.Errors:
			if !ok {
				t.Fatal("dialog event error channel closed")
			}
			t.Fatal(err)
		case <-dialogCtx.Done():
			t.Fatal("timed out waiting for JavaScript dialog event")
		}
	}
dialogReady:
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindDialog, Accept: true, PromptText: "accepted"}))
	if err := <-dialogDone; err != nil {
		t.Fatal(err)
	}
	denied, _ := NewRuntime(f.page, f.observer, func(context.Context, Request) error { return fmt.Errorf("policy says no") })
	out := denied.Execute(ctx, Request{Kind: KindScreenshot})
	if out.Failure != FailurePolicy {
		t.Fatalf("policy outcome=%#v", out)
	}
	out = f.runtime.Execute(ctx, Request{Kind: KindClick, Ref: "e999"})
	if out.Failure != FailureTarget {
		t.Fatalf("unknown ref outcome=%#v", out)
	}
	out = f.runtime.Execute(ctx, Request{Kind: KindScreenshot, RetryMax: 1})
	if out.Failure != FailureValidation {
		t.Fatalf("unsafe retry outcome=%#v", out)
	}
	out = f.runtime.Execute(ctx, Request{Kind: KindAssert, Expression: "false"})
	if out.Failure != FailurePostcondition {
		t.Fatalf("false assertion outcome=%#v", out)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	out = f.runtime.Execute(cancelled, Request{Kind: KindScreenshot})
	if out.Failure != FailureCancelled && out.Failure != FailureProtocol {
		t.Fatalf("cancel outcome=%#v", out)
	}
}

func TestRuntimeBoundedRetryAndTabClose(t *testing.T) {
	f := newActionFixture(t)
	defer f.close()
	ctx := context.Background()
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindTabClose}))
	out := f.runtime.Execute(ctx, Request{Kind: KindScreenshot, Idempotent: true, RetryMax: 2, Timeout: time.Second})
	if out.Success || out.Evidence.Attempts != 3 || out.Failure != FailureProtocol {
		t.Fatalf("retry outcome=%#v", out)
	}
}

func TestFormBatchWithPropagatesEveryFailure(t *testing.T) {
	results := FormBatchWith(context.Background(), nil, []FormAction{NewFormFill("e1", "a"), NewFormSelect("e2", "b"), NewFormCheck("e3")})
	if len(results) != 3 {
		t.Fatalf("results=%d", len(results))
	}
	for i, result := range results {
		if result.Success || result.Error == "" {
			t.Fatalf("result %d=%#v", i, result)
		}
	}
}

func TestRuntimeCrossOriginOOPIFObservationAndAction(t *testing.T) {
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatal(err)
	}
	childListener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatalf("IPv6 loopback required for OOPIF fixture: %v", err)
	}
	child := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><button aria-label="OOPIF action" onclick="this.dataset.hit='yes'">OOPIF action</button>`))
	}))
	child.Listener = childListener
	child.Start()
	defer child.Close()
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<!doctype html><iframe src="%s"></iframe>`, child.URL)
	}))
	defer main.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, ExtraArgs: []string{"--site-per-process"}, AllowPrivateNetworks: true,
		AllowedPorts: []int{actionTestURLPort(t, main.URL), actionTestURLPort(t, child.URL)},
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
	page, err := owner.NewPage(ctx, main.URL)
	if err != nil {
		t.Fatal(err)
	}
	waitRuntimeReady(t, ctx, page)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for len(page.FrameSessions()) == 0 {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatal("OOPIF session was not attached")
		case <-time.After(25 * time.Millisecond):
		}
	}
	observer, err := bridgeobserve.NewCollector(page, bridgeobserve.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := observer.Capture(ctx, bridgeobserve.ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	var ref, frameID string
	for _, node := range snapshot.Nodes {
		if node.Name == "OOPIF action" && node.Role == "button" {
			ref = node.Ref
			frameID = node.FrameID
			break
		}
	}
	if ref == "" || frameID == "" {
		t.Fatalf("OOPIF action absent: sessions=%v warnings=%v", page.FrameSessions(), snapshot.Warnings)
	}
	runtime, err := NewRuntime(page, observer, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireAction(t, runtime.Execute(ctx, Request{Kind: KindClick, Ref: ref}))
	out := runtime.Execute(ctx, Request{Kind: KindFrameEvaluate, FrameID: frameID, Expression: "document.querySelector('button').dataset.hit"})
	requireAction(t, out)
	if out.Value != "yes" {
		t.Fatalf("OOPIF action value=%#v", out.Value)
	}
}

func actionTestURLPort(t *testing.T, rawURL string) int {
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

func requireAction(t *testing.T, out Outcome) {
	t.Helper()
	if !out.Success {
		t.Fatalf("action %s failed class=%s error=%s evidence=%#v", out.Evidence.Action, out.Failure, out.Error, out.Evidence)
	}
}
func assertValue(t *testing.T, r *Runtime, expression string) {
	t.Helper()
	requireAction(t, r.Execute(context.Background(), Request{Kind: KindAssert, Expression: expression}))
}
func waitRuntimeReady(t *testing.T, ctx context.Context, page *bridge.Page) {
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
