package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/stealth"
)

func TestChromiumTargetScriptsRunBeforePageAndWorkerCode(t *testing.T) {
	binary := requireChromium(t)
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprint(w, `<!doctype html><title>stealth-fixture</title><script>
		window.pageProbe = JSON.stringify({webdriver:navigator.webdriver,ua:navigator.userAgent,platform:navigator.platform,language:navigator.language});
		const worker = new Worker(URL.createObjectURL(new Blob(["postMessage(JSON.stringify({webdriver:navigator.webdriver,ua:navigator.userAgent,platform:navigator.platform}))"], {type:"text/javascript"})));
		worker.onmessage = (event) => { document.body.dataset.workerProbe = event.data };
		</script><body>ready</body>`); err != nil {
			t.Errorf("write stealth fixture response: %v", err)
		}
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := LaunchChromium(ctx, browserprocess.LaunchConfig{
		BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second,
		AllowPrivateNetworks: true, AllowedPorts: []int{testURLPort(t, fixture.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "browser", browser.Close)
	version := browser.Version()
	major := stealth.ParseChromeVersion(version.UserAgent)
	if major == "" {
		t.Fatalf("cannot parse Chrome version from %q", version.UserAgent)
	}
	profile, err := stealth.NewEnvironmentProfile("integration-session", stealth.StealthStealth, stealth.EnvironmentFacts{
		UserAgent: version.UserAgent, ChromeVersion: major, Platform: platformForTest(), Locale: "en-US", Languages: []string{"en-US", "en"}, Timezone: "UTC", ViewportWidth: 1280, ViewportHeight: 720, DevicePixelRatio: 1, HardwareConcurrency: runtime.NumCPU(), Measured: true,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	pageScript, err := stealth.NewDocumentScript(profile)
	if err != nil {
		t.Fatal(err)
	}
	workerScript, err := stealth.NewWorkerScript(profile)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "browser context", owner.Close)
	ua := strings.ReplaceAll(version.UserAgent, "HeadlessChrome/", "Chrome/")
	chromeVersion := version.Product
	if idx := strings.IndexByte(chromeVersion, '/'); idx >= 0 {
		chromeVersion = chromeVersion[idx+1:]
	}
	page, err := owner.NewPageWithScripts(ctx, fixture.URL, TargetScriptConfig{
		Version: "integration", PageScript: pageScript, WorkerScript: workerScript,
		Emulation: EmulationOverrides{
			UserAgent: ua, AcceptLanguage: "en-US,en", Locale: "en-US", TimezoneID: "UTC",
			Platform: "macOS", ChromeVersion: chromeVersion,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "page", page.Close)
	waitForDocumentTitle(t, ctx, page, "stealth-fixture")
	var result struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err = page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": `JSON.stringify({page:window.pageProbe,worker:document.body.dataset.workerProbe||""})`, "returnByValue": true}, &result)
		if err == nil {
			var probes struct{ Page, Worker string }
			if jsonErr := json.Unmarshal([]byte(result.Result.Value), &probes); jsonErr == nil && strings.Contains(probes.Page, `"webdriver":false`) && strings.Contains(probes.Worker, `"webdriver":false`) {
				if version, statusErr := page.TargetScriptStatus(); version != "integration" || statusErr != nil {
					t.Fatalf("target script status version=%q err=%v", version, statusErr)
				}
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	var probes struct{ Page, Worker string }
	_ = json.Unmarshal([]byte(result.Result.Value), &probes)
	if !strings.Contains(probes.Page, `"webdriver":false`) {
		t.Fatalf("pre-script probes missing: %q err=%v browser=%v", result.Result.Value, err, browser.Err())
	}

	// Lies-detection surface: every patched surface must report native code.
	const liesProbe = `(() => { const safe = (fn) => { try { return fn(); } catch (e) { return "ERR:" + e.message; } }; return JSON.stringify({
		uaGetter: safe(() => Object.getOwnPropertyDescriptor(Navigator.prototype, "userAgent").get.toString()),
		uaValue: safe(() => navigator.userAgent),
		tzo: safe(() => new Date().getTimezoneOffset()),
		getParam: safe(() => WebGLRenderingContext.prototype.getParameter.toString()),
		permQuery: safe(() => navigator.permissions.query.toString()),
		uaDataType: safe(() => navigator.userAgentData.constructor.name),
		chrome: safe(() => typeof window.chrome === "object" && typeof window.chrome.runtime === "object"),
		voices: safe(() => speechSynthesis.getVoices().length)
	}); })()`
	var lies struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": liesProbe, "returnByValue": true}, &lies); err != nil {
		t.Fatalf("lies probe: %v", err)
	}
	var surface struct {
		UAGetter  string `json:"uaGetter"`
		UAValue   string `json:"uaValue"`
		TZO       int    `json:"tzo"`
		GetParam  string `json:"getParam"`
		PermQuery string `json:"permQuery"`
		UAData    string `json:"uaDataType"`
		Chrome    bool   `json:"chrome"`
		Voices    any    `json:"voices"`
	}
	if err := json.Unmarshal([]byte(lies.Result.Value), &surface); err != nil {
		t.Fatalf("decode lies probe: %v (%q)", err, lies.Result.Value)
	}
	for name, got := range map[string]string{
		"userAgent getter": surface.UAGetter, "getParameter": surface.GetParam,
		"permissions.query": surface.PermQuery,
	} {
		if !strings.Contains(got, "[native code]") {
			t.Errorf("%s leaks JS source: %q", name, got)
		}
	}
	if strings.Contains(surface.UAValue, "Headless") {
		t.Errorf("navigator.userAgent still contains Headless token: %q", surface.UAValue)
	}
	if surface.TZO != 0 {
		t.Errorf("timezone override not applied: getTimezoneOffset=%d, want 0 (UTC)", surface.TZO)
	}
	if surface.UAData != "NavigatorUAData" {
		t.Errorf("navigator.userAgentData prototype: %q", surface.UAData)
	}
	if !surface.Chrome {
		t.Error("window.chrome shape missing")
	}
	if voices, ok := surface.Voices.(float64); !ok || voices == 0 {
		t.Errorf("speechSynthesis.getVoices() = %v — headless tell", surface.Voices)
	}
}

func TestTargetScriptConfigIsVersionedAndImmutable(t *testing.T) {
	browser := &ChromiumBrowser{}
	if err := browser.ConfigureTargetScripts(TargetScriptConfig{PageScript: "(() => {})()"}); err == nil {
		t.Fatal("unversioned target script was accepted")
	}
	config := TargetScriptConfig{Version: "sha256:test", PageScript: "(() => {})()"}
	if err := browser.ConfigureTargetScripts(config); err != nil {
		t.Fatal(err)
	}
	if err := browser.ConfigureTargetScripts(config); err != nil {
		t.Fatal(err)
	}
	if err := browser.ConfigureTargetScripts(TargetScriptConfig{}); err == nil {
		t.Fatal("immutable target script contract was reset")
	}
}

func platformForTest() string {
	if runtime.GOOS == "darwin" {
		return "MacIntel"
	}
	return "Linux x86_64"
}
