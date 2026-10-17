package router

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Christopher-Schulze/Artemis/playwright"
)

// AgentBrowserIntent is the caller's engine intent, resolved by the
// BrowserToolRouter before the direct BrowserExecutionRouter (docs/spec.md:L3992).
type AgentBrowserIntent string

const (
	IntentAuto       AgentBrowserIntent = "auto"
	IntentArtemis    AgentBrowserIntent = "artemis"
	IntentPlaywright AgentBrowserIntent = "playwright"
)

// ParseIntent maps an optional request field to an intent; empty means auto.
func ParseIntent(value string) (AgentBrowserIntent, error) {
	switch AgentBrowserIntent(strings.TrimSpace(value)) {
	case "", IntentAuto:
		return IntentAuto, nil
	case IntentArtemis:
		return IntentArtemis, nil
	case IntentPlaywright:
		return IntentPlaywright, nil
	default:
		return "", fmt.Errorf("unknown browser engine intent %q (auto|artemis|playwright)", value)
	}
}

// ToolEngine is the engine that will own the operation or session.
type ToolEngine string

const (
	EngineArtemis    ToolEngine = "artemis"
	EnginePlaywright ToolEngine = "playwright"
)

// ToolRequirement is one capability the operation needs. Requirements are
// "browser:<name>" or "capability:<playwright capability>".
type ToolRequirement string

// directUnsupported lists requirements the Artemis direct engines cannot
// satisfy; every other declared requirement is direct-capable.
var directUnsupported = map[ToolRequirement]bool{
	"browser:firefox":           true,
	"browser:webkit":            true,
	"capability:video":          true,
	"capability:har":            true,
	"capability:trace":          true,
	"capability:browser_script": true,
}

// ToolDecision is the auditable routing outcome.
type ToolDecision struct {
	Intent         AgentBrowserIntent `json:"intent"`
	Engine         ToolEngine         `json:"engine"`
	Requirements   []ToolRequirement  `json:"requirements,omitempty"`
	ArtifactDigest string             `json:"artifact_digest,omitempty"`
	Reason         string             `json:"reason"`
}

// ToolUnavailableError is the typed CapabilityUnavailable result. It never
// carries a substitute engine: the caller must change its intent.
type ToolUnavailableError struct {
	Intent       AgentBrowserIntent `json:"intent"`
	Engine       ToolEngine         `json:"engine"`
	Unmet        []ToolRequirement  `json:"unmet,omitempty"`
	Availability string             `json:"availability,omitempty"`
	Diagnostics  []string           `json:"diagnostics,omitempty"`
	Remediation  string             `json:"remediation"`
}

func (e *ToolUnavailableError) Error() string {
	detail := e.Remediation
	if len(e.Unmet) > 0 {
		names := make([]string, len(e.Unmet))
		for i, requirement := range e.Unmet {
			names[i] = string(requirement)
		}
		detail = "unmet " + strings.Join(names, ", ") + "; " + detail
	}
	return fmt.Sprintf("browser engine %s unavailable for intent %s: %s", e.Engine, e.Intent, detail)
}

// ErrInvalidRequirement reports a requirement outside the declared set.
var ErrInvalidRequirement = errors.New("invalid browser tool requirement")

// ResolveTool deterministically selects the engine. Explicit Playwright
// selects the admitted lane or fails typed; explicit Artemis selects the
// direct engines or fails typed; auto prefers the direct engines when they
// satisfy every requirement and otherwise the lane only when it satisfies
// all of them. No branch substitutes an engine the caller did not allow.
func ResolveTool(intent AgentBrowserIntent, requirements []ToolRequirement, lane playwright.Projection) (ToolDecision, error) {
	required, err := normalizeRequirements(requirements)
	if err != nil {
		return ToolDecision{}, err
	}
	directUnmet := unmetDirect(required)
	laneUnmet := unmetLane(required, lane)
	decision := ToolDecision{Intent: intent, Requirements: required}
	switch intent {
	case IntentArtemis:
		if len(directUnmet) > 0 {
			return ToolDecision{}, &ToolUnavailableError{Intent: intent, Engine: EngineArtemis, Unmet: directUnmet, Remediation: "these requirements need the Playwright lane; request engine playwright"}
		}
		decision.Engine, decision.Reason = EngineArtemis, "explicit artemis"
	case IntentPlaywright:
		if lane.Availability == playwright.AvailabilityUnavailable || len(laneUnmet) > 0 {
			return ToolDecision{}, laneUnavailable(intent, laneUnmet, lane)
		}
		decision.Engine, decision.ArtifactDigest, decision.Reason = EnginePlaywright, lane.ArtifactDigest, "explicit playwright"
	case IntentAuto:
		switch {
		case len(directUnmet) == 0:
			decision.Engine, decision.Reason = EngineArtemis, "direct engines satisfy every requirement"
		case lane.Availability != playwright.AvailabilityUnavailable && len(laneUnmet) == 0:
			decision.Engine, decision.ArtifactDigest, decision.Reason = EnginePlaywright, lane.ArtifactDigest, "only the Playwright lane satisfies every requirement"
		default:
			return ToolDecision{}, laneUnavailable(intent, laneUnmet, lane)
		}
	default:
		return ToolDecision{}, fmt.Errorf("unknown browser engine intent %q", intent)
	}
	return decision, nil
}

func laneUnavailable(intent AgentBrowserIntent, unmet []ToolRequirement, lane playwright.Projection) *ToolUnavailableError {
	remediation := lane.Remediation
	if remediation == "" {
		remediation = "the admitted Playwright artifact does not provide these requirements"
	}
	return &ToolUnavailableError{
		Intent: intent, Engine: EnginePlaywright, Unmet: unmet, Availability: string(lane.Availability),
		Diagnostics: append([]string(nil), lane.Diagnostics...), Remediation: remediation,
	}
}

func normalizeRequirements(requirements []ToolRequirement) ([]ToolRequirement, error) {
	seen := make(map[ToolRequirement]bool, len(requirements))
	var result []ToolRequirement
	for _, requirement := range requirements {
		kind, name, ok := strings.Cut(string(requirement), ":")
		valid := ok && name != "" && (kind == "browser" && knownBrowser(name) || kind == "capability" && knownCapability(name))
		if !valid {
			return nil, fmt.Errorf("%w: %q", ErrInvalidRequirement, requirement)
		}
		if !seen[requirement] {
			seen[requirement] = true
			result = append(result, requirement)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func knownBrowser(name string) bool {
	switch name {
	case "chromium", "chrome", "msedge", "firefox", "webkit":
		return true
	default:
		return false
	}
}

func knownCapability(name string) bool {
	return playwright.KnownCapability(playwright.Capability(name))
}

func unmetDirect(required []ToolRequirement) []ToolRequirement {
	var unmet []ToolRequirement
	for _, requirement := range required {
		if directUnsupported[requirement] {
			unmet = append(unmet, requirement)
		}
	}
	return unmet
}

func unmetLane(required []ToolRequirement, lane playwright.Projection) []ToolRequirement {
	var unmet []ToolRequirement
	for _, requirement := range required {
		kind, name, _ := strings.Cut(string(requirement), ":")
		if kind == "browser" && !lane.SupportsBrowser(name) || kind == "capability" && !lane.Supports(playwright.Capability(name)) {
			unmet = append(unmet, requirement)
		}
	}
	return unmet
}
