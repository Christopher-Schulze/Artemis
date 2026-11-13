package stealth

import (
	"testing"
)

// ==================== launch.go facade tests ====================

// TestLaunchConfigAlias verifies LaunchConfig aliases LaunchFlags
// (launch.go - Chrome launch flags).
func TestLaunchConfigAlias(t *testing.T) {
	cfg := DefaultLaunchConfig()
	if len(cfg.Args) == 0 {
		t.Error("default launch config should have args")
	}
}

// TestStealthLaunchConfig verifies StealthLaunchConfig
// (launch.go - Chrome launch flags).
func TestStealthLaunchConfig(t *testing.T) {
	cfg := StealthLaunchConfig(StealthStealth)
	if !cfg.HasStealthArg() {
		t.Error("stealth config should have stealth args")
	}
}

// TestSTEALTH_ARGS verifies STEALTH_ARGS is populated
// (launch.go - Chrome launch flags).
func TestSTEALTH_ARGS(t *testing.T) {
	if len(STEALTH_ARGS) == 0 {
		t.Error("STEALTH_ARGS should not be empty")
	}
}

// ==================== emulation.go tests ====================

// TestDefaultDesktopEmulation verifies desktop emulation
// (emulation.go - device/screen emulation).
func TestDefaultDesktopEmulation(t *testing.T) {
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

// TestDefaultMobileEmulation verifies mobile emulation
// (emulation.go - device/screen emulation).
func TestDefaultMobileEmulation(t *testing.T) {
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

// TestEmulationManagerSetGet verifies SetEmulation/Current
// (emulation.go).
func TestEmulationManagerSetGet(t *testing.T) {
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

// TestEmulationManagerIsMobile verifies IsMobile
// (emulation.go).
func TestEmulationManagerIsMobile(t *testing.T) {
	m := NewEmulationManager()
	if m.IsMobile() {
		t.Error("desktop should not be mobile")
	}
	m.SetEmulation(DefaultMobileEmulation(), "mobile")
	if !m.IsMobile() {
		t.Error("mobile should be mobile")
	}
}

// TestEmulationValidate verifies validation
// (viewport{width∈[320,3840], height∈[240,2160]}).
func TestEmulationValidate(t *testing.T) {
	valid := DefaultDesktopEmulation()
	if err := valid.Validate(); err != nil {
		t.Errorf("valid emulation: %v", err)
	}
	invalid := DeviceEmulation{Width: 100, Height: 100}
	if err := invalid.Validate(); err == nil {
		t.Error("invalid emulation should fail validation")
	}
}

// TestEmulationValidateWidthTooLarge verifies width > 3840
// fails.
func TestEmulationValidateWidthTooLarge(t *testing.T) {
	e := DeviceEmulation{Width: 5000, Height: 1080}
	if err := e.Validate(); err == nil {
		t.Error("width > 3840 should fail")
	}
}

// TestEmulationNilSafe verifies nil EmulationManager is safe.
func TestEmulationNilSafe(t *testing.T) {
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

// TestDefaultUAInfo verifies default UA info
// (ua.go - UA mgmt + version coherence).
func TestDefaultUAInfo(t *testing.T) {
	ua := DefaultUAInfo()
	if ua.Browser != "chrome" {
		t.Errorf("browser: got %s, want chrome", ua.Browser)
	}
	if ua.Version != "126" {
		t.Errorf("version: got %s, want 126", ua.Version)
	}
}

// TestUAManagerSetGet verifies SetUA/Current
// (ua.go).
func TestUAManagerSetGet(t *testing.T) {
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

// TestUACheckCoherence verifies coherence check
// (version coherence).
func TestUACheckCoherence(t *testing.T) {
	ua := DefaultUAInfo()
	if !ua.IsCoherent() {
		t.Error("default UA should be coherent")
	}
}

// TestUACheckCoherenceMismatch verifies mismatched version
// fails coherence (version coherence).
func TestUACheckCoherenceMismatch(t *testing.T) {
	ua := UAInfo{
		UserAgent: "Mozilla/5.0 Chrome/126.0.0.0 Safari/537.36",
		Version:   "125", // mismatch
	}
	if ua.IsCoherent() {
		t.Error("mismatched version should NOT be coherent")
	}
}

// TestUACheckCoherenceEmpty verifies empty UA fails.
func TestUACheckCoherenceEmpty(t *testing.T) {
	ua := UAInfo{}
	if ua.IsCoherent() {
		t.Error("empty UA should NOT be coherent")
	}
}

// TestParseChromeVersion verifies Chrome version parsing
// (version coherence).
func TestParseChromeVersion(t *testing.T) {
	ua := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	v := ParseChromeVersion(ua)
	if v != "126" {
		t.Errorf("version: got %s, want 126", v)
	}
}

// TestParseChromeVersionNotFound verifies no Chrome in UA.
func TestParseChromeVersionNotFound(t *testing.T) {
	v := ParseChromeVersion("Mozilla/5.0 Firefox/120.0")
	if v != "" {
		t.Errorf("non-Chrome UA: got %s, want empty", v)
	}
}

// TestUANilSafe verifies nil UAManager is safe.
func TestUANilSafe(t *testing.T) {
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

// TestNewWorkerTracker verifies NewWorkerTracker
// (worker.go - web worker stealth parity).
func TestNewWorkerTracker(t *testing.T) {
	tracker := NewWorkerTracker()
	if tracker == nil {
		t.Fatal("tracker should not be nil")
	}
}

// TestWorkerTargetAlias verifies WorkerTarget alias
// (worker.go).
func TestWorkerTargetAlias(t *testing.T) {
	var w WorkerTarget
	w.TargetID = "test"
	if w.TargetID != "test" {
		t.Error("WorkerTarget alias should work")
	}
}

// ==================== fingerprint.go tests ====================

// TestDefaultFingerprintConfig verifies default config
// (fingerprint.go - browser fingerprint spoofing).
func TestDefaultFingerprintConfig(t *testing.T) {
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

// TestFingerprintManagerSetGet verifies SetConfig/Current
// (fingerprint.go).
func TestFingerprintManagerSetGet(t *testing.T) {
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

// TestFingerprintIsCanvasNoiseEnabled verifies IsCanvasNoiseEnabled
// (fingerprint.go).
func TestFingerprintIsCanvasNoiseEnabled(t *testing.T) {
	m := NewFingerprintManager()
	if !m.IsCanvasNoiseEnabled() {
		t.Error("canvas noise should be enabled by default")
	}
	m.SetConfig(FingerprintConfig{CanvasNoise: false, HardwareConcurrency: 8, DeviceMemory: 8})
	if m.IsCanvasNoiseEnabled() {
		t.Error("canvas noise should be disabled after SetConfig")
	}
}

// TestFingerprintValidate verifies validation
// (fingerprint.go).
func TestFingerprintValidate(t *testing.T) {
	valid := DefaultFingerprintConfig()
	if err := valid.Validate(); err != nil {
		t.Errorf("valid config: %v", err)
	}
	invalid := FingerprintConfig{HardwareConcurrency: 0, DeviceMemory: 8}
	if err := invalid.Validate(); err == nil {
		t.Error("invalid config should fail validation")
	}
}

// TestFingerprintNilSafe verifies nil FingerprintManager is safe.
func TestFingerprintNilSafe(t *testing.T) {
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

// TestNewPopupGuard verifies creation with defaults
// (popup.go - popup guard).
func TestNewPopupGuard(t *testing.T) {
	g := NewPopupGuard()
	if g.Policy() != PopupPolicyBlock {
		t.Error("default policy should be block")
	}
	if !g.ShouldBlock() {
		t.Error("should block by default")
	}
}

// TestPopupGuardSetPolicy verifies SetPolicy
// (popup.go - popup guard).
func TestPopupGuardSetPolicy(t *testing.T) {
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

// TestPopupGuardRecordCount verifies RecordPopup/Count
// (popup.go - popup guard).
func TestPopupGuardRecordCount(t *testing.T) {
	g := NewPopupGuard()
	g.RecordPopup()
	g.RecordPopup()
	g.RecordPopup()
	if g.Count() != 3 {
		t.Errorf("count: got %d, want 3", g.Count())
	}
}

// TestPopupGuardNilSafe verifies nil PopupGuard is safe.
func TestPopupGuardNilSafe(t *testing.T) {
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

// TestBuiltinGeoPresets verifies 8 built-in presets
// (8 built-in region-presets).
func TestBuiltinGeoPresets(t *testing.T) {
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

// TestBuiltinGeoPresetsValues verifies preset values
// ({locale BCP47, timezoneId IANA, geolocation}).
func TestBuiltinGeoPresetsValues(t *testing.T) {
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

// TestGeoPresetManagerResolve verifies ResolvePreset
// (case-insensitive lookup).
func TestGeoPresetManagerResolve(t *testing.T) {
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

// TestGeoPresetManagerResolveNotFound verifies not found.
func TestGeoPresetManagerResolveNotFound(t *testing.T) {
	m := NewGeoPresetManager()
	_, ok := m.ResolvePreset("nonexistent")
	if ok {
		t.Error("nonexistent should not be found")
	}
}

// TestGeoPresetManagerCustomOverride verifies custom overrides
// built-in (custom overrides built-in same-name).
func TestGeoPresetManagerCustomOverride(t *testing.T) {
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

// TestGeoPresetManagerListPresets verifies ListPresets.
func TestGeoPresetManagerListPresets(t *testing.T) {
	m := NewGeoPresetManager()
	names := m.ListPresets()
	if len(names) != 8 {
		t.Errorf("expected 8 presets, got %d", len(names))
	}
}

// TestStealthFacadesValidatePresetValid verifies valid preset passes
// (BCP47 + IANA + lat/long + viewport validation).
func TestStealthFacadesValidatePresetValid(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 40.7128, Longitude: -74.006},
	}
	if err := ValidatePreset(p); err != nil {
		t.Errorf("valid preset: %v", err)
	}
}

// TestStealthFacadesValidatePresetInvalidLocale verifies invalid locale fails
// (BCP47-locale-regex).
func TestStealthFacadesValidatePresetInvalidLocale(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "invalid_locale!",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 40.7, Longitude: -74.0},
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("invalid locale should fail validation")
	}
}

// TestStealthFacadesValidatePresetLatOutOfRange verifies lat > 90 fails
// (lat∈[-90,90]).
func TestStealthFacadesValidatePresetLatOutOfRange(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 91, Longitude: 0},
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("lat > 90 should fail validation")
	}
}

// TestStealthFacadesValidatePresetLongOutOfRange verifies long > 180 fails
// (long∈[-180,180]).
func TestStealthFacadesValidatePresetLongOutOfRange(t *testing.T) {
	p := GeoPresetConfig{
		Locale:      "en-US",
		TimezoneID:  "America/New_York",
		Geolocation: GeoCoord{Latitude: 0, Longitude: 181},
	}
	if err := ValidatePreset(p); err == nil {
		t.Error("long > 180 should fail validation")
	}
}

// TestValidatePresetViewportOutOfRange verifies viewport
// out of range fails (width∈[320,3840], height∈[240,2160]).
func TestValidatePresetViewportOutOfRange(t *testing.T) {
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

// TestResolveContextOptions verifies context options
// resolution (individual-fields-override-preset-defaults).
func TestResolveContextOptions(t *testing.T) {
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

// TestResolveContextOptionsNotFound verifies not-found preset
// errors.
func TestResolveContextOptionsNotFound(t *testing.T) {
	m := NewGeoPresetManager()
	_, err := m.ResolveContextOptions(ContextOptions{Preset: "nonexistent"})
	if err == nil {
		t.Error("nonexistent preset should error")
	}
}

// TestContextHash verifies context hash is deterministic
// (contextHash for session-isolation).
func TestContextHash(t *testing.T) {
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

// TestContextHashDifferent verifies different options produce
// different hashes (session-isolation).
func TestContextHashDifferent(t *testing.T) {
	h1 := ContextHash(ContextOptions{Preset: "germany"})
	h2 := ContextHash(ContextOptions{Preset: "japan"})
	if h1 == h2 {
		t.Error("different presets should produce different hashes")
	}
}

// ==================== parity test ====================

// TestStealthFacadesParity verifies all 7 spec-mandated files exist
// and have the spec-mandated functionality.
func TestStealthFacadesParity(t *testing.T) {
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
