package network

import (
	"strings"
	"testing"
)

func TestParseAndAllow(t *testing.T) {
	body := `
User-agent: *
Disallow: /private/
Allow: /private/public
Disallow: /
User-agent: Artemis
Disallow: /no-bots/
`
	p, err := ParseRobots(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cases := []struct {
		ua, path string
		want     bool
	}{
		{"unknown-bot/1", "/", false},
		{"unknown-bot/1", "/x", false},
		{"unknown-bot/1", "/private/x", false},
		{"unknown-bot/1", "/private/public", true},
		{"Artemis/0.1", "/no-bots/x", false},
		{"Artemis/0.1", "/x", true}, // matches Artemis group, no Disallow rule for /x
	}
	for _, tc := range cases {
		if got := p.Allowed(tc.ua, tc.path); got != tc.want {
			t.Errorf("Allowed(%q, %q) = %v, want %v", tc.ua, tc.path, got, tc.want)
		}
	}
}

func TestEmptyDisallowAllowsEverything(t *testing.T) {
	p, err := ParseRobots(strings.NewReader("User-agent: *\nDisallow:\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Allowed("anything", "/x") {
		t.Error("empty Disallow should allow")
	}
}

func TestNilPolicyAllows(t *testing.T) {
	var p *RobotsPolicy
	if !p.Allowed("a", "/b") {
		t.Error("nil policy should allow")
	}
}

func TestParseRobotsAccumulatesConsecutiveUserAgents(t *testing.T) {
	p, err := ParseRobots(strings.NewReader("User-agent: Artemis\nUser-agent: Artemis\nDisallow: /private\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, userAgent := range []string{"Artemis/1.0", "Artemis/1.0"} {
		if p.Allowed(userAgent, "/private/data") {
			t.Fatalf("%s lost shared group rule", userAgent)
		}
	}
}

func TestRobotsSelectsLongestMatchingUserAgentDeterministically(t *testing.T) {
	p, err := ParseRobots(strings.NewReader("User-agent: bot\nDisallow: /broad\nUser-agent: artemis-bot\nDisallow: /specific\n"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if p.Allowed("Artemis-Bot/1.0", "/specific") {
			t.Fatal("longest user-agent match was not selected")
		}
		if !p.Allowed("Artemis-Bot/1.0", "/broad") {
			t.Fatal("shorter matching group was incorrectly merged")
		}
	}
}

func TestParseRobotsRejectsNilReader(t *testing.T) {
	if _, err := ParseRobots(nil); err == nil {
		t.Fatal("nil reader accepted")
	}
}
