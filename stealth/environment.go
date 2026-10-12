package stealth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// EnvironmentProfile is the immutable, session-scoped browser environment
// contract. Every value that is exposed to a page is derived from this
// profile, so UA, client hints, locale, timezone and viewport cannot drift
// independently during a session.
type EnvironmentProfile struct {
	SchemaVersion       string       `json:"schema_version"`
	ProfileID           string       `json:"profile_id"`
	SessionIdentity     string       `json:"session_identity"`
	Level               StealthLevel `json:"level"`
	UserAgent           string       `json:"user_agent"`
	ChromeVersion       string       `json:"chrome_version"`
	Platform            string       `json:"platform"`
	PlatformVersion     string       `json:"platform_version,omitempty"`
	Architecture        string       `json:"architecture,omitempty"`
	Locale              string       `json:"locale"`
	Languages           []string     `json:"languages"`
	Timezone            string       `json:"timezone"`
	ViewportWidth       int          `json:"viewport_width"`
	ViewportHeight      int          `json:"viewport_height"`
	DevicePixelRatio    float64      `json:"device_pixel_ratio"`
	HardwareConcurrency int          `json:"hardware_concurrency"`
	DeviceMemoryGB      int          `json:"device_memory_gb,omitempty"`
	WebGLVendor         string       `json:"webgl_vendor,omitempty"`
	WebGLRenderer       string       `json:"webgl_renderer,omitempty"`
	NetworkRTTMillis    int          `json:"network_rtt_ms,omitempty"`
	Measured            bool         `json:"measured"`
	LegalGateSatisfied  bool         `json:"legal_gate_satisfied"`
}

const EnvironmentSchemaVersion = "artemis.environment/v1"

// EnvironmentFacts contains values measured or explicitly supplied by the
// browser/runtime owner. Zero values are not silently replaced for advanced
// spoofing; unsupported measurements simply remain unmodified.
type EnvironmentFacts struct {
	UserAgent           string
	ChromeVersion       string
	Platform            string
	PlatformVersion     string
	Architecture        string
	Locale              string
	Languages           []string
	Timezone            string
	ViewportWidth       int
	ViewportHeight      int
	DevicePixelRatio    float64
	HardwareConcurrency int
	DeviceMemoryGB      int
	WebGLVendor         string
	WebGLRenderer       string
	NetworkRTTMillis    int
	Measured            bool
}

// NewEnvironmentProfile validates and freezes the runtime facts into a
// deterministic session profile. The session identity is never rotated by a
// navigation or route fallback.
func NewEnvironmentProfile(sessionIdentity string, level StealthLevel, facts EnvironmentFacts, legalGateSatisfied bool) (EnvironmentProfile, error) {
	if strings.TrimSpace(sessionIdentity) == "" {
		return EnvironmentProfile{}, errors.New("stealth environment: session identity required")
	}
	if level == "" {
		level = StealthDefault
	}
	facts = normalizeEnvironmentFacts(facts)
	profile := EnvironmentProfile{
		SchemaVersion:       EnvironmentSchemaVersion,
		ProfileID:           stableProfileID(sessionIdentity),
		SessionIdentity:     sessionIdentity,
		Level:               level,
		UserAgent:           facts.UserAgent,
		ChromeVersion:       facts.ChromeVersion,
		Platform:            facts.Platform,
		PlatformVersion:     facts.PlatformVersion,
		Architecture:        facts.Architecture,
		Locale:              facts.Locale,
		Languages:           append([]string(nil), facts.Languages...),
		Timezone:            facts.Timezone,
		ViewportWidth:       facts.ViewportWidth,
		ViewportHeight:      facts.ViewportHeight,
		DevicePixelRatio:    facts.DevicePixelRatio,
		HardwareConcurrency: facts.HardwareConcurrency,
		DeviceMemoryGB:      facts.DeviceMemoryGB,
		WebGLVendor:         facts.WebGLVendor,
		WebGLRenderer:       facts.WebGLRenderer,
		NetworkRTTMillis:    facts.NetworkRTTMillis,
		Measured:            facts.Measured,
		LegalGateSatisfied:  legalGateSatisfied,
	}
	if err := profile.Validate(); err != nil {
		return EnvironmentProfile{}, err
	}
	return profile, nil
}

func normalizeEnvironmentFacts(facts EnvironmentFacts) EnvironmentFacts {
	if facts.ChromeVersion == "" {
		facts.ChromeVersion = ParseChromeVersion(facts.UserAgent)
	}
	if facts.Locale == "" && len(facts.Languages) > 0 {
		facts.Locale = facts.Languages[0]
	}
	if facts.Locale == "" {
		facts.Locale = "en-US"
	}
	if len(facts.Languages) == 0 {
		facts.Languages = []string{facts.Locale}
	}
	if facts.Timezone == "" {
		facts.Timezone = time.Local.String()
		if facts.Timezone == "" || facts.Timezone == "Local" {
			facts.Timezone = "UTC"
		}
	}
	if facts.ViewportWidth == 0 {
		facts.ViewportWidth = 1280
	}
	if facts.ViewportHeight == 0 {
		facts.ViewportHeight = 720
	}
	if facts.DevicePixelRatio == 0 {
		facts.DevicePixelRatio = 1
	}
	if facts.HardwareConcurrency == 0 {
		facts.HardwareConcurrency = runtime.NumCPU()
	}
	return facts
}

func stableProfileID(identity string) string {
	sum := sha256.Sum256([]byte(identity))
	return "stealth_" + hex.EncodeToString(sum[:16])
}

// Validate enforces cross-surface consistency and rejects values that could
// create JavaScript or URL injection when serialized into a CDP script.
func (p EnvironmentProfile) Validate() error {
	if p.SchemaVersion != EnvironmentSchemaVersion {
		return fmt.Errorf("stealth environment: unsupported schema %q", p.SchemaVersion)
	}
	if p.ProfileID == "" || p.SessionIdentity == "" {
		return errors.New("stealth environment: identity required")
	}
	if p.Level != StealthDefault && p.Level != StealthStealth && p.Level != StealthParanoid {
		return fmt.Errorf("stealth environment: invalid level %q", p.Level)
	}
	if p.Level != StealthDefault && !p.LegalGateSatisfied {
		return errors.New("stealth environment: advanced level requires legal gate")
	}
	if p.Level != StealthDefault && !p.Measured {
		return errors.New("stealth environment: advanced level requires measured facts")
	}
	if p.UserAgent == "" || strings.ContainsAny(p.UserAgent, "\r\n") {
		return errors.New("stealth environment: valid user agent required")
	}
	if p.ChromeVersion == "" || ParseChromeVersion(p.UserAgent) != majorVersion(p.ChromeVersion) {
		return fmt.Errorf("stealth environment: UA and Chrome version disagree")
	}
	if p.Platform == "" || strings.ContainsAny(p.Platform, "\r\n'\"") {
		return errors.New("stealth environment: valid platform required")
	}
	if p.Locale == "" || len(p.Languages) == 0 || p.Languages[0] != p.Locale {
		return errors.New("stealth environment: locale and languages disagree")
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return fmt.Errorf("stealth environment: timezone: %w", err)
	}
	if p.ViewportWidth < 320 || p.ViewportWidth > 3840 || p.ViewportHeight < 240 || p.ViewportHeight > 2160 {
		return errors.New("stealth environment: viewport outside supported range")
	}
	if p.DevicePixelRatio <= 0 || p.DevicePixelRatio > 8 {
		return errors.New("stealth environment: device pixel ratio outside supported range")
	}
	if p.HardwareConcurrency < 1 || p.HardwareConcurrency > 256 {
		return errors.New("stealth environment: hardware concurrency outside supported range")
	}
	if p.DeviceMemoryGB < 0 || p.DeviceMemoryGB > 1024 {
		return errors.New("stealth environment: device memory outside supported range")
	}
	if (p.WebGLVendor == "") != (p.WebGLRenderer == "") {
		return errors.New("stealth environment: WebGL vendor and renderer must be paired")
	}
	if p.NetworkRTTMillis < 0 || p.NetworkRTTMillis > 60000 {
		return errors.New("stealth environment: network RTT outside supported range")
	}
	return nil
}

func majorVersion(version string) string {
	version = strings.TrimSpace(version)
	if idx := strings.IndexByte(version, '.'); idx >= 0 {
		return version[:idx]
	}
	return version
}

// AsPatchProfile projects the validated profile into the legacy patch
// generator while preserving the stable identity and measured values.
func (p EnvironmentProfile) AsPatchProfile() Profile {
	return Profile{
		ViewportWidth:       p.ViewportWidth,
		ViewportHeight:      p.ViewportHeight,
		DevicePixelRatio:    p.DevicePixelRatio,
		UserAgent:           p.UserAgent,
		Vendor:              "Google Inc.",
		Platform:            p.Platform,
		Languages:           strings.Join(p.Languages, ","),
		Timezone:            p.Timezone,
		Seed:                p.ProfileID,
		ChromeVersion:       p.ChromeVersion,
		PlatformVersion:     p.PlatformVersion,
		Architecture:        p.Architecture,
		HardwareConcurrency: p.HardwareConcurrency,
		DeviceMemoryGB:      p.DeviceMemoryGB,
		WebGLVendor:         p.WebGLVendor,
		WebGLRenderer:       p.WebGLRenderer,
	}
}

// ScriptHash returns the content hash of the canonical page script.
func (p EnvironmentProfile) ScriptHash() (string, error) {
	script, err := NewDocumentScript(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(script))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ParseChromeMajor returns the major component of a full browser version.
func ParseChromeMajor(version string) (int, error) {
	major := majorVersion(version)
	value, err := strconv.Atoi(major)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("invalid Chrome version %q", version)
	}
	return value, nil
}

// ConsistencyProbe contains values observed from a live page. It is kept
// separate from EnvironmentProfile so a failed probe can disable overrides
// without mutating the session identity.
type ConsistencyProbe struct {
	UserAgent        string
	ClientHintsUA    string
	ClientHintsPlat  string
	Platform         string
	Locale           string
	Timezone         string
	ViewportWidth    int
	ViewportHeight   int
	DevicePixelRatio float64
	WebGLVendor      string
	WebGLRenderer    string
}

// ValidateConsistency compares all supplied live signals to the immutable
// profile. Empty optional signals are not treated as evidence of success.
func (p EnvironmentProfile) ValidateConsistency(probe ConsistencyProbe) error {
	checks := []struct{ name, want, got string }{
		{"user_agent", p.UserAgent, probe.UserAgent},
		{"client_hints_ua", majorVersion(p.ChromeVersion), clientHintsMajor(probe.ClientHintsUA)},
		{"client_hints_platform", platformHint(p.Platform), platformHint(probe.ClientHintsPlat)},
		{"platform", p.Platform, probe.Platform},
		{"locale", p.Locale, probe.Locale},
		{"timezone", p.Timezone, probe.Timezone},
	}
	for _, check := range checks {
		if check.got != "" && !strings.EqualFold(check.want, check.got) {
			return fmt.Errorf("stealth environment: %s mismatch", check.name)
		}
	}
	if probe.ViewportWidth != 0 && probe.ViewportWidth != p.ViewportWidth {
		return errors.New("stealth environment: viewport width mismatch")
	}
	if probe.ViewportHeight != 0 && probe.ViewportHeight != p.ViewportHeight {
		return errors.New("stealth environment: viewport height mismatch")
	}
	if probe.DevicePixelRatio != 0 && probe.DevicePixelRatio != p.DevicePixelRatio {
		return errors.New("stealth environment: device pixel ratio mismatch")
	}
	if (probe.WebGLVendor != "" || probe.WebGLRenderer != "") && (probe.WebGLVendor != p.WebGLVendor || probe.WebGLRenderer != p.WebGLRenderer) {
		return errors.New("stealth environment: WebGL mismatch")
	}
	return nil
}

func clientHintsMajor(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, "."); idx >= 0 {
		value = value[:idx]
	}
	start := -1
	for i, r := range value {
		if r >= '0' && r <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			return value[start:i]
		}
	}
	if start >= 0 {
		return value[start:]
	}
	return value
}

// ClientHintsPlatform maps a navigator.platform value to the client-hints
// platform token (Sec-CH-UA-Platform / userAgentData.platform).
func ClientHintsPlatform(value string) string {
	return platformHint(value)
}

func platformHint(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "macintel", "macos":
		return "macos"
	case "win32", "windows":
		return "windows"
	case "linux x86_64", "linux":
		return "linux"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

// URLSafeIdentity returns a redacted identity suitable for evidence labels.
func (p EnvironmentProfile) URLSafeIdentity() string {
	return url.PathEscape(p.ProfileID)
}
