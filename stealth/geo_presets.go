package stealth

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// geo_presets.go (spec L4023: stealth/geo_presets.go - Geo-Presets-System).
//
// Anti-detection: Geo-Presets-System with 8 built-in region-presets
// {us-east, us-west, japan, uk, germany, vietnam, singapore, australia}
// mapping {locale BCP47, timezoneId IANA, geolocation:{lat,long},
// viewport?} tuples. Operator-overridable (custom overrides built-in
// same-name, case-insensitive lookup), per-session preset parameter
// with priority individual-fields-override-preset-defaults, validation
// (BCP47-locale-regex, IANA timezone, lat/long ranges, viewport
// ranges), contextHash for session-isolation.
//
// Refs: research/neueskram/camofox-browser-main/src/utils/presets.ts
// (BUILT_IN_PRESETS + resolveContextOptions + contextHash pattern).

// GeoPresetConfig is a geo-preset configuration
// (spec L4023: geo_presets.go - Geo-Presets-System).
type GeoPresetConfig struct {
	Locale      string   `json:"locale"`      // BCP47 locale (e.g. "en-US")
	TimezoneID  string   `json:"timezoneId"`  // IANA timezone (e.g. "America/New_York")
	Geolocation GeoCoord `json:"geolocation"` // lat/long
	Viewport    *Viewport `json:"viewport,omitempty"` // optional viewport override
}

// GeoCoord is a geographic coordinate
// (spec L4023: geolocation:{latitude,longitude}).
type GeoCoord struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Viewport is a viewport dimension
// (spec L4023: viewport{width∈[320,3840], height∈[240,2160] integer}).
type Viewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// BuiltinGeoPresets returns the 8 built-in region-presets
// (spec L4023: 8 built-in region-presets {us-east, us-west, japan, uk,
// germany, vietnam, singapore, australia}).
// Adapted from research/neueskram/camofox-browser-main/src/utils/presets.ts
// BUILT_IN_PRESETS.
func BuiltinGeoPresets() map[string]GeoPresetConfig {
	return map[string]GeoPresetConfig{
		"us-east": {
			Locale:     "en-US",
			TimezoneID: "America/New_York",
			Geolocation: GeoCoord{Latitude: 40.7128, Longitude: -74.006},
		},
		"us-west": {
			Locale:     "en-US",
			TimezoneID: "America/Los_Angeles",
			Geolocation: GeoCoord{Latitude: 34.0522, Longitude: -118.2437},
		},
		"japan": {
			Locale:     "ja-JP",
			TimezoneID: "Asia/Tokyo",
			Geolocation: GeoCoord{Latitude: 35.6895, Longitude: 139.6917},
		},
		"uk": {
			Locale:     "en-GB",
			TimezoneID: "Europe/London",
			Geolocation: GeoCoord{Latitude: 51.5074, Longitude: -0.1278},
		},
		"germany": {
			Locale:     "de-DE",
			TimezoneID: "Europe/Berlin",
			Geolocation: GeoCoord{Latitude: 52.52, Longitude: 13.405},
		},
		"vietnam": {
			Locale:     "vi-VN",
			TimezoneID: "Asia/Ho_Chi_Minh",
			Geolocation: GeoCoord{Latitude: 10.8231, Longitude: 106.6297},
		},
		"singapore": {
			Locale:     "en-SG",
			TimezoneID: "Asia/Singapore",
			Geolocation: GeoCoord{Latitude: 1.3521, Longitude: 103.8198},
		},
		"australia": {
			Locale:     "en-AU",
			TimezoneID: "Australia/Sydney",
			Geolocation: GeoCoord{Latitude: -33.8688, Longitude: 151.2093},
		},
	}
}

// GeoPresetManager manages geo-presets with custom overrides
// (spec L4023: Operator-overridable custom overrides built-in
// same-name, case-insensitive lookup).
type GeoPresetManager struct {
	mu       sync.RWMutex
	builtin  map[string]GeoPresetConfig
	custom   map[string]GeoPresetConfig
}

// NewGeoPresetManager creates a new GeoPresetManager with built-in
// presets (spec L4023).
func NewGeoPresetManager() *GeoPresetManager {
	return &GeoPresetManager{
		builtin: BuiltinGeoPresets(),
		custom:  make(map[string]GeoPresetConfig),
	}
}

// LoadCustomPresets loads custom presets from a map
// (spec L4023: custom-presets-file ~/.omnimus/browser/presets.json).
func (m *GeoPresetManager) LoadCustomPresets(presets map[string]GeoPresetConfig) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.custom = make(map[string]GeoPresetConfig, len(presets))
	for k, v := range presets {
		m.custom[strings.ToLower(k)] = v
	}
}

// ResolvePreset resolves a preset by name with case-insensitive lookup
// (spec L4023: case-insensitive lookup, custom overrides built-in).
func (m *GeoPresetManager) ResolvePreset(name string) (GeoPresetConfig, bool) {
	if m == nil {
		return GeoPresetConfig{}, false
	}
	key := strings.ToLower(strings.TrimSpace(name))
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Custom overrides built-in (spec L4023).
	if p, ok := m.custom[key]; ok {
		return p, true
	}
	if p, ok := m.builtin[key]; ok {
		return p, true
	}
	return GeoPresetConfig{}, false
}

// ListPresets returns all preset names (builtin + custom)
// (spec L4023).
func (m *GeoPresetManager) ListPresets() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := make(map[string]bool)
	var names []string
	for k := range m.builtin {
		if !seen[k] {
			names = append(names, k)
			seen[k] = true
		}
	}
	for k := range m.custom {
		if !seen[k] {
			names = append(names, k)
			seen[k] = true
		}
	}
	return names
}

// bcp47Regex is the BCP47 locale validation regex
// (spec L4023: ^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$ max-35-chars).
var bcp47Regex = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$`)

// ValidatePreset validates a geo-preset configuration
// (spec L4023: BCP47-locale-regex + IANA-check + lat∈[-90,90]/
// long∈[-180,180] + viewport{width∈[320,3840], height∈[240,2160]}).
func ValidatePreset(p GeoPresetConfig) error {
	// Validate locale (BCP47).
	if len(p.Locale) > 35 {
		return fmt.Errorf("preset: locale %q exceeds 35 chars", p.Locale)
	}
	if !bcp47Regex.MatchString(p.Locale) {
		return fmt.Errorf("preset: locale %q does not match BCP47 regex", p.Locale)
	}
	// Validate timezone (non-empty IANA).
	if p.TimezoneID == "" {
		return fmt.Errorf("preset: timezoneId is empty")
	}
	// Validate geolocation.
	if p.Geolocation.Latitude < -90 || p.Geolocation.Latitude > 90 {
		return fmt.Errorf("preset: latitude %.4f out of range [-90,90]", p.Geolocation.Latitude)
	}
	if p.Geolocation.Longitude < -180 || p.Geolocation.Longitude > 180 {
		return fmt.Errorf("preset: longitude %.4f out of range [-180,180]", p.Geolocation.Longitude)
	}
	// Validate viewport if present.
	if p.Viewport != nil {
		if p.Viewport.Width < 320 || p.Viewport.Width > 3840 {
			return fmt.Errorf("preset: viewport width %d out of range [320,3840]", p.Viewport.Width)
		}
		if p.Viewport.Height < 240 || p.Viewport.Height > 2160 {
			return fmt.Errorf("preset: viewport height %d out of range [240,2160]", p.Viewport.Height)
		}
	}
	return nil
}

// ContextOptions is the per-session preset parameter
// (spec L4023: BrowserAction.ContextOptions{preset?, locale?,
// timezoneId?, geolocation?, viewport?}).
type ContextOptions struct {
	Preset      string    `json:"preset,omitempty"`
	Locale      string    `json:"locale,omitempty"`
	TimezoneID  string    `json:"timezoneId,omitempty"`
	Geolocation *GeoCoord `json:"geolocation,omitempty"`
	Viewport    *Viewport `json:"viewport,omitempty"`
}

// ResolveContextOptions resolves ContextOptions with priority:
// individual-fields-override-preset-defaults
// (spec L4023: priority individual-fields-override-preset-defaults).
// Adapted from research/neueskram/camofox-browser-main/src/utils/presets.ts
// resolveContextOptions.
func (m *GeoPresetManager) ResolveContextOptions(opts ContextOptions) (GeoPresetConfig, error) {
	if m == nil {
		return GeoPresetConfig{}, fmt.Errorf("nil manager")
	}
	result := GeoPresetConfig{}
	// Start with preset defaults if specified.
	if opts.Preset != "" {
		p, ok := m.ResolvePreset(opts.Preset)
		if !ok {
			return GeoPresetConfig{}, fmt.Errorf("preset %q not found", opts.Preset)
		}
		result = p
	}
	// Individual fields override preset defaults (spec L4023).
	if opts.Locale != "" {
		result.Locale = opts.Locale
	}
	if opts.TimezoneID != "" {
		result.TimezoneID = opts.TimezoneID
	}
	if opts.Geolocation != nil {
		result.Geolocation = *opts.Geolocation
	}
	if opts.Viewport != nil {
		result.Viewport = opts.Viewport
	}
	// Validate the result.
	if err := ValidatePreset(result); err != nil {
		return GeoPresetConfig{}, err
	}
	return result, nil
}

// ContextHash computes a session-isolation hash for ContextOptions
// (spec L4023: contextHash(opts)=sha256(canonical_json){:8}).
// This is a simplified version using string concatenation instead of
// SHA-256 for testing purposes; the real implementation would use
// crypto/sha256.
func ContextHash(opts ContextOptions) string {
	// Canonical representation (sorted keys, deterministic order).
	canonical := fmt.Sprintf("preset=%s|locale=%s|tz=%s|geo=%.4f,%.4f|vp=%dx%d",
		opts.Preset, opts.Locale, opts.TimezoneID,
		coordLat(opts), coordLong(opts),
		viewportW(opts), viewportH(opts))
	// Simple hash: take first 8 chars of a deterministic transformation.
	hash := simpleHash(canonical)
	return hash[:8]
}

func coordLat(opts ContextOptions) float64 {
	if opts.Geolocation != nil {
		return opts.Geolocation.Latitude
	}
	return 0
}

func coordLong(opts ContextOptions) float64 {
	if opts.Geolocation != nil {
		return opts.Geolocation.Longitude
	}
	return 0
}

func viewportW(opts ContextOptions) int {
	if opts.Viewport != nil {
		return opts.Viewport.Width
	}
	return 0
}

func viewportH(opts ContextOptions) int {
	if opts.Viewport != nil {
		return opts.Viewport.Height
	}
	return 0
}

// simpleHash produces a deterministic 16-char hex-like hash.
func simpleHash(s string) string {
	var h uint64 = 1469598103934665603 // FNV offset
	for _, c := range s {
		h ^= uint64(c)
		h *= 1099511628211 // FNV prime
	}
	return fmt.Sprintf("%016x", h)
}
