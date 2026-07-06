package stealth

import (
	"testing"
)

// ==================== webgl.go tests ====================

// TestTASK2243_NewWebGLOverride verifies creation with defaults
// (spec L4089).
func TestTASK2243_NewWebGLOverride(t *testing.T) {
	w := NewWebGLOverride()
	if w.IsEnabled() {
		t.Error("new override should be disabled")
	}
}

// TestTASK2243_IsSwiftShader verifies SwiftShader detection
// (spec L4089: Override headless "SwiftShader").
func TestTASK2243_IsSwiftShader(t *testing.T) {
	cases := []struct {
		renderer string
		expected bool
	}{
		{"SwiftShader", true},
		{"ANGLE (SwiftShader)", true},
		{"swiftshader", true},
		{"Apple GPU", false},
		{"NVIDIA GeForce RTX 3080", false},
		{"", false},
	}
	for _, c := range cases {
		if IsSwiftShader(c.renderer) != c.expected {
			t.Errorf("IsSwiftShader(%q): got %v, want %v", c.renderer, IsSwiftShader(c.renderer), c.expected)
		}
	}
}

// TestTASK2243_DetectGPU verifies DetectGPU returns a GPUInfo
// (spec L4089: MEASURE-FIRST pattern).
func TestTASK2243_DetectGPU(t *testing.T) {
	gpu := DetectGPU()
	// On any OS, DetectGPU should return a GPUInfo (may or may not be detected).
	if gpu.Source == "" {
		t.Error("source should not be empty")
	}
}

// TestTASK2243_MeasureAndOverride verifies the MEASURE-FIRST pattern
// (spec L4089: MEASURE-FIRST. Override headless "SwiftShader" with
// REAL GPU name. Fallback: GPU undetectable -> DON'T spoof).
func TestTASK2243_MeasureAndOverride(t *testing.T) {
	w := NewWebGLOverride()
	result := w.MeasureAndOverride()
	gpu := w.GPU()
	// If GPU was detected, override should be enabled (if consistency passes).
	if gpu.Detected && result {
		if !w.IsEnabled() {
			t.Error("should be enabled after successful MeasureAndOverride")
		}
	}
	// If GPU was not detected, override should be disabled.
	if !gpu.Detected {
		if w.IsEnabled() {
			t.Error("should be disabled when GPU undetectable")
		}
	}
}

// TestTASK2243_OverrideSwiftShaderWhenEnabled verifies that when the
// override is enabled, SwiftShader is replaced with the real GPU
// (spec L4089).
func TestTASK2243_OverrideSwiftShaderWhenEnabled(t *testing.T) {
	w := NewWebGLOverride()
	// Manually set GPU info for deterministic test.
	w.mu.Lock()
	w.gpu = GPUInfo{
		Vendor:   "NVIDIA",
		Renderer: "NVIDIA GeForce RTX 3080",
		Source:   "test",
		Detected: true,
	}
	w.consistencyChecked = true
	w.consistencyOK = true
	w.enabled = true
	w.mu.Unlock()

	override := w.OverrideSwiftShader("SwiftShader")
	if override != "NVIDIA GeForce RTX 3080" {
		t.Errorf("override: got %s, want NVIDIA GeForce RTX 3080", override)
	}
}

// TestTASK2243_OverrideSwiftShaderNotSwiftShader verifies no override
// when the renderer is NOT SwiftShader.
func TestTASK2243_OverrideSwiftShaderNotSwiftShader(t *testing.T) {
	w := NewWebGLOverride()
	w.mu.Lock()
	w.gpu = GPUInfo{Vendor: "NVIDIA", Renderer: "RTX 3080", Detected: true}
	w.enabled = true
	w.mu.Unlock()

	override := w.OverrideSwiftShader("Apple GPU")
	if override != "" {
		t.Errorf("should not override non-SwiftShader: got %s", override)
	}
}

// TestTASK2243_OverrideSwiftShaderDisabled verifies no override when
// disabled (spec L4089: GPU undetectable -> DON'T spoof).
func TestTASK2243_OverrideSwiftShaderDisabled(t *testing.T) {
	w := NewWebGLOverride()
	override := w.OverrideSwiftShader("SwiftShader")
	if override != "" {
		t.Error("disabled override should return empty string")
	}
}

// TestTASK2243_OverrideSwiftShaderNilSafe verifies nil is safe.
func TestTASK2243_OverrideSwiftShaderNilSafe(t *testing.T) {
	var w *WebGLOverride
	if w.OverrideSwiftShader("SwiftShader") != "" {
		t.Error("nil override should return empty string")
	}
	if w.IsEnabled() {
		t.Error("nil should not be enabled")
	}
	if w.GPU().Detected {
		t.Error("nil GPU should not be detected")
	}
}

// TestTASK2243_ConsistencyChecked verifies consistency check state.
func TestTASK2243_ConsistencyChecked(t *testing.T) {
	w := NewWebGLOverride()
	if w.ConsistencyChecked() {
		t.Error("should not be checked before MeasureAndOverride")
	}
	w.MeasureAndOverride()
	// MeasureAndOverride only runs the GPU-consistency check when a GPU is
	// actually detected; on a headless / GPU-less host it bails out early
	// (honest > fake, spec L4089) and leaves consistencyChecked false. Assert
	// against the real GPU-detection outcome so the test is correct both on a
	// GPU host and on a headless CI runner.
	if got, want := w.ConsistencyChecked(), DetectGPU().Detected; got != want {
		t.Errorf("ConsistencyChecked() = %v after MeasureAndOverride, want %v (must match GPU detection)", got, want)
	}
}

// TestTASK2243_WebGLString verifies String method.
func TestTASK2243_WebGLString(t *testing.T) {
	w := NewWebGLOverride()
	s := w.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTASK2243_WebGLNilString verifies nil String is safe.
func TestTASK2243_WebGLNilString(t *testing.T) {
	var w *WebGLOverride
	if w.String() != "WebGLOverride(nil)" {
		t.Error("nil String should return WebGLOverride(nil)")
	}
}

// TestTASK2243_ExtractVendor verifies vendor extraction.
func TestTASK2243_ExtractVendor(t *testing.T) {
	cases := []struct {
		gpuName  string
		expected string
	}{
		{"NVIDIA GeForce RTX 3080", "NVIDIA"},
		{"AMD Radeon RX 6800", "AMD"},
		{"Intel UHD Graphics 630", "Intel"},
		{"Unknown GPU", "Unknown"},
	}
	for _, c := range cases {
		if extractVendor(c.gpuName) != c.expected {
			t.Errorf("extractVendor(%q): got %s, want %s", c.gpuName, extractVendor(c.gpuName), c.expected)
		}
	}
}

// ==================== permissions.go tests ====================

// TestTASK2243_PermissionStateConstants verifies the state constants
// (spec L4091).
func TestTASK2243_PermissionStateConstants(t *testing.T) {
	if PermissionStatePrompt != "prompt" {
		t.Error("PermissionStatePrompt mismatch")
	}
	if PermissionStateDenied != "denied" {
		t.Error("PermissionStateDenied mismatch")
	}
	if PermissionStateGranted != "granted" {
		t.Error("PermissionStateGranted mismatch")
	}
}

// TestTASK2243_PermissionNameConstants verifies the permission name
// constants (spec L4091).
func TestTASK2243_PermissionNameConstants(t *testing.T) {
	if PermissionGeolocation != "geolocation" {
		t.Error("PermissionGeolocation mismatch")
	}
	if PermissionNotifications != "notifications" {
		t.Error("PermissionNotifications mismatch")
	}
}

// TestTASK2243_NewPermissionAPI verifies creation with defaults
// (spec L4091).
func TestTASK2243_NewPermissionAPI(t *testing.T) {
	p := NewPermissionAPI()
	if p.IsActive() {
		t.Error("new PermissionAPI should be inactive")
	}
	if !p.IsPassiveOnly() {
		t.Error("should always be passive-only")
	}
}

// TestTASK2243_PermissionQueryInactive verifies that when inactive,
// query returns "denied" (natural headless behavior)
// (spec L4091: Real requests -> "denied").
func TestTASK2243_PermissionQueryInactive(t *testing.T) {
	p := NewPermissionAPI()
	result := p.Query(PermissionGeolocation)
	if result.State != PermissionStateDenied {
		t.Errorf("inactive query: got %s, want denied", result.State)
	}
}

// TestTASK2243_PermissionQueryActive verifies that when active,
// query returns "prompt" (PASSIVE-ONLY, safe)
// (spec L4091: permissions.query() -> "prompt" (passive, safe)).
func TestTASK2243_PermissionQueryActive(t *testing.T) {
	p := NewPermissionAPI()
	p.Activate()
	result := p.Query(PermissionGeolocation)
	if result.State != PermissionStatePrompt {
		t.Errorf("active query: got %s, want prompt", result.State)
	}
}

// TestTASK2243_PermissionQueryName verifies the result has the
// correct permission name.
func TestTASK2243_PermissionQueryName(t *testing.T) {
	p := NewPermissionAPI()
	p.Activate()
	result := p.Query(PermissionCamera)
	if result.Name != PermissionCamera {
		t.Errorf("name: got %s, want camera", result.Name)
	}
}

// TestTASK2243_PermissionRequestAlwaysDenied verifies that
// requestPermission() always returns "denied"
// (spec L4091: requestPermission() NOT touched. Real requests ->
// "denied" (headless can't ask = natural)).
func TestTASK2243_PermissionRequestAlwaysDenied(t *testing.T) {
	p := NewPermissionAPI()
	p.Activate()
	// Even when active, requestPermission returns "denied"
	if p.Request(PermissionGeolocation) != PermissionStateDenied {
		t.Error("request should always return denied")
	}
	if p.Request(PermissionCamera) != PermissionStateDenied {
		t.Error("request should always return denied")
	}
}

// TestTASK2243_PermissionRequestInactive verifies request returns
// denied when inactive.
func TestTASK2243_PermissionRequestInactive(t *testing.T) {
	p := NewPermissionAPI()
	if p.Request(PermissionNotifications) != PermissionStateDenied {
		t.Error("inactive request should return denied")
	}
}

// TestTASK2243_PermissionActivateDeactivate verifies activate/deactivate
// cycle (spec L4091: Activation: StealthStealth (Patch 7)).
func TestTASK2243_PermissionActivateDeactivate(t *testing.T) {
	p := NewPermissionAPI()
	if p.IsActive() {
		t.Error("should start inactive")
	}
	p.Activate()
	if !p.IsActive() {
		t.Error("should be active after Activate")
	}
	p.Deactivate()
	if p.IsActive() {
		t.Error("should be inactive after Deactivate")
	}
}

// TestTASK2243_PermissionQueryAll verifies QueryAll returns all
// permissions with correct states.
func TestTASK2243_PermissionQueryAll(t *testing.T) {
	p := NewPermissionAPI()
	p.Activate()
	all := p.QueryAll()
	if len(all) != 6 {
		t.Errorf("expected 6 permissions, got %d", len(all))
	}
	for name, state := range all {
		if state != PermissionStatePrompt {
			t.Errorf("permission %s: got %s, want prompt", name, state)
		}
	}
}

// TestTASK2243_PermissionQueryAllInactive verifies QueryAll returns
// "denied" for all when inactive.
func TestTASK2243_PermissionQueryAllInactive(t *testing.T) {
	p := NewPermissionAPI()
	all := p.QueryAll()
	for name, state := range all {
		if state != PermissionStateDenied {
			t.Errorf("permission %s: got %s, want denied", name, state)
		}
	}
}

// TestTASK2243_PermissionIsPassiveOnly verifies IsPassiveOnly is
// always true (spec L4091: PASSIVE-ONLY override).
func TestTASK2243_PermissionIsPassiveOnly(t *testing.T) {
	p := NewPermissionAPI()
	if !p.IsPassiveOnly() {
		t.Error("should always be passive-only")
	}
	p.Activate()
	if !p.IsPassiveOnly() {
		t.Error("should always be passive-only even when active")
	}
}

// TestTASK2243_PermissionNilSafe verifies nil PermissionAPI is safe.
func TestTASK2243_PermissionNilSafe(t *testing.T) {
	var p *PermissionAPI
	p.Activate()
	p.Deactivate()
	if p.IsActive() {
		t.Error("nil should not be active")
	}
	if !p.IsPassiveOnly() {
		t.Error("nil should still be passive-only")
	}
	result := p.Query(PermissionGeolocation)
	if result.State != PermissionStateDenied {
		t.Error("nil query should return denied")
	}
	if p.Request(PermissionCamera) != PermissionStateDenied {
		t.Error("nil request should return denied")
	}
	if len(p.QueryAll()) != 0 {
		t.Error("nil QueryAll should return empty map")
	}
}

// TestTASK2243_PermissionString verifies String method.
func TestTASK2243_PermissionString(t *testing.T) {
	p := NewPermissionAPI()
	s := p.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTASK2243_PermissionNilString verifies nil String.
func TestTASK2243_PermissionNilString(t *testing.T) {
	var p *PermissionAPI
	if p.String() != "PermissionAPI(nil)" {
		t.Error("nil String should return PermissionAPI(nil)")
	}
}

// ==================== full spec parity test ====================

// TestTASK2243_FullSpecParity verifies full spec parity for L4089+L4091
// (spec L4089: WebGL MEASURE-FIRST, L4091: Permission API PASSIVE-ONLY).
func TestTASK2243_FullSpecParity(t *testing.T) {
	// 1. WebGL override
	w := NewWebGLOverride()
	if w.IsEnabled() {
		t.Error("WebGL should start disabled")
	}
	w.MeasureAndOverride()
	// After measure, GPU info should be populated
	gpu := w.GPU()
	if gpu.Source == "" {
		t.Error("GPU source should be set after MeasureAndOverride")
	}

	// 2. SwiftShader detection
	if !IsSwiftShader("SwiftShader") {
		t.Error("SwiftShader should be detected")
	}

	// 3. Permission API
	p := NewPermissionAPI()
	if p.IsActive() {
		t.Error("PermissionAPI should start inactive")
	}

	// 4. Query returns "denied" when inactive
	if p.Query(PermissionGeolocation).State != PermissionStateDenied {
		t.Error("inactive query should return denied")
	}

	// 5. Query returns "prompt" when active (PASSIVE-ONLY)
	p.Activate()
	if p.Query(PermissionGeolocation).State != PermissionStatePrompt {
		t.Error("active query should return prompt")
	}

	// 6. Request always returns "denied" (NOT overridden)
	if p.Request(PermissionGeolocation) != PermissionStateDenied {
		t.Error("request should always return denied")
	}

	// 7. IsPassiveOnly is always true
	if !p.IsPassiveOnly() {
		t.Error("should be passive-only")
	}
}
