package engine

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/security"
)

// adFixture serves a site whose content is written by scripts from a second
// server under /ads/, which the operator rule "/ads/" blocks.
type adFixture struct {
	site, ads *httptest.Server
	adHits    atomic.Int64
	page      string
}

func newAdFixture(t *testing.T, page func(adBase string) string) *adFixture {
	t.Helper()
	fixture := &adFixture{}
	fixture.ads = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.adHits.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, ".css"):
			w.Header().Set("Content-Type", "text/css")
			writeTestBody(t, w, "body{color:red}")
		default:
			w.Header().Set("Content-Type", "application/javascript")
			writeTestBody(t, w, `document.getElementById("app").textContent += "`+strings.Repeat("content ", 8)+`";`)
		}
	}))
	t.Cleanup(fixture.ads.Close)
	fixture.page = page(fixture.ads.URL)
	fixture.site = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		writeTestBody(t, w, fixture.page)
	}))
	t.Cleanup(fixture.site.Close)
	return fixture
}

func (f *adFixture) engine(t *testing.T, blocker *security.AdBlocker, disable bool) *Engine {
	t.Helper()
	config := testConfig(f.site, f.ads)
	config.AdBlock, config.DisableAdBlock = blocker, disable
	eng, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestResource(t, "engine close", eng.Close) })
	return eng
}

func adScripts(adBase string, count int) string {
	var scripts strings.Builder
	for i := range count {
		fmt.Fprintf(&scripts, `<script src="%s/ads/slot%d.js"></script>`, adBase, i)
	}
	return scripts.String()
}

func adPathBlocker(t *testing.T) *security.AdBlocker {
	t.Helper()
	blocker := security.NewAdBlocker()
	if err := blocker.AddRules("/ads/"); err != nil {
		t.Fatal(err)
	}
	return blocker
}

func TestRenderlessBlocksAdSubresourcesByDefaultAndKeepsThePageWorking(t *testing.T) {
	fixture := newAdFixture(t, func(adBase string) string {
		return `<!doctype html><title>Site</title><link rel="stylesheet" href="` + adBase + `/ads/x.css"><iframe src="` + adBase + `/ads/frame.html"></iframe><body><div id="app">` + strings.Repeat("article ", 40) + `</div><script>window.frameDocument = document.querySelector("iframe").contentDocument;</script>` + adScripts(adBase, 1) + `</body>`
	})
	blocked := fixture.engine(t, adPathBlocker(t), false)
	page, err := blocked.Fetch(t.Context(), fixture.site.URL, FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatalf("a blocked stylesheet or frame failed the page: %v", err)
	}
	defer closeTestResource(t, "page close", page.Close)
	if fixture.adHits.Load() != 0 || page.AdBlockedRequests() != 3 || strings.Contains(page.Text(), "content") {
		t.Fatalf("ad subresources were not blocked: hits=%d blocked=%d", fixture.adHits.Load(), page.AdBlockedRequests())
	}
	unblocked := fixture.engine(t, nil, true)
	open, err := unblocked.Fetch(t.Context(), fixture.site.URL, FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page close", open.Close)
	if fixture.adHits.Load() == 0 || open.AdBlockedRequests() != 0 || !strings.Contains(open.Text(), "content") {
		t.Fatalf("DisableAdBlock did not turn blocking off: hits=%d", fixture.adHits.Load())
	}
}

func TestRenderlessLayer2WhitelistsBrokenSiteAfterVerifiedReload(t *testing.T) {
	fixture := newAdFixture(t, func(adBase string) string {
		return `<!doctype html><title>Wall</title><body><div id="app"></div>` + adScripts(adBase, 6) + `</body>`
	})
	blocker := adPathBlocker(t)
	eng := fixture.engine(t, blocker, false)
	page, err := eng.Fetch(t.Context(), fixture.site.URL, FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page close", page.Close)
	finding := page.AdBlockFinding()
	if finding == nil || finding.Case != security.AdBlockCaseBreakage || finding.Decision != security.AdBlockDecisionWhitelisted || finding.BlockedRequests != 6 {
		t.Fatalf("broken page was not verified and whitelisted: %+v", finding)
	}
	if !strings.Contains(page.Text(), "content") || page.AdBlockedRequests() != 0 {
		t.Fatalf("the unblocked verification page did not replace the broken page: %q", page.Text())
	}
	if !blocker.Whitelist().IsWhitelisted("127.0.0.1") {
		t.Fatal("site not whitelisted")
	}
	again, err := eng.Fetch(t.Context(), fixture.site.URL, FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page close", again.Close)
	if again.AdBlockedRequests() != 0 || again.AdBlockFinding() != nil {
		t.Fatalf("whitelisted site still blocked: %d", again.AdBlockedRequests())
	}
}

func TestRenderlessLayer2ReportsAdblockWallForUserDecision(t *testing.T) {
	fixture := newAdFixture(t, func(adBase string) string {
		return `<!doctype html><title>Wall</title><body><div id="app"></div><div class="adblock-notice">Please disable your adblocker to continue reading.</div>` + adScripts(adBase, 1) + `</body>`
	})
	blocker := adPathBlocker(t)
	page, err := fixture.engine(t, blocker, false).Fetch(t.Context(), fixture.site.URL, FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page close", page.Close)
	finding := page.AdBlockFinding()
	if finding == nil || finding.Case != security.AdBlockCaseOverlay || finding.Decision != security.AdBlockDecisionUser || blocker.Whitelist().IsWhitelisted("127.0.0.1") {
		t.Fatalf("adblock wall not reported for a user decision: %+v", finding)
	}
}

func TestRenderlessLayer2KeepsBlockingWhenVerificationIsNotBetter(t *testing.T) {
	fixture := newAdFixture(t, func(adBase string) string {
		return `<!doctype html><title>Thin</title><body><p id="app">short</p>` + strings.ReplaceAll(adScripts(adBase, 6), "/ads/slot", "/ads/none") + `</body>`
	})
	fixture.ads.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fixture.adHits.Add(1)
		w.Header().Set("Content-Type", "application/javascript")
		writeTestBody(t, w, "void 0;")
	})
	blocker := adPathBlocker(t)
	page, err := fixture.engine(t, blocker, false).Fetch(t.Context(), fixture.site.URL, FetchOpts{RunScripts: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "page close", page.Close)
	finding := page.AdBlockFinding()
	if finding == nil || finding.Decision != security.AdBlockDecisionKept || blocker.Whitelist().IsWhitelisted("127.0.0.1") || page.AdBlockedRequests() != 6 {
		t.Fatalf("blocking was dropped without a better verification load: %+v", finding)
	}
}
