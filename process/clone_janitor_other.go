//go:build !darwin

package process

// SweepOrphanCodeSignClones is a no-op on non-darwin platforms. macOS code-sign
// clones are a macOS-only artifact; other platforms never create them.
func SweepOrphanCodeSignClones() {}
