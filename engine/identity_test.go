package engine

import (
	"runtime"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/js"
)

func TestNavigatorDefaultsCoherentWithWireUA(t *testing.T) {
	e := &Engine{cfg: Config{UserAgent: "Mozilla/5.0 (Macintosh) Chrome/152.0.0.0 Safari/537.36"}}
	nav := e.navigatorDefaults(js.NavigatorConfig{})
	if nav.UserAgent != e.cfg.UserAgent {
		t.Fatalf("navigator UA %q must equal wire UA %q", nav.UserAgent, e.cfg.UserAgent)
	}
	wantPlatform := "Linux x86_64"
	if runtime.GOOS == "darwin" {
		wantPlatform = "MacIntel"
	} else if runtime.GOOS == "windows" {
		wantPlatform = "Win32"
	}
	if nav.Platform != wantPlatform {
		t.Fatalf("platform %q, want host-matched %q", nav.Platform, wantPlatform)
	}
}

func TestNavigatorDefaultsPreserveOverrides(t *testing.T) {
	e := &Engine{cfg: Config{UserAgent: "wire-ua"}}
	nav := e.navigatorDefaults(js.NavigatorConfig{UserAgent: "custom", Platform: "Custom64"})
	if nav.UserAgent != "custom" || nav.Platform != "Custom64" {
		t.Fatalf("caller overrides must win: %+v", nav)
	}
}

func TestDefaultIdentityIsChromeLike(t *testing.T) {
	c := &Config{}
	c.applyDefaults()
	if !c.chromeDefault {
		t.Fatal("empty UserAgent must select the Chrome default identity")
	}
	if !strings.Contains(c.UserAgent, "Chrome/") || strings.Contains(c.UserAgent, "Artemis") {
		t.Fatalf("default UA must be Chrome, not bot-labeled: %q", c.UserAgent)
	}
	c2 := Config{UserAgent: DefaultUserAgent}
	c2.applyDefaults()
	if c2.chromeDefault {
		t.Fatal("explicit UserAgent must disable the Chrome-header identity")
	}
}
