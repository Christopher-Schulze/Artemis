// Package playwright owns the Playwright peer lane provisioning contract: a
// signed, self-contained artifact (Playwright Core, its private runtime,
// optional agent-source compiler and declarations, SBOM, licenses and
// provenance) is admitted only when every listed byte, role, ABI and
// capability verifies, is installed into a shared cache with a
// last-known-good pointer, and is projected into a truthful tool capability
// view. Execution of admitted artifacts belongs to an embedding host's
// sandboxed runtime; without one the lane reports itself unavailable.
package playwright

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
)

// File names of the signed artifact envelope at the artifact root.
const (
	ManifestFile  = "artemis-playwright-manifest.json"
	SignatureFile = "artemis-playwright-manifest.sig"
	// SessionABIVersion is the agent session ABI this Artemis release binds.
	SessionABIVersion = "1"
	schemaVersion     = 1
	maxManifestBytes  = 1 << 20
	maxListedFiles    = 100000
)

// Role classifies one artifact file.
type Role string

const (
	RoleCore         Role = "core"
	RoleRuntime      Role = "runtime"
	RoleBrowser      Role = "browser"
	RoleCompiler     Role = "compiler"
	RoleDeclarations Role = "declarations"
	RoleSBOM         Role = "sbom"
	RoleLicenses     Role = "licenses"
	RoleProvenance   Role = "provenance"
	RoleSupport      Role = "support"
)

var requiredRoles = []Role{RoleCore, RoleRuntime, RoleSBOM, RoleLicenses, RoleProvenance}

var knownRoles = map[Role]bool{
	RoleCore: true, RoleRuntime: true, RoleBrowser: true, RoleCompiler: true, RoleDeclarations: true,
	RoleSBOM: true, RoleLicenses: true, RoleProvenance: true, RoleSupport: true,
}

// AdmissionMode is how verified bytes reached the unit.
type AdmissionMode string

const (
	AdmissionInstaller AdmissionMode = "installer_bundle"
	AdmissionUpdate    AdmissionMode = "signed_update_bundle"
	AdmissionOnDemand  AdmissionMode = "policy_approved_on_demand"
)

// Capability is one Playwright-lane operation family (docs/spec.md:L2760).
type Capability string

const (
	CapNavigation    Capability = "navigation"
	CapLocators      Capability = "locators"
	CapInput         Capability = "input"
	CapUpload        Capability = "upload"
	CapDownload      Capability = "download"
	CapTabs          Capability = "tabs"
	CapFrames        Capability = "frames"
	CapDialogs       Capability = "dialogs"
	CapConsole       Capability = "console"
	CapDOM           Capability = "dom"
	CapAccessibility Capability = "accessibility"
	CapScreenshot    Capability = "screenshot"
	CapVideo         Capability = "video"
	CapTrace         Capability = "trace"
	CapHAR           Capability = "har"
	CapStorage       Capability = "storage"
	CapEmulation     Capability = "emulation"
	CapEvaluation    Capability = "evaluation"
	// CapBrowserScript is the agent-source escape lane; it additionally needs
	// the admitted compiler and context declarations.
	CapBrowserScript Capability = "browser_script"
)

var knownCapabilities = map[Capability]bool{
	CapNavigation: true, CapLocators: true, CapInput: true, CapUpload: true, CapDownload: true, CapTabs: true,
	CapFrames: true, CapDialogs: true, CapConsole: true, CapDOM: true, CapAccessibility: true, CapScreenshot: true,
	CapVideo: true, CapTrace: true, CapHAR: true, CapStorage: true, CapEmulation: true, CapEvaluation: true,
	CapBrowserScript: true,
}

var knownBrowsers = map[string]bool{"chromium": true, "chrome": true, "msedge": true, "firefox": true, "webkit": true}

// File is one listed artifact file with its exact digest and size.
type File struct {
	Path   string `json:"path"`
	Role   Role   `json:"role"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// BrowserRevision is one declared browser build of the artifact.
type BrowserRevision struct {
	Browser  string `json:"browser"`
	Revision string `json:"revision"`
}

// Manifest is the signed PlaywrightProvisioningContract payload.
type Manifest struct {
	SchemaVersion         int               `json:"schema_version"`
	ArtifactVersion       string            `json:"artifact_version"`
	PlaywrightCoreVersion string            `json:"playwright_core_version"`
	RuntimeVersion        string            `json:"runtime_version"`
	AdmissionMode         AdmissionMode     `json:"admission_mode"`
	OfflineReady          bool              `json:"offline_ready"`
	SessionABIVersion     string            `json:"agent_session_abi_version"`
	Browsers              []BrowserRevision `json:"browsers"`
	Capabilities          []Capability      `json:"capabilities"`
	Files                 []File            `json:"files"`
}

// ErrManifestInvalid reports a manifest that violates the contract.
var ErrManifestInvalid = errors.New("playwright manifest invalid")

func decodeManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if len(data) == 0 || len(data) > maxManifestBytes {
		return manifest, fmt.Errorf("%w: manifest must be 1..%d bytes", ErrManifestInvalid, maxManifestBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	if decoder.More() {
		return manifest, fmt.Errorf("%w: trailing data", ErrManifestInvalid)
	}
	return manifest, manifest.validate()
}

func (m Manifest) validate() error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrManifestInvalid, fmt.Sprintf(format, args...))
	}
	switch {
	case m.SchemaVersion != schemaVersion:
		return invalid("schema_version %d", m.SchemaVersion)
	case m.ArtifactVersion == "" || m.PlaywrightCoreVersion == "" || m.RuntimeVersion == "":
		return invalid("artifact, Playwright Core and runtime versions are required")
	case m.AdmissionMode != AdmissionInstaller && m.AdmissionMode != AdmissionUpdate && m.AdmissionMode != AdmissionOnDemand:
		return invalid("admission_mode %q", m.AdmissionMode)
	case !m.OfflineReady:
		return invalid("artifact must be offline ready")
	case len(m.Browsers) == 0 || len(m.Capabilities) == 0:
		return invalid("browsers and capabilities are required")
	case len(m.Files) == 0 || len(m.Files) > maxListedFiles:
		return invalid("1..%d files are required", maxListedFiles)
	}
	for _, browser := range m.Browsers {
		if !knownBrowsers[browser.Browser] || browser.Revision == "" {
			return invalid("browser %q revision %q", browser.Browser, browser.Revision)
		}
	}
	for _, capability := range m.Capabilities {
		if !knownCapabilities[capability] {
			return invalid("unknown capability %q", capability)
		}
	}
	seen := make(map[string]bool, len(m.Files))
	roles := make(map[Role]bool)
	for _, file := range m.Files {
		if err := validateArtifactPath(file.Path); err != nil {
			return invalid("%v", err)
		}
		if seen[file.Path] {
			return invalid("duplicate file %q", file.Path)
		}
		seen[file.Path] = true
		if !knownRoles[file.Role] {
			return invalid("file %q has unknown role %q", file.Path, file.Role)
		}
		if len(file.SHA256) != 64 || strings.Trim(file.SHA256, "0123456789abcdef") != "" || file.Size < 0 {
			return invalid("file %q digest or size", file.Path)
		}
		roles[file.Role] = true
	}
	for _, role := range requiredRoles {
		if !roles[role] {
			return invalid("required role %q has no file", role)
		}
	}
	return nil
}

// validateArtifactPath accepts only clean, relative, slash-separated paths
// inside the artifact root.
func validateArtifactPath(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || path.Clean(name) != name || name == "." || strings.HasPrefix(name, "../") || name == ".." {
		return fmt.Errorf("unsafe artifact path %q", name)
	}
	if name == ManifestFile || name == SignatureFile {
		return fmt.Errorf("artifact path %q is reserved", name)
	}
	return nil
}

// hasRole reports whether at least one listed file has role.
func (m Manifest) hasRole(role Role) bool {
	for _, file := range m.Files {
		if file.Role == role {
			return true
		}
	}
	return false
}

func (m Manifest) totalBytes() int64 {
	var total int64
	for _, file := range m.Files {
		total += file.Size
	}
	return total
}

// KnownCapability reports whether capability is a declared lane capability.
func KnownCapability(capability Capability) bool {
	return knownCapabilities[capability]
}
