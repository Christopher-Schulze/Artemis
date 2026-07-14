package fixture

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/engine"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/profile"
	"github.com/Christopher-Schulze/Artemis/router"
	"github.com/Christopher-Schulze/Artemis/serve"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// runner is a cross-adapter fixture surface. Each concrete implementation
// executes the same deterministic fixture scenarios through a single
// Artemis path: engine, Chromium bridge, hybrid router, serve, Agent API, or
// Omnimus BrowserRuntime.
type runner interface {
	name() string
	start(ctx context.Context, t *testing.T, srv *Server)
	stop(ctx context.Context, t *testing.T)
	canRun(sc Scenario) (bool, string)
	run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult
}

// TestFixtureCorpusThroughAllPaths runs the deterministic local fixture
// corpus through every supported Artemis path and asserts the same semantic
// outcomes wherever a path is capable of exercising the scenario.
func TestFixtureCorpusThroughAllPaths(t *testing.T) {
	srv := NewServerWithDefaults()
	defer srv.Close()
	t.Setenv("ARTEMIS_REPLAY_DIR", t.TempDir())

	runners := []runner{
		&engineRunner{},
		&bridgeRunner{},
		&routerRunner{},
		&serveRunner{},
		&agentRunner{},
		&omnimusRunner{},
	}

	for _, r := range runners {
		r := r
		t.Run(r.name(), func(t *testing.T) {
			ctx := context.Background()
			r.start(ctx, t, srv)
			defer r.stop(ctx, t)

			for _, sc := range DefaultScenarios() {
				sc := sc
				ok, reason := r.canRun(sc)
				if !ok {
					t.Logf("%s: skip %s: %s", r.name(), sc.ID, reason)
					continue
				}
				t.Run(sc.ID, func(t *testing.T) {
					result := r.run(ctx, t, sc, srv)
					if replayDir := os.Getenv("ARTEMIS_REPLAY_DIR"); replayDir != "" {
						if _, err := CaptureReplay(sc, r.name(), result, 0, SystemMetadata(), DefaultRedactionConfig(), replayDir); err != nil {
							t.Errorf("replay capture: %v", err)
						}
					}
					assertCrossResult(t, sc, result)
				})
			}
		})
	}
}

func assertCrossResult(t *testing.T, sc Scenario, got CrossResult) {
	wantStatus := sc.Expect.Status
	if wantStatus == 0 {
		wantStatus = 200
	}
	if got.StatusCode != 0 && got.StatusCode != wantStatus {
		t.Errorf("status = %d, want %d", got.StatusCode, wantStatus)
	}
	if sc.Expect.Title != "" && got.Title != sc.Expect.Title {
		t.Errorf("title = %q, want %q", got.Title, sc.Expect.Title)
	}
	for _, s := range sc.Expect.Contains {
		if !strings.Contains(got.Text, s) {
			t.Errorf("text missing %q", s)
		}
	}
	for _, s := range sc.Expect.NotContains {
		if strings.Contains(got.Text, s) {
			t.Errorf("text contains %q", s)
		}
	}
	if sc.Expect.URL != "" && !strings.Contains(got.URL, sc.Expect.URL) {
		t.Errorf("url = %q, want containing %q", got.URL, sc.Expect.URL)
	}
	if sc.Expect.Eval != "" {
		if got.EvalErr != "" {
			t.Errorf("eval %q error: %s", sc.Expect.Eval, got.EvalErr)
		}
		if !strings.Contains(got.EvalResult, sc.Expect.EvalContains) {
			t.Errorf("eval %q = %q, want containing %q", sc.Expect.Eval, got.EvalResult, sc.Expect.EvalContains)
		}
	}
}

// -----------------------------------------------------------------------------
// engine runner (renderless)
// -----------------------------------------------------------------------------

type engineRunner struct{ eng *engine.Engine }

func (r *engineRunner) name() string { return "renderless" }

func (r *engineRunner) start(ctx context.Context, t *testing.T, srv *Server) {
	t.Helper()
	eng, err := engine.New(engine.Config{
		PolicyConfig:      srv.PolicyConfig(),
		Timeout:           30 * time.Second,
		MaxBodyBytes:      10 * 1024 * 1024,
		JSContextPoolSize: 4,
		JSContextPoolWarm: false,
	})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	r.eng = eng
}

func (r *engineRunner) stop(ctx context.Context, t *testing.T) {
	if r.eng != nil {
		_ = r.eng.Close()
	}
}

func (r *engineRunner) canRun(sc Scenario) (bool, string) { return true, "" }

func (r *engineRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	page, err := r.eng.Fetch(ctx, srv.URL(sc.Path), engine.FetchOpts{
		RunScripts: sc.RunScripts,
		AsyncFetch: sc.AsyncFetch,
	})
	if err != nil {
		t.Fatalf("engine Fetch: %v", err)
	}
	defer page.Close()
	if sc.RunScripts && sc.WaitForIdle {
		if err := page.WaitIdle(ctx); err != nil {
			t.Fatalf("engine WaitIdle: %v", err)
		}
	}
	return resultFromEnginePage(ctx, t, page, sc)
}

func resultFromEnginePage(ctx context.Context, t *testing.T, page *engine.Page, sc Scenario) CrossResult {
	t.Helper()
	var evalRes string
	var evalErr string
	if sc.Expect.Eval != "" {
		v, err := page.Eval(ctx, sc.Expect.Eval)
		if err != nil {
			evalErr = err.Error()
		} else {
			evalRes = v.String()
		}
	}
	links := make([]string, 0, len(page.Links()))
	for _, l := range page.Links() {
		links = append(links, l.Href)
	}
	return CrossResult{
		URL:        page.URL(),
		StatusCode: page.StatusCode(),
		Title:      page.Title(),
		Text:       page.Text(),
		HTML:       page.HTML(),
		Markdown:   page.Markdown(),
		EvalResult: evalRes,
		EvalErr:    evalErr,
		Links:      links,
	}
}

// -----------------------------------------------------------------------------
// bridge runner (Chromium CDP)
// -----------------------------------------------------------------------------

type bridgeRunner struct {
	browser *bridge.ChromiumBrowser
	ctx     *bridge.BrowserContext
	page    *bridge.Page
}

func (r *bridgeRunner) name() string { return "chromium" }

func (r *bridgeRunner) start(ctx context.Context, t *testing.T, srv *Server) {
	t.Helper()
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium discovery: %v", err)
	}
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath:      binary.Path,
		Headless:        true,
		StartupTimeout:  15 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("LaunchChromium: %v", err)
	}
	r.browser = browser
	ctx2, err := browser.NewContext(ctx)
	if err != nil {
		browser.Close()
		t.Fatalf("NewContext: %v", err)
	}
	r.ctx = ctx2
	page, err := ctx2.NewPage(ctx, "about:blank")
	if err != nil {
		ctx2.Close()
		browser.Close()
		t.Fatalf("NewPage: %v", err)
	}
	r.page = page
}

func (r *bridgeRunner) stop(ctx context.Context, t *testing.T) {
	if r.page != nil {
		_ = r.page.Close()
	}
	if r.ctx != nil {
		_ = r.ctx.Close()
	}
	if r.browser != nil {
		_ = r.browser.Close()
	}
}

func (r *bridgeRunner) canRun(sc Scenario) (bool, string) {
	if sc.AsyncFetch {
		return false, "bridge does not support AsyncFetch interception"
	}
	if sc.Expect.Status != 0 && sc.Expect.Status != 200 {
		return false, "bridge cannot verify non-200 HTTP status"
	}
	return true, ""
}

func (r *bridgeRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	if _, _, err := r.page.Navigate(ctx, srv.URL(sc.Path)); err != nil {
		t.Fatalf("bridge Navigate: %v", err)
	}
	if err := waitForReadyStateComplete(ctx, r.page); err != nil {
		t.Fatalf("bridge wait for ready state: %v", err)
	}
	return resultFromBridgePage(ctx, t, r.page, sc)
}

func waitForReadyStateComplete(ctx context.Context, page callCaller) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var res struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": "document.readyState", "returnByValue": true}, &res); err != nil {
			return err
		}
		if res.Result.Value == "complete" {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("document.readyState did not become complete")
}

type callCaller interface {
	Call(context.Context, string, any, any) error
}

func resultFromBridgePage(ctx context.Context, t *testing.T, page *bridge.Page, sc Scenario) CrossResult {
	t.Helper()
	var snap struct {
		Result struct {
			Value struct {
				URL      string       `json:"url"`
				Title    string       `json:"title"`
				HTML     string       `json:"html"`
				Text     string       `json:"text"`
				Markdown string       `json:"markdown"`
				Links    []bridgeLink `json:"links"`
			} `json:"value"`
		} `json:"result"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    chromiumSnapshotExpression,
		"returnByValue": true,
		"awaitPromise":  true,
	}, &snap); err != nil {
		t.Fatalf("bridge snapshot: %v", err)
	}
	var evalRes, evalErr string
	if sc.Expect.Eval != "" {
		var eval struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err := page.Call(ctx, "Runtime.evaluate", map[string]any{
			"expression":    sc.Expect.Eval,
			"returnByValue": true,
		}, &eval); err != nil {
			evalErr = err.Error()
		} else {
			evalRes = eval.Result.Value
		}
	}
	links := make([]string, 0, len(snap.Result.Value.Links))
	for _, l := range snap.Result.Value.Links {
		links = append(links, l.Href)
	}
	return CrossResult{
		URL:        snap.Result.Value.URL,
		StatusCode: 200,
		Title:      snap.Result.Value.Title,
		Text:       snap.Result.Value.Text,
		HTML:       snap.Result.Value.HTML,
		Markdown:   snap.Result.Value.Markdown,
		EvalResult: evalRes,
		EvalErr:    evalErr,
		Links:      links,
	}
}

type bridgeLink struct {
	Href  string `json:"href"`
	Text  string `json:"text"`
	Title string `json:"title"`
}

const chromiumSnapshotExpression = `(async () => {
  if (document.readyState === "loading") {
    await new Promise((resolve) => document.addEventListener("DOMContentLoaded", resolve, {once: true}));
  }
  const root = document.documentElement;
  const body = document.body;
  const links = Array.from(document.querySelectorAll("a[href]")).map((a) => ({
    href: a.href,
    text: (a.innerText || a.textContent || "").trim(),
    title: a.getAttribute("title") || ""
  }));
  return {
    url: location.href,
    title: document.title,
    html: root ? root.outerHTML : "",
    text: body ? (body.innerText || body.textContent || "") : "",
    markdown: "",
    links
  };
})()`

// -----------------------------------------------------------------------------
// router runner (hybrid)
// -----------------------------------------------------------------------------

type routerRunner struct {
	eng     *engine.Engine
	browser *bridge.ChromiumBrowser
	ctx     *bridge.BrowserContext
	page    *bridge.Page
	rtr     *router.HybridRouter
}

func (r *routerRunner) name() string { return "hybrid" }

func (r *routerRunner) start(ctx context.Context, t *testing.T, srv *Server) {
	t.Helper()
	eng, err := engine.New(engine.Config{
		PolicyConfig:      srv.PolicyConfig(),
		Timeout:           30 * time.Second,
		MaxBodyBytes:      10 * 1024 * 1024,
		JSContextPoolSize: 4,
		JSContextPoolWarm: false,
	})
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	r.eng = eng

	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		eng.Close()
		t.Fatalf("Chromium discovery: %v", err)
	}
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath:      binary.Path,
		Headless:        true,
		StartupTimeout:  15 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	})
	if err != nil {
		eng.Close()
		t.Fatalf("LaunchChromium: %v", err)
	}
	ctx2, err := browser.NewContext(ctx)
	if err != nil {
		browser.Close()
		eng.Close()
		t.Fatalf("NewContext: %v", err)
	}
	page, err := ctx2.NewPage(ctx, "about:blank")
	if err != nil {
		ctx2.Close()
		browser.Close()
		eng.Close()
		t.Fatalf("NewPage: %v", err)
	}
	r.browser = browser
	r.ctx = ctx2
	r.page = page

	rtr, err := router.New(router.Config{
		Executors: map[router.Mode]router.Executor{
			router.ModeStaticFetch:  router.RuntimeExecutor{Runtime: eng, RunScripts: true},
			router.ModeRenderlessJS: router.RuntimeExecutor{Runtime: eng, RunScripts: true},
			router.ModeChromiumCDP:  router.ChromiumExecutor{Page: page},
			router.ModeStealth:      router.ChromiumExecutor{Page: page},
		},
	})
	if err != nil {
		page.Close()
		ctx2.Close()
		browser.Close()
		eng.Close()
		t.Fatalf("router.New: %v", err)
	}
	r.rtr = rtr
}

func (r *routerRunner) stop(ctx context.Context, t *testing.T) {
	if r.eng != nil {
		_ = r.eng.Close()
	}
	if r.page != nil {
		_ = r.page.Close()
	}
	if r.ctx != nil {
		_ = r.ctx.Close()
	}
	if r.browser != nil {
		_ = r.browser.Close()
	}
	if r.rtr != nil {
		// no explicit close needed
	}
}

func (r *routerRunner) canRun(sc Scenario) (bool, string) {
	if sc.Expect.Eval != "" {
		return false, "router does not support eval"
	}
	if sc.WaitForIdle || sc.AsyncFetch {
		return false, "router does not support WaitForIdle or AsyncFetch"
	}
	if sc.Status != 0 && sc.Status != 200 {
		return false, "router cannot verify non-200 HTTP status"
	}
	return true, ""
}

func (r *routerRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	signals := signalsForScenario(sc)
	req := router.RouteRequest{
		URL:     srv.URL(sc.Path),
		Signals: signals,
		Action:  router.ActionFetch,
		Policy:  router.Policy{},
		State:   router.BrowserState{},
	}
	result, err := r.rtr.Execute(ctx, req)
	if err != nil {
		t.Fatalf("router.Execute: %v", err)
	}
	if result.Resource != nil {
		defer result.Resource.Close()
	}
	return CrossResult{
		URL:        result.Output.URL,
		StatusCode: result.Output.StatusCode,
		Title:      result.Output.Title,
		Text:       result.Output.Text,
		HTML:       result.Output.HTML,
		Markdown:   result.Output.Markdown,
		Links:      linkHrefs(result.Output.Links),
	}
}

func linkHrefs(links []agent.Link) []string {
	out := make([]string, 0, len(links))
	for i := range links {
		out = append(out, links[i].Href)
	}
	return out
}

func signalsForScenario(sc Scenario) bridge.RouterSignals {
	s := bridge.RouterSignals{
		IsHTML:      true,
		ScriptCount: sc.ScriptCount,
	}
	if s.ScriptCount == 0 && sc.RunScripts {
		s.ScriptCount = 1
	}
	s.HasExternalScripts = s.ScriptCount > 0 && strings.Contains(sc.HTML, "<script src=")
	switch sc.Kind {
	case KindCanvas:
		s.NeedsCanvas = true
	case KindShadowDOM:
		s.HasShadowDOM = true
	case KindFrame, KindDialog:
		s.NeedsLayout = true
	case KindWebSocket, KindServiceWorker:
		s.HasWebComponents = true
	}
	return s
}

// -----------------------------------------------------------------------------
// serve runner (Serve + WebSocket API)
// -----------------------------------------------------------------------------

type serveRunner struct {
	agent      *artemis.Agent
	server     *serve.Server
	httpServer *http.Server
	addr       string
	sessID     string
}

func (r *serveRunner) name() string { return "serve" }

func (r *serveRunner) start(ctx context.Context, t *testing.T, srv *Server) {
	t.Helper()
	agent, err := artemis.NewAgent(artemis.AgentConfig{PolicyConfig: srv.PolicyConfig()})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if err := agent.Start(ctx); err != nil {
		t.Fatalf("agent.Start: %v", err)
	}
	r.agent = agent
	r.server = serve.New(agent, serve.Opts{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	r.addr = ln.Addr().String()
	r.httpServer = &http.Server{Handler: http.HandlerFunc(r.server.HandleWSForTest)}
	go func() {
		_ = r.httpServer.Serve(ln)
	}()
	c := r.dial(t)
	defer c.Close(websocket.StatusNormalClosure, "")
	resp := r.call(t, c, serve.CmdSessionNew, serve.SessionNewParams{})
	var sess serve.SessionNewResult
	if err := serve.DecodeTypedResult(&resp, &sess); err != nil {
		t.Fatalf("session.new decode: %v", err)
	}
	r.sessID = sess.SessionID
}

func (r *serveRunner) stop(ctx context.Context, t *testing.T) {
	if r.httpServer != nil {
		_ = r.httpServer.Close()
	}
	if r.server != nil {
		_ = r.server.Close()
	}
	if r.agent != nil {
		_ = r.agent.Stop()
	}
}

func (r *serveRunner) canRun(sc Scenario) (bool, string) {
	if sc.AsyncFetch {
		return false, "serve page.open does not enable AsyncFetch"
	}
	return true, ""
}

func (r *serveRunner) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(context.Background(), "ws://"+r.addr+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c.SetReadLimit(8 << 20)
	return c
}

func (r *serveRunner) call(t *testing.T, c *websocket.Conn, cmd serve.Command, params interface{}) serve.Response {
	t.Helper()
	req, err := serve.MarshalTyped(strconv.FormatInt(time.Now().UnixNano(), 10), cmd, params)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := wsjson.Write(context.Background(), c, req); err != nil {
		t.Fatalf("ws write: %v", err)
	}
	var resp serve.Response
	if err := wsjson.Read(context.Background(), c, &resp); err != nil {
		t.Fatalf("ws read: %v", err)
	}
	return resp
}

func (r *serveRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	c := r.dial(t)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := r.call(t, c, serve.CmdPageOpen, serve.PageOpenParams{
		SessionID:  r.sessID,
		URL:        srv.URL(sc.Path),
		RunScripts: sc.RunScripts,
	})
	var open serve.PageOpenResult
	if err := serve.DecodeTypedResult(&resp, &open); err != nil {
		t.Fatalf("page.open: %v", err)
	}
	pageID := open.PageID
	if sc.RunScripts && sc.WaitForIdle {
		resp := r.call(t, c, serve.CmdPageWaitIdle, serve.PageWaitIdleParams{SessionID: r.sessID, PageID: pageID})
		if !resp.OK {
			t.Fatalf("page.wait_idle: %v", resp.Error)
		}
	}

	var evalRes, evalErr string
	if sc.Expect.Eval != "" {
		resp := r.call(t, c, serve.CmdPageEval, serve.PageEvalParams{
			SessionID: r.sessID,
			PageID:    pageID,
			Expr:      sc.Expect.Eval,
		})
		if !resp.OK {
			evalErr = fmt.Sprintf("%s: %s", resp.Error.Code, resp.Error.Message)
		} else {
			var eval serve.PageEvalResult
			if err := serve.DecodeTypedResult(&resp, &eval); err != nil {
				t.Fatalf("page.eval decode: %v", err)
			}
			evalRes = eval.Value
		}
	}

	resp = r.call(t, c, serve.CmdPageDump, serve.PageDumpParams{
		SessionID: r.sessID,
		PageID:    pageID,
		Format:    string(serve.DumpText),
	})
	if !resp.OK {
		t.Fatalf("page.dump text: %v", resp.Error)
	}
	var textDump serve.PageDumpResult
	if err := serve.DecodeTypedResult(&resp, &textDump); err != nil {
		t.Fatalf("page.dump text decode: %v", err)
	}
	text := stringifyDump(textDump.Data)

	resp = r.call(t, c, serve.CmdPageDump, serve.PageDumpParams{
		SessionID: r.sessID,
		PageID:    pageID,
		Format:    string(serve.DumpTitle),
	})
	var titleDump serve.PageDumpResult
	if err := serve.DecodeTypedResult(&resp, &titleDump); err != nil {
		t.Fatalf("page.dump title decode: %v", err)
	}
	title := stringifyDump(titleDump.Data)

	resp = r.call(t, c, serve.CmdPageDump, serve.PageDumpParams{
		SessionID: r.sessID,
		PageID:    pageID,
		Format:    string(serve.DumpLinks),
	})
	var linksDump serve.PageDumpResult
	if err := serve.DecodeTypedResult(&resp, &linksDump); err != nil {
		t.Fatalf("page.dump links decode: %v", err)
	}
	links := linkStrings(linksDump.Data)

	closeResp := r.call(t, c, serve.CmdPageClose, serve.PageCloseParams{SessionID: r.sessID, PageID: pageID})
	if !closeResp.OK {
		t.Fatalf("page.close: %v", closeResp.Error)
	}

	return CrossResult{
		URL:        open.URL,
		StatusCode: open.Status,
		Title:      title,
		Text:       text,
		EvalResult: evalRes,
		EvalErr:    evalErr,
		Links:      links,
	}
}

func stringifyDump(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

func linkStrings(v any) []string {
	raw, _ := json.Marshal(v)
	var links []struct{ Href string }
	_ = json.Unmarshal(raw, &links)
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, l.Href)
	}
	return out
}

// -----------------------------------------------------------------------------
// agent runner (public Agent API)
// -----------------------------------------------------------------------------

type agentRunner struct{ agent *artemis.Agent }

func (r *agentRunner) name() string { return "agent" }

func (r *agentRunner) start(ctx context.Context, t *testing.T, srv *Server) {
	t.Helper()
	agent, err := artemis.NewAgent(artemis.AgentConfig{PolicyConfig: srv.PolicyConfig()})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if err := agent.Start(ctx); err != nil {
		t.Fatalf("agent.Start: %v", err)
	}
	r.agent = agent
}

func (r *agentRunner) stop(ctx context.Context, t *testing.T) {
	if r.agent != nil {
		_ = r.agent.Stop()
	}
}

func (r *agentRunner) canRun(sc Scenario) (bool, string) {
	if sc.Expect.Eval != "" {
		return false, "Agent.ExecuteTask does not support Eval"
	}
	if sc.WaitForIdle || sc.AsyncFetch {
		return false, "Agent.ExecuteTask does not support WaitForIdle or AsyncFetch"
	}
	return true, ""
}

func (r *agentRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	session, err := r.agent.CreateSession("fixture-runner")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	result := r.agent.ExecuteTask(ctx, artemis.Task{
		ID:        sc.ID,
		SessionID: session.SessionID(),
		Timeout:   30 * time.Second,
		Action:    artemis.FetchAction{URL: srv.URL(sc.Path), RunScripts: sc.RunScripts},
	})
	if !result.Success {
		t.Fatalf("ExecuteTask failed: code=%s error=%q", result.ErrorCode, result.Error)
	}
	if result.Data == nil {
		t.Fatal("ExecuteTask returned no data")
	}
	links := make([]string, 0, len(result.Data.Links))
	for _, l := range result.Data.Links {
		links = append(links, l.Href)
	}
	return CrossResult{
		URL:        result.Data.URL,
		StatusCode: result.Data.StatusCode,
		Title:      result.Data.Title,
		Text:       result.Data.Text,
		HTML:       result.Data.HTML,
		Markdown:   result.Data.Markdown,
		Links:      links,
	}
}

// -----------------------------------------------------------------------------
// omnimus runner (profile BrowserRuntime)
// -----------------------------------------------------------------------------

type omnimusRunner struct {
	manager *profile.RuntimeManager
	runtime *profile.BrowserRuntime
	session profile.SessionID
	pageID  profile.PageID
	page    *bridge.Page
	binary  browserprocess.Binary
}

func (r *omnimusRunner) name() string { return "omnimus" }

func (r *omnimusRunner) start(ctx context.Context, t *testing.T, srv *Server) {
	t.Helper()
	manager, err := profile.NewRuntimeManager(filepath.Join(t.TempDir(), "omnimus"))
	if err != nil {
		t.Fatalf("NewRuntimeManager: %v", err)
	}
	runtime, err := profile.NewBrowserRuntime(manager)
	if err != nil {
		t.Fatalf("NewBrowserRuntime: %v", err)
	}
	r.runtime = runtime
	r.manager = manager

	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium discovery: %v", err)
	}
	r.binary = binary
}

func (r *omnimusRunner) stop(ctx context.Context, t *testing.T) {
	if r.session != "" {
		_ = r.runtime.Close(ctx, r.session, "owner")
	}
}

func (r *omnimusRunner) canRun(sc Scenario) (bool, string) {
	if sc.AsyncFetch {
		return false, "BrowserRuntime does not support AsyncFetch interception"
	}
	if sc.Expect.Status != 0 && sc.Expect.Status != 200 {
		return false, "BrowserRuntime cannot verify non-200 HTTP status"
	}
	return true, ""
}

func (r *omnimusRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	if r.session == "" {
		sess, err := r.runtime.Open(ctx, profile.OpenSessionRequest{
			ProfileID:    "fixture-omnimus",
			OwnerUserRef: "owner",
			Class:        profile.ProfileEphemeral,
			Lifetime:     5 * time.Minute,
		}, browserprocess.LaunchConfig{
			BinaryPath:      r.binary.Path,
			Headless:        true,
			StartupTimeout:  15 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatalf("BrowserRuntime.Open: %v", err)
		}
		r.session = sess.ID
	}
	if r.pageID != "" {
		if err := r.runtime.ClosePage(r.session, r.pageID, "owner"); err != nil {
			t.Fatalf("ClosePage: %v", err)
		}
		r.pageID = ""
		r.page = nil
	}
	pid, page, err := r.runtime.NewPage(ctx, r.session, "owner", srv.URL(sc.Path))
	if err != nil {
		r.pageID = ""
		r.page = nil
		t.Fatalf("NewPage: %v", err)
	}
	r.pageID = pid
	r.page = page
	if err := waitForReadyStateComplete(ctx, page); err != nil {
		t.Fatalf("omnimus wait for ready state: %v", err)
	}
	return resultFromBridgePage(ctx, t, page, sc)
}
