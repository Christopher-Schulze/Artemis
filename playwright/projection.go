package playwright

import "sort"

// Availability is the tool-facing state of the Playwright lane.
type Availability string

const (
	AvailabilityReady       Availability = "ready"
	AvailabilityDegraded    Availability = "degraded"
	AvailabilityUnavailable Availability = "unavailable"
)

// Limits are the spec defaults of the lane (docs/spec.md:L2760).
type Limits struct {
	DefaultTTLSeconds       int   `json:"default_ttl_seconds"`
	MaxTTLSeconds           int   `json:"max_ttl_seconds"`
	SessionsPerProcess      int   `json:"sessions_per_process"`
	CommandTimeoutSeconds   int   `json:"command_timeout_seconds"`
	AuthoringSourceBytes    int64 `json:"authoring_source_bytes"`
	CompiledModuleBytes     int64 `json:"compiled_module_bytes"`
	CompileTimeoutSeconds   int   `json:"compile_timeout_seconds"`
	CompileAttemptsPerRun   int   `json:"compile_attempts_per_run"`
	CompileDiagnosticsBytes int64 `json:"compile_diagnostics_bytes"`
	ResultBytes             int64 `json:"result_bytes"`
	EvidenceBytesPerSession int64 `json:"evidence_bytes_per_session"`
}

// DefaultLimits returns the specified lane limits.
func DefaultLimits() Limits {
	return Limits{
		DefaultTTLSeconds: 30 * 60, MaxTTLSeconds: 8 * 60 * 60, SessionsPerProcess: 4, CommandTimeoutSeconds: 5 * 60,
		AuthoringSourceBytes: 256 << 10, CompiledModuleBytes: 512 << 10, CompileTimeoutSeconds: 30, CompileAttemptsPerRun: 3,
		CompileDiagnosticsBytes: 64 << 10, ResultBytes: 8 << 20, EvidenceBytesPerSession: 256 << 20,
	}
}

// Projection is the BrowserToolCapabilityProjection of the Playwright lane.
type Projection struct {
	ArtifactDigest    string       `json:"artifact_digest,omitempty"`
	Availability      Availability `json:"availability"`
	SupportedBrowsers []string     `json:"supported_browsers"`
	SupportedModes    []string     `json:"supported_modes"`
	Capabilities      []Capability `json:"capabilities"`
	ProfileModes      []string     `json:"profile_modes"`
	Limits            Limits       `json:"limits"`
	PolicyRef         string       `json:"policy_ref,omitempty"`
	HealthRef         string       `json:"health_ref,omitempty"`
	Diagnostics       []string     `json:"diagnostics"`
	Remediation       string       `json:"remediation,omitempty"`
}

// Project derives the tool projection from the verified cache status and
// whether a sandboxed execution runtime is attached. Without an admitted
// artifact or an execution runtime the lane is unavailable; without the
// agent-source compiler only browser_script is removed and the lane is
// degraded.
func Project(status Status, executionRuntime bool, policyRef string) Projection {
	projection := Projection{
		Availability: AvailabilityUnavailable, SupportedBrowsers: []string{}, SupportedModes: []string{},
		Capabilities: []Capability{}, ProfileModes: []string{}, Limits: DefaultLimits(), PolicyRef: policyRef,
		HealthRef: "playwright.lane", Diagnostics: append([]string{}, status.Diagnostics...), Remediation: status.Remediation,
	}
	if !status.Admitted {
		return projection
	}
	admission := status.Admission
	projection.ArtifactDigest = admission.ArtifactDigest
	if !executionRuntime {
		projection.Diagnostics = append(projection.Diagnostics, "admitted artifact has no sandboxed execution runtime attached")
		projection.Remediation = "attach the host's sandboxed Playwright execution runtime"
		return projection
	}
	for _, browser := range admission.Manifest.Browsers {
		projection.SupportedBrowsers = append(projection.SupportedBrowsers, browser.Browser)
	}
	sort.Strings(projection.SupportedBrowsers)
	projection.SupportedModes = []string{"isolated", "managed_profile", "authorized_attach"}
	projection.ProfileModes = []string{"isolated", "managed_profile", "authorized_attach"}
	projection.Availability = AvailabilityReady
	for _, capability := range admission.Manifest.Capabilities {
		if capability == CapBrowserScript && !admission.ScriptCompiler {
			projection.Availability = AvailabilityDegraded
			projection.Diagnostics = append(projection.Diagnostics, "agent-source compiler or context declarations missing: browser_script unavailable")
			continue
		}
		projection.Capabilities = append(projection.Capabilities, capability)
	}
	projection.Remediation = ""
	return projection
}

// Supports reports whether the projection admits a capability.
func (p Projection) Supports(capability Capability) bool {
	for _, have := range p.Capabilities {
		if have == capability {
			return true
		}
	}
	return false
}

// SupportsBrowser reports whether the projection admits a browser.
func (p Projection) SupportsBrowser(browser string) bool {
	for _, have := range p.SupportedBrowsers {
		if have == browser {
			return true
		}
	}
	return false
}
