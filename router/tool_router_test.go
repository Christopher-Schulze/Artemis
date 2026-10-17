package router

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Christopher-Schulze/Artemis/playwright"
)

func readyLane() playwright.Projection {
	return playwright.Projection{
		ArtifactDigest: "sha256:lane", Availability: playwright.AvailabilityReady,
		SupportedBrowsers: []string{"chromium", "firefox"},
		Capabilities:      []playwright.Capability{playwright.CapNavigation, playwright.CapVideo, playwright.CapHAR},
	}
}

func unavailableLane() playwright.Projection {
	return playwright.Projection{Availability: playwright.AvailabilityUnavailable, Diagnostics: []string{"no artifact"}, Remediation: "install a signed Playwright artifact"}
}

func TestResolveToolNeverSubstitutesAnExplicitEngine(t *testing.T) {
	cases := []struct {
		name       string
		intent     AgentBrowserIntent
		require    []ToolRequirement
		lane       playwright.Projection
		wantEngine ToolEngine
		wantErr    ToolEngine
		wantUnmet  []ToolRequirement
	}{
		{"explicit playwright unavailable stays failed", IntentPlaywright, nil, unavailableLane(), "", EnginePlaywright, nil},
		{"explicit playwright ready", IntentPlaywright, []ToolRequirement{"capability:video"}, readyLane(), EnginePlaywright, "", nil},
		{"explicit playwright missing capability", IntentPlaywright, []ToolRequirement{"browser:webkit"}, readyLane(), "", EnginePlaywright, []ToolRequirement{"browser:webkit"}},
		{"explicit artemis ignores a ready lane", IntentArtemis, nil, readyLane(), EngineArtemis, "", nil},
		{"explicit artemis never escalates to playwright", IntentArtemis, []ToolRequirement{"capability:har"}, readyLane(), "", EngineArtemis, []ToolRequirement{"capability:har"}},
		{"auto prefers direct engines", IntentAuto, []ToolRequirement{"capability:screenshot"}, readyLane(), EngineArtemis, "", nil},
		{"auto uses the lane only when it covers everything", IntentAuto, []ToolRequirement{"capability:video", "browser:firefox"}, readyLane(), EnginePlaywright, "", nil},
		{"auto fails typed when nothing covers everything", IntentAuto, []ToolRequirement{"capability:video", "browser:webkit"}, readyLane(), "", EnginePlaywright, []ToolRequirement{"browser:webkit"}},
		{"auto fails typed without a lane", IntentAuto, []ToolRequirement{"capability:video"}, unavailableLane(), "", EnginePlaywright, []ToolRequirement{"capability:video"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := ResolveTool(tc.intent, tc.require, tc.lane)
			if tc.wantErr != "" {
				var unavailable *ToolUnavailableError
				if !errors.As(err, &unavailable) || unavailable.Engine != tc.wantErr || unavailable.Remediation == "" || !reflect.DeepEqual(unavailable.Unmet, tc.wantUnmet) {
					t.Fatalf("ResolveTool = %+v, %v; want %s unavailable with unmet %v", decision, err, tc.wantErr, tc.wantUnmet)
				}
				if decision.Engine != "" {
					t.Fatalf("failed resolution returned engine %q", decision.Engine)
				}
				return
			}
			if err != nil || decision.Engine != tc.wantEngine || decision.Intent != tc.intent || decision.Reason == "" {
				t.Fatalf("ResolveTool = %+v, %v; want engine %s", decision, err, tc.wantEngine)
			}
			if decision.Engine == EnginePlaywright && decision.ArtifactDigest != "sha256:lane" {
				t.Fatalf("playwright decision lost the artifact digest: %+v", decision)
			}
		})
	}
}

func TestResolveToolIsDeterministicAndValidatesInput(t *testing.T) {
	first, err := ResolveTool(IntentAuto, []ToolRequirement{"capability:video", "browser:firefox", "capability:video"}, readyLane())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveTool(IntentAuto, []ToolRequirement{"browser:firefox", "capability:video"}, readyLane())
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent requirements resolved differently: %+v / %+v (%v)", first, second, err)
	}
	for _, bad := range []ToolRequirement{"browser:netscape", "capability:mind_reading", "video", "capability:"} {
		if _, err := ResolveTool(IntentAuto, []ToolRequirement{bad}, readyLane()); !errors.Is(err, ErrInvalidRequirement) {
			t.Fatalf("requirement %q accepted: %v", bad, err)
		}
	}
	if _, err := ParseIntent("chromium"); err == nil {
		t.Fatal("unknown intent accepted")
	}
	if intent, err := ParseIntent(""); err != nil || intent != IntentAuto {
		t.Fatalf("empty intent = %q, %v", intent, err)
	}
}
