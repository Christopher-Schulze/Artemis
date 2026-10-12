package artemis

import "fmt"

// Version is the release identity shared by the library, CLI, and capability contract.
const Version = "0.1.0"

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
	ArtemisTools   []string      `json:"artemisTools,omitempty"`
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
		BehaviorTest: "agent.TestMarkdownHeadings", ArtemisTools: []string{"scrape", "scrape_static", "scrape_batch"},
	},
	{
		ID: "renderless.login", Description: "Detect, fill, submit, and verify a credential-backed login form", Mode: ModeRenderless,
		State: SupportSupported, Since: Version, Entrypoint: "actions.DetectLoginForm/agent.(*Form).Submit", Owner: "actions/agent/engine",
		BehaviorTest: "browser.TestSessionLoginDetectsResolvesSubmitsAndVerifiesPersistence", ArtemisTools: []string{"login"},
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
		State: SupportSupported, Since: Version, Entrypoint: "stealth.NewDocumentScript/bridge.BrowserContext.NewPageWithScripts", Owner: "stealth.EnvironmentProfile/bridge.TargetScriptConfig",
		BehaviorTest: "bridge.TestChromiumTargetScriptsRunBeforePageAndWorkerCode",
	},
	{
		ID: "chromium.challenge", Description: "Detect and resolve browser challenges through policy-gated verified outcomes", Mode: ModeChromium,
		State: SupportExperimental, Since: Version, Entrypoint: "solver.ChallengeDetector/solver.ChallengeResolver", Owner: "solver.ChallengeDetector/solver.ChallengeResolver",
		BehaviorTest: "solver.TestChallengeResolverRequiresPolicyAndPostcondition",
	},
	{
		ID: "chromium.h2_fingerprint", Description: "Match browser HTTP/2 fingerprint settings to a measured Chromium build", Mode: ModeChromium,
		State: SupportUnavailable, Entrypoint: "stealth.H2Fingerprint", Owner: "stealth.H2Fingerprint",
		BehaviorTest: "artemis.TestUnavailableCapabilities", UnavailableWhy: "no verified Chromium transport-level H2 parity contract",
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
		result[i].ArtemisTools = append([]string(nil), capability.ArtemisTools...)
	}
	return result
}

// CapabilityByID returns the capability with id.
func CapabilityByID(id string) (Capability, bool) {
	for _, capability := range capabilityRegistry {
		if capability.ID == id {
			capability.ArtemisTools = append([]string(nil), capability.ArtemisTools...)
			return capability, true
		}
	}
	return Capability{}, false
}

// ArtemisToolSupported reports whether a tool is backed by a supported capability.
func ArtemisToolSupported(name string) bool {
	for _, capability := range capabilityRegistry {
		if capability.State != SupportSupported {
			continue
		}
		for _, tool := range capability.ArtemisTools {
			if tool == name {
				return true
			}
		}
	}
	return false
}

// CompatibilityMatrix is a deterministic view of the capability registry by
// execution mode and support state. It is the canonical source for which
// public API claims are supported on which engine surface.
type CompatibilityMatrix struct {
	Version string       `json:"version"`
	Modes   []string     `json:"modes"`
	Rows    []Capability `json:"rows"`
}

// DefaultCompatibilityMatrix returns the canonical matrix for the current release.
func DefaultCompatibilityMatrix() CompatibilityMatrix {
	return CompatibilityMatrix{
		Version: Version,
		Modes:   []string{string(ModeRenderless), string(ModeChromium), string(ModeHybrid)},
		Rows:    Capabilities(),
	}
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
			if capability.Since != "" || capability.UnavailableWhy == "" || len(capability.ArtemisTools) != 0 {
				return fmt.Errorf("artemis: invalid unavailable capability %q", capability.ID)
			}
		default:
			return fmt.Errorf("artemis: invalid support state %q", capability.State)
		}
		for _, tool := range capability.ArtemisTools {
			if owner, exists := seenTools[tool]; exists {
				return fmt.Errorf("artemis: Artemis tool %q claimed by %q and %q", tool, owner, capability.ID)
			}
			seenTools[tool] = capability.ID
		}
	}
	return nil
}
