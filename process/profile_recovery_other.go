//go:build !darwin && !linux

package process

import "context"

// recordLeaseBrowserGroup is a no-op where no profile lease recovery exists.
func recordLeaseBrowserGroup(string, int) error { return nil }

// RecoverOrphanedProfile is a no-op where no profile lease recovery exists.
func RecoverOrphanedProfile(context.Context, string) error { return nil }

// processStartID has no process identity source here; leases record none.
func processStartID(int) (string, error) { return "", nil }
