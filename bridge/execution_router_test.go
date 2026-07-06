package bridge

import (
	"context"
	"testing"

	"github.com/Christopher-Schulze/Artemis/scraper"
)

func TestFullExecutionRouterRouteRules(t *testing.T) {
	cases := []struct {
		name       string
		signals    RouterSignals
		wantMode   ExecutionRouterMode
		wantReason string
	}{
		{
			name:       "operator_override_wins",
			signals:    RouterSignals{OperatorOverride: "stealth"},
			wantMode:   ModeStealth,
			wantReason: "operator_override",
		},
		{
			name:       "bot_detection_to_stealth",
			signals:    RouterSignals{IsHTML: true, HasBotDetection: true},
			wantMode:   ModeStealth,
			wantReason: "bot_waf_captcha_detected",
		},
		{
			name:       "captcha_to_stealth",
			signals:    RouterSignals{IsHTML: true, HasCAPTCHA: true},
			wantMode:   ModeStealth,
			wantReason: "bot_waf_captcha_detected",
		},
		{
			name:       "waf_to_stealth",
			signals:    RouterSignals{IsHTML: true, HasWAF: true},
			wantMode:   ModeStealth,
			wantReason: "bot_waf_captcha_detected",
		},
		{
			name:       "webauthn_to_stealth",
			signals:    RouterSignals{IsHTML: true, HasWebAuthn: true},
			wantMode:   ModeStealth,
			wantReason: "auth_profile_required",
		},
		{
			name:       "auth_profile_to_stealth",
			signals:    RouterSignals{IsHTML: true, NeedsAuthProfile: true},
			wantMode:   ModeStealth,
			wantReason: "auth_profile_required",
		},
		{
			name:       "layout_to_chromium",
			signals:    RouterSignals{IsHTML: true, NeedsLayout: true},
			wantMode:   ModeChromiumCDP,
			wantReason: "layout_or_render_required",
		},
		{
			name:       "canvas_to_chromium",
			signals:    RouterSignals{IsHTML: true, NeedsCanvas: true},
			wantMode:   ModeChromiumCDP,
			wantReason: "layout_or_render_required",
		},
		{
			name:       "screenshot_to_chromium",
			signals:    RouterSignals{IsHTML: true, NeedsScreenshot: true},
			wantMode:   ModeChromiumCDP,
			wantReason: "layout_or_render_required",
		},
		{
			name:       "shadow_dom_to_chromium",
			signals:    RouterSignals{IsHTML: true, HasShadowDOM: true},
			wantMode:   ModeChromiumCDP,
			wantReason: "layout_or_render_required",
		},
		{
			name:       "layout_observers_to_chromium",
			signals:    RouterSignals{IsHTML: true, HasLayoutObservers: true},
			wantMode:   ModeChromiumCDP,
			wantReason: "layout_or_render_required",
		},
		{
			name:       "service_worker_to_chromium",
			signals:    RouterSignals{IsHTML: true, HasServiceWorker: true},
			wantMode:   ModeChromiumCDP,
			wantReason: "layout_or_render_required",
		},
		{
			name:       "form_actionability_to_chromium",
			signals:    RouterSignals{IsHTML: true, NeedsFormActionability: true},
			wantMode:   ModeChromiumCDP,
			wantReason: "form_actionability_required",
		},
		{
			name:       "scripts_to_renderless",
			signals:    RouterSignals{IsHTML: true, ScriptCount: 5},
			wantMode:   ModeRenderlessJS,
			wantReason: "script_execution_needed",
		},
		{
			name:       "fetch_to_renderless",
			signals:    RouterSignals{IsHTML: true, HasFetch: true},
			wantMode:   ModeRenderlessJS,
			wantReason: "script_execution_needed",
		},
		{
			name:       "xhr_to_renderless",
			signals:    RouterSignals{IsHTML: true, HasXHR: true},
			wantMode:   ModeRenderlessJS,
			wantReason: "script_execution_needed",
		},
		{
			name:       "document_write_to_renderless",
			signals:    RouterSignals{IsHTML: true, HasDocumentWrite: true},
			wantMode:   ModeRenderlessJS,
			wantReason: "script_execution_needed",
		},
		{
			name:       "dom_mutation_to_renderless",
			signals:    RouterSignals{IsHTML: true, HasDOMMutation: true},
			wantMode:   ModeRenderlessJS,
			wantReason: "script_execution_needed",
		},
		{
			name:       "web_components_to_renderless",
			signals:    RouterSignals{IsHTML: true, HasWebComponents: true},
			wantMode:   ModeRenderlessJS,
			wantReason: "script_execution_needed",
		},
		{
			name:       "api_to_static",
			signals:    RouterSignals{IsAPI: true},
			wantMode:   ModeStaticFetch,
			wantReason: "api_or_rss_content",
		},
		{
			name:       "rss_to_static",
			signals:    RouterSignals{IsRSS: true},
			wantMode:   ModeStaticFetch,
			wantReason: "api_or_rss_content",
		},
		{
			name:       "plain_html_to_static",
			signals:    RouterSignals{IsHTML: true},
			wantMode:   ModeStaticFetch,
			wantReason: "static_html_no_scripts",
		},
		{
			name:       "default_to_static",
			signals:    RouterSignals{},
			wantMode:   ModeStaticFetch,
			wantReason: "default_static",
		},
	}

	r := NewFullExecutionRouter()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := r.Route(c.signals)
			if d.Mode != c.wantMode {
				t.Errorf("mode = %s, want %s (reason: %s)", d.Mode, c.wantMode, d.Reason)
			}
			if d.Reason != c.wantReason {
				t.Errorf("reason = %s, want %s", d.Reason, c.wantReason)
			}
		})
	}
}

func TestFullExecutionRouterEscalate(t *testing.T) {
	r := NewFullExecutionRouter()
	cases := []struct {
		current  ExecutionRouterMode
		wantNext ExecutionRouterMode
		wantErr  bool
	}{
		{ModeStaticFetch, ModeRenderlessJS, false},
		{ModeRenderlessJS, ModeChromiumCDP, false},
		{ModeChromiumCDP, ModeStealth, false},
		{ModeStealth, "", true},
		{ModeScrape, ModeChromiumCDP, false},
	}
	for _, c := range cases {
		t.Run(string(c.current), func(t *testing.T) {
			next, err := r.EscalateMode(c.current, "test")
			if c.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (next: %s)", next)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if next != c.wantNext {
					t.Errorf("next = %s, want %s", next, c.wantNext)
				}
			}
		})
	}
}

func TestShouldEscalateMode(t *testing.T) {
	validReasons := []EscalateReason{
		EscalateUnsupportedWebAPI,
		EscalateScriptTimeout,
		EscalateLayoutQueryReliance,
		EscalateAuthProfileRequired,
		EscalatePolicyDenied,
		EscalateLowConfidence,
	}
	for _, r := range validReasons {
		if !ShouldEscalateMode(r) {
			t.Errorf("ShouldEscalateMode(%s) = false, want true", r)
		}
	}
	if ShouldEscalateMode(EscalateReason("unknown")) {
		t.Error("ShouldEscalateMode(unknown) = true, want false")
	}
}

func TestIsValidExecutionRouterMode(t *testing.T) {
	valid := []ExecutionRouterMode{
		ModeStaticFetch, ModeRenderlessJS, ModeChromiumCDP, ModeStealth, ModeScrape,
	}
	for _, m := range valid {
		if !IsValidExecutionRouterMode(m) {
			t.Errorf("IsValidExecutionRouterMode(%s) = false, want true", m)
		}
	}
	if IsValidExecutionRouterMode("invalid") {
		t.Error("IsValidExecutionRouterMode(invalid) = true, want false")
	}
}

func TestToExtractionMode(t *testing.T) {
	cases := []struct {
		router ExecutionRouterMode
		want   scraper.ExtractionMode
	}{
		{ModeStaticFetch, scraper.ExtractionModeStaticFetch},
		{ModeRenderlessJS, scraper.ExtractionModeRenderlessJS},
		{ModeChromiumCDP, scraper.ExtractionModeChromiumCDP},
		{ModeStealth, scraper.ExtractionModeStealth},
		{ModeScrape, scraper.ExtractionModeScrape},
	}
	for _, c := range cases {
		got := ToExtractionMode(c.router)
		if got != c.want {
			t.Errorf("ToExtractionMode(%s) = %s, want %s", c.router, got, c.want)
		}
	}
}

func TestRouteFromEscalation(t *testing.T) {
	r := NewFullExecutionRouter()

	// CAPTCHA -> stealth
	d := r.RouteFromEscalation(scraper.EscalationSignals{HasCAPTCHA: true})
	if d.Mode != ModeStealth {
		t.Errorf("CAPTCHA: mode = %s, want stealth", d.Mode)
	}

	// Scripts -> renderless_js
	d = r.RouteFromEscalation(scraper.EscalationSignals{ScriptCount: 5})
	if d.Mode != ModeRenderlessJS {
		t.Errorf("scripts: mode = %s, want renderless_js", d.Mode)
	}

	// Service worker -> chromium_cdp
	d = r.RouteFromEscalation(scraper.EscalationSignals{HasServiceWorker: true})
	if d.Mode != ModeChromiumCDP {
		t.Errorf("service worker: mode = %s, want chromium_cdp", d.Mode)
	}

	// Login wall -> stealth (auth profile)
	d = r.RouteFromEscalation(scraper.EscalationSignals{HasLoginWall: true})
	if d.Mode != ModeStealth {
		t.Errorf("login wall: mode = %s, want stealth", d.Mode)
	}

	// No signals -> static_fetch
	d = r.RouteFromEscalation(scraper.EscalationSignals{})
	if d.Mode != ModeStaticFetch {
		t.Errorf("empty: mode = %s, want static_fetch", d.Mode)
	}
}

func TestFallbackEvent(t *testing.T) {
	ev := NewFallbackEvent(ModeRenderlessJS, ModeChromiumCDP, "layout_query_reliance", []string{"getBoundingClientRect"})
	if ev.EventType != "browser.execution_mode_escalated" {
		t.Errorf("eventType = %s, want browser.execution_mode_escalated", ev.EventType)
	}
	if ev.FromMode != ModeRenderlessJS {
		t.Errorf("fromMode = %s, want renderless_js", ev.FromMode)
	}
	if ev.ToMode != ModeChromiumCDP {
		t.Errorf("toMode = %s, want chromium_cdp", ev.ToMode)
	}
	if ev.FallbackReason != "layout_query_reliance" {
		t.Errorf("fallbackReason = %s, want layout_query_reliance", ev.FallbackReason)
	}
	if len(ev.UnsupportedFeatures) != 1 || ev.UnsupportedFeatures[0] != "getBoundingClientRect" {
		t.Errorf("unsupportedFeatures = %v, want [getBoundingClientRect]", ev.UnsupportedFeatures)
	}
	s := ev.String()
	if s == "" {
		t.Error("String() returned empty")
	}
}

func TestFormatFallbackReason(t *testing.T) {
	got := FormatFallbackReason(EscalateScriptTimeout, "5000ms exceeded")
	want := "script_timeout: 5000ms exceeded"
	if got != want {
		t.Errorf("FormatFallbackReason with detail = %q, want %q", got, want)
	}

	got = FormatFallbackReason(EscalatePolicyDenied, "")
	want = "policy_denied"
	if got != want {
		t.Errorf("FormatFallbackReason without detail = %q, want %q", got, want)
	}
}

func TestEnsureNoBlank(t *testing.T) {
	if got := EnsureNoBlank(""); got != ModeStaticFetch {
		t.Errorf("EnsureNoBlank('') = %s, want static_fetch", got)
	}
	if got := EnsureNoBlank("  "); got != ModeStaticFetch {
		t.Errorf("EnsureNoBlank('  ') = %s, want static_fetch", got)
	}
	if got := EnsureNoBlank(ModeStealth); got != ModeStealth {
		t.Errorf("EnsureNoBlank(stealth) = %s, want stealth", got)
	}
}

func TestDecisionContext(t *testing.T) {
	d := RouterDecision{Mode: ModeRenderlessJS, Reason: "script_execution_needed"}
	ctx := WithDecision(context.Background(), d)
	retrieved, ok := DecisionFromContext(ctx)
	if !ok {
		t.Fatal("DecisionFromContext returned false")
	}
	if retrieved.Mode != d.Mode {
		t.Errorf("mode = %s, want %s", retrieved.Mode, d.Mode)
	}
}
