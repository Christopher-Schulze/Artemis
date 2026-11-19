//go:build darwin

package process

import (
	"bytes"
	"fmt"

	"golang.org/x/sys/unix"
)

// processStartID identifies one process incarnation: its start time, which a
// later process reusing the pid does not share.
func processStartID(pid int) (string, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", fmt.Errorf("process %d start: %w", pid, err)
	}
	start := info.Proc.P_starttime
	if start.Sec == 0 && start.Usec == 0 {
		return "", fmt.Errorf("process %d start: %w", pid, errProcessGone)
	}
	return fmt.Sprintf("%d.%06d", start.Sec, start.Usec), nil
}

// processEnvironContains reports whether the process started with entry in
// its environment; kern.procargs2 holds the initial argv and environment of
// the user's own processes. A process whose arguments are withheld, such as a
// platform binary, does not prove anything.
func processEnvironContains(pid int, entry string) (bool, error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return false, fmt.Errorf("process %d environment: %w", pid, err)
	}
	for _, value := range bytes.Split(raw, []byte{0}) {
		if string(value) == entry {
			return true, nil
		}
	}
	return false, nil
}
