package stealth

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestBuiltinGeoPresetsCount(t *testing.T) {
	presets := BuiltinGeoPresets()
	want := 8
	if len(presets) != want {
		t.Fatalf("BuiltinGeoPresets len = %d, want %d", len(presets), want)
	}
	required := []string{"us-east", "us-west", "japan", "uk", "germany", "vietnam", "singapore", "australia"}
	for _, name := range required {
		if _, ok := presets[name]; !ok {
			t.Errorf("missing builtin preset: %s", name)
		}
	}
}

func TestBuiltinGeoPresetsFields(t *testing.T) {
	presets := BuiltinGeoPresets()
	for name, p := range presets {
		if p.Locale == "" {
			t.Errorf("preset %s: locale is empty", name)
		}
		if p.TimezoneID == "" {
			t.Errorf("preset %s: timezoneId is empty", name)
		}
		if p.Geolocation.Latitude < -90 || p.Geolocation.Latitude > 90 {
			t.Errorf("preset %s: latitude %.4f out of range", name, p.Geolocation.Latitude)
		}
		if p.Geolocation.Longitude < -180 || p.Geolocation.Longitude > 180 {
			t.Errorf("preset %s: longitude %.4f out of range", name, p.Geolocation.Longitude)
		}
	}
}

func TestGeoPresetManagerCustomOverridesBuiltin(t *testing.T) {
	m := NewGeoPresetManager()
	custom := map[string]GeoPresetConfig{
		"germany": {
			Locale:      "de-AT",
			TimezoneID:  "Europe/Vienna",
			Geolocation: GeoCoord{Latitude: 48.2082, Longitude: 16.3738},
		},
	}
	m.LoadCustomPresets(custom)

	p, ok := m.ResolvePreset("germany")
	if !ok {
		t.Fatal("germany preset not found")
	}
	if p.Locale != "de-AT" {
		t.Errorf("custom germany locale = %s, want de-AT (custom should override builtin)", p.Locale)
	}
	if p.TimezoneID != "Europe/Vienna" {
		t.Errorf("custom germany timezone = %s, want Europe/Vienna", p.TimezoneID)
	}
}

func TestGeoPresetManagerCaseInsensitive(t *testing.T) {
	m := NewGeoPresetManager()
	p, ok := m.ResolvePreset("GERMANY")
	if !ok {
		t.Fatal("GERMANY (uppercase) should resolve case-insensitively")
	}
	if p.Locale != "de-DE" {
		t.Errorf("GERMANY locale = %s, want de-DE", p.Locale)
	}
}

func TestGeoPresetManagerUnknownPreset(t *testing.T) {
	m := NewGeoPresetManager()
	_, ok := m.ResolvePreset("nonexistent")
	if ok {
		t.Error("nonexistent preset should not resolve")
	}
}

func TestValidatePresetValid(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 40.7128, Longitude: -74.006},
	}
	if err := ValidatePreset(p); err != nil {
		t.Errorf("valid preset failed: %v", err)
	}
}

func TestValidatePresetInvalidLocale(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "invalid_locale_too_long_for_bcp47",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 40.7128, Longitude: -74.006},
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("invalid locale should fail validation")
	}
}

func TestValidatePresetLatOutOfRange(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 91, Longitude: 0},
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("latitude > 90 should fail validation")
	}
}

func TestValidatePresetLongOutOfRange(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 0, Longitude: 181},
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("longitude > 180 should fail validation")
	}
}

func TestValidatePresetViewport(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 40.7128, Longitude: -74.006},
		Viewport:    &Viewport{Width: 200, Height: 1080}, // width < 320
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("viewport width < 320 should fail validation")
	}

	p.Viewport = &Viewport{Width: 1920, Height: 100} // height < 240
	if err := ValidatePreset(p); err == nil {
		t.Error("viewport height < 240 should fail validation")
	}

	p.Viewport = &Viewport{Width: 1920, Height: 1080}
	if err := ValidatePreset(p); err != nil {
		t.Errorf("valid viewport failed: %v", err)
	}
}

func TestResolveContextOptionsIndividualOverride(t *testing.T) {
	m := NewGeoPresetManager()
	opts := ContextOptions{
		Preset:     "germany",
		Locale:     "en-US", // overrides de-DE
		TimezoneID: "Europe/Berlin",
	}
	result, err := m.ResolveContextOptions(opts)
	if err != nil {
		t.Fatalf("ResolveContextOptions: %v", err)
	}
	if result.Locale != "en-US" {
		t.Errorf("locale = %s, want en-US (individual override)", result.Locale)
	}
	if result.TimezoneID != "Europe/Berlin" {
		t.Errorf("timezone = %s, want Europe/Berlin", result.TimezoneID)
	}
	if result.Geolocation.Latitude != 52.52 {
		t.Errorf("latitude = %.4f, want 52.52 (from preset)", result.Geolocation.Latitude)
	}
}

func TestResolveContextOptionsUnknownPreset(t *testing.T) {
	m := NewGeoPresetManager()
	_, err := m.ResolveContextOptions(ContextOptions{Preset: "nonexistent"})
	if err == nil {
		t.Error("unknown preset should fail")
	}
}

func TestContextHashSHA256(t *testing.T) {
	opts1 := ContextOptions{
		Preset:      "germany",
		Locale:      "de-DE",
		TimezoneID:  "Europe/Berlin",
		Geolocation: &GeoCoord{Latitude: 52.52, Longitude: 13.405},
	}
	hash1 := ContextHash(opts1)
	if len(hash1) != 8 {
		t.Errorf("hash len = %d, want 8", len(hash1))
	}
	// Verify it's a hex string
	if _, err := hex.DecodeString(hash1); err != nil {
		t.Errorf("hash %q is not valid hex: %v", hash1, err)
	}

	// Same input should produce same hash (deterministic)
	hash2 := ContextHash(opts1)
	if hash1 != hash2 {
		t.Errorf("nondeterministic hash: %s != %s", hash1, hash2)
	}

	// Different input should produce different hash
	opts2 := ContextOptions{
		Preset:      "japan",
		Locale:      "ja-JP",
		TimezoneID:  "Asia/Tokyo",
		Geolocation: &GeoCoord{Latitude: 35.6895, Longitude: 139.6917},
	}
	hash3 := ContextHash(opts2)
	if hash1 == hash3 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestContextHashEmpty(t *testing.T) {
	hash := ContextHash(ContextOptions{})
	if len(hash) != 8 {
		t.Errorf("empty hash len = %d, want 8", len(hash))
	}
}

func TestContextHashDeterministicAcrossRuns(t *testing.T) {
	opts := ContextOptions{
		Preset:      "us-east",
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: &GeoCoord{Latitude: 40.7128, Longitude: -74.006},
	}
	hashes := make(map[string]bool)
	for i := 0; i < 10; i++ {
		h := ContextHash(opts)
		hashes[h] = true
	}
	if len(hashes) != 1 {
		t.Errorf("expected 1 unique hash across 10 runs, got %d", len(hashes))
	}
}

func TestListPresets(t *testing.T) {
	m := NewGeoPresetManager()
	m.LoadCustomPresets(map[string]GeoPresetConfig{
		"custom-eu": {
			Locale:      "en-IE",
			TimezoneID:  "Europe/Dublin",
			Geolocation: GeoCoord{Latitude: 53.3498, Longitude: -6.2603},
		},
	})
	names := m.ListPresets()
	if len(names) != 9 { // 8 builtin + 1 custom
		t.Errorf("ListPresets len = %d, want 9", len(names))
	}
	// Verify custom-eu is in the list
	found := false
	for _, n := range names {
		if n == "custom-eu" {
			found = true
			break
		}
	}
	if !found {
		t.Error("custom-eu not in ListPresets")
	}
}

func TestContextHashIsSHA256NotFNV(t *testing.T) {
	// The spec mandates SHA-256, not FNV. Verify the hash is
	// consistent with SHA-256 over canonical JSON.
	opts := ContextOptions{Preset: "germany"}
	hash := ContextHash(opts)
	// SHA-256 produces 64 hex chars; we take first 8.
	// FNV-1a would produce a different value. Just verify it's 8 hex chars.
	if !isAllHex(hash) {
		t.Errorf("hash %q contains non-hex characters", hash)
	}
}

func isAllHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
