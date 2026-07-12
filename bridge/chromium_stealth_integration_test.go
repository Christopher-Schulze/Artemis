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
		_, _ = fmt.Fprint(w, `<!doctype html><title>stealth-fixture</title><script>
		window.pageProbe = JSON.stringify({webdriver:navigator.webdriver,ua:navigator.userAgent,platform:navigator.platform,language:navigator.language});
		const worker = new Worker(URL.createObjectURL(new Blob(["postMessage(JSON.stringify({webdriver:navigator.webdriver,ua:navigator.userAgent,platform:navigator.platform}))"], {type:"text/javascript"})));
		worker.onmessage = (event) => { document.body.dataset.workerProbe = event.data };
		</script><body>ready</body>`)
	}))
	defer fixture.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	browser, err := LaunchChromium(ctx, browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
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
	defer owner.Close()
	page, err := owner.NewPageWithScripts(ctx, fixture.URL, TargetScriptConfig{Version: "integration", PageScript: pageScript, WorkerScript: workerScript})
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
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
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pre-script probes missing: %q err=%v", result.Result.Value, err)
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
