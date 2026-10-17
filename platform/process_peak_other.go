//go:build !darwin && !linux && !windows

package platform

func processPeakMemory() ProbeResult {
	return ProbeResult{State: StateUnavailable, Source: "process lifetime peak RSS", Reason: "this build has no process peak-memory observation backend"}
}
