package security

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network"
)

func compileRules(t *testing.T, rules ...string) *AdBlockRuleSet {
	t.Helper()
	set, err := ParseAdBlockRules(strings.NewReader(strings.Join(rules, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func request(url, site string, resource AdBlockResource) AdBlockRequest {
	host := strings.SplitN(strings.SplitN(url, "://", 2)[1], "/", 2)[0]
	host = strings.SplitN(host, ":", 2)[0]
	return AdBlockRequest{URL: url, Host: host, SiteHost: site, Resource: resource, ThirdParty: isThirdParty(host, site)}
}

func TestAdBlockRuleSyntaxMatchesABPSemantics(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rules   []string
		url     string
		site    string
		res     AdBlockResource
		blocked bool
	}{
		{"domain anchor subdomain", []string{"||ads.test^"}, "https://cdn.ads.test/x.js", "news.test", ResourceScript, true},
		{"domain anchor label boundary", []string{"||ads.test^"}, "https://badads.test/x.js", "news.test", ResourceScript, false},
		{"domain anchor with path", []string{"||cdn.test/ads/"}, "https://www.cdn.test/ads/1.gif", "news.test", ResourceImage, true},
		{"separator matches end", []string{"||tracker.test^"}, "https://tracker.test", "news.test", ResourceImage, true},
		{"separator rejects letter", []string{"/banner^"}, "https://x.test/bannerx", "news.test", ResourceImage, false},
		{"separator accepts query", []string{"/banner^"}, "https://x.test/banner?id=1", "news.test", ResourceImage, true},
		{"wildcard", []string{"/ads/*/pixel.gif"}, "https://x.test/ads/a/b/pixel.gif", "x.test", ResourceImage, true},
		{"token next to wildcard is substring", []string{"*superbanner*"}, "https://x.test/img/mysuperbannerx.png", "x.test", ResourceImage, true},
		{"start anchor", []string{"|https://evil.test/"}, "https://evil.test/a", "x.test", ResourceScript, true},
		{"start anchor miss", []string{"|https://evil.test/"}, "https://x.test/?u=https://evil.test/", "x.test", ResourceScript, false},
		{"end anchor", []string{"swf|"}, "https://x.test/a.swf", "x.test", ResourceObject, true},
		{"end anchor miss", []string{"swf|"}, "https://x.test/a.swf?x", "x.test", ResourceObject, false},
		{"exception", []string{"||ads.test^", "@@||ads.test/ok.js"}, "https://ads.test/ok.js", "x.test", ResourceScript, false},
		{"important beats exception", []string{"||ads.test^$important", "@@||ads.test/ok.js"}, "https://ads.test/ok.js", "x.test", ResourceScript, true},
		{"third-party only", []string{"||cdn.test^$third-party"}, "https://cdn.test/a.js", "www.cdn.test", ResourceScript, false},
		{"third-party only foreign", []string{"||cdn.test^$third-party"}, "https://cdn.test/a.js", "news.test", ResourceScript, true},
		{"first-party only", []string{"/track.js$~third-party"}, "https://news.test/track.js", "news.test", ResourceScript, true},
		{"resource include", []string{"||cdn.test^$image"}, "https://cdn.test/a.js", "x.test", ResourceScript, false},
		{"resource exclude", []string{"||cdn.test^$~script"}, "https://cdn.test/a.js", "x.test", ResourceScript, false},
		{"domain option", []string{"/promo.js$domain=shop.test|~safe.shop.test"}, "https://cdn.x/promo.js", "www.shop.test", ResourceScript, true},
		{"domain option negated", []string{"/promo.js$domain=shop.test|~safe.shop.test"}, "https://cdn.x/promo.js", "safe.shop.test", ResourceScript, false},
		{"case insensitive", []string{"/AdServer/"}, "https://x.test/adserver/a", "x.test", ResourceScript, true},
		{"match-case", []string{"/AdServer/$match-case"}, "https://x.test/adserver/a", "x.test", ResourceScript, false},
		{"regex", []string{`/\/ad[0-9]+\.js/`}, "https://x.test/ad123.js", "x.test", ResourceScript, true},
		{"cosmetic ignored", []string{"example.test##.ad"}, "https://example.test/ad", "x.test", ResourceScript, false},
		{"unsupported option skipped", []string{"||cdn.test^$csp=script-src 'none'"}, "https://cdn.test/a", "x.test", ResourceScript, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocked, _ := compileRules(t, tc.rules...).Match(request(tc.url, tc.site, tc.res))
			if blocked != tc.blocked {
				t.Fatalf("Match = %v, want %v", blocked, tc.blocked)
			}
		})
	}
}

func TestAdBlockRuleStatsCountEveryLineClass(t *testing.T) {
	set := compileRules(t, "[Adblock Plus 2.0]", "! comment", "||a.test^", "@@||b.test^", "x.test##.ad", "||c.test^$redirect=noop.js", "*")
	if stats := set.Stats(); stats != (AdBlockRuleStats{Lines: 5, Blocking: 1, Exceptions: 1, Cosmetic: 1, Unsupported: 2}) {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestAdBlockerConcurrentMatchingRolloutAndWhitelist(t *testing.T) {
	blocker := NewAdBlocker()
	target, err := url.Parse("https://www.google-analytics.com/analytics.js")
	if err != nil {
		t.Fatal(err)
	}
	ctx := network.WithAdBlockScope(context.Background(), network.AdBlockScope{SiteHost: "news.example", Resource: "script", Blocked: new(atomic.Int64)})
	var group sync.WaitGroup
	for worker := range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := range 200 {
				if !blocker.BlockRequest(ctx, target, network.TargetSubresource, fmt.Sprintf("s%d-%d", worker, i)) {
					t.Error("base tracker rule stopped matching under concurrency")
					return
				}
				switch i % 50 {
				case 0:
					set := compileRules(t, fmt.Sprintf("||w%d-%d.test^", worker, i))
					if err := blocker.Install(&AdBlockList{Manifest: AdBlockListManifest{SHA256: fmt.Sprintf("%d-%d", worker, i)}, sets: []categorizedRules{{category: AdBlockCategoryAd, rules: set}}}); err != nil {
						t.Error(err)
					}
				case 25:
					blocker.RecordBreakage()
					blocker.Advance()
				}
			}
		}()
	}
	group.Wait()
	if blocker.BlockedTotal() != 1600 {
		t.Fatalf("blocked total = %d, want 1600", blocker.BlockedTotal())
	}
	if err := blocker.Whitelist().AddToWhitelist("news.example"); err != nil {
		t.Fatal(err)
	}
	if blocker.BlockRequest(ctx, target, network.TargetSubresource, "s") {
		t.Fatal("whitelisted site still blocked")
	}
}
