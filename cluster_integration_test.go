package artemis_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/actions"
	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/input"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/observe"
	"github.com/Christopher-Schulze/Artemis/scraper"
	"github.com/Christopher-Schulze/Artemis/security"
	"github.com/Christopher-Schulze/Artemis/stealth"
)

func TestArtemisPackageLayout(t *testing.T) {
	if stealth.Quick() == "" {
		t.Fatal("stealth quick script required")
	}
	if bridge.NewBatcher(nil, 4) == nil {
		t.Fatal("bridge batcher required")
	}
}

func TestPublicAPI(t *testing.T) {
	_ = stealth.Defaults()
	_ = scraper.NewFinder()
	_ = network.Netguard{BlockPrivate: true}
}

func TestCheckHealthRecovery(t *testing.T) {
	h := bridge.NewHealthChecker(2)
	st, err := bridge.CheckHealthRecovery(func() bool { return true }, h, 2)
	if err != nil || st != bridge.HealthHealthy {
		t.Fatalf("st=%s err=%v", st, err)
	}
}

func TestCDPContextHierarchy(t *testing.T) {
	tree := bridge.NewCDPContextTree()
	_ = tree.Attach(bridge.CDPContextNode{ID: "root", Kind: "browser"})
	_ = tree.Attach(bridge.CDPContextNode{ID: "tab1", ParentID: "root", Kind: "page"})
	chain, err := tree.Hierarchy("tab1")
	if err != nil || len(chain) != 2 {
		t.Fatalf("chain=%v err=%v", chain, err)
	}
}

func TestTabRegistry(t *testing.T) {
	reg := bridge.NewTabRegistry()
	_ = reg.Register(bridge.Tab{ID: "t1", URL: "https://example.com", ContextID: "ctx1"})
	if reg.Len() != 1 {
		t.Fatalf("len=%d", reg.Len())
	}
}

func TestTabExecutorLifecycle(t *testing.T) {
	reg := bridge.NewTabRegistry()
	_ = reg.Register(bridge.Tab{ID: "t1", ContextID: "c1"})
	reg.Remove("t1")
	if reg.Len() != 0 {
		t.Fatal("tab should be removed")
	}
}

func TestSelectorResolutionPriority(t *testing.T) {
	f := scraper.NewFinder()
	priority := []string{"#id", ".class", "tag"}
	got := f.ResolvePriority("submit", priority)
	if got != "#id" {
		t.Fatalf("priority=%q", got)
	}
}

func TestGeoPresetResolve(t *testing.T) {
	p := stealth.ResolveGeoPreset("de")
	if p.Timezone != "Europe/Berlin" {
		t.Fatalf("tz=%s", p.Timezone)
	}
}

func TestContextHashIsolation(t *testing.T) {
	a := stealth.Profile{Seed: "a"}
	b := stealth.Profile{Seed: "b"}
	if stealth.Script(a) == stealth.Script(b) {
		t.Fatal("profiles must diverge")
	}
}

func TestStealthTwentySevenPatches(t *testing.T) {
	if stealth.PatchCount() < 27 {
		t.Fatalf("patches=%d", stealth.PatchCount())
	}
}

func TestWorkerStealthParity(t *testing.T) {
	if stealth.Quick() != stealth.Script(stealth.Defaults()) {
		t.Fatal("worker stealth must match defaults profile")
	}
}

func TestStealthExtensionLoaded(t *testing.T) {
	s := stealth.Quick()
	if !strings.Contains(s, "webdriver") {
		t.Fatal("stealth bundle must patch webdriver")
	}
}

func TestNoPreDOMInjectionMarker(t *testing.T) {
	if strings.Contains(stealth.Quick(), "data-artemis-injected") {
		t.Fatal("pre-dom marker forbidden")
	}
}

func TestRequestTimingJitter(t *testing.T) {
	start := time.Now()
	network.RequestTimingJitter(5*time.Millisecond, 15*time.Millisecond, rand.New(rand.NewPCG(1, 2)))
	if time.Since(start) < 5*time.Millisecond {
		t.Fatal("jitter too short")
	}
}

func TestDNSProxyConsistency(t *testing.T) {
	g := network.Netguard{BlockPrivate: true}
	if g.Allow("http://127.0.0.1/") == nil {
		t.Fatal("loopback must be blocked")
	}
}

func TestNetguardPrivateIPBlock(t *testing.T) {
	g := network.Netguard{BlockPrivate: true}
	if err := g.Allow("http://10.0.0.1/"); err == nil {
		t.Fatal("private ip must be blocked")
	}
}

func TestAdblockEasyListUpdate(t *testing.T) {
	f := network.FilterEasyListRules([]string{"||ads.example.com^"})
	if len(f) == 0 {
		t.Fatal("expected rules")
	}
}

func TestHoneypotSkipInvisible(t *testing.T) {
	d := security.ClassifyHoneypot(map[string]string{"style": "display:none", "name": "email"})
	if !d.Skip {
		t.Fatal("invisible honeypot must skip")
	}
}

func TestBezierMousePath(t *testing.T) {
	path := input.BezierPath(input.Point{X: 0, Y: 0}, input.Point{X: 100, Y: 50}, 10, rand.New(rand.NewPCG(2, 3)))
	if len(path) != 10 || input.PathLength(path) <= 0 {
		t.Fatalf("path=%d", len(path))
	}
}

func TestClickGaussianOffset(t *testing.T) {
	dx, dy := input.ClickGaussianOffset(2, rand.New(rand.NewPCG(4, 5)))
	if dx == 0 && dy == 0 {
		t.Fatal("expected non-zero offset")
	}
}

func TestScraperRecoveryChain(t *testing.T) {
	ex := &scraper.RecoveryExecutor{
		Backoff: func(ctx context.Context, n int) error { return nil },
	}
	step, err := ex.Run(context.Background(), "https://example.com", scraper.DefaultRecoveryPlan())
	if err != nil || step != scraper.StepBackoff {
		t.Fatalf("step=%s err=%v", step, err)
	}
}

func TestConcurrentScrapeStaticBulk(t *testing.T) {
	c := &scraper.ConcurrentScraper{
		Workers: 2,
		Scrape: func(ctx context.Context, job scraper.ConcurrentJob) (string, error) {
			return "ok:" + job.URL, nil
		},
	}
	res, err := c.Run(context.Background(), []scraper.ConcurrentJob{{URL: "https://a", Mode: scraper.ModeStaticBulk}})
	if err != nil || len(res) != 1 || res[0].Body == "" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestDomainRateLimiter(t *testing.T) {
	l := scraper.NewDomainRateLimiter(100 * time.Millisecond)
	l.RecordImpact("https://example.com/x", true)
	if l.Interval("example.com") >= 100*time.Millisecond {
		t.Fatal("impact must halve interval")
	}
}

func TestImpactHalveRate(t *testing.T) {
	l := scraper.NewDomainRateLimiter(400 * time.Millisecond)
	l.RecordImpact("https://host.test/", true)
	if l.Interval("host.test") != 200*time.Millisecond {
		t.Fatalf("iv=%s", l.Interval("host.test"))
	}
}

func TestNetworkRingBufferCap(t *testing.T) {
	rb := observe.NewNetworkRingBuffer(2)
	rb.Push(observe.NetworkEvent{URL: "a"})
	rb.Push(observe.NetworkEvent{URL: "b"})
	rb.Push(observe.NetworkEvent{URL: "c"})
	if rb.Len() != 2 || rb.Snapshot()[0].URL != "b" {
		t.Fatalf("snap=%v", rb.Snapshot())
	}
}

func TestAXTreeMyersDiff(t *testing.T) {
	before := []observe.AXNode{{ID: "1", Role: "button", Name: "ok"}}
	after := []observe.AXNode{{ID: "2", Role: "button", Name: "ok"}}
	if observe.MyersDiff(before, after) == 0 {
		t.Fatal("id change must produce diff")
	}
}

func TestRoleSnapshotDedup(t *testing.T) {
	nodes := []observe.AXNode{{Role: "button", Name: "x"}, {Role: "button", Name: "x"}}
	out := observe.DedupRoleSnapshot(nodes)
	if len(out) != 1 {
		t.Fatalf("len=%d", len(out))
	}
}

func TestFormIntentPrefetch(t *testing.T) {
	f := actions.FormIntent{
		ActionURL: "https://example.com/submit",
		Method:    "POST",
		Fields:    []actions.FormField{{Name: "email", Value: "a@b.c"}},
		Prefetch:  true,
	}
	if err := f.Validate(); err != nil || f.PrefetchKey() == "" {
		t.Fatalf("err=%v key=%q", err, f.PrefetchKey())
	}
}

func TestCDPBatchNavigateSnapshot(t *testing.T) {
	var flushed int
	b := bridge.NewBatcher(func(ctx context.Context, cmds []bridge.Command) ([]bridge.Response, error) {
		flushed = len(cmds)
		return nil, nil
	}, 4)
	if err := bridge.NavigateSnapshot(context.Background(), b, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if flushed != 2 {
		t.Fatalf("flushed=%d", flushed)
	}
}

func TestStructuredExtractSchema(t *testing.T) {
	schema := scraper.StructuredSchema{Fields: []string{"title", "price"}}
	if len(schema.Required()) == 0 {
		t.Fatal("schema fields required")
	}
}

func TestFrameAwareClick(t *testing.T) {
	if !bridge.FrameAwareSelector("#main", "button.submit") {
		t.Fatal("frame aware selector expected")
	}
}

func TestStealthLaunchFlagsBlockedHarmful(t *testing.T) {
	flags := stealth.DefaultLaunchFlags()
	if flags.Allows("--disable-web-security") {
		t.Fatal("harmful flag must be blocked")
	}
}

func TestResourceBlockingDefaults(t *testing.T) {
	cfg := stealth.DefaultResourceBlocking()
	if !cfg.BlockImages && !cfg.BlockFonts {
		t.Fatal("resource blocking defaults expected")
	}
}

func TestCodeModeBrowserEvaluate(t *testing.T) {
	out, err := bridge.EvaluateExpression("1+1", nil)
	if err != nil || out != "2" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestStealthBundleSingleCDPInject(t *testing.T) {
	bundle := stealth.Quick()
	if strings.Count(bundle, "(() => {") != 1 {
		t.Fatal("single IIFE inject expected")
	}
}

func TestStreamingParseTee(t *testing.T) {
	a, b, err := scraper.TeeParseReaders(strings.NewReader("hello"), strings.NewReader("world"))
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || b == nil {
		t.Fatal("tee readers required")
	}
}

func TestParseWorkerPool(t *testing.T) {
	pool := scraper.NewParseWorkerPool(2)
	if pool.Workers() != 2 {
		t.Fatalf("workers=%d", pool.Workers())
	}
}

func TestSnapshotBuilderPool(t *testing.T) {
	pool := scraper.NewSnapshotBuilderPool(4)
	if pool.Cap() != 4 {
		t.Fatalf("cap=%d", pool.Cap())
	}
}

func TestTabRecycleAboutBlank(t *testing.T) {
	tab := bridge.RecycleTab("tab-1")
	if tab.URL != "about:blank" {
		t.Fatalf("url=%s", tab.URL)
	}
}

func TestConsentAutoConfirmDenyDefault(t *testing.T) {
	if security.DefaultConsentAction() != security.ConsentDeny {
		t.Fatal("consent must default deny")
	}
}

func TestLoginFlowDetect(t *testing.T) {
	if !security.DetectLoginFlow("Sign in to continue") {
		t.Fatal("login flow expected")
	}
}

func TestBrowserPromptTemplatesRender(t *testing.T) {
	out := bridge.RenderBrowserPrompt("navigate", map[string]string{"url": "https://example.com"})
	if !strings.Contains(out, "example.com") {
		t.Fatalf("out=%q", out)
	}
}

func TestBrowserProviderSelect(t *testing.T) {
	prov, err := bridge.SelectBrowserProvider([]string{"artemis", "external"}, "artemis")
	if err != nil || prov != "artemis" {
		t.Fatalf("prov=%s err=%v", prov, err)
	}
}

func TestWaitForTextGone(t *testing.T) {
	ok := bridge.WaitForTextGone(func() (string, error) { return "", nil }, "loading", 10*time.Millisecond)
	if !ok {
		t.Fatal("empty text means gone")
	}
}

func TestCDPConnectDualMode(t *testing.T) {
	mode, err := bridge.ResolveCDPConnectMode("attach")
	if err != nil || mode != bridge.CDPAttach {
		t.Fatalf("mode=%s err=%v", mode, err)
	}
}

func TestExecutionRouterEscalation(t *testing.T) {
	r := bridge.NewExecutionRouter()
	next, err := r.Escalate(bridge.RouteStatic, errors.New("timeout"))
	if err != nil || next != bridge.RouteRendered {
		t.Fatalf("next=%s err=%v", next, err)
	}
}
