//go:build darwin || linux

package policy

import (
	"os"

	"golang.org/x/sys/unix"
)

// Nonblocking/no-follow open prevents a raced FIFO or symlink from owning Stop.
func openNavigationPolicyFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}
