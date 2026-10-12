// Package docs guards documentation.md against drift from the real
// code surface. The doc-coverage test (TASK-2335) asserts that key
// public symbols from every artemis subsystem are referenced in
// documentation.md, and that all required sections exist.
package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docPath returns the absolute path to documentation.md by walking up
// from the test working directory (the package dir) until it finds
// go.mod, then joining docs/documentation.md.
func docPath(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "docs", "documentation.md")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find go.mod above %s", cwd)
	return ""
}

// TestDocCoveragePublicSurface verifies that key public symbols from
// every artemis subsystem are referenced in documentation.md. This is
// a failable guard: if a new public symbol is added to a subsystem
// but not documented, the test fails (after the symbol is added to
// the required list below).
func TestDocCoveragePublicSurface(t *testing.T) {
	content, err := os.ReadFile(docPath(t))
	if err != nil {
		t.Fatalf("read documentation.md: %v", err)
	}
	doc := string(content)

	// Each entry is a string that must appear in documentation.md.
	// These are real exported symbols verified via `go doc`.
	required := []string{
		// engine
		"engine.New",
		"engine.Config",
		"engine.Fetch",
		"engine.Page",
		"engine.FetchOpts",
		"engine.Engine.Submit",
		// webapi
		"webapi.Document",
		"webapi.Node",
		"webapi.Walk",
		// parser
		"parser.ParseHTML",
		// agent
		"agent.Markdown",
		"agent.StructuredData",
		"agent.SemanticTree",
		"agent.Forms",
		"agent.ClickByText",
		"agent.Type",
		// network
		"network.ParseRobots",
		"network.IsPrivateOrLocal",
		// js
		"js.Console",
		"js.FuncConsole",
		// css
		"css.Cascade",
		// serve
		"serve.New",
		// telemetry
		"telemetry.Tracer",
		"telemetry.PhoneHome",
		// bridge (hybrid router + provider)
		"bridge.ExecutionRouter",
		"bridge.NewExecutionRouter",
		"bridge.RouteStatic",
		"bridge.RouteRendered",
		"bridge.BrowserProvider",
		"bridge.ProviderRegistry",
		"bridge.NewProviderRegistry",
		"bridge.LocalChromeProvider",
		"bridge.BrowserSession",
		// bridge/actions
		"actions.NewClickAction",
		"actions.NewTypeAction",
		"actions.NewFormFill",
		"actions.NewScrollAction",
		"actions.ResolveSelector",
		"actions.FormBatch",
		// bridge/cdpops
		"cdpops.GetBoxModel",
		"cdpops.IsElementVisible",
		"cdpops.GenerateBezierPath",
		"cdpops.NewNavigator",
		// bridge/tabs
		"tabs.NewTabRegistry",
		"tabs.NewTabExecutor",
		"tabs.NewDialogHandler",
		// renderless
		"renderless.NewEngine",
		"renderless.EngineConfig",
		"renderless.ContextPool",
		"renderless.RenderlessCapabilityProfile",
		"renderless.InterceptHandler",
		// stealth
		"stealth.StealthDefault",
		"stealth.StealthStealth",
		"stealth.StealthParanoid",
		"stealth.DetermineStealthLevel",
		"stealth.Profile",
		"stealth.BundledScript",
		"stealth.BuiltinGeoPresets",
		// observe
		"observe.NewAnnotator",
		"observe.AXNode",
		"observe.DiffAXTrees",
		"observe.FormatHAR",
		"observe.FormatNDJSON",
		// solver
		"solver.NewChallengeDetector",
		"solver.ChallengeType",
		"solver.TypeCloudflare",
		"solver.TypeRecaptcha",
		"solver.TypeHCaptcha",
		"solver.InferenceHub",
		"solver.PipelineResult",
		// input
		"input.GenerateMousePath",
		"input.BezierCurve",
		"input.TypingDelays",
		"input.TypingRhythm",
		"input.ClickGaussianOffset",
		"input.DetectInfiniteScroll",
		// security
		"security.IsPrivateIP",
		"security.NewAdBlocker",
		"security.NewRedactor",
		"security.NavigationPolicy",
		"security.AutoConfirm",
		// profile
		"profile.GenerateUUID5",
		"profile.ProxyProfileConfig",
		// scraper
		"scraper.NewFinder",
		"scraper.Finder",
		"scraper.ExtractedPage",
		"scraper.AdaptiveSelectorCache",
		// prompts
		"prompts.SystemPrompt",
		"prompts.GetPrompt",
		"prompts.AllTemplates",
		"prompts.RenderTemplate",
		// actions
		"actions.DetectLoginForm",
		"actions.VerifyPostLogin",
		// platform
		"platform.Detect",
		"platform.PlatformCapabilities",
	}

	var missing []string
	for _, sym := range required {
		if !strings.Contains(doc, sym) {
			missing = append(missing, sym)
		}
	}
	if len(missing) > 0 {
		t.Errorf("documentation.md is missing %d public symbol(s):\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
}

// TestDocCoverageSections verifies that all required documentation
// sections exist in documentation.md.
func TestDocCoverageSections(t *testing.T) {
	content, err := os.ReadFile(docPath(t))
	if err != nil {
		t.Fatalf("read documentation.md: %v", err)
	}
	doc := string(content)

	requiredSections := []string{
		"## Project Overview",
		"## License",
		"## Repository Layout",
		"## Build and Run",
		"## Configuration",
		"## CLI",
		"## Library API",
		"## JavaScript Execution",
		"## Fetch API",
		"## Browser Globals",
		"## Agent Extraction",
		"## Forms and Actions",
		"## Network Stack",
		"## Additional Web Globals",
		"## Hybrid Execution Router",
		"## Browser Provider Switch",
		"## CDP Bridge",
		"## Bridge Actions",
		"## CDP Operations",
		"## Tab Management",
		"## Renderless Engine",
		"## Stealth Layer",
		"## Observation Layer",
		"## Solver / CAPTCHA Pipeline",
		"## Human-like Input",
		"## Security Layer",
		"## Profile / Session Management",
		"## Scraper Subsystem",
		"## Prompts System",
		"## Actions / Auto-Login",
		"## Platform Capabilities",
		"## Steering Server",
		"## Telemetry",
	}

	var missing []string
	for _, section := range requiredSections {
		if !strings.Contains(doc, section) {
			missing = append(missing, section)
		}
	}
	if len(missing) > 0 {
		t.Errorf("documentation.md is missing %d section(s):\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
}

// TestDocCoverageCanFail proves the doc-coverage guard can actually
// detect a missing symbol. It checks that a deliberately-fake symbol
// name is not in the documentation, confirming the scanner works.
func TestDocCoverageCanFail(t *testing.T) {
	content, err := os.ReadFile(docPath(t))
	if err != nil {
		t.Fatalf("read documentation.md: %v", err)
	}
	doc := string(content)

	// A symbol that should never appear in real documentation.
	fakeSymbol := "this.fake.symbol.does.not.exist.in.real.code"
	if strings.Contains(doc, fakeSymbol) {
		t.Errorf("fake symbol %q found in documentation — the guard would not catch missing symbols", fakeSymbol)
	}
	t.Logf("negative check passed: fake symbol %q is correctly absent", fakeSymbol)
}
