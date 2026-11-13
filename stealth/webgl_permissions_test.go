package stealth

import (
	"testing"
)

// ==================== webgl.go tests ====================

// TestNewWebGLOverride verifies creation with defaults.
func TestNewWebGLOverride(t *testing.T) {
	w := NewWebGLOverride()
	if w.IsEnabled() {
		t.Error("new override should be disabled")
	}
}

// TestIsSwiftShader verifies SwiftShader detection
// (Override headless "SwiftShader").
func TestIsSwiftShader(t *testing.T) {
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

// TestDetectGPU verifies DetectGPU returns a GPUInfo
// (MEASURE-FIRST pattern).
func TestDetectGPU(t *testing.T) {
	gpu := DetectGPU()
	// On any OS, DetectGPU should return a GPUInfo (may or may not be detected).
	if gpu.Source == "" {
		t.Error("source should not be empty")
	}
}

// TestMeasureAndOverride verifies the MEASURE-FIRST pattern
// (MEASURE-FIRST. Override headless "SwiftShader" with
// REAL GPU name. Fallback: GPU undetectable -> DON'T spoof).
func TestMeasureAndOverride(t *testing.T) {
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

// TestOverrideSwiftShaderWhenEnabled verifies that when the
// override is enabled, SwiftShader is replaced with the real GPU.
func TestOverrideSwiftShaderWhenEnabled(t *testing.T) {
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
	w.consistencyResult = ConsistencyResult{Status: ConsistencyValid, Reason: "test"}
	w.enabled = true
	w.mu.Unlock()

	override := w.OverrideSwiftShader("SwiftShader")
	if override != "NVIDIA GeForce RTX 3080" {
		t.Errorf("override: got %s, want NVIDIA GeForce RTX 3080", override)
	}
}

// TestOverrideSwiftShaderNotSwiftShader verifies no override
// when the renderer is NOT SwiftShader.
func TestOverrideSwiftShaderNotSwiftShader(t *testing.T) {
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

// TestOverrideSwiftShaderDisabled verifies no override when
// disabled (GPU undetectable -> DON'T spoof).
func TestOverrideSwiftShaderDisabled(t *testing.T) {
	w := NewWebGLOverride()
	override := w.OverrideSwiftShader("SwiftShader")
	if override != "" {
		t.Error("disabled override should return empty string")
	}
}

// TestOverrideSwiftShaderNilSafe verifies nil is safe.
func TestOverrideSwiftShaderNilSafe(t *testing.T) {
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

// TestConsistencyChecked verifies consistency check state.
func TestConsistencyChecked(t *testing.T) {
	w := NewWebGLOverride()
	if w.ConsistencyChecked() {
		t.Error("should not be checked before MeasureAndOverride")
	}
	w.MeasureAndOverride()
	// MeasureAndOverride only runs the GPU-consistency check when a GPU is
	// actually detected; on a headless / GPU-less host it bails out early
	// (honest > fake) and leaves consistencyChecked false. Assert
	// against the real GPU-detection outcome so the test is correct both on a
	// GPU host and on a headless CI runner.
	if got, want := w.ConsistencyChecked(), DetectGPU().Detected; got != want {
		t.Errorf("ConsistencyChecked() = %v after MeasureAndOverride, want %v (must match GPU detection)", got, want)
	}
}

// TestWebGLString verifies String method.
func TestWebGLString(t *testing.T) {
	w := NewWebGLOverride()
	s := w.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestWebGLNilString verifies nil String is safe.
func TestWebGLNilString(t *testing.T) {
	var w *WebGLOverride
	if w.String() != "WebGLOverride(nil)" {
		t.Error("nil String should return WebGLOverride(nil)")
	}
}

// TestExtractVendor verifies vendor extraction.
func TestExtractVendor(t *testing.T) {
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

// ==================== consistency validation tests ====================

func TestCheckConsistencyValidApple(t *testing.T) {
	result := checkConsistency(GPUInfo{Vendor: "Apple", Renderer: "Apple M1 Pro", Source: "test", Detected: true})
	if result.Status != ConsistencyValid {
		t.Errorf("Apple M1 Pro: got status %s, want valid; reason: %s", result.Status, result.Reason)
	}
}

func TestCheckConsistencyValidNVIDIA(t *testing.T) {
	result := checkConsistency(GPUInfo{Vendor: "NVIDIA", Renderer: "NVIDIA GeForce RTX 4090", Source: "test", Detected: true})
	if result.Status != ConsistencyValid {
		t.Errorf("RTX 4090: got status %s, want valid; reason: %s", result.Status, result.Reason)
	}
}

func TestCheckConsistencyValidIntel(t *testing.T) {
	result := checkConsistency(GPUInfo{Vendor: "Intel", Renderer: "Intel Iris OpenGL Engine", Source: "test", Detected: true})
	if result.Status != ConsistencyValid {
		t.Errorf("Intel Iris: got status %s, want valid; reason: %s", result.Status, result.Reason)
	}
}

func TestCheckConsistencyValidAMD(t *testing.T) {
	result := checkConsistency(GPUInfo{Vendor: "AMD", Renderer: "AMD Radeon RX 7900 XTX", Source: "test", Detected: true})
	if result.Status != ConsistencyValid {
		t.Errorf("Radeon: got status %s, want valid; reason: %s", result.Status, result.Reason)
	}
}

func TestCheckConsistencyUnknownGPUFailsClosed(t *testing.T) {
	result := checkConsistency(GPUInfo{Vendor: "Qualcomm", Renderer: "Adreno 730", Source: "test", Detected: true})
	if result.Status != ConsistencyUnknownGPU {
		t.Errorf("unknown GPU: got status %s, want unknown_gpu; reason: %s", result.Status, result.Reason)
	}
	if result.Reason == "" {
		t.Error("unknown GPU must have a diagnostic reason")
	}
}

func TestCheckConsistencyMismatch(t *testing.T) {
	result := checkConsistency(GPUInfo{Vendor: "NVIDIA", Renderer: "Some Unknown Board", Source: "test", Detected: true})
	if result.Status != ConsistencyMismatch {
		t.Errorf("mismatch: got status %s, want mismatch; reason: %s", result.Status, result.Reason)
	}
}

func TestCheckConsistencyUndetectable(t *testing.T) {
	result := checkConsistency(GPUInfo{Detected: false, Source: "test"})
	if result.Status != ConsistencyUndetectable {
		t.Errorf("undetectable: got status %s, want undetectable", result.Status)
	}
}

func TestMeasureAndOverrideDisablesOnUnknownGPU(t *testing.T) {
	w := NewWebGLOverride()
	w.mu.Lock()
	w.gpu = GPUInfo{Vendor: "Qualcomm", Renderer: "Adreno 730", Source: "test", Detected: true}
	w.mu.Unlock()
	result := checkConsistency(w.GPU())
	if result.Status == ConsistencyValid {
		t.Error("unknown GPU must not pass consistency")
	}
}

func TestConsistencyDiagnosticNilSafe(t *testing.T) {
	var w *WebGLOverride
	diag := w.ConsistencyDiagnostic()
	if diag.Status != ConsistencyNotChecked {
		t.Errorf("nil diagnostic: got %s, want not_checked", diag.Status)
	}
}

func TestConsistencyDiagnosticBeforeCheck(t *testing.T) {
	w := NewWebGLOverride()
	diag := w.ConsistencyDiagnostic()
	if diag.Status != ConsistencyNotChecked {
		t.Errorf("before check: got %s, want not_checked", diag.Status)
	}
}

func TestConsistencyDiagnosticAfterMeasure(t *testing.T) {
	w := NewWebGLOverride()
	w.MeasureAndOverride()
	diag := w.ConsistencyDiagnostic()
	if diag.Status == ConsistencyNotChecked {
		t.Error("after MeasureAndOverride, diagnostic must not be not_checked")
	}
	if diag.Reason == "" {
		t.Error("diagnostic must carry a reason after check")
	}
}

func TestExpectedWebGLVendor(t *testing.T) {
	cases := []struct {
		gpu  GPUInfo
		want string
	}{
		{GPUInfo{Vendor: "Apple", Renderer: "Apple M2 Ultra", Detected: true}, "Apple"},
		{GPUInfo{Vendor: "NVIDIA", Renderer: "NVIDIA GeForce RTX 3080", Detected: true}, "NVIDIA Corporation"},
		{GPUInfo{Vendor: "Intel", Renderer: "Intel UHD Graphics 630", Detected: true}, "Intel Inc."},
		{GPUInfo{Vendor: "AMD", Renderer: "AMD Radeon Pro W6800", Detected: true}, "ATI Technologies Inc."},
		{GPUInfo{Vendor: "Qualcomm", Renderer: "Adreno 730", Detected: true}, ""},
	}
	for _, c := range cases {
		got := ExpectedWebGLVendor(c.gpu)
		if got != c.want {
			t.Errorf("ExpectedWebGLVendor(%s/%s): got %q, want %q", c.gpu.Vendor, c.gpu.Renderer, got, c.want)
		}
	}
}

func TestConcurrentMeasureAndRead(t *testing.T) {
	w := NewWebGLOverride()
	w.MeasureAndOverride()
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 200; j++ {
				_ = w.IsEnabled()
				_ = w.ConsistencyOK()
				_ = w.ConsistencyChecked()
				_ = w.ConsistencyDiagnostic()
				_ = w.GPU()
				_ = w.String()
				_ = w.OverrideSwiftShader("SwiftShader")
				w.mu.Lock()
				w.consistencyResult = ConsistencyResult{Status: ConsistencyValid, Reason: "concurrent"}
				w.mu.Unlock()
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

// ==================== permissions.go tests ====================

// TestPermissionStateConstants verifies the state constants.
func TestPermissionStateConstants(t *testing.T) {
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

// TestPermissionNameConstants verifies the permission name
// constants.
func TestPermissionNameConstants(t *testing.T) {
	if PermissionGeolocation != "geolocation" {
		t.Error("PermissionGeolocation mismatch")
	}
	if PermissionNotifications != "notifications" {
		t.Error("PermissionNotifications mismatch")
	}
}

// TestNewPermissionAPI verifies creation with defaults.
func TestNewPermissionAPI(t *testing.T) {
	p := NewPermissionAPI()
	if p.IsActive() {
		t.Error("new PermissionAPI should be inactive")
	}
	if !p.IsPassiveOnly() {
		t.Error("should always be passive-only")
	}
}

// TestPermissionQueryInactive verifies that when inactive,
// query returns "denied" (natural headless behavior)
// (Real requests -> "denied").
func TestPermissionQueryInactive(t *testing.T) {
	p := NewPermissionAPI()
	result := p.Query(PermissionGeolocation)
	if result.State != PermissionStateDenied {
		t.Errorf("inactive query: got %s, want denied", result.State)
	}
}

// TestPermissionQueryActive verifies that when active,
// query returns "prompt" (PASSIVE-ONLY, safe)
// (permissions.query() -> "prompt" (passive, safe)).
func TestPermissionQueryActive(t *testing.T) {
	p := NewPermissionAPI()
	p.Activate()
	result := p.Query(PermissionGeolocation)
	if result.State != PermissionStatePrompt {
		t.Errorf("active query: got %s, want prompt", result.State)
	}
}

// TestPermissionQueryName verifies the result has the
// correct permission name.
func TestPermissionQueryName(t *testing.T) {
	p := NewPermissionAPI()
	p.Activate()
	result := p.Query(PermissionCamera)
	if result.Name != PermissionCamera {
		t.Errorf("name: got %s, want camera", result.Name)
	}
}

// TestPermissionRequestAlwaysDenied verifies that
// requestPermission() always returns "denied"
// (requestPermission() NOT touched. Real requests ->
// "denied" (headless can't ask = natural)).
func TestPermissionRequestAlwaysDenied(t *testing.T) {
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

// TestPermissionRequestInactive verifies request returns
// denied when inactive.
func TestPermissionRequestInactive(t *testing.T) {
	p := NewPermissionAPI()
	if p.Request(PermissionNotifications) != PermissionStateDenied {
		t.Error("inactive request should return denied")
	}
}

// TestPermissionActivateDeactivate verifies activate/deactivate
// cycle (Activation: StealthStealth (Patch 7)).
func TestPermissionActivateDeactivate(t *testing.T) {
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

// TestPermissionQueryAll verifies QueryAll returns all
// permissions with correct states.
func TestPermissionQueryAll(t *testing.T) {
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

// TestPermissionQueryAllInactive verifies QueryAll returns
// "denied" for all when inactive.
func TestPermissionQueryAllInactive(t *testing.T) {
	p := NewPermissionAPI()
	all := p.QueryAll()
	for name, state := range all {
		if state != PermissionStateDenied {
			t.Errorf("permission %s: got %s, want denied", name, state)
		}
	}
}

// TestPermissionIsPassiveOnly verifies IsPassiveOnly is
// always true (PASSIVE-ONLY override).
func TestPermissionIsPassiveOnly(t *testing.T) {
	p := NewPermissionAPI()
	if !p.IsPassiveOnly() {
		t.Error("should always be passive-only")
	}
	p.Activate()
	if !p.IsPassiveOnly() {
		t.Error("should always be passive-only even when active")
	}
}

// TestPermissionNilSafe verifies nil PermissionAPI is safe.
func TestPermissionNilSafe(t *testing.T) {
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

// TestPermissionString verifies String method.
func TestPermissionString(t *testing.T) {
	p := NewPermissionAPI()
	s := p.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestPermissionNilString verifies nil String.
func TestPermissionNilString(t *testing.T) {
	var p *PermissionAPI
	if p.String() != "PermissionAPI(nil)" {
		t.Error("nil String should return PermissionAPI(nil)")
	}
}

// ==================== parity test ====================

// TestWebGLPermissionsParity verifies full parity.
// (WebGL MEASURE-FIRST, Permission API PASSIVE-ONLY).
func TestWebGLPermissionsParity(t *testing.T) {
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
