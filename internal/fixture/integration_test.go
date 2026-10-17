package fixture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/engine"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/profile"
	"github.com/Christopher-Schulze/Artemis/router"
	"github.com/Christopher-Schulze/Artemis/serve"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const serveFixtureToken = "artemis-fixture-test-token"

// runner is a cross-adapter fixture surface. Each concrete implementation
// executes the same deterministic fixture scenarios through a single
// Artemis path: engine, Chromium bridge, hybrid router, serve, Agent API, or
// Artemis BrowserRuntime.
type runner interface {
	name() string
	start(ctx context.Context, t *testing.T, srv *Server)
	stop(ctx context.Context, t *testing.T)
	run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult
}

// TestFixtureCorpusThroughAllPaths runs the deterministic local fixture
// corpus through every supported Artemis path and asserts the same semantic
// outcomes wherever a path is capable of exercising the scenario.
func TestFixtureCorpusThroughAllPaths(t *testing.T) {
	srv := newDefaultTestServer(t)
	defer srv.Close()
	t.Setenv("ARTEMIS_REPLAY_DIR", t.TempDir())
	scenarios, err := srv.CorpusScenarios()
	if err != nil {
		t.Fatal(err)
	}

	runners := []runner{
		&engineRunner{},
		&bridgeRunner{},
		&routerRunner{},
		&serveRunner{},
		&agentRunner{},
		&artemisRunner{},
	}

	for _, r := range runners {
		r := r
		t.Run(r.name(), func(t *testing.T) {
			ctx := context.Background()
			r.start(ctx, t, srv)
			defer r.stop(ctx, t)

			for _, sc := range scenarios {
				sc := sc
				t.Run(sc.ID, func(t *testing.T) {
					manifest, err := srv.CorpusManifest(r.name(), sc)
					if err != nil {
						t.Fatal(err)
					}
					eligibility, err := AssessEligibility(r.name(), sc, time.Now())
					if err != nil {
						t.Fatalf("fixture eligibility: %v", err)
					}
					if eligibility.State != EligibilityReady {
						metadata, err := json.Marshal(eligibility)
						if err != nil {
							t.Fatal(err)
						}
						t.Fatalf("zero passing coverage: %s", metadata)
					}
					if assertCorpusChallengeDenial(ctx, t, r, sc, srv) {
						return
					}
					result := r.run(ctx, t, sc, srv)
					result.FixtureManifest = &manifest
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

// Guarded runners must deny the actual challenge. This verifies a failure,
// contributes zero passing page coverage and cannot produce a page replay.
func assertCorpusChallengeDenial(ctx context.Context, t *testing.T, candidate runner, sc Scenario, srv *Server) bool {
	t.Helper()
	if sc.Kind != KindChallenge {
		return false
	}
	switch owner := candidate.(type) {
	case *routerRunner:
		result, err := owner.rtr.Execute(ctx, router.RouteRequest{URL: srv.URL(sc.Path), Signals: hybridSignals(sc), Action: router.ActionFetch})
		defer closeTestResource(t, "denied route close", result.Close)
		var failure *router.RouteError
		if !errors.As(err, &failure) || failure.Code != router.ErrorChallenge || !errors.Is(err, router.ErrChallengeDetected) || result.Success || result.Resource != nil || result.Output.StatusCode != 0 {
			t.Fatalf("challenge guard lost its actual denial: result=%+v err=%v", result, err)
		}
	case *agentRunner:
		session, err := owner.agent.CreateSession("fixture-runner")
		if err != nil {
			t.Fatal(err)
		}
		defer closeTestResource(t, "denied Agent session close", session.Close)
		result := owner.agent.ExecuteTask(ctx, artemis.Task{ID: sc.ID, SessionID: session.SessionID(), Timeout: 30 * time.Second,
			Action: artemis.FetchAction{URL: srv.URL(sc.Path)}})
		if result.Success || result.Data != nil || result.ErrorCode != artemis.TaskErrorExecutionFailed || !strings.Contains(result.Error, router.ErrorChallenge) {
			t.Fatalf("Agent challenge guard lost its actual denial: %+v", result)
		}
	default:
		return false
	}
	t.Log("actual challenge denied; zero passing page coverage; no page replay")
	return true
}

func assertCrossResult(t *testing.T, sc Scenario, got CrossResult) {
	t.Helper()
	if err := ValidateCrossResult(sc, got); err != nil {
		t.Error(err)
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
		closeTestResource(t, "engine close", r.eng.Close)
	}
}

func (r *engineRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	result, err := RunEngineScenario(ctx, r.eng, sc, srv)
	if err != nil {
		t.Fatalf("engine fixture: %v", err)
	}
	return result
}

func resultFromEnginePage(ctx context.Context, t *testing.T, page *engine.Page, sc Scenario) CrossResult {
	t.Helper()
	return ObserveEnginePage(ctx, page, sc)
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
	policyConfig := srv.PolicyConfig()
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatalf("Chromium discovery: %v", err)
	}
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath:           binary.Path,
		Headless:             true,
		StartupTimeout:       15 * time.Second,
		ShutdownTimeout:      5 * time.Second,
		AllowPrivateNetworks: policyConfig.AllowPrivateNetworks,
		AllowedPorts:         policyConfig.AllowedPorts,
	})
	if err != nil {
		t.Fatalf("LaunchChromium: %v", err)
	}
	r.browser = browser
	ctx2, err := browser.NewContext(ctx)
	if err != nil {
		closeTestResource(t, "browser close after context failure", browser.Close)
		t.Fatalf("NewContext: %v", err)
	}
	r.ctx = ctx2
	page, err := ctx2.NewPage(ctx, "about:blank")
	if err != nil {
		closeTestResource(t, "context close after page failure", ctx2.Close)
		closeTestResource(t, "browser close after page failure", browser.Close)
		t.Fatalf("NewPage: %v", err)
	}
	r.page = page
}

func (r *bridgeRunner) stop(ctx context.Context, t *testing.T) {
	if r.page != nil {
		closeTestResource(t, "page close", r.page.Close)
	}
	if r.ctx != nil {
		closeTestResource(t, "context close", r.ctx.Close)
	}
	if r.browser != nil {
		closeTestResource(t, "browser close", r.browser.Close)
	}
}

func (r *bridgeRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	return navigateFixturePage(ctx, t, r.page, sc, srv.URL(sc.Path))
}

func navigateFixturePage(ctx context.Context, t *testing.T, page *bridge.Page, sc Scenario, target string) CrossResult {
	t.Helper()
	result, err := RunNativeScenario(ctx, page, sc, target)
	if err != nil {
		t.Fatalf("native fixture navigation/observation: %v", err)
	}
	return result
}

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
	policyConfig := srv.PolicyConfig()
	eng, err := engine.New(engine.Config{
		PolicyConfig:      policyConfig,
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
		closeTestResource(t, "engine close after Chromium discovery failure", eng.Close)
		t.Fatalf("Chromium discovery: %v", err)
	}
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath:           binary.Path,
		Headless:             true,
		StartupTimeout:       15 * time.Second,
		ShutdownTimeout:      5 * time.Second,
		AllowPrivateNetworks: policyConfig.AllowPrivateNetworks,
		AllowedPorts:         policyConfig.AllowedPorts,
	})
	if err != nil {
		closeTestResource(t, "engine close after Chromium launch failure", eng.Close)
		t.Fatalf("LaunchChromium: %v", err)
	}
	ctx2, err := browser.NewContext(ctx)
	if err != nil {
		closeTestResource(t, "browser close after context failure", browser.Close)
		closeTestResource(t, "engine close after context failure", eng.Close)
		t.Fatalf("NewContext: %v", err)
	}
	page, err := ctx2.NewPage(ctx, "about:blank")
	if err != nil {
		closeTestResource(t, "context close after page failure", ctx2.Close)
		closeTestResource(t, "browser close after page failure", browser.Close)
		closeTestResource(t, "engine close after page failure", eng.Close)
		t.Fatalf("NewPage: %v", err)
	}
	r.browser = browser
	r.ctx = ctx2
	r.page = page

	rtr, err := router.New(router.Config{
		Executors: map[router.Mode]router.Executor{
			router.ModeStaticFetch:  router.RuntimeExecutor{Runtime: eng, RunScripts: true, AsyncFetch: true},
			router.ModeRenderlessJS: router.RuntimeExecutor{Runtime: eng, RunScripts: true, AsyncFetch: true},
			router.ModeChromiumCDP:  router.ChromiumExecutor{Page: page},
			router.ModeStealth:      router.ChromiumExecutor{Page: page},
		},
	})
	if err != nil {
		closeTestResource(t, "page close after router failure", page.Close)
		closeTestResource(t, "context close after router failure", ctx2.Close)
		closeTestResource(t, "browser close after router failure", browser.Close)
		closeTestResource(t, "engine close after router failure", eng.Close)
		t.Fatalf("router.New: %v", err)
	}
	r.rtr = rtr
}

func (r *routerRunner) stop(ctx context.Context, t *testing.T) {
	if r.eng != nil {
		closeTestResource(t, "engine close", r.eng.Close)
	}
	if r.page != nil {
		closeTestResource(t, "page close", r.page.Close)
	}
	if r.ctx != nil {
		closeTestResource(t, "context close", r.ctx.Close)
	}
	if r.browser != nil {
		closeTestResource(t, "browser close", r.browser.Close)
	}
}

func (r *routerRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	result, err := RunHybridScenario(ctx, r.rtr, r.page, sc, srv)
	if err != nil {
		t.Fatalf("router.Execute: %v", err)
	}
	return result
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
	clientID   string
}

func (r *serveRunner) name() string { return "serve" }

func (r *serveRunner) start(ctx context.Context, t *testing.T, srv *Server) {
	t.Helper()
	agent, err := artemis.NewAgent(artemis.AgentConfig{PolicyConfig: srv.PolicyConfig(), PolicyEngine: fixtureControlPolicy(t, srv)})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if startErr := agent.Start(ctx); startErr != nil {
		t.Fatalf("agent.Start: %v", startErr)
	}
	r.agent = agent
	r.server = serve.New(agent, serve.Opts{
		AuthToken: serveFixtureToken,
		RateLimit: serve.RateLimit{
			RequestsPerSecond: 1000, Burst: 1000,
			ClientRequestsPerMinute: 60000, ClientBurst: 1000, ClientBucketTTL: time.Hour,
		},
	})
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	r.addr = ln.Addr().String()
	r.httpServer = &http.Server{Handler: http.HandlerFunc(r.server.HandleWSForTest), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := r.httpServer.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Errorf("fixture HTTP server: %v", serveErr)
		}
	}()
	c := r.dial(t)
	defer closeTestResource(t, "session websocket close", func() error {
		return c.Close(websocket.StatusNormalClosure, "")
	})
	resp := r.call(t, c, serve.CmdSessionNew, serve.SessionNewParams{})
	var sess serve.SessionNewResult
	if err := serve.DecodeTypedResult(&resp, &sess); err != nil {
		t.Fatalf("session.new decode: %v", err)
	}
	r.sessID = sess.SessionID
}

func (r *serveRunner) stop(ctx context.Context, t *testing.T) {
	if r.httpServer != nil {
		closeTestResource(t, "HTTP server close", r.httpServer.Close)
	}
	if r.server != nil {
		closeTestResource(t, "serve server close", r.server.Close)
	}
	if r.agent != nil {
		closeTestResource(t, "agent stop", r.agent.Stop)
	}
}

func (r *serveRunner) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	header := http.Header{"Authorization": []string{"Bearer " + serveFixtureToken}}
	if r.clientID != "" {
		header.Set(serve.ClientIDHeader, r.clientID)
	}
	dialOptions := &websocket.DialOptions{HTTPHeader: header}
	c, response, err := websocket.Dial(context.Background(), "ws://"+r.addr+"/", dialOptions)
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close handshake response body: %v", closeErr)
		}
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	r.clientID = response.Header.Get(serve.ClientIDHeader)
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
	defer closeTestResource(t, "scenario websocket close", func() error {
		return c.Close(websocket.StatusNormalClosure, "")
	})

	resp := r.call(t, c, serve.CmdPageOpen, serve.PageOpenParams{
		SessionID:  r.sessID,
		URL:        srv.URL(sc.Path),
		RunScripts: sc.RunScripts,
		AsyncFetch: sc.AsyncFetch,
	})
	var open serve.PageOpenResult
	if err := serve.DecodeTypedResult(&resp, &open); err != nil {
		t.Fatalf("page.open: %v", err)
	}
	pageID := open.PageID
	if sc.RunScripts && sc.WaitForIdle {
		waitResp := r.call(t, c, serve.CmdPageWaitIdle, serve.PageWaitIdleParams{SessionID: r.sessID, PageID: pageID})
		if !waitResp.OK {
			t.Fatalf("page.wait_idle: %v", waitResp.Error)
		}
	}

	var evalRes, evalErr string
	if sc.Expect.Eval != "" {
		evalResp := r.call(t, c, serve.CmdPageEval, serve.PageEvalParams{
			SessionID: r.sessID,
			PageID:    pageID,
			Expr:      sc.Expect.Eval,
		})
		if !evalResp.OK {
			evalErr = fmt.Sprintf("%s: %s", evalResp.Error.Code, evalResp.Error.Message)
		} else {
			var eval serve.PageEvalResult
			if decodeErr := serve.DecodeTypedResult(&evalResp, &eval); decodeErr != nil {
				t.Fatalf("page.eval decode: %v", decodeErr)
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
	links, err := linkStrings(linksDump.Data)
	if err != nil {
		t.Fatalf("page.dump links: %v", err)
	}

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

func linkStrings(v any) ([]string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal links: %w", err)
	}
	var links []struct{ Href string }
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil, fmt.Errorf("decode links: %w", err)
	}
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, l.Href)
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// agent runner (public Agent API)
// -----------------------------------------------------------------------------

type agentRunner struct{ agent *artemis.Agent }

func (r *agentRunner) name() string { return "agent" }

func (r *agentRunner) start(ctx context.Context, t *testing.T, srv *Server) {
	t.Helper()
	agent, err := artemis.NewAgent(artemis.AgentConfig{PolicyConfig: srv.PolicyConfig(), PolicyEngine: fixtureControlPolicy(t, srv)})
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
		closeTestResource(t, "agent stop", r.agent.Stop)
	}
}

func (r *agentRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	session, err := r.agent.CreateSession("fixture-runner")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer closeTestResource(t, "fixture Agent session close", session.Close)
	result := r.agent.ExecuteTask(ctx, artemis.Task{
		ID:        sc.ID,
		SessionID: session.SessionID(),
		Timeout:   30 * time.Second,
		Action:    artemis.FetchAction{URL: srv.URL(sc.Path), RunScripts: sc.RunScripts, AsyncFetch: sc.AsyncFetch, EvaluateExpression: sc.Expect.Eval},
	})
	if !result.Success {
		t.Fatalf("ExecuteTask failed: code=%s error=%q", result.ErrorCode, result.Error)
	}
	if result.Data == nil {
		t.Fatal("ExecuteTask returned no data")
	}
	var evaluation string
	if sc.Expect.Eval != "" {
		if result.Data.Evaluation == nil {
			t.Fatal("ExecuteTask omitted requested evaluation")
		}
		evaluation = *result.Data.Evaluation
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
		EvalResult: evaluation,
	}
}

func fixtureControlPolicy(t *testing.T, server *Server) *bridge.NavigationPolicy {
	t.Helper()
	data, err := json.Marshal(bridge.PolicyFile{Version: bridge.PolicyFileVersion, Default: bridge.PolicyFileDeny,
		Allow: []string{"navigate", "evaluate", "wait", "extract", "tab_close"},
		Rules: []bridge.PolicyRule{{Action: bridge.PolicyFileAllow, Pattern: regexp.QuoteMeta(server.BaseURL()) + "/.*"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := bridge.NewStaticNavigationPolicy(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(policy.Stop)
	return policy
}

// -----------------------------------------------------------------------------
// artemis runner (profile BrowserRuntime)
// -----------------------------------------------------------------------------

type artemisRunner struct {
	manager *profile.RuntimeManager
	runtime *profile.BrowserRuntime
	session profile.SessionID
	pageID  profile.PageID
	page    *bridge.Page
	binary  browserprocess.Binary
}

func (r *artemisRunner) name() string { return "artemis" }

func (r *artemisRunner) start(ctx context.Context, t *testing.T, srv *Server) {
	t.Helper()
	manager, err := profile.NewRuntimeManager(filepath.Join(t.TempDir(), "artemis"))
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

func (r *artemisRunner) stop(ctx context.Context, t *testing.T) {
	if r.session != "" {
		closeTestResource(t, "BrowserRuntime session close", func() error {
			return r.runtime.Close(ctx, r.session, "owner")
		})
	}
}

func (r *artemisRunner) run(ctx context.Context, t *testing.T, sc Scenario, srv *Server) CrossResult {
	t.Helper()
	if r.session == "" {
		policyConfig := srv.PolicyConfig()
		sess, err := r.runtime.Open(ctx, profile.OpenSessionRequest{
			ProfileID:    "fixture-artemis",
			OwnerUserRef: "owner",
			Class:        profile.ProfileEphemeral,
			Lifetime:     5 * time.Minute,
		}, browserprocess.LaunchConfig{
			BinaryPath:           r.binary.Path,
			Headless:             true,
			StartupTimeout:       15 * time.Second,
			ShutdownTimeout:      5 * time.Second,
			AllowPrivateNetworks: policyConfig.AllowPrivateNetworks,
			AllowedPorts:         policyConfig.AllowedPorts,
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
	pid, page, err := r.runtime.NewPage(ctx, r.session, "owner", "about:blank")
	if err != nil {
		r.pageID = ""
		r.page = nil
		t.Fatalf("NewPage: %v", err)
	}
	r.pageID = pid
	r.page = page
	return navigateFixturePage(ctx, t, page, sc, srv.URL(sc.Path))
}
