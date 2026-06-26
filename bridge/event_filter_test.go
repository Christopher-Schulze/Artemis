package bridge

import (
	"sync"
	"testing"
)

func TestDefaultEventFilterConfig(t *testing.T) {
	cfg := DefaultEventFilterConfig()
	if !cfg.PageEnabled {
		t.Fatal("expected PageEnabled=true in default config")
	}
	if cfg.NetworkMonitoring {
		t.Fatal("expected NetworkMonitoring=false in default config")
	}
	if cfg.AccessibilitySnapshot {
		t.Fatal("expected AccessibilitySnapshot=false in default config")
	}
	if cfg.JSEvaluate {
		t.Fatal("expected JSEvaluate=false in default config")
	}
	if cfg.CSSNeeded || cfg.DOMNeeded || cfg.DOMStorageNeeded || cfg.ProfilerNeeded {
		t.Fatal("expected all optional domain flags false in default config")
	}
}

func TestNewEventFilterDefaultStates(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())

	if ef.GetDomainState(DomainPage) != DomainStateEnabled {
		t.Fatal("expected Page enabled by default")
	}
	if ef.GetDomainState(DomainNetwork) != DomainStateNotNeeded {
		t.Fatalf("expected Network not_needed, got %s", ef.GetDomainState(DomainNetwork))
	}
	if ef.GetDomainState(DomainAccessibility) != DomainStateNotNeeded {
		t.Fatalf("expected Accessibility not_needed, got %s", ef.GetDomainState(DomainAccessibility))
	}
	if ef.GetDomainState(DomainRuntime) != DomainStateNotNeeded {
		t.Fatalf("expected Runtime not_needed, got %s", ef.GetDomainState(DomainRuntime))
	}
}

func TestShouldEnableAllDomains(t *testing.T) {
	cfg := EventFilterConfig{
		NetworkMonitoring:     true,
		PageEnabled:           true,
		AccessibilitySnapshot: true,
		JSEvaluate:            true,
		CSSNeeded:             true,
		DOMNeeded:             true,
		DOMStorageNeeded:      true,
		ProfilerNeeded:        true,
	}
	ef := NewEventFilter(cfg)

	needed := []CDPDomain{
		DomainNetwork, DomainPage, DomainAccessibility, DomainRuntime,
		DomainCSS, DomainDOM, DomainDOMStorage, DomainProfiler,
	}
	for _, d := range needed {
		if !ef.ShouldEnable(d) {
			t.Fatalf("expected ShouldEnable(%s)=true", d)
		}
		if !ef.IsNeeded(d) {
			t.Fatalf("expected IsNeeded(%s)=true", d)
		}
	}
	// Overlay and Emulation are never needed by config flags.
	if ef.ShouldEnable(DomainOverlay) {
		t.Fatal("expected ShouldEnable(Overlay)=false")
	}
	if ef.ShouldEnable(DomainEmulation) {
		t.Fatal("expected ShouldEnable(Emulation)=false")
	}
}

func TestShouldEnableDefaultConfig(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())
	if !ef.ShouldEnable(DomainPage) {
		t.Fatal("expected Page should enable")
	}
	for _, d := range []CDPDomain{DomainNetwork, DomainAccessibility, DomainRuntime, DomainOverlay, DomainEmulation} {
		if ef.ShouldEnable(d) {
			t.Fatalf("expected ShouldEnable(%s)=false in default config", d)
		}
	}
}

func TestShouldDisableForBlockedDomains(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())

	blocked := []CDPDomain{DomainCSS, DomainDOM, DomainDOMStorage, DomainProfiler}
	for _, d := range blocked {
		if !ef.ShouldDisable(d) {
			t.Fatalf("expected ShouldDisable(%s)=true in default config", d)
		}
		if ef.GetDomainState(d) != DomainStateDisabled {
			t.Fatalf("expected %s disabled, got %s", d, ef.GetDomainState(d))
		}
	}
}

func TestShouldDisableNotBlockedWhenNeeded(t *testing.T) {
	cfg := DefaultEventFilterConfig()
	cfg.CSSNeeded = true
	cfg.DOMNeeded = true
	ef := NewEventFilter(cfg)
	if ef.ShouldDisable(DomainCSS) {
		t.Fatal("expected CSS not blocked when CSSNeeded=true")
	}
	if ef.ShouldDisable(DomainDOM) {
		t.Fatal("expected DOM not blocked when DOMNeeded=true")
	}
	if ef.GetDomainState(DomainCSS) != DomainStateEnabled {
		t.Fatalf("expected CSS enabled when needed, got %s", ef.GetDomainState(DomainCSS))
	}
}

func TestEnableCommands(t *testing.T) {
	cfg := EventFilterConfig{
		NetworkMonitoring:     true,
		PageEnabled:           true,
		AccessibilitySnapshot: true,
		JSEvaluate:            true,
	}
	ef := NewEventFilter(cfg)
	cmds := ef.EnableCommands()

	if len(cmds) != 4 {
		t.Fatalf("expected 4 enable commands, got %d", len(cmds))
	}

	want := map[string]bool{
		"Network.enable":       true,
		"Page.enable":          true,
		"Accessibility.enable": true,
		"Runtime.enable":       true,
	}
	got := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		got[c.String()] = true
	}
	for w := range want {
		if !got[w] {
			t.Fatalf("missing enable command %s", w)
		}
	}
	for g := range got {
		if !want[g] {
			t.Fatalf("unexpected enable command %s", g)
		}
	}
	// All commands must use "enable" method.
	for _, c := range cmds {
		if c.Method != "enable" {
			t.Fatalf("expected method enable, got %s", c.Method)
		}
	}
}

func TestEnableCommandsDefaultConfig(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())
	cmds := ef.EnableCommands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 enable command (Page), got %d", len(cmds))
	}
	if cmds[0].String() != "Page.enable" {
		t.Fatalf("expected Page.enable, got %s", cmds[0].String())
	}
}

func TestDisableCommands(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())
	cmds := ef.DisableCommands()

	want := map[string]bool{
		"CSS.disable":        true,
		"DOM.disable":        true,
		"DOMStorage.disable": true,
		"Profiler.disable":   true,
	}
	if len(cmds) != len(want) {
		t.Fatalf("expected %d disable commands, got %d", len(want), len(cmds))
	}
	got := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		got[c.String()] = true
	}
	for w := range want {
		if !got[w] {
			t.Fatalf("missing disable command %s", w)
		}
	}
	for _, c := range cmds {
		if c.Method != "disable" {
			t.Fatalf("expected method disable, got %s", c.Method)
		}
	}
}

func TestDisableCommandsEmptyWhenAllNeeded(t *testing.T) {
	cfg := EventFilterConfig{
		PageEnabled:      true,
		CSSNeeded:        true,
		DOMNeeded:        true,
		DOMStorageNeeded: true,
		ProfilerNeeded:   true,
	}
	ef := NewEventFilter(cfg)
	cmds := ef.DisableCommands()
	if len(cmds) != 0 {
		t.Fatalf("expected 0 disable commands when all blocked domains needed, got %d", len(cmds))
	}
}

func TestApplyFilterOrderDisableBeforeEnable(t *testing.T) {
	cfg := DefaultEventFilterConfig()
	cfg.NetworkMonitoring = true
	ef := NewEventFilter(cfg)
	cmds := ef.ApplyFilter()

	// Find the index of the first enable and the last disable.
	firstEnable := -1
	lastDisable := -1
	for i, c := range cmds {
		if c.Method == "enable" && firstEnable == -1 {
			firstEnable = i
		}
		if c.Method == "disable" {
			lastDisable = i
		}
	}
	if firstEnable < 0 {
		t.Fatal("expected at least one enable command")
	}
	if lastDisable < 0 {
		t.Fatal("expected at least one disable command")
	}
	if lastDisable > firstEnable {
		t.Fatalf("expected all disables before enables; last disable at %d, first enable at %d", lastDisable, firstEnable)
	}

	// Verify the stats were updated.
	stats := ef.Stats()
	if stats.CommandsIssued != len(cmds) {
		t.Fatalf("expected CommandsIssued=%d, got %d", len(cmds), stats.CommandsIssued)
	}
}

func TestApplyFilterContent(t *testing.T) {
	cfg := DefaultEventFilterConfig()
	cfg.NetworkMonitoring = true
	cfg.JSEvaluate = true
	ef := NewEventFilter(cfg)
	cmds := ef.ApplyFilter()

	// Expected: 4 disables (CSS, DOM, DOMStorage, Profiler) + 3 enables (Page, Network, Runtime) = 7.
	if len(cmds) != 7 {
		t.Fatalf("expected 7 commands, got %d", len(cmds))
	}

	// First 4 must be disables.
	for i := 0; i < 4; i++ {
		if cmds[i].Method != "disable" {
			t.Fatalf("command %d: expected disable, got %s (%s)", i, cmds[i].Method, cmds[i].String())
		}
	}
	// Last 3 must be enables.
	for i := 4; i < 7; i++ {
		if cmds[i].Method != "enable" {
			t.Fatalf("command %d: expected enable, got %s (%s)", i, cmds[i].Method, cmds[i].String())
		}
	}
}

func TestSetConfigRecompute(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())

	// Initially Network is not_needed.
	if ef.GetDomainState(DomainNetwork) != DomainStateNotNeeded {
		t.Fatalf("expected Network not_needed initially, got %s", ef.GetDomainState(DomainNetwork))
	}

	// Enable monitoring → Network becomes enabled.
	ef.SetConfig(EventFilterConfig{
		PageEnabled:       true,
		NetworkMonitoring: true,
	})
	if ef.GetDomainState(DomainNetwork) != DomainStateEnabled {
		t.Fatalf("expected Network enabled after recompute, got %s", ef.GetDomainState(DomainNetwork))
	}

	// Disable monitoring again → Network back to not_needed.
	ef.SetConfig(DefaultEventFilterConfig())
	if ef.GetDomainState(DomainNetwork) != DomainStateNotNeeded {
		t.Fatalf("expected Network not_needed after second recompute, got %s", ef.GetDomainState(DomainNetwork))
	}
}

func TestSetConfigUpdatesStats(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())
	stats := ef.Stats()
	// Default: 1 enabled (Page), 4 disabled (CSS, DOM, DOMStorage, Profiler).
	if stats.EnabledCount != 1 {
		t.Fatalf("expected EnabledCount=1, got %d", stats.EnabledCount)
	}
	if stats.DisabledCount != 4 {
		t.Fatalf("expected DisabledCount=4, got %d", stats.DisabledCount)
	}

	ef.SetConfig(EventFilterConfig{
		PageEnabled:           true,
		NetworkMonitoring:     true,
		AccessibilitySnapshot: true,
		JSEvaluate:            true,
		CSSNeeded:             true,
		DOMNeeded:             true,
		DOMStorageNeeded:      true,
		ProfilerNeeded:        true,
	})
	stats = ef.Stats()
	if stats.EnabledCount != 8 {
		t.Fatalf("expected EnabledCount=8, got %d", stats.EnabledCount)
	}
	if stats.DisabledCount != 0 {
		t.Fatalf("expected DisabledCount=0, got %d", stats.DisabledCount)
	}
}

func TestGetDomainState(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())
	if ef.GetDomainState(DomainPage) != DomainStateEnabled {
		t.Fatal("expected Page enabled")
	}
	if ef.GetDomainState(DomainCSS) != DomainStateDisabled {
		t.Fatal("expected CSS disabled")
	}
	if ef.GetDomainState(DomainNetwork) != DomainStateNotNeeded {
		t.Fatal("expected Network not_needed")
	}
}

func TestAllDomainStates(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())
	all := ef.AllDomainStates()
	if len(all) != len(allDomains) {
		t.Fatalf("expected %d domain states, got %d", len(allDomains), len(all))
	}
	if all[DomainPage] != DomainStateEnabled {
		t.Fatal("expected Page enabled in all states")
	}
	if all[DomainCSS] != DomainStateDisabled {
		t.Fatal("expected CSS disabled in all states")
	}
	// Mutating the returned map must not affect the filter.
	all[DomainPage] = DomainStateDisabled
	if ef.GetDomainState(DomainPage) != DomainStateEnabled {
		t.Fatal("mutating returned map affected internal state")
	}
}

func TestNeededDomains(t *testing.T) {
	cfg := EventFilterConfig{
		PageEnabled:       true,
		NetworkMonitoring: true,
		JSEvaluate:        true,
	}
	ef := NewEventFilter(cfg)
	needed := ef.NeededDomains()
	want := sortedDomains([]CDPDomain{DomainPage, DomainNetwork, DomainRuntime})
	got := sortedDomains(needed)
	if len(got) != len(want) {
		t.Fatalf("expected %d needed domains, got %d", len(want), len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("needed domain %d: expected %s, got %s", i, want[i], got[i])
		}
	}
}

func TestNeededDomainsDefault(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())
	needed := ef.NeededDomains()
	if len(needed) != 1 || needed[0] != DomainPage {
		t.Fatalf("expected only Page needed, got %v", needed)
	}
}

func TestBlockedDomains(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())
	blocked := ef.BlockedDomains()
	want := sortedDomains([]CDPDomain{DomainCSS, DomainDOM, DomainDOMStorage, DomainProfiler})
	got := sortedDomains(blocked)
	if len(got) != len(want) {
		t.Fatalf("expected %d blocked domains, got %d", len(want), len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("blocked domain %d: expected %s, got %s", i, want[i], got[i])
		}
	}
}

func TestBlockedDomainsEmptyWhenAllNeeded(t *testing.T) {
	cfg := EventFilterConfig{
		PageEnabled:      true,
		CSSNeeded:        true,
		DOMNeeded:        true,
		DOMStorageNeeded: true,
		ProfilerNeeded:   true,
	}
	ef := NewEventFilter(cfg)
	if len(ef.BlockedDomains()) != 0 {
		t.Fatalf("expected 0 blocked domains, got %v", ef.BlockedDomains())
	}
}

func TestStats(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())
	stats := ef.Stats()
	if stats.EnabledCount != 1 {
		t.Fatalf("expected EnabledCount=1, got %d", stats.EnabledCount)
	}
	if stats.DisabledCount != 4 {
		t.Fatalf("expected DisabledCount=4, got %d", stats.DisabledCount)
	}
	if stats.CommandsIssued != 0 {
		t.Fatalf("expected CommandsIssued=0 before ApplyFilter, got %d", stats.CommandsIssued)
	}

	ef.ApplyFilter()
	stats = ef.Stats()
	// 4 disables + 1 enable = 5.
	if stats.CommandsIssued != 5 {
		t.Fatalf("expected CommandsIssued=5 after ApplyFilter, got %d", stats.CommandsIssued)
	}
}

func TestConcurrentAccess(t *testing.T) {
	ef := NewEventFilter(DefaultEventFilterConfig())

	const goroutines = 20
	var wg sync.WaitGroup

	// Half the goroutines reconfigure, half read.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if n%2 == 0 {
					cfg := DefaultEventFilterConfig()
					if j%2 == 0 {
						cfg.NetworkMonitoring = true
						cfg.JSEvaluate = true
					}
					ef.SetConfig(cfg)
				} else {
					_ = ef.ShouldEnable(DomainPage)
					_ = ef.ShouldDisable(DomainCSS)
					_ = ef.GetDomainState(DomainNetwork)
					_ = ef.NeededDomains()
					_ = ef.BlockedDomains()
					_ = ef.EnableCommands()
					_ = ef.DisableCommands()
					_ = ef.ApplyFilter()
					_ = ef.Stats()
				}
			}
		}(i)
	}
	wg.Wait()

	// After all goroutines complete, the filter must be in a consistent state.
	states := ef.AllDomainStates()
	for d, s := range states {
		switch s {
		case DomainStateEnabled, DomainStateDisabled, DomainStateNotNeeded:
			// valid
		default:
			t.Fatalf("domain %s has invalid state %s after concurrent access", d, s)
		}
	}
}

func TestConfigWithMonitoringOn(t *testing.T) {
	cfg := DefaultEventFilterConfig()
	cfg.NetworkMonitoring = true
	ef := NewEventFilter(cfg)

	if !ef.IsNeeded(DomainNetwork) {
		t.Fatal("expected Network needed when monitoring on")
	}
	if ef.GetDomainState(DomainNetwork) != DomainStateEnabled {
		t.Fatalf("expected Network enabled, got %s", ef.GetDomainState(DomainNetwork))
	}
	// Blocked domains still blocked.
	if !ef.ShouldDisable(DomainCSS) {
		t.Fatal("expected CSS still blocked with monitoring on")
	}
}

func TestConfigAllEnabled(t *testing.T) {
	cfg := EventFilterConfig{
		NetworkMonitoring:     true,
		PageEnabled:           true,
		AccessibilitySnapshot: true,
		JSEvaluate:            true,
		CSSNeeded:             true,
		DOMNeeded:             true,
		DOMStorageNeeded:      true,
		ProfilerNeeded:        true,
	}
	ef := NewEventFilter(cfg)

	for _, d := range allDomains {
		if d == DomainOverlay || d == DomainEmulation {
			// These are not driven by config flags.
			if ef.GetDomainState(d) != DomainStateNotNeeded {
				t.Fatalf("expected %s not_needed, got %s", d, ef.GetDomainState(d))
			}
			continue
		}
		if ef.GetDomainState(d) != DomainStateEnabled {
			t.Fatalf("expected %s enabled, got %s", d, ef.GetDomainState(d))
		}
	}
	if len(ef.BlockedDomains()) != 0 {
		t.Fatalf("expected no blocked domains when all enabled, got %v", ef.BlockedDomains())
	}
}

func TestCDPCommandString(t *testing.T) {
	c := CDPCommand{Domain: DomainNetwork, Method: "enable"}
	if c.String() != "Network.enable" {
		t.Fatalf("expected Network.enable, got %s", c.String())
	}
}

func TestPageAlwaysOnEvenWhenFlagFalse(t *testing.T) {
	// Even if PageEnabled is false, the spec invariant is Page always on.
	cfg := EventFilterConfig{PageEnabled: false}
	ef := NewEventFilter(cfg)
	if ef.GetDomainState(DomainPage) != DomainStateEnabled {
		t.Fatalf("expected Page always enabled regardless of flag, got %s", ef.GetDomainState(DomainPage))
	}
	if !ef.IsNeeded(DomainPage) {
		t.Fatal("expected Page always needed")
	}
}
