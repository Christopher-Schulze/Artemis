package stealth

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// GPUInfo describes the real GPU detected on the system
// (spec L4091: MEASURE-FIRST pattern. Real GPU via system_profiler
// (macOS) / lspci (Linux)).
type GPUInfo struct {
	Vendor   string
	Renderer string
	Source   string // "system_profiler", "lspci", "fallback"
	Detected bool
}

// ConsistencyStatus is the outcome of a WebGL consistency validation.
type ConsistencyStatus string

const (
	ConsistencyValid        ConsistencyStatus = "valid"
	ConsistencyMismatch     ConsistencyStatus = "mismatch"
	ConsistencyUnknownGPU   ConsistencyStatus = "unknown_gpu"
	ConsistencyNotChecked   ConsistencyStatus = "not_checked"
	ConsistencyUndetectable ConsistencyStatus = "undetectable"
)

// ConsistencyResult carries the full diagnostic truth of a consistency
// check (spec L4091: Mismatch -> disable + warn; honest > fake).
type ConsistencyResult struct {
	Status ConsistencyStatus
	Reason string
}

// gpuFamily is a build-time known GPU family with its expected WebGL
// vendor string and renderer pattern (spec L4091: lookup table, build-time).
type gpuFamily struct {
	VendorPattern   string
	RendererPattern string
	WebGLVendor     string
}

// gpuLookupTable is the build-time GPU-to-WebGL consistency table.
// Only GPUs in this table are eligible for override; unknown GPUs
// fail closed (spec L4091: Mismatch -> disable + warn).
var gpuLookupTable = []gpuFamily{
	{VendorPattern: "apple", RendererPattern: "apple", WebGLVendor: "Apple"},
	{VendorPattern: "apple", RendererPattern: "apple m", WebGLVendor: "Apple"},
	{VendorPattern: "intel", RendererPattern: "intel", WebGLVendor: "Intel Inc."},
	{VendorPattern: "intel", RendererPattern: "iris", WebGLVendor: "Intel Inc."},
	{VendorPattern: "intel", RendererPattern: "uhd", WebGLVendor: "Intel Inc."},
	{VendorPattern: "intel", RendererPattern: "hd graphics", WebGLVendor: "Intel Inc."},
	{VendorPattern: "nvidia", RendererPattern: "geforce", WebGLVendor: "NVIDIA Corporation"},
	{VendorPattern: "nvidia", RendererPattern: "quadro", WebGLVendor: "NVIDIA Corporation"},
	{VendorPattern: "nvidia", RendererPattern: "tesla", WebGLVendor: "NVIDIA Corporation"},
	{VendorPattern: "nvidia", RendererPattern: "nvidia", WebGLVendor: "NVIDIA Corporation"},
	{VendorPattern: "amd", RendererPattern: "radeon", WebGLVendor: "ATI Technologies Inc."},
	{VendorPattern: "amd", RendererPattern: "firepro", WebGLVendor: "ATI Technologies Inc."},
	{VendorPattern: "amd", RendererPattern: "amd", WebGLVendor: "ATI Technologies Inc."},
}

// WebGLOverride is the WebGL renderer override configuration
// (spec L4091: Override headless "SwiftShader" with REAL GPU name).
type WebGLOverride struct {
	mu                 sync.RWMutex
	gpu                GPUInfo
	enabled            bool
	consistencyChecked bool
	consistencyResult  ConsistencyResult
}

// NewWebGLOverride creates a new WebGL override instance
// (spec L4091: MEASURE-FIRST pattern).
func NewWebGLOverride() *WebGLOverride {
	return &WebGLOverride{enabled: false}
}

// DetectGPU detects the real GPU on the system
// (spec L4091: Real GPU via system_profiler SPDisplaysDataType (macOS)
// / lspci | grep VGA (Linux)).
func DetectGPU() GPUInfo {
	switch runtime.GOOS {
	case "darwin":
		return detectGPUMacOS()
	case "linux":
		return detectGPULinux()
	default:
		return GPUInfo{Detected: false, Source: "unsupported"}
	}
}

// detectGPUMacOS detects GPU on macOS via system_profiler
// (spec L4091: system_profiler SPDisplaysDataType).
func detectGPUMacOS() GPUInfo {
	cmd := exec.Command("system_profiler", "SPDisplaysDataType", "-detailLevel", "mini")
	output, err := cmd.Output()
	if err != nil {
		return GPUInfo{Detected: false, Source: "system_profiler"}
	}
	lines := strings.Split(string(output), "\n")
	var vendor, renderer string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Vendor:") {
			vendor = strings.TrimSpace(strings.TrimPrefix(line, "Vendor:"))
		}
		if strings.Contains(line, "Resolution:") || strings.Contains(line, "Displays:") {
			break
		}
	}
	// On macOS, the GPU is usually Apple GPU or Intel/AMD.
	if vendor == "" {
		// Try to extract from the output.
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "Apple") {
				vendor = "Apple"
				renderer = "Apple GPU"
				break
			}
		}
	}
	if vendor == "" {
		return GPUInfo{Detected: false, Source: "system_profiler"}
	}
	if renderer == "" {
		renderer = vendor + " GPU"
	}
	return GPUInfo{
		Vendor:   vendor,
		Renderer: renderer,
		Source:   "system_profiler",
		Detected: true,
	}
}

// detectGPULinux detects GPU on Linux via lspci
// (spec L4091: lspci | grep VGA).
func detectGPULinux() GPUInfo {
	cmd := exec.Command("lspci")
	output, err := cmd.Output()
	if err != nil {
		return GPUInfo{Detected: false, Source: "lspci"}
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "VGA compatible controller:") {
			// Extract GPU name after "VGA compatible controller: "
			parts := strings.SplitN(line, "VGA compatible controller:", 2)
			if len(parts) == 2 {
				gpuName := strings.TrimSpace(parts[1])
				return GPUInfo{
					Vendor:   extractVendor(gpuName),
					Renderer: gpuName,
					Source:   "lspci",
					Detected: true,
				}
			}
		}
	}
	return GPUInfo{Detected: false, Source: "lspci"}
}

// extractVendor extracts the GPU vendor from a GPU name string.
func extractVendor(gpuName string) string {
	name := strings.ToLower(gpuName)
	if strings.Contains(name, "nvidia") {
		return "NVIDIA"
	}
	if strings.Contains(name, "amd") || strings.Contains(name, "radeon") {
		return "AMD"
	}
	if strings.Contains(name, "intel") {
		return "Intel"
	}
	return "Unknown"
}

// MeasureAndOverride performs the MEASURE-FIRST pattern: detect the
// real GPU, then override SwiftShader if detected
// (spec L4091: MEASURE-FIRST. Override headless "SwiftShader" with
// REAL GPU name. Fallback: GPU undetectable -> DON'T spoof).
func (w *WebGLOverride) MeasureAndOverride() bool {
	if w == nil {
		return false
	}
	gpu := DetectGPU()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gpu = gpu
	if !gpu.Detected {
		w.enabled = false
		w.consistencyChecked = true
		w.consistencyResult = ConsistencyResult{
			Status: ConsistencyUndetectable,
			Reason: "GPU undetectable; override disabled (honest > fake)",
		}
		return false
	}
	result := checkConsistency(gpu)
	w.consistencyResult = result
	w.consistencyChecked = true
	if result.Status != ConsistencyValid {
		w.enabled = false
		return false
	}
	w.enabled = true
	return true
}

// checkConsistency validates the detected GPU against the build-time
// lookup table (spec L4091: Consistency check: WebGL extensions must
// match GPU (lookup table, build-time). Mismatch -> disable + warn).
// Unknown GPUs fail closed.
func checkConsistency(gpu GPUInfo) ConsistencyResult {
	if !gpu.Detected {
		return ConsistencyResult{
			Status: ConsistencyUndetectable,
			Reason: "GPU not detected",
		}
	}
	vendor := strings.ToLower(gpu.Vendor)
	renderer := strings.ToLower(gpu.Renderer)
	for _, family := range gpuLookupTable {
		if !strings.Contains(vendor, family.VendorPattern) && !strings.Contains(renderer, family.VendorPattern) {
			continue
		}
		if strings.Contains(renderer, family.RendererPattern) {
			return ConsistencyResult{
				Status: ConsistencyValid,
				Reason: fmt.Sprintf("GPU %q matches known family %q (WebGL vendor: %s)", gpu.Renderer, family.RendererPattern, family.WebGLVendor),
			}
		}
		return ConsistencyResult{
			Status: ConsistencyMismatch,
			Reason: fmt.Sprintf("vendor %q matches family %q but renderer %q does not match expected pattern %q", gpu.Vendor, family.VendorPattern, gpu.Renderer, family.RendererPattern),
		}
	}
	return ConsistencyResult{
		Status: ConsistencyUnknownGPU,
		Reason: fmt.Sprintf("GPU %q (vendor %q) not in build-time lookup table; fail closed", gpu.Renderer, gpu.Vendor),
	}
}

// ExpectedWebGLVendor returns the WebGL vendor string for the detected
// GPU based on the lookup table, or empty if unknown.
func ExpectedWebGLVendor(gpu GPUInfo) string {
	vendor := strings.ToLower(gpu.Vendor)
	renderer := strings.ToLower(gpu.Renderer)
	for _, family := range gpuLookupTable {
		if !strings.Contains(vendor, family.VendorPattern) && !strings.Contains(renderer, family.VendorPattern) {
			continue
		}
		if strings.Contains(renderer, family.RendererPattern) {
			return family.WebGLVendor
		}
	}
	return ""
}

// IsEnabled reports whether the WebGL override is active.
func (w *WebGLOverride) IsEnabled() bool {
	if w == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.enabled
}

// GPU returns the detected GPU info.
func (w *WebGLOverride) GPU() GPUInfo {
	if w == nil {
		return GPUInfo{}
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.gpu
}

// IsSwiftShader reports whether the given renderer string is the
// headless SwiftShader (spec L4091: Override headless "SwiftShader").
func IsSwiftShader(renderer string) bool {
	r := strings.ToLower(renderer)
	return strings.Contains(r, "swiftshader")
}

// OverrideSwiftShader returns the real GPU renderer to replace
// SwiftShader, or empty string if no override is available
// (spec L4091).
func (w *WebGLOverride) OverrideSwiftShader(currentRenderer string) string {
	if w == nil || !w.IsEnabled() {
		return ""
	}
	if !IsSwiftShader(currentRenderer) {
		return "" // no override needed
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.gpu.Renderer
}

// ConsistencyChecked reports whether the consistency check was performed.
func (w *WebGLOverride) ConsistencyChecked() bool {
	if w == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.consistencyChecked
}

// ConsistencyOK reports whether the consistency check passed.
func (w *WebGLOverride) ConsistencyOK() bool {
	if w == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.consistencyResult.Status == ConsistencyValid
}

// ConsistencyDiagnostic returns the full consistency result for
// truthful reporting (spec L4091: honest > fake).
func (w *WebGLOverride) ConsistencyDiagnostic() ConsistencyResult {
	if w == nil {
		return ConsistencyResult{Status: ConsistencyNotChecked, Reason: "nil override"}
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.consistencyChecked {
		return ConsistencyResult{Status: ConsistencyNotChecked, Reason: "check not yet performed"}
	}
	return w.consistencyResult
}

// String returns a diagnostic summary.
func (w *WebGLOverride) String() string {
	if w == nil {
		return "WebGLOverride(nil)"
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return fmt.Sprintf("WebGLOverride{enabled:%v gpu:%s/%s source:%s detected:%v consistency:%s reason:%q}",
		w.enabled, w.gpu.Vendor, w.gpu.Renderer, w.gpu.Source, w.gpu.Detected, w.consistencyResult.Status, w.consistencyResult.Reason)
}
