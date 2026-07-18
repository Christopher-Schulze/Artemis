//go:build !darwin && !linux

package process

import "os/exec"

func newProcessCommand(binaryPath, _ string, _ bool, args []string) *exec.Cmd {
	return exec.Command(binaryPath, args...)
}
