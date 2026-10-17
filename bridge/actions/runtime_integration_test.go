package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	formactions "github.com/Christopher-Schulze/Artemis/actions"
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

func newActionFixture(t *testing.T, launchArgs ...string) *actionFixture {
	t.Helper()
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, writeErr := fmt.Fprintf(w, `<!doctype html><title>Actions</title><style>body{height:3000px}#drag,#drop{width:100px;height:50px;margin:10px}</style><button aria-label="Counter" onclick="this.dataset.count=String(Number(this.dataset.count||0)+1)">Counter</button><input aria-label="Name"><form id="profile"><input aria-label="First" name="first"><input aria-label="Last" name="last"></form><select aria-label="Choice"><option value="a">A</option><option value="b">B</option></select><input aria-label="Agree" type="checkbox"><input aria-label="Upload" type="file"><a aria-label="Download" download="proof.txt" href="%s/download">Download</a><div id="drag" draggable="true" aria-label="Drag">Drag</div><div id="drop" aria-label="Drop">Drop</div><div id="host"></div><iframe srcdoc="<button aria-label='Frame action' onclick='this.dataset.hit=1'>Frame action</button>"></iframe><script>host.attachShadow({mode:'open'}).innerHTML='<button aria-label="Shadow click" onclick="this.dataset.hit=1">shadow</button>'</script>`, server.URL); writeErr != nil {
			t.Errorf("write action fixture response: %v", writeErr)
		}
	})
	mux.HandleFunc("/second", func(w http.ResponseWriter, _ *http.Request) {
		if _, writeErr := w.Write([]byte("<!doctype html><title>Second</title><p>second</p>")); writeErr != nil {
			t.Errorf("write second action fixture response: %v", writeErr)
		}
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="proof.txt"`)
		if _, writeErr := w.Write([]byte("verified-download")); writeErr != nil {
			t.Errorf("write action download response: %v", writeErr)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	var browser *bridge.ChromiumBrowser
	var owner *bridge.BrowserContext
	var page *bridge.Page
	var runtime *Runtime
	var closeOnce sync.Once
	cleanup := func() {
		closeOnce.Do(func() {
			if runtime != nil {
				if runtimeErr := runtime.Close(); runtimeErr != nil {
					t.Errorf("close action fixture runtime: %v", runtimeErr)
				}
			}
			if page != nil {
				if pageErr := page.Close(); pageErr != nil {
					t.Errorf("close action fixture page: %v", pageErr)
				}
			}
			if owner != nil {
				if ownerErr := owner.Close(); ownerErr != nil {
					t.Errorf("close action fixture context: %v", ownerErr)
				}
			}
			if browser != nil {
				if browserErr := browser.Close(); browserErr != nil {
					t.Errorf("close action fixture browser: %v", browserErr)
				}
			}
			cancel()
			server.Close()
		})
	}
	t.Cleanup(cleanup)
	browser, err = bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, AllowPrivateNetworks: true,
		ExtraArgs:    launchArgs,
		AllowedPorts: []int{actionTestURLPort(t, server.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, err = browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err = owner.NewPage(ctx, "about:blank")
	if err != nil {
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
	runtime, err = NewRuntimeWithConfig(page, observer, nil, RuntimeConfig{
		Downloads: downloads, DefaultTimeout: 60 * time.Second, UserID: "action-fixture-owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &actionFixture{runtime: runtime, page: page, observer: observer, server: server, close: cleanup}
}

func TestRuntimeRealChromiumFormIntentBatchCacheAndMutationInvalidation(t *testing.T) {
	f := newActionFixture(t)
	defer f.close()
	ctx := context.Background()
	intent := &formactions.FormIntent{
		SessionID: f.page.SessionID(), PageID: f.page.TargetID(), FormRoot: "#profile",
		Fields: []formactions.FormField{
			{Name: "first", Selector: `[name="first"]`, Value: "Ada"},
			{Name: "last", Selector: `[name="last"]`, Value: "Lovelace"},
		},
	}
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindFillForm, FormIntent: intent}))
	intent.Fields[0].Value = "Grace"
	intent.Fields[1].Value = "Hopper"
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindFillForm, FormIntent: intent}))
	assertValue(t, f.runtime, `document.querySelector('[name="first"]').value==="Grace"&&document.querySelector('[name="last"]').value==="Hopper"`)
	metrics := f.runtime.FormIntentMetrics()
	if metrics.FieldsPrefetchedTotal != 2 || metrics.CacheHitsTotal != 4 || metrics.MultiFieldFormsTotal != 2 {
		t.Fatalf("metrics before mutation=%+v", metrics)
	}
	params := runtimeEvaluateParams{Expression: `document.querySelector("#profile").setAttribute("data-version","2")`}
	if err := f.page.Call(ctx, "Runtime.evaluate", params, &emptyResult{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for f.runtime.FormIntentMetrics().CacheInvalidationsTotal == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("mutation invalidation metrics=%+v", f.runtime.FormIntentMetrics())
		}
		time.Sleep(time.Millisecond)
	}
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindFillForm, FormIntent: intent}))
	metrics = f.runtime.FormIntentMetrics()
	if metrics.FieldsPrefetchedTotal != 4 || metrics.CacheInvalidationsTotal != 1 {
		t.Fatalf("metrics after mutation=%+v", metrics)
	}
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindReload}))
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindFillForm, FormIntent: intent}))
	metrics = f.runtime.FormIntentMetrics()
	if metrics.FieldsPrefetchedTotal != 6 || metrics.CacheInvalidationsTotal != 2 {
		t.Fatalf("metrics after reload=%+v", metrics)
	}
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
	if !ok || active.State != artemistabs.TabStateActive || active.CDPID != tab.TargetID || active.LastActive.IsZero() {
		t.Fatalf("active tab projection=%#v", active)
	}
	sibling, err := f.page.NewSibling(ctx, f.server.URL+"/second")
	if err != nil {
		t.Fatalf("create same-context sibling page: %v", err)
	}
	listed := f.runtime.Execute(ctx, Request{Kind: KindTabList})
	requireAction(t, listed)
	ids, ok := listed.Value.([]string)
	if !ok || len(ids) != 3 {
		t.Fatalf("tab list=%#v", listed.Value)
	}
	root, ok := f.runtime.tabs.GetTab(f.page.TargetID())
	if !ok || root.UserID != "action-fixture-owner" || root.LastActive.IsZero() {
		t.Fatalf("successful page actions were not projected as activity: %#v", root)
	}
	ownedSibling, ok := f.runtime.tabs.GetTab(sibling.TargetID())
	if !ok || ownedSibling.UserID != "action-fixture-owner" {
		t.Fatalf("same-context sibling owner=%#v, want action-fixture-owner", ownedSibling)
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
	if err := f.page.Call(ctx, "Page.enable", emptyParams{}, &emptyResult{}); err != nil {
		t.Fatal(err)
	}
	dialogEvents, err := f.page.SubscribeBrowserEvents(8)
	if err != nil {
		t.Fatal(err)
	}
	defer dialogEvents.Close()
	dialogDone := make(chan error, 1)
	go func() {
		dialogDone <- f.page.Call(ctx, "Runtime.evaluate", runtimeEvaluateParams{Expression: "prompt('proof','x')"}, &emptyResult{})
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
		case eventErr, ok := <-dialogEvents.Errors:
			if !ok {
				t.Fatal("dialog event error channel closed")
			}
			t.Fatal(eventErr)
		case <-dialogCtx.Done():
			t.Fatal("timed out waiting for JavaScript dialog event")
		}
	}
dialogReady:
	waitPendingDialog(t, f.page)
	requireAction(t, f.runtime.Execute(ctx, Request{Kind: KindDialog, Accept: true, PromptText: "accepted"}))
	if dialogErr := <-dialogDone; dialogErr != nil {
		t.Fatal(dialogErr)
	}
	denied, err := NewRuntime(f.page, f.observer, func(context.Context, Request) error { return fmt.Errorf("policy says no") })
	if err != nil {
		t.Fatalf("create denied runtime: %v", err)
	}
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
	closed := f.runtime.Execute(ctx, Request{Kind: KindTabClose})
	if closed.Success || closed.Failure != FailureEvidence || closed.Evidence.Effect != EffectDispatched || closed.Evidence.Attempts != 1 || f.page.State() != bridge.TargetStateClosed {
		t.Fatalf("closed target invented live after-evidence: %+v state=%s", closed, f.page.State())
	}
	out := f.runtime.Execute(ctx, Request{Kind: KindScreenshot, Idempotent: true, RetryMax: 2, Timeout: time.Second})
	// Canonical lifecycle admission rejects a retired target before retries or
	// observation calls can consume resources against a known-closed page.
	if out.Success || out.Evidence.CaptureAttempts != 0 || out.Evidence.Attempts != 0 || out.Evidence.Effect != EffectNotAttempted || out.Failure != FailureTarget {
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
	childListener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp6", "[::1]:0")
	if err != nil {
		t.Fatalf("IPv6 loopback required for OOPIF fixture: %v", err)
	}
	child := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, writeErr := w.Write([]byte(`<!doctype html><style>body{height:1800px}iframe{display:block;width:300px;height:100px}</style><button aria-label="OOPIF action" onclick="this.dataset.count=String(Number(this.dataset.count||0)+1)">OOPIF action</button><div id="host"></div><iframe srcdoc="<button aria-label='Nested action' onclick='this.dataset.count=1'>Nested action</button>"></iframe><script>host.attachShadow({mode:'open'}).innerHTML='<button aria-label="OOPIF shadow" onclick="this.dataset.count=1">OOPIF shadow</button>'</script>`)); writeErr != nil {
			t.Errorf("write OOPIF fixture response: %v", writeErr)
		}
	}))
	child.Listener = childListener
	child.Start()
	defer child.Close()
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, writeErr := fmt.Fprintf(w, `<!doctype html><style>body{height:2000px}iframe{display:block;margin:80px 0 0 40px;border:5px solid;width:600px;height:400px}</style><iframe src="%s"></iframe>`, child.URL); writeErr != nil {
			t.Errorf("write OOPIF main fixture response: %v", writeErr)
		}
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
	defer closeActionTestResource(t, "browser", browser.Close)
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeActionTestResource(t, "browser context", owner.Close)
	page, err := owner.NewPage(ctx, main.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer closeActionTestResource(t, "page", page.Close)
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
			if !node.Interactable || node.Hit != bridgeobserve.HitClear {
				var mainTree, childTree json.RawMessage
				mainErr := page.Call(ctx, "Page.getFrameTree", nil, &mainTree)
				childErr := page.CallFrame(ctx, node.FrameID, "Page.getFrameTree", nil, &childTree)
				t.Fatalf("OOPIF action has no verified hit: node=%+v warnings=%v sessions=%v mainTree=%+v mainErr=%v childTree=%+v childErr=%v", node, snapshot.Warnings, page.FrameSessions(), mainTree, mainErr, childTree, childErr)
			}
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
	defer closeActionTestResource(t, "runtime", runtime.Close)
	requireAction(t, runtime.Execute(ctx, Request{Kind: KindClick, Ref: ref}))
	hitDeadline := time.Now().Add(10 * time.Second)
	for {
		out := runtime.Execute(ctx, Request{Kind: KindFrameEvaluate, FrameID: frameID, Expression: "document.querySelector('button').dataset.count"})
		requireAction(t, out)
		if out.Value == "1" {
			break
		}
		if time.Now().After(hitDeadline) {
			t.Fatalf("OOPIF action never observed, last value=%#v", out.Value)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	verifyOOPIFPolicyContext(t, ctx, page, observer, ref, frameID, main.URL, child.URL)
	verifyOOPIFObservationFailures(t, ctx, page, frameID)
	verifyOOPIFPointerBoundaries(t, ctx, page, observer, runtime, frameID)
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
	if len(out.Evidence.ActionID) != 32 || out.Evidence.AfterHash == "" || !out.Evidence.Postcondition.Passed || out.Evidence.Attempts < 1 {
		t.Fatalf("success lacks verified action evidence: %#v", out.Evidence)
	}
}

func closeActionTestResource(t *testing.T, label string, close func() error) {
	t.Helper()
	if err := close(); err != nil {
		t.Errorf("close %s: %v", label, err)
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
		var result runtimeCallResult
		params := runtimeEvaluateParams{Expression: "document.readyState", ReturnByValue: true}
		if err := page.Call(ctx, "Runtime.evaluate", params, &result); err == nil {
			var readyState string
			if result.valueInto(&readyState) == nil && readyState == "complete" {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
