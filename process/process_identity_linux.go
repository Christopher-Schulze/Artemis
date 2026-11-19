//go:build linux

package process

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// processStartID identifies one process incarnation: its start time in clock
// ticks since boot (field 22 of /proc/<pid>/stat), which a later process
// reusing the pid does not share.
func processStartID(pid int) (string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat") // #nosec G304 -- fixed procfs path of a numeric pid.
	if err != nil {
		return "", fmt.Errorf("process %d start: %w", pid, err)
	}
	// The command name may contain spaces and parentheses; the fields after
	// its closing parenthesis start with field 3.
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 {
		return "", fmt.Errorf("process %d start: malformed stat", pid)
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return "", fmt.Errorf("process %d start: malformed stat", pid)
	}
	return fields[19], nil
}

// processEnvironContains reports whether the process started with entry in
// its environment.
func processEnvironContains(pid int, entry string) (bool, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ") // #nosec G304 -- fixed procfs path of a numeric pid.
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
