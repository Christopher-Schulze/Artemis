//go:build unix

package playwright

import "golang.org/x/sys/unix"

// availableBytes reports the bytes an unprivileged writer may still use on
// the volume holding dir.
func availableBytes(dir string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}
