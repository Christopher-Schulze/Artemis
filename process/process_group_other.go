//go:build !darwin && !linux

package process

import (
	"os"
	"os/exec"
	"syscall"
)

// ProcessGroupAlive is false where no owned process group exists.
func ProcessGroupAlive(*os.Process) bool { return false }

// ConfigureProcessGroup retains the platform's direct-process semantics.
func ConfigureProcessGroup(_ *exec.Cmd) {}

// SignalProcessGroup signals the owned process on platforms without groups.
func SignalProcessGroup(process *os.Process, signal syscall.Signal) error {
	if process == nil {
		return nil
	}
	return process.Signal(signal)
}
