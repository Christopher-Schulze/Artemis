//go:build darwin || linux

package process

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
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
	// Darwin reports EPERM instead of ESRCH for an owned group whose members
	// are all zombies (the leader exited but is not reaped yet); any live
	// member would have been signaled. Nothing remains to signal.
	if runtime.GOOS == "darwin" && errors.Is(err, syscall.EPERM) {
		return nil
	}
	return err
}
