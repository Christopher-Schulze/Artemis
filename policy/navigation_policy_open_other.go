//go:build !darwin && !linux

package policy

import "os"

// The descriptor's regular-file identity is validated by the shared reader.
func openNavigationPolicyFile(path string) (*os.File, error) {
	return os.Open(path)
}
