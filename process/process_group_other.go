//go:build !darwin && !linux

package process

import (
	"os"
	"os/exec"
	"syscall"
)

func configureProcessGroup(_ *exec.Cmd) {}

func signalProcessGroup(process *os.Process, signal syscall.Signal) error {
	if process == nil {
		return nil
	}
	return process.Signal(signal)
}
