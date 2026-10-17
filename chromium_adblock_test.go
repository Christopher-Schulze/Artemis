package artemis

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/security"
)

// adSite serves a page with six ad scripts under /ads/; each adds an ad
// iframe with markup. With adContent the page's main text comes only from
// those scripts, so blocking breaks it (Layer 2 Case B); otherwise the page
// carries first-party content and the ads are pure overhead (Layer 3).
func adSite(t *testing.T, hits *atomic.Int64, adContent bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/ads/") && strings.HasSuffix(r.URL.Path, ".js"):
			hits.Add(1)
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, `(() => { document.getElementById("app").textContent += "`+strings.Repeat("story ", 10)+`";
const frame = document.createElement("iframe"); frame.src = "/ads/frame.html"; document.body.appendChild(frame); })();`)
		case strings.HasPrefix(r.URL.Path, "/ads/"):
			hits.Add(1)
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<!doctype html><div><a href="/buy">Buy</a><img alt="ad" src="/ads/pixel.gif"><span>sponsored</span></div>`)
		default:
			w.Header().Set("Content-Type", "text/html")
			var scripts strings.Builder
			for i := range 6 {
				fmt.Fprintf(&scripts, `<script src="/ads/slot%d.js"></script>`, i)
			}
			main := strings.Repeat("first-party article text ", 20)
			if adContent {
				main = ""
			}
			fmt.Fprint(w, `<!doctype html><title>Ad funded</title><body><main id="app">`+main+`</main>`+scripts.String()+`</body>`)
		}
	}))
}

func startAdBlockAgent(t *testing.T, server *httptest.Server, blocker *security.AdBlocker, disable bool) *Agent {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := NewAgent(AgentConfig{PolicyEngine: fixtureActionPolicy(t), Chromium: &browserprocess.LaunchConfig{Headless: true}, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}, AdBlock: blocker, DisableAdBlock: disable})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := agent.Stop(); err != nil {
			t.Error(err)
		}
	})
	return agent
}

func TestChromiumLayer2VerifiesWhitelistsAndReloadsBrokenSite(t *testing.T) {
	var hits atomic.Int64
	server := adSite(t, &hits, true)
	defer server.Close()
	blocker := security.NewAdBlocker()
	if err := blocker.AddRules("/ads/"); err != nil {
		t.Fatal(err)
	}
	agent := startAdBlockAgent(t, server, blocker, false)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	session, err := agent.CreateSession("layer2")
	if err != nil {
		t.Fatal(err)
	}
	pageID, evidence, taskErr := session.OpenChromiumPage(ctx, server.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	finding := session.AdBlockFinding(pageID)
	if finding == nil || finding.Case != security.AdBlockCaseBreakage || finding.Decision != security.AdBlockDecisionWhitelisted || finding.BlockedRequests != 6 {
		t.Fatalf("broken page was not verified, whitelisted and reloaded: %+v", finding)
	}
	if !blocker.Whitelist().IsWhitelisted("127.0.0.1") {
		t.Fatal("site not whitelisted after the verified reload")
	}
	dump, taskErr := session.DumpChromiumPage(ctx, pageID, "text")
	if taskErr != nil || !strings.Contains(fmt.Sprint(dump), "story") {
		t.Fatalf("reloaded page lacks the restored content: %v %v", dump, taskErr)
	}
	if len(evidence.Snapshot.Nodes) == 0 {
		t.Fatal("reloaded page was not re-observed")
	}
	domains, werr := agent.AdBlockWhitelist("remove", "127.0.0.1")
	if werr != nil || len(domains) != 0 {
		t.Fatalf("operator whitelist removal failed: %v %v", domains, werr)
	}
	if _, werr := agent.AdBlockWhitelist("drop", ""); werr == nil {
		t.Fatal("unknown whitelist operation accepted")
	}
}

// TestAdBlockLayer3ReducesTrafficAndSnapshotNodes is the measured proof for
// the Layer 3 performance statement (spec L4210): the same page observed with
// and without blocking.
func TestAdBlockLayer3ReducesTrafficAndSnapshotNodes(t *testing.T) {
	measure := func(disable bool) (requests int64, nodes int, network int) {
		var hits atomic.Int64
		server := adSite(t, &hits, false)
		defer server.Close()
		blocker := security.NewAdBlocker()
		if err := blocker.AddRules("/ads/"); err != nil {
			t.Fatal(err)
		}
		agent := startAdBlockAgent(t, server, blocker, disable)
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		session, err := agent.CreateSession("layer3")
		if err != nil {
			t.Fatal(err)
		}
		pageID, evidence, taskErr := session.OpenChromiumPage(ctx, server.URL)
		if taskErr != nil {
			t.Fatal(taskErr)
		}
		if finding := session.AdBlockFinding(pageID); finding != nil {
			t.Fatalf("first-party page raised a Layer 2 finding: %+v", finding)
		}
		return hits.Load(), len(evidence.Snapshot.Nodes), len(evidence.Network)
	}
	blockedHits, blockedNodes, blockedNetwork := measure(false)
	openHits, openNodes, openNetwork := measure(true)
	t.Logf("layer3 evidence: ad requests %d -> %d, snapshot nodes %d -> %d, network events %d -> %d", openHits, blockedHits, openNodes, blockedNodes, openNetwork, blockedNetwork)
	if blockedHits != 0 || openHits == 0 || blockedNetwork >= openNetwork || blockedNodes >= openNodes {
		t.Fatalf("blocking did not reduce traffic: hits %d vs %d, network %d vs %d", blockedHits, openHits, blockedNetwork, openNetwork)
	}
}
