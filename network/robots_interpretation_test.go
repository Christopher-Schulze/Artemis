package network

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRobotsActualGroupsCannotContaminateWildcardOrLoseEmptyGroup(t *testing.T) {
	for _, test := range []struct {
		name, body, agent, path string
		allow                   bool
	}{
		{"first named is not wildcard", "User-agent: Artemis\nDisallow: /named\n", "other", "/named", true},
		{"orphan ignored", "Disallow: /\nUser-agent: Artemis\nAllow: /public\n", "other", "/private", true},
		{"empty specific overrides wildcard", "User-agent: *\nDisallow: /\nUser-agent: Artemis\n", "Artemis/1", "/private", true},
		{"consecutive distinct agents", "User-agent: Artemis\nUser-agent: OtherBot\nDisallow: /shared\n", "OtherBot/1", "/shared", false},
		{"repeated group merged", "User-agent: Artemis\nDisallow: /first\nUser-agent: Artemis\nDisallow: /second\n", "Artemis/1", "/first", false},
		{"CR line endings", "User-agent: *\rDisallow: /private\r", "other", "/private", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy, err := ParseRobots(strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			if got := policy.Allowed(test.agent, test.path); got != test.allow {
				t.Fatalf("allow=%t want=%t groups=%v", got, test.allow, policy.Groups)
			}
		})
	}
}

func TestRobotsPatternOctetsWildcardAndSpecificity(t *testing.T) {
	policy, err := ParseRobots(strings.NewReader("User-agent: *\nDisallow: /private/*\nAllow: /private/open$\nAllow: /private/open/deep\nDisallow: /*.zip$\nDisallow: /search?token=\nDisallow: /literal%2A\nDisallow: /price%24\nDisallow: /encoded%2Fprivate\nDisallow: /café\nDisallow: /~owner\nAllow: /tie\nDisallow: /tie\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path  string
		allow bool
	}{
		{"/private/closed", false}, {"/private/open", true}, {"/private/open/closed", false}, {"/private/open/deep/page", true},
		{"/file.zip", false}, {"/file.zip?download=1", true}, {"/search?token=value", false}, {"/search?name=value", true},
		{"/literal*", false}, {"/literalOther", true}, {"/price$", false}, {"/encoded%2fprivate", false}, {"/encoded/private", true},
		{"/caf%C3%A9", false}, {"/café", false}, {"/%7Eowner", false}, {"/tie", true}, {"/robots.txt", true},
	} {
		t.Run(test.path, func(t *testing.T) {
			if got := policy.Allowed("other", test.path); got != test.allow {
				t.Fatalf("allow=%t want=%t", got, test.allow)
			}
		})
	}
}

func TestRobotsMetadataCannotSplitGroupAndRulesRemainDetached(t *testing.T) {
	body := "User-agent: Alpha\nCrawl-delay: 0.125\nSitemap: https://example.com/sitemap.xml\nUser-agent: Beta\nDisallow: /shared\nUser-agent: *\nCrawl-delay: 3\nDisallow: /general\nSitemap: javascript:unsafe\nSitemap: https://example..com/map.xml\nSitemap: https://example.com:70000/map.xml\nSitemap: https://example.com:0/map.xml\nSitemap: https://user:secret@example.com/map.xml\n"
	policy, err := ParseRobots(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"Alpha", "Beta"} {
		if policy.CrawlDelayFor(agent) != 125*time.Millisecond || policy.Allowed(agent, "/shared") {
			t.Fatalf("metadata split group for %s: %+v", agent, policy)
		}
	}
	if policy.CrawlDelayFor("unknown") != 3*time.Second || len(policy.Sitemaps) != 1 || policy.Sitemaps[0] != "https://example.com/sitemap.xml" {
		t.Fatalf("metadata=%+v", policy)
	}
	rules := policy.RulesFor("Alpha")
	rules[0].Allow = true
	rules[0].Path = "/changed"
	if policy.Allowed("Alpha", "/shared") {
		t.Fatal("exported rules mutated active interpretation")
	}
}

func TestRobotsInterpretationRejectsBudgetExhaustionWithoutPartialPolicy(t *testing.T) {
	var agents, wildcards, sitemaps, groups, assignments, payload, combined strings.Builder
	for index := range 33 {
		fmt.Fprintf(&agents, "User-agent: agent%c\n", 'a'+rune(index/26))
		fmt.Fprintf(&agents, "User-agent: other%c%c\n", 'a'+rune(index/26), 'a'+rune(index%26))
	}
	for index := range 1025 {
		fmt.Fprintf(&wildcards, "Disallow: /path-%d*\n", index)
	}
	for index := range 129 {
		fmt.Fprintf(&sitemaps, "Sitemap: https://example.com/map-%d.xml\n", index)
	}
	for index := range 1025 {
		fmt.Fprintf(&groups, "User-agent: bot%c%c%c\nDisallow: /group\n", 'a'+rune(index/676), 'a'+rune(index/26%26), 'a'+rune(index%26))
	}
	for index := range 32 {
		fmt.Fprintf(&assignments, "User-agent: bot%c%c\n", 'a'+rune(index/26), 'a'+rune(index%26))
		fmt.Fprintf(&payload, "User-agent: bot%c%c\n", 'a'+rune(index/26), 'a'+rune(index%26))
		fmt.Fprintf(&combined, "User-agent: bot%c%c\n", 'a'+rune(index/26), 'a'+rune(index%26))
	}
	for index := range 1025 {
		fmt.Fprintf(&assignments, "Disallow: /literal-%d\n", index)
	}
	for range 20 {
		fmt.Fprintf(&payload, "Disallow: /%s\n", strings.Repeat("x", 7499))
	}
	for range 15 {
		fmt.Fprintf(&combined, "Disallow: /%s\n", strings.Repeat("x", 7499))
	}
	for index := range 80 {
		fmt.Fprintf(&combined, "Sitemap: https://example.com/%d/%s\n", index, strings.Repeat("x", 7470))
	}
	for _, test := range []struct{ name, body string }{
		{"document bytes", strings.Repeat("#", int(MaxRobotsDocumentBytes)+1)},
		{"rule bytes", "User-agent: *\nDisallow: /" + strings.Repeat("x", 8192) + "\n"},
		{"consecutive agents", agents.String() + "Disallow: /\n"},
		{"wildcard programs", "User-agent: *\n" + wildcards.String()},
		{"sitemaps", sitemaps.String()},
		{"groups", groups.String()},
		{"rule assignments", assignments.String()},
		{"policy payload", payload.String()},
		{"combined rule and metadata payload", combined.String()},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy, err := ParseRobots(strings.NewReader(test.body))
			if err == nil || policy != nil {
				t.Fatalf("exhausted parser published policy: present=%t error=%v", policy != nil, err)
			}
		})
	}
}
