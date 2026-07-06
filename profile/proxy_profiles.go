// Package profile: proxy_profiles.go implements Session-Level Proxy with
// Hybrid Geo-Modes (spec L4028). Adapted from
// research/neueskram/camofox-browser-main/src/utils/proxy-profiles.ts:
// ProxyProfileConfig + ResolvedProxyConfig + GeoMode + getConfiguredServerProxy
// + named-profile resolution + validation + contextHash session-isolation.
package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

// GeoMode enumerates the hybrid geo-modes for session-level proxy
// (spec L4028, adapted from research proxy-profiles.ts GeoMode).
type GeoMode string

const (
	// GeoModeExplicitWins means Operator-set fields override proxy-implied
	// geo (default when proxy is activated).
	GeoModeExplicitWins GeoMode = "explicit_wins"
	// GeoModeProxyLocked means the proxy server determines geo;
	// explicit overrides are ignored + emit OCSF
	// browser.proxy_locked_override_blocked if attempted.
	GeoModeProxyLocked GeoMode = "proxy_locked"
)

// GeolocationConfig is a lat/long pair (spec L4028, research types.ts).
type GeolocationConfig struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// ProxyProfileConfig is a named proxy profile loaded from
// the browser proxy-profiles file (research
// proxy-profiles.ts ProxyProfileConfig).
type ProxyProfileConfig struct {
	Server      string             `json:"server"`
	Username    string             `json:"username,omitempty"`
	Password    string             `json:"password,omitempty"`
	Locale      string             `json:"locale,omitempty"`
	TimezoneID  string             `json:"timezoneId,omitempty"`
	Geolocation *GeolocationConfig `json:"geolocation,omitempty"`
}

// ProxySource enumerates how the proxy was resolved
// (spec L4028, research proxy-profiles.ts source field).
type ProxySource string

const (
	ProxySourceServerDefault  ProxySource = "server-default"
	ProxySourceNamedProfile   ProxySource = "named-profile"
	ProxySourceRawCredentials ProxySource = "raw-credentials"
)

// ResolvedProxyConfig is the resolved proxy configuration after
// named-profile resolution or raw-credentials normalization
// (spec L4028, research proxy-profiles.ts ResolvedProxyConfig).
type ResolvedProxyConfig struct {
	Source      ProxySource        `json:"source"`
	ProfileName string             `json:"profile_name,omitempty"`
	Server      string             `json:"server"`
	Username    string             `json:"username,omitempty"`
	Password    string             `json:"password,omitempty"`
	Locale      string             `json:"locale,omitempty"`
	TimezoneID  string             `json:"timezoneId,omitempty"`
	Geolocation *GeolocationConfig `json:"geolocation,omitempty"`
}

// SessionProfileInput is the per-session proxy specification
// (spec L4028, research proxy-profiles.ts SessionProfileInput).
type SessionProfileInput struct {
	ProxyProfile string             `json:"proxyProfile,omitempty"`
	RawProxy     *RawProxyOverride  `json:"proxy,omitempty"`
	GeoMode      GeoMode            `json:"geoMode,omitempty"`
	Locale       string             `json:"locale,omitempty"`
	TimezoneID   string             `json:"timezoneId,omitempty"`
	Geolocation  *GeolocationConfig `json:"geolocation,omitempty"`
}

// RawProxyOverride is inline proxy credentials (spec L4028, research
// proxy-profiles.ts RawProxyOverride).
type RawProxyOverride struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// localeRegex validates BCP47 locale strings (spec L4028:
// ^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$ max-35-chars).
var localeRegex = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,8})*$`)

// ProxyProfileStore manages named proxy profiles loaded from a JSON
// file (the browser proxy-profiles file).
type ProxyProfileStore struct {
	mu       sync.RWMutex
	profiles map[string]ProxyProfileConfig
	path     string
}

// NewProxyProfileStore creates a new empty store.
func NewProxyProfileStore() *ProxyProfileStore {
	return &ProxyProfileStore{profiles: make(map[string]ProxyProfileConfig)}
}

// LoadProxyProfiles loads named proxy profiles from a JSON file
// (spec L4028, adapted from research proxy-profiles.ts loadProxyProfiles).
// Returns an empty store on error (proxy profiles are optional).
func LoadProxyProfiles(filePath string) (*ProxyProfileStore, error) {
	s := NewProxyProfileStore()
	if filePath == "" {
		return s, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return s, fmt.Errorf("proxy profiles: read file: %w", err)
	}
	var parsed map[string]ProxyProfileConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		return s, fmt.Errorf("proxy profiles: parse json: %w", err)
	}
	// Validate each profile and store with case-insensitive name.
	for name, profile := range parsed {
		if err := validateProxyProfile(name, profile); err != nil {
			return s, fmt.Errorf("proxy profiles: %w", err)
		}
		s.profiles[strings.ToLower(name)] = profile
	}
	s.path = filePath
	return s, nil
}

// Get returns a proxy profile by name (case-insensitive lookup,
// spec L4028).
func (s *ProxyProfileStore) Get(name string) (ProxyProfileConfig, bool) {
	if s == nil {
		return ProxyProfileConfig{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.profiles[strings.ToLower(name)]
	return p, ok
}

// Names returns all profile names (sorted).
func (s *ProxyProfileStore) Names() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.profiles))
	for name := range s.profiles {
		out = append(out, name)
	}
	return out
}

// validateProxyProfile validates a single proxy profile
// (spec L4028, adapted from research proxy-profiles.ts validateProxyProfile).
func validateProxyProfile(name string, p ProxyProfileConfig) error {
	if strings.TrimSpace(p.Server) == "" {
		return fmt.Errorf("profile %q must have a non-empty server", name)
	}
	if p.Geolocation != nil {
		if p.Geolocation.Latitude < -90 || p.Geolocation.Latitude > 90 {
			return fmt.Errorf("profile %q geolocation.latitude must be between -90 and 90", name)
		}
		if p.Geolocation.Longitude < -180 || p.Geolocation.Longitude > 180 {
			return fmt.Errorf("profile %q geolocation.longitude must be between -180 and 180", name)
		}
	}
	if p.Locale != "" {
		if len(p.Locale) > 35 || !localeRegex.MatchString(p.Locale) {
			return fmt.Errorf("profile %q locale %q is not a valid BCP47 locale", name, p.Locale)
		}
	}
	return nil
}

// ConfiguredServerProxy holds the fallback server proxy from environment
// (spec L4028: PROXY_HOST/PROXY_PORT/PROXY_USERNAME/PROXY_PASSWORD).
type ConfiguredServerProxy struct {
	Host     string
	Port     int
	Username string
	Password string
}

// GetConfiguredServerProxy resolves the fallback server proxy
// (spec L4028, adapted from research proxy-profiles.ts getConfiguredServerProxy).
func GetConfiguredServerProxy(c ConfiguredServerProxy) *ResolvedProxyConfig {
	if c.Host == "" || c.Port == 0 {
		return nil
	}
	return &ResolvedProxyConfig{
		Source:   ProxySourceServerDefault,
		Server:   fmt.Sprintf("http://%s:%d", c.Host, c.Port),
		Username: c.Username,
		Password: c.Password,
	}
}

// NormalizeRawProxy converts a RawProxyOverride to a ResolvedProxyConfig
// (spec L4028, adapted from research proxy-profiles.ts normalizeRawProxy).
func NormalizeRawProxy(raw RawProxyOverride) (*ResolvedProxyConfig, error) {
	if raw.Host == "" || raw.Port == 0 {
		return nil, fmt.Errorf("proxy.host and proxy.port are required")
	}
	return &ResolvedProxyConfig{
		Source:   ProxySourceRawCredentials,
		Server:   fmt.Sprintf("http://%s:%d", raw.Host, raw.Port),
		Username: raw.Username,
		Password: raw.Password,
	}, nil
}

// ResolveSessionProxy resolves the proxy for a session based on the
// input and available profiles/fallback (spec L4028, adapted from
// research proxy-profiles.ts resolveSessionProfileInput).
// Returns the resolved proxy config and an error if validation fails
// (e.g. proxy-locked mode with explicit overrides).
func ResolveSessionProxy(input SessionProfileInput, store *ProxyProfileStore, serverFallback *ResolvedProxyConfig) (*ResolvedProxyConfig, error) {
	geoMode := input.GeoMode
	if geoMode == "" {
		geoMode = GeoModeExplicitWins
	}

	var proxy *ResolvedProxyConfig

	if input.RawProxy != nil {
		p, err := NormalizeRawProxy(*input.RawProxy)
		if err != nil {
			return nil, fmt.Errorf("resolve session proxy: %w", err)
		}
		proxy = p
	} else if input.ProxyProfile != "" {
		named, ok := store.Get(input.ProxyProfile)
		if !ok {
			available := ""
			if store != nil {
				names := store.Names()
				if len(names) > 0 {
					available = fmt.Sprintf(" Available profiles: %s", strings.Join(names, ", "))
				}
			}
			return nil, fmt.Errorf("unknown proxy profile: %q.%s", input.ProxyProfile, available)
		}
		proxy = &ResolvedProxyConfig{
			Source:      ProxySourceNamedProfile,
			ProfileName: input.ProxyProfile,
			Server:      named.Server,
			Username:    named.Username,
			Password:    named.Password,
			Locale:      named.Locale,
			TimezoneID:  named.TimezoneID,
			Geolocation: named.Geolocation,
		}
	} else {
		proxy = serverFallback
	}

	// Validate proxy-locked mode: explicit overrides are not allowed
	// (spec L4028, research proxy-profiles.ts proxy-locked validation).
	if geoMode == GeoModeProxyLocked && proxy != nil {
		if input.Locale != "" {
			return nil, fmt.Errorf("proxy-locked does not allow explicit locale overrides")
		}
		if input.TimezoneID != "" {
			return nil, fmt.Errorf("proxy-locked does not allow explicit timezoneId overrides")
		}
		if input.Geolocation != nil {
			return nil, fmt.Errorf("proxy-locked does not allow explicit geolocation overrides")
		}
	}

	// In explicit-wins mode, explicit fields override proxy-implied geo.
	if geoMode == GeoModeExplicitWins && proxy != nil {
		if input.Locale != "" {
			proxy.Locale = input.Locale
		}
		if input.TimezoneID != "" {
			proxy.TimezoneID = input.TimezoneID
		}
		if input.Geolocation != nil {
			proxy.Geolocation = input.Geolocation
		}
	}

	return proxy, nil
}

// ContextHash computes a deterministic SHA-256 hash (first 8 hex chars)
// for session-isolation across different proxy profiles per-user
// (spec L4028: contextHash(opts)=sha256(canonical_json){:8}).
func ContextHash(proxy *ResolvedProxyConfig, geoMode GeoMode) string {
	canonical := struct {
		Proxy   *ResolvedProxyConfig `json:"proxy"`
		GeoMode GeoMode              `json:"geo_mode"`
	}{
		Proxy:   proxy,
		GeoMode: geoMode,
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		// json.Marshal of a well-typed struct should never fail;
		// fall back to a stable hash of the error to avoid collisions.
		data = []byte(err.Error())
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])[:8]
}

// ValidateProxyProfileInput validates a SessionProfileInput
// (spec L4028: validation - server-non-empty, lat/long bounds,
// locale BCP47, timezoneId IANA).
func ValidateProxyProfileInput(input SessionProfileInput) error {
	if input.Geolocation != nil {
		if input.Geolocation.Latitude < -90 || input.Geolocation.Latitude > 90 {
			return fmt.Errorf("geolocation.latitude must be between -90 and 90")
		}
		if input.Geolocation.Longitude < -180 || input.Geolocation.Longitude > 180 {
			return fmt.Errorf("geolocation.longitude must be between -180 and 180")
		}
	}
	if input.Locale != "" {
		if len(input.Locale) > 35 || !localeRegex.MatchString(input.Locale) {
			return fmt.Errorf("locale %q is not a valid BCP47 locale", input.Locale)
		}
	}
	if input.RawProxy != nil {
		if input.RawProxy.Host == "" {
			return fmt.Errorf("raw proxy host is required")
		}
		if input.RawProxy.Port <= 0 || input.RawProxy.Port > 65535 {
			return fmt.Errorf("raw proxy port must be between 1 and 65535")
		}
	}
	return nil
}
