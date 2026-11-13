package stealth

// launch.go (stealth/launch.go - Chrome launch flags).
//
// This file is the spec-mandated facade for Chrome launch flag
// management. The implementation lives in launch_flags.go; this file
// re-exports the key types and functions under the spec-mandated
// file name.
//
// Anti-detection: Chrome launch flags including --disable-automation,
// stealth args, WebRTC leak prevention, and canvas noise.

// LaunchConfig is the spec-mandated name for the launch flags
// configuration (launch.go). It aliases LaunchFlags.
type LaunchConfig = LaunchFlags

// DefaultLaunchConfig returns the default Chrome launch configuration
// (launch.go - Chrome launch flags).
func DefaultLaunchConfig() LaunchConfig {
	return DefaultLaunchFlags()
}

// StealthLaunchConfig returns the stealth launch configuration for the
// given level (launch.go - Chrome launch flags).
func StealthLaunchConfig(level StealthLevel) LaunchConfig {
	return StealthLaunchFlags(level)
}

// STEALTH_ARGS is the spec-mandated name for the stealth launch args
// (launch.go - Chrome launch flags).
var STEALTH_ARGS = stealthArgs
