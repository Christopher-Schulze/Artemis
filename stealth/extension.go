package stealth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// EvasionMode controls how stealth patches are injected to avoid
// DevTools Protocol detection (spec ss28 detection evasion).
type EvasionMode int

const (
	// EvasionExtension uses a Chrome Extension (--load-extension)
	// which runs in extension context, not detectable via page DOM.
	// This is the DEFAULT mode (Option B).
	EvasionExtension EvasionMode = iota
	// EvasionRuntime uses Runtime.evaluate AFTER page load.
	// This is the fallback mode (Option A).
	EvasionRuntime
)

// String returns the mode name for logging/config.
func (m EvasionMode) String() string {
	switch m {
	case EvasionExtension:
		return "extension"
	case EvasionRuntime:
		return "runtime"
	default:
		return "unknown"
	}
}

// DefaultEvasionMode returns the default evasion mode (Option B).
func DefaultEvasionMode() EvasionMode {
	return EvasionExtension
}

// StealthExtension generates a Chrome Extension (Manifest V3) that
// injects stealth patches in extension context, avoiding detection
// via Page.addScriptToEvaluateOnNewDocument which is itself detectable
// (scripts before DOMContentLoaded = suspicious).
type StealthExtension struct {
	// Profile is the stealth profile used to generate the patch script.
	Profile Profile
	// Mode is the injection evasion mode.
	Mode EvasionMode
}

// NewStealthExtension creates a StealthExtension with the given profile
// and default evasion mode (Option B - Chrome Extension).
func NewStealthExtension(p Profile) *StealthExtension {
	return &StealthExtension{Profile: p, Mode: EvasionExtension}
}

// manifestV3 describes a Chrome Manifest V3 extension.
type manifestV3 struct {
	ManifestVersion int                  `json:"manifest_version"`
	Name            string               `json:"name"`
	Version         string               `json:"version"`
	Description     string               `json:"description"`
	ContentScripts  []contentScriptEntry `json:"content_scripts"`
}

type contentScriptEntry struct {
	Matches   []string `json:"matches"`
	JS        []string `json:"js"`
	RunAt     string   `json:"run_at"`
	AllFrames bool     `json:"all_frames"`
}

// GenerateStealthExtension writes the Chrome Extension files (manifest.json
// and content.js) to the specified directory. The extension runs in extension
// context, not detectable via page DOM.
func (e *StealthExtension) GenerateStealthExtension(dir string) error {
	if dir == "" {
		return fmt.Errorf("stealth extension: directory required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("stealth extension: mkdir: %w", err)
	}

	// Write manifest.json (Manifest V3)
	manifest := manifestV3{
		ManifestVersion: 3,
		Name:            "Omnimus Stealth",
		Version:         "1.0.0",
		Description:     "Stealth patch injection in extension context",
		ContentScripts: []contentScriptEntry{
			{
				Matches:   []string{"<all_urls>"},
				JS:        []string{"content.js"},
				RunAt:     "document_start",
				AllFrames: true,
			},
		},
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("stealth extension: marshal manifest: %w", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		return fmt.Errorf("stealth extension: write manifest: %w", err)
	}

	// Write content.js with the assembled stealth patch script
	script := Script(e.Profile)
	contentPath := filepath.Join(dir, "content.js")
	if err := os.WriteFile(contentPath, []byte(script), 0o644); err != nil {
		return fmt.Errorf("stealth extension: write content: %w", err)
	}

	return nil
}

// ExtensionLoadFlag returns the Chrome launch flag for loading the extension.
// Returns "--load-extension=<dir>" for Option B mode.
func (e *StealthExtension) ExtensionLoadFlag(dir string) string {
	if dir == "" {
		dir = DefaultExtensionDir()
	}
	return fmt.Sprintf("--load-extension=%s", dir)
}

// DefaultExtensionDir returns the default extension directory path.
func DefaultExtensionDir() string {
	return filepath.Join(os.Getenv("HOME"), ".omnimus", "browser", "stealth-ext")
}

// ValidateManifest checks that a manifest.json at the given directory
// is a valid Manifest V3 with content_scripts.
func ValidateManifest(dir string) error {
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("stealth extension: read manifest: %w", err)
	}
	var m manifestV3
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("stealth extension: parse manifest: %w", err)
	}
	if m.ManifestVersion != 3 {
		return fmt.Errorf("stealth extension: expected manifest_version 3, got %d", m.ManifestVersion)
	}
	if len(m.ContentScripts) == 0 {
		return fmt.Errorf("stealth extension: no content_scripts in manifest")
	}
	cs := m.ContentScripts[0]
	if len(cs.Matches) == 0 {
		return fmt.Errorf("stealth extension: no matches in content_scripts")
	}
	if len(cs.JS) == 0 {
		return fmt.Errorf("stealth extension: no JS in content_scripts")
	}
	if cs.RunAt != "document_start" {
		return fmt.Errorf("stealth extension: expected run_at document_start, got %q", cs.RunAt)
	}
	return nil
}
