//go:build !darwin && !linux

package process

import "context"

// recordLeaseBrowserGroup is a no-op where no profile lease recovery exists.
func recordLeaseBrowserGroup(string, int) error { return nil }

// RecoverOrphanedProfile is a no-op where no profile lease recovery exists.
func RecoverOrphanedProfile(context.Context, string) error { return nil }
