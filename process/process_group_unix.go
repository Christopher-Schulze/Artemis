//go:build darwin || linux

package process

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// ConfigureProcessGroup isolates an owned command and its descendants.
func ConfigureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// SignalProcessGroup signals only the group created for an owned command.
func SignalProcessGroup(process *os.Process, signal syscall.Signal) error {
	if process == nil {
		return nil
	}
	err := syscall.Kill(-process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
