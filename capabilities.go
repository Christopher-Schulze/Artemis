package artemis

import "fmt"

// Version is the release identity shared by the library, CLI, and capability contract.
const Version = "0.1.0-alpha.1"

// SupportState is the release support level of a public capability.
type SupportState string

const (
	SupportSupported    SupportState = "supported"
	SupportExperimental SupportState = "experimental"
	SupportUnavailable  SupportState = "unavailable"
)

// ExecutionMode identifies the runtime that must perform a capability.
type ExecutionMode string

const (
	ModeRenderless ExecutionMode = "renderless_js"
	ModeChromium   ExecutionMode = "chromium_cdp"
	ModeHybrid     ExecutionMode = "hybrid"
)

// Capability is one auditable public release claim.
type Capability struct {
	ID             string        `json:"id"`
	Description    string        `json:"description"`
	Mode           ExecutionMode `json:"mode"`
	State          SupportState  `json:"state"`
	Since          string        `json:"since,omitempty"`
	Entrypoint     string        `json:"entrypoint"`
	Owner          string        `json:"owner"`
	BehaviorTest   string        `json:"behaviorTest"`
	OmnimusTools   []string      `json:"omnimusTools,omitempty"`
	UnavailableWhy string        `json:"unavailableWhy,omitempty"`
}

var capabilityRegistry = []Capability{
	{
		ID: "renderless.fetch", Description: "Fetch, parse, and retain an HTML page", Mode: ModeRenderless,
		State: SupportSupported, Since: Version, Entrypoint: "engine.(*Engine).Fetch", Owner: "engine.Engine/engine.Page",
		BehaviorTest: "engine.TestEngineFetchEndToEnd",
	},
	{
		ID: "renderless.javascript", Description: "Execute page and caller JavaScript against the retained DOM", Mode: ModeRenderless,
		State: SupportSupported, Since: Version, Entrypoint: "engine.(*Page).Eval", Owner: "js.Runtime/js.Context/engine.Page",
		BehaviorTest: "engine.TestPageMarkdownReflectsJSMutation",
	},
	{
		ID: "renderless.extract", Description: "Extract Markdown, text, links, semantic, and structured data", Mode: ModeRenderless,
		State: SupportSupported, Since: Version, Entrypoint: "engine.Page extraction methods", Owner: "engine.Page/agent",
		BehaviorTest: "agent.TestMarkdownHeadings", OmnimusTools: []string{"scrape", "scrape_static", "scrape_batch"},
	},
	{
		ID: "renderless.steering", Description: "Drive persistent renderless sessions over JSON WebSocket commands", Mode: ModeRenderless,
		State: SupportSupported, Since: Version, Entrypoint: "serve.(*Server).ListenAndServe", Owner: "serve.Server/session",
		BehaviorTest: "serve.TestSessionOpenEvalDump",
	},
	{
		ID: "agent.high_level", Description: "Execute typed renderless fetch actions through an owned Agent lifecycle", Mode: ModeRenderless,
		State: SupportSupported, Since: Version, Entrypoint: "artemis.(*Agent).ExecuteTask", Owner: "artemis.Agent",
		BehaviorTest: "artemis.TestAgentExecutesFetchWithObservableEvidence",
	},
	{
		ID: "chromium.cdp", Description: "Launch and control Chromium through CDP", Mode: ModeChromium,
		State: SupportSupported, Since: Version, Entrypoint: "bridge.LaunchChromium/bridge.ConnectChromium", Owner: "bridge.ChromiumBrowser/process.Browser/bridge.CDPTransport",
		BehaviorTest: "bridge.TestChromiumLifecycleIntegration",
	},
	{ID: "chromium.observe", Description: "Capture bounded DOM and accessibility observations with stable references", Mode: ModeChromium, State: SupportSupported, Since: Version, Entrypoint: "bridge/observe.(*Collector).Capture", Owner: "bridge/observe.Collector", BehaviorTest: "observe_test.TestChromiumObservationFixture"},
	{ID: "chromium.actions", Description: "Execute typed browser actions with actionability and postcondition evidence", Mode: ModeChromium, State: SupportSupported, Since: Version, Entrypoint: "bridge/actions.(*Runtime).Execute", Owner: "bridge/actions.Runtime", BehaviorTest: "actions.TestRuntimeRealChromiumInteractionMatrix"},
	{
		ID: "hybrid.routing", Description: "Escalate deterministically from renderless execution to Chromium", Mode: ModeHybrid,
		State: SupportSupported, Since: Version, Entrypoint: "router.New/router.(*HybridRouter).Execute", Owner: "router.HybridRouter/router.ChromiumExecutor",
		BehaviorTest: "router.TestChromiumExecutorAgainstRealChromiumFixture",
	},
	{
		ID: "chromium.screenshot", Description: "Capture pixels rendered by Chromium", Mode: ModeChromium,
		State: SupportSupported, Since: Version, Entrypoint: "bridge/actions.(*Runtime).Execute", Owner: "bridge/actions.Runtime",
		BehaviorTest: "actions.TestRuntimeRealChromiumInteractionMatrix",
	},
	{
		ID: "chromium.stealth", Description: "Apply and verify Chromium anti-detection controls", Mode: ModeChromium,
		State: SupportUnavailable, Entrypoint: "stealth/bridge", Owner: "unassigned",
		BehaviorTest: "artemis.TestUnavailableCapabilities", UnavailableWhy: "no real Chromium behavior probe exists",
	},
	{
		ID: "profiles.persistent", Description: "Persist isolated authenticated browser profiles", Mode: ModeChromium,
		State: SupportSupported, Since: Version, Entrypoint: "profile.NewBrowserRuntime", Owner: "profile.RuntimeManager",
		BehaviorTest: "profile.TestBrowserRuntimePersistentCookieAndStorageAcrossRestart",
	},
}

// Capabilities returns a defensive copy of the canonical capability registry.
func Capabilities() []Capability {
	result := make([]Capability, len(capabilityRegistry))
	for i, capability := range capabilityRegistry {
		result[i] = capability
		result[i].OmnimusTools = append([]string(nil), capability.OmnimusTools...)
	}
	return result
}

// CapabilityByID returns the capability with id.
func CapabilityByID(id string) (Capability, bool) {
	for _, capability := range capabilityRegistry {
		if capability.ID == id {
			capability.OmnimusTools = append([]string(nil), capability.OmnimusTools...)
			return capability, true
		}
	}
	return Capability{}, false
}

// OmnimusToolSupported reports whether a tool is backed by a supported capability.
func OmnimusToolSupported(name string) bool {
	for _, capability := range capabilityRegistry {
		if capability.State != SupportSupported {
			continue
		}
		for _, tool := range capability.OmnimusTools {
			if tool == name {
				return true
			}
		}
	}
	return false
}

// ValidateCapabilityRegistry rejects claims that lack executable evidence metadata.
func ValidateCapabilityRegistry() error {
	seenIDs := make(map[string]struct{}, len(capabilityRegistry))
	seenTools := make(map[string]string)
	for _, capability := range capabilityRegistry {
		if capability.ID == "" || capability.Description == "" || capability.Entrypoint == "" || capability.Owner == "" || capability.BehaviorTest == "" {
			return fmt.Errorf("artemis: incomplete capability %q", capability.ID)
		}
		if _, exists := seenIDs[capability.ID]; exists {
			return fmt.Errorf("artemis: duplicate capability %q", capability.ID)
		}
		seenIDs[capability.ID] = struct{}{}
		switch capability.State {
		case SupportSupported:
			if capability.Since == "" || capability.UnavailableWhy != "" {
				return fmt.Errorf("artemis: invalid supported capability %q", capability.ID)
			}
		case SupportExperimental:
			if capability.Since == "" {
				return fmt.Errorf("artemis: experimental capability %q has no version", capability.ID)
			}
		case SupportUnavailable:
			if capability.Since != "" || capability.UnavailableWhy == "" || len(capability.OmnimusTools) != 0 {
				return fmt.Errorf("artemis: invalid unavailable capability %q", capability.ID)
			}
		default:
			return fmt.Errorf("artemis: invalid support state %q", capability.State)
		}
		for _, tool := range capability.OmnimusTools {
			if owner, exists := seenTools[tool]; exists {
				return fmt.Errorf("artemis: Omnimus tool %q claimed by %q and %q", tool, owner, capability.ID)
			}
			seenTools[tool] = capability.ID
		}
	}
	return nil
}
