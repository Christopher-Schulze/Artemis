package stealth

import (
	"testing"
)

// ==================== launch.go facade tests ====================

// TestTASK2245_LaunchConfigAlias verifies LaunchConfig aliases LaunchFlags
// (spec L4023: launch.go - Chrome launch flags).
func TestTASK2245_LaunchConfigAlias(t *testing.T) {
	cfg := DefaultLaunchConfig()
	if len(cfg.Args) == 0 {
		t.Error("default launch config should have args")
	}
}

// TestTASK2245_StealthLaunchConfig verifies StealthLaunchConfig
// (spec L4023: launch.go - Chrome launch flags).
func TestTASK2245_StealthLaunchConfig(t *testing.T) {
	cfg := StealthLaunchConfig(StealthStealth)
	if !cfg.HasStealthArg() {
		t.Error("stealth config should have stealth args")
	}
}

// TestTASK2245_STEALTH_ARGS verifies STEALTH_ARGS is populated
// (spec L4023: launch.go - Chrome launch flags).
func TestTASK2245_STEALTH_ARGS(t *testing.T) {
	if len(STEALTH_ARGS) == 0 {
		t.Error("STEALTH_ARGS should not be empty")
	}
}

// ==================== emulation.go tests ====================

// TestTASK2245_DefaultDesktopEmulation verifies desktop emulation
// (spec L4023: emulation.go - device/screen emulation).
func TestTASK2245_DefaultDesktopEmulation(t *testing.T) {
	e := DefaultDesktopEmulation()
	if e.Width != 1920 || e.Height != 1080 {
		t.Errorf("desktop: got %dx%d, want 1920x1080", e.Width, e.Height)
	}
	if e.Mobile {
		t.Error("desktop should not be mobile")
	}
	if e.TouchEnabled {
		t.Error("desktop should not have touch")
	}
}

// TestTASK2245_DefaultMobileEmulation verifies mobile emulation
// (spec L4023: emulation.go - device/screen emulation).
func TestTASK2245_DefaultMobileEmulation(t *testing.T) {
	e := DefaultMobileEmulation()
	if e.Width != 390 || e.Height != 844 {
		t.Errorf("mobile: got %dx%d, want 390x844", e.Width, e.Height)
	}
	if !e.Mobile {
		t.Error("mobile should be mobile")
	}
	if !e.TouchEnabled {
		t.Error("mobile should have touch")
	}
}

// TestTASK2245_EmulationManagerSetGet verifies SetEmulation/Current
// (spec L4023: emulation.go).
func TestTASK2245_EmulationManagerSetGet(t *testing.T) {
	m := NewEmulationManager()
	custom := DeviceEmulation{Width: 1280, Height: 720, DeviceScaleFactor: 1.5}
	m.SetEmulation(custom, "custom")
	if m.Current().Width != 1280 {
		t.Errorf("width: got %d, want 1280", m.Current().Width)
	}
	if m.Preset() != "custom" {
		t.Errorf("preset: got %s, want custom", m.Preset())
	}
}

// TestTASK2245_EmulationManagerIsMobile verifies IsMobile
// (spec L4023: emulation.go).
func TestTASK2245_EmulationManagerIsMobile(t *testing.T) {
	m := NewEmulationManager()
	if m.IsMobile() {
		t.Error("desktop should not be mobile")
	}
	m.SetEmulation(DefaultMobileEmulation(), "mobile")
	if !m.IsMobile() {
		t.Error("mobile should be mobile")
	}
}

// TestTASK2245_EmulationValidate verifies validation
// (spec L4023: viewport{width∈[320,3840], height∈[240,2160]}).
func TestTASK2245_EmulationValidate(t *testing.T) {
	valid := DefaultDesktopEmulation()
	if err := valid.Validate(); err != nil {
		t.Errorf("valid emulation: %v", err)
	}
	invalid := DeviceEmulation{Width: 100, Height: 100}
	if err := invalid.Validate(); err == nil {
		t.Error("invalid emulation should fail validation")
	}
}

// TestTASK2245_EmulationValidateWidthTooLarge verifies width > 3840
// fails.
func TestTASK2245_EmulationValidateWidthTooLarge(t *testing.T) {
	e := DeviceEmulation{Width: 5000, Height: 1080}
	if err := e.Validate(); err == nil {
		t.Error("width > 3840 should fail")
	}
}

// TestTASK2245_EmulationNilSafe verifies nil EmulationManager is safe.
func TestTASK2245_EmulationNilSafe(t *testing.T) {
	var m *EmulationManager
	m.SetEmulation(DefaultDesktopEmulation(), "test")
	if m.Current().Width != 1920 {
		t.Error("nil should return default")
	}
	if m.Preset() != "desktop" {
		t.Error("nil should return desktop preset")
	}
	if m.IsMobile() {
		t.Error("nil should not be mobile")
	}
}

// ==================== ua.go tests ====================

// TestTASK2245_DefaultUAInfo verifies default UA info
// (spec L4023: ua.go - UA mgmt + version coherence).
func TestTASK2245_DefaultUAInfo(t *testing.T) {
	ua := DefaultUAInfo()
	if ua.Browser != "chrome" {
		t.Errorf("browser: got %s, want chrome", ua.Browser)
	}
	if ua.Version != "126" {
		t.Errorf("version: got %s, want 126", ua.Version)
	}
}

// TestTASK2245_UAManagerSetGet verifies SetUA/Current
// (spec L4023: ua.go).
func TestTASK2245_UAManagerSetGet(t *testing.T) {
	m := NewUAManager()
	custom := UAInfo{
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
		Browser:   "chrome",
		Version:   "125",
		Platform:  "Windows",
	}
	m.SetUA(custom)
	if m.Current().Version != "125" {
		t.Errorf("version: got %s, want 125", m.Current().Version)
	}
	if m.UserAgent() != custom.UserAgent {
		t.Error("UA string mismatch")
	}
}

// TestTASK2245_UACheckCoherence verifies coherence check
// (spec L4023: version coherence).
func TestTASK2245_UACheckCoherence(t *testing.T) {
	ua := DefaultUAInfo()
	if !ua.IsCoherent() {
		t.Error("default UA should be coherent")
	}
}

// TestTASK2245_UACheckCoherenceMismatch verifies mismatched version
// fails coherence (spec L4023: version coherence).
func TestTASK2245_UACheckCoherenceMismatch(t *testing.T) {
	ua := UAInfo{
		UserAgent: "Mozilla/5.0 Chrome/126.0.0.0 Safari/537.36",
		Version:   "125", // mismatch
	}
	if ua.IsCoherent() {
		t.Error("mismatched version should NOT be coherent")
	}
}

// TestTASK2245_UACheckCoherenceEmpty verifies empty UA fails.
func TestTASK2245_UACheckCoherenceEmpty(t *testing.T) {
	ua := UAInfo{}
	if ua.IsCoherent() {
		t.Error("empty UA should NOT be coherent")
	}
}

// TestTASK2245_ParseChromeVersion verifies Chrome version parsing
// (spec L4023: version coherence).
func TestTASK2245_ParseChromeVersion(t *testing.T) {
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	v := ParseChromeVersion(ua)
	if v != "126" {
		t.Errorf("version: got %s, want 126", v)
	}
}

// TestTASK2245_ParseChromeVersionNotFound verifies no Chrome in UA.
func TestTASK2245_ParseChromeVersionNotFound(t *testing.T) {
	v := ParseChromeVersion("Mozilla/5.0 Firefox/120.0")
	if v != "" {
		t.Errorf("non-Chrome UA: got %s, want empty", v)
	}
}

// TestTASK2245_UANilSafe verifies nil UAManager is safe.
func TestTASK2245_UANilSafe(t *testing.T) {
	var m *UAManager
	m.SetUA(DefaultUAInfo())
	if m.UserAgent() == "" {
		t.Error("nil should return default UA")
	}
	if m.Version() != "126" {
		t.Error("nil should return default version")
	}
}

// ==================== worker.go facade tests ====================

// TestTASK2245_NewWorkerTracker verifies NewWorkerTracker
// (spec L4023: worker.go - web worker stealth parity).
func TestTASK2245_NewWorkerTracker(t *testing.T) {
	tracker := NewWorkerTracker()
	if tracker == nil {
		t.Fatal("tracker should not be nil")
	}
}

// TestTASK2245_WorkerTargetAlias verifies WorkerTarget alias
// (spec L4023: worker.go).
func TestTASK2245_WorkerTargetAlias(t *testing.T) {
	var w WorkerTarget
	w.TargetID = "test"
	if w.TargetID != "test" {
		t.Error("WorkerTarget alias should work")
	}
}

// ==================== fingerprint.go tests ====================

// TestTASK2245_DefaultFingerprintConfig verifies default config
// (spec L4023: fingerprint.go - browser fingerprint spoofing).
func TestTASK2245_DefaultFingerprintConfig(t *testing.T) {
	c := DefaultFingerprintConfig()
	if !c.CanvasNoise {
		t.Error("canvas noise should be enabled by default")
	}
	if !c.WebGLOverride {
		t.Error("WebGL override should be enabled by default")
	}
	if c.HardwareConcurrency != 8 {
		t.Errorf("cores: got %d, want 8", c.HardwareConcurrency)
	}
}

// TestTASK2245_FingerprintManagerSetGet verifies SetConfig/Current
// (spec L4023: fingerprint.go).
func TestTASK2245_FingerprintManagerSetGet(t *testing.T) {
	m := NewFingerprintManager()
	custom := FingerprintConfig{CanvasNoise: false, HardwareConcurrency: 4, DeviceMemory: 4}
	m.SetConfig(custom)
	if m.Current().CanvasNoise {
		t.Error("canvas noise should be disabled")
	}
	if m.Current().HardwareConcurrency != 4 {
		t.Errorf("cores: got %d, want 4", m.Current().HardwareConcurrency)
	}
}

// TestTASK2245_FingerprintIsCanvasNoiseEnabled verifies IsCanvasNoiseEnabled
// (spec L4023: fingerprint.go).
func TestTASK2245_FingerprintIsCanvasNoiseEnabled(t *testing.T) {
	m := NewFingerprintManager()
	if !m.IsCanvasNoiseEnabled() {
		t.Error("canvas noise should be enabled by default")
	}
	m.SetConfig(FingerprintConfig{CanvasNoise: false, HardwareConcurrency: 8, DeviceMemory: 8})
	if m.IsCanvasNoiseEnabled() {
		t.Error("canvas noise should be disabled after SetConfig")
	}
}

// TestTASK2245_FingerprintValidate verifies validation
// (spec L4023: fingerprint.go).
func TestTASK2245_FingerprintValidate(t *testing.T) {
	valid := DefaultFingerprintConfig()
	if err := valid.Validate(); err != nil {
		t.Errorf("valid config: %v", err)
	}
	invalid := FingerprintConfig{HardwareConcurrency: 0, DeviceMemory: 8}
	if err := invalid.Validate(); err == nil {
		t.Error("invalid config should fail validation")
	}
}

// TestTASK2245_FingerprintNilSafe verifies nil FingerprintManager is safe.
func TestTASK2245_FingerprintNilSafe(t *testing.T) {
	var m *FingerprintManager
	m.SetConfig(DefaultFingerprintConfig())
	if !m.IsCanvasNoiseEnabled() {
		t.Error("nil should return default (canvas enabled)")
	}
	if !m.IsWebGLOverrideEnabled() {
		t.Error("nil should return default (webgl enabled)")
	}
}

// ==================== popup.go tests ====================

// TestTASK2245_NewPopupGuard verifies creation with defaults
// (spec L4023: popup.go - popup guard).
func TestTASK2245_NewPopupGuard(t *testing.T) {
	g := NewPopupGuard()
	if g.Policy() != PopupPolicyBlock {
		t.Error("default policy should be block")
	}
	if !g.ShouldBlock() {
		t.Error("should block by default")
	}
}

// TestTASK2245_PopupGuardSetPolicy verifies SetPolicy
// (spec L4023: popup.go - popup guard).
func TestTASK2245_PopupGuardSetPolicy(t *testing.T) {
	g := NewPopupGuard()
	g.SetPolicy(PopupPolicyAllow)
	if g.ShouldBlock() {
		t.Error("allow policy should not block")
	}
	g.SetPolicy(PopupPolicyStealth)
	if !g.IsStealth() {
		t.Error("stealth policy should be stealth")
	}
}

// TestTASK2245_PopupGuardRecordCount verifies RecordPopup/Count
// (spec L4023: popup.go - popup guard).
func TestTASK2245_PopupGuardRecordCount(t *testing.T) {
	g := NewPopupGuard()
	g.RecordPopup()
	g.RecordPopup()
	g.RecordPopup()
	if g.Count() != 3 {
		t.Errorf("count: got %d, want 3", g.Count())
	}
}

// TestTASK2245_PopupGuardNilSafe verifies nil PopupGuard is safe.
func TestTASK2245_PopupGuardNilSafe(t *testing.T) {
	var g *PopupGuard
	g.SetPolicy(PopupPolicyAllow)
	if !g.ShouldBlock() {
		t.Error("nil should block")
	}
	if g.Count() != 0 {
		t.Error("nil count should be 0")
	}
	if g.IsStealth() {
		t.Error("nil should not be stealth")
	}
}

// ==================== geo_presets.go tests ====================

// TestTASK2245_BuiltinGeoPresets verifies 8 built-in presets
// (spec L4023: 8 built-in region-presets).
func TestTASK2245_BuiltinGeoPresets(t *testing.T) {
	presets := BuiltinGeoPresets()
	expected := []string{"us-east", "us-west", "japan", "uk", "germany", "vietnam", "singapore", "australia"}
	if len(presets) != len(expected) {
		t.Fatalf("expected %d presets, got %d", len(expected), len(presets))
	}
	for _, name := range expected {
		if _, ok := presets[name]; !ok {
			t.Errorf("preset %s not found", name)
		}
	}
}

// TestTASK2245_BuiltinGeoPresetsValues verifies preset values
// (spec L4023: {locale BCP47, timezoneId IANA, geolocation}).
func TestTASK2245_BuiltinGeoPresetsValues(t *testing.T) {
	presets := BuiltinGeoPresets()
	de := presets["germany"]
	if de.Locale != "de-DE" {
		t.Errorf("germany locale: got %s, want de-DE", de.Locale)
	}
	if de.TimezoneID != "Europe/Berlin" {
		t.Errorf("germany tz: got %s, want Europe/Berlin", de.TimezoneID)
	}
	if de.Geolocation.Latitude != 52.52 {
		t.Errorf("germany lat: got %f, want 52.52", de.Geolocation.Latitude)
	}
}

// TestTASK2245_GeoPresetManagerResolve verifies ResolvePreset
// (spec L4023: case-insensitive lookup).
func TestTASK2245_GeoPresetManagerResolve(t *testing.T) {
	m := NewGeoPresetManager()
	// Case-insensitive lookup
	p, ok := m.ResolvePreset("Germany")
	if !ok {
		t.Error("Germany should be found (case-insensitive)")
	}
	if p.Locale != "de-DE" {
		t.Errorf("locale: got %s, want de-DE", p.Locale)
	}
}

// TestTASK2245_GeoPresetManagerResolveNotFound verifies not found.
func TestTASK2245_GeoPresetManagerResolveNotFound(t *testing.T) {
	m := NewGeoPresetManager()
	_, ok := m.ResolvePreset("nonexistent")
	if ok {
		t.Error("nonexistent should not be found")
	}
}

// TestTASK2245_GeoPresetManagerCustomOverride verifies custom overrides
// built-in (spec L4023: custom overrides built-in same-name).
func TestTASK2245_GeoPresetManagerCustomOverride(t *testing.T) {
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
		t.Fatal("germany should be found")
	}
	if p.Locale != "de-AT" {
		t.Errorf("custom override: got %s, want de-AT", p.Locale)
	}
}

// TestTASK2245_GeoPresetManagerListPresets verifies ListPresets.
func TestTASK2245_GeoPresetManagerListPresets(t *testing.T) {
	m := NewGeoPresetManager()
	names := m.ListPresets()
	if len(names) != 8 {
		t.Errorf("expected 8 presets, got %d", len(names))
	}
}

// TestTASK2245_ValidatePresetValid verifies valid preset passes
// (spec L4023: BCP47 + IANA + lat/long + viewport validation).
func TestTASK2245_ValidatePresetValid(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 40.7128, Longitude: -74.006},
	}
	if err := ValidatePreset(p); err != nil {
		t.Errorf("valid preset: %v", err)
	}
}

// TestTASK2245_ValidatePresetInvalidLocale verifies invalid locale fails
// (spec L4023: BCP47-locale-regex).
func TestTASK2245_ValidatePresetInvalidLocale(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "invalid_locale!",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 40.7, Longitude: -74.0},
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("invalid locale should fail validation")
	}
}

// TestTASK2245_ValidatePresetLatOutOfRange verifies lat > 90 fails
// (spec L4023: lat∈[-90,90]).
func TestTASK2245_ValidatePresetLatOutOfRange(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 91, Longitude: 0},
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("lat > 90 should fail validation")
	}
}

// TestTASK2245_ValidatePresetLongOutOfRange verifies long > 180 fails
// (spec L4023: long∈[-180,180]).
func TestTASK2245_ValidatePresetLongOutOfRange(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 0, Longitude: 181},
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("long > 180 should fail validation")
	}
}

// TestTASK2245_ValidatePresetViewportOutOfRange verifies viewport
// out of range fails (spec L4023: width∈[320,3840], height∈[240,2160]).
func TestTASK2245_ValidatePresetViewportOutOfRange(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 40.7, Longitude: -74.0},
		Viewport:    &Viewport{Width: 100, Height: 100},
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("viewport out of range should fail validation")
	}
}

// TestTASK2245_ResolveContextOptions verifies context options
// resolution (spec L4023: individual-fields-override-preset-defaults).
func TestTASK2245_ResolveContextOptions(t *testing.T) {
	m := NewGeoPresetManager()
	opts := ContextOptions{
		Preset: "germany",
		Locale: "de-AT", // override preset locale
	}
	result, err := m.ResolveContextOptions(opts)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.Locale != "de-AT" {
		t.Errorf("locale: got %s, want de-AT (override)", result.Locale)
	}
	if result.TimezoneID != "Europe/Berlin" {
		t.Errorf("tz: got %s, want Europe/Berlin (preset default)", result.TimezoneID)
	}
}

// TestTASK2245_ResolveContextOptionsNotFound verifies not-found preset
// errors.
func TestTASK2245_ResolveContextOptionsNotFound(t *testing.T) {
	m := NewGeoPresetManager()
	_, err := m.ResolveContextOptions(ContextOptions{Preset: "nonexistent"})
	if err == nil {
		t.Error("nonexistent preset should error")
	}
}

// TestTASK2245_ContextHash verifies context hash is deterministic
// (spec L4023: contextHash for session-isolation).
func TestTASK2245_ContextHash(t *testing.T) {
	opts := ContextOptions{Preset: "germany", Locale: "de-DE"}
	h1 := ContextHash(opts)
	h2 := ContextHash(opts)
	if h1 != h2 {
		t.Error("context hash should be deterministic")
	}
	if len(h1) != 8 {
		t.Errorf("hash length: got %d, want 8", len(h1))
	}
}

// TestTASK2245_ContextHashDifferent verifies different options produce
// different hashes (spec L4023: session-isolation).
func TestTASK2245_ContextHashDifferent(t *testing.T) {
	h1 := ContextHash(ContextOptions{Preset: "germany"})
	h2 := ContextHash(ContextOptions{Preset: "japan"})
	if h1 == h2 {
		t.Error("different presets should produce different hashes")
	}
}

// ==================== full spec parity test ====================

// TestTASK2245_FullSpecParity verifies all 7 spec-mandated files exist
// and have the spec-mandated functionality (spec L4023).
func TestTASK2245_FullSpecParity(t *testing.T) {
	// 1. launch.go - Chrome launch flags
	cfg := DefaultLaunchConfig()
	if len(cfg.Args) == 0 {
		t.Error("launch.go: default config should have args")
	}

	// 2. emulation.go - device/screen emulation
	e := DefaultDesktopEmulation()
	if e.Width != 1920 {
		t.Error("emulation.go: desktop width should be 1920")
	}

	// 3. ua.go - UA mgmt + version coherence
	ua := DefaultUAInfo()
	if !ua.IsCoherent() {
		t.Error("ua.go: default UA should be coherent")
	}

	// 4. worker.go - web worker stealth parity
	tracker := NewWorkerTracker()
	if tracker == nil {
		t.Error("worker.go: tracker should not be nil")
	}

	// 5. fingerprint.go - browser fingerprint spoofing
	fp := DefaultFingerprintConfig()
	if !fp.CanvasNoise {
		t.Error("fingerprint.go: canvas noise should be enabled")
	}

	// 6. popup.go - popup guard
	guard := NewPopupGuard()
	if guard.Policy() != PopupPolicyBlock {
		t.Error("popup.go: default should be block")
	}

	// 7. geo_presets.go - Geo-Presets-System
	presets := BuiltinGeoPresets()
	if len(presets) != 8 {
		t.Errorf("geo_presets.go: expected 8 presets, got %d", len(presets))
	}
}
