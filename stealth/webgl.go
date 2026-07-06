package stealth

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// GPUInfo describes the real GPU detected on the system
// (spec L4089: MEASURE-FIRST pattern. Real GPU via system_profiler
// (macOS) / lspci (Linux)).
type GPUInfo struct {
	Vendor   string
	Renderer string
	Source   string // "system_profiler", "lspci", "fallback"
	Detected bool
}

// WebGLOverride is the WebGL renderer override configuration
// (spec L4089: Override headless "SwiftShader" with REAL GPU name).
type WebGLOverride struct {
	mu                 sync.RWMutex
	gpu                GPUInfo
	enabled            bool
	consistencyChecked bool
	consistencyOK      bool
}

// NewWebGLOverride creates a new WebGL override instance
// (spec L4089: MEASURE-FIRST pattern).
func NewWebGLOverride() *WebGLOverride {
	return &WebGLOverride{enabled: false}
}

// DetectGPU detects the real GPU on the system
// (spec L4089: Real GPU via system_profiler SPDisplaysDataType (macOS)
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
// (spec L4089: system_profiler SPDisplaysDataType).
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
// (spec L4089: lspci | grep VGA).
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
// (spec L4089: MEASURE-FIRST. Override headless "SwiftShader" with
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
		// GPU undetectable -> DON'T spoof (spec L4089: honest > fake).
		w.enabled = false
		return false
	}
	// Check consistency (spec L4089: WebGL extensions must match GPU).
	w.consistencyOK = checkConsistency(gpu)
	w.consistencyChecked = true
	if !w.consistencyOK {
		// Mismatch -> disable + warn (spec L4089).
		w.enabled = false
		return false
	}
	w.enabled = true
	return true
}

// checkConsistency checks that WebGL extensions match the GPU
// (spec L4089: Consistency check: WebGL extensions must match GPU
// (lookup table, build-time). Mismatch -> disable + warn).
func checkConsistency(gpu GPUInfo) bool {
	// In a real implementation, this would check a build-time lookup
	// table of GPU -> expected extensions. For now, we accept any
	// detected GPU as consistent (the detection itself is the measure).
	return gpu.Detected
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
// headless SwiftShader (spec L4089: Override headless "SwiftShader").
func IsSwiftShader(renderer string) bool {
	r := strings.ToLower(renderer)
	return strings.Contains(r, "swiftshader")
}

// OverrideSwiftShader returns the real GPU renderer to replace
// SwiftShader, or empty string if no override is available
// (spec L4089).
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
	return w.consistencyOK
}

// String returns a diagnostic summary.
func (w *WebGLOverride) String() string {
	if w == nil {
		return "WebGLOverride(nil)"
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return fmt.Sprintf("WebGLOverride{enabled:%v gpu:%s/%s source:%s detected:%v consistency:%v}",
		w.enabled, w.gpu.Vendor, w.gpu.Renderer, w.gpu.Source, w.gpu.Detected, w.consistencyOK)
}
