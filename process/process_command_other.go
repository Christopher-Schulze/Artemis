//go:build !darwin && !linux

package process

import (
	"context"
	"os/exec"
)

func newProcessCommand(ctx context.Context, binaryPath, _ string, _ bool, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, binaryPath, args...)
}
