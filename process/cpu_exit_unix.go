//go:build darwin || linux

package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func processGroupCPUStopped(ctx context.Context, group int) (bool, error) {
	if group <= 0 {
		return false, fmt.Errorf("positive owned process group required")
	}
	output := newCappedOutput(1 << 20)
	command := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,pgid=,stat=")
	command.Env = append(os.Environ(), "LC_ALL=C")
	command.Stdout, command.Stderr = output, newCappedOutput(4096)
	if err := command.Run(); err != nil {
		return false, fmt.Errorf("observe CPU owner exit: %w", err)
	}
	if output.Truncated() {
		return false, fmt.Errorf("CPU owner exit observation exceeded bound")
	}
	return parseProcessGroupCPUStopped(output.String(), group)
}

func parseProcessGroupCPUStopped(output string, group int) (bool, error) {
	rows := 0
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 {
			return false, fmt.Errorf("malformed CPU owner process observation")
		}
		rows++
		pid, pidErr := strconv.Atoi(fields[0])
		pgid, groupErr := strconv.Atoi(fields[1])
		if pidErr != nil || groupErr != nil || pid < 0 || pgid < 0 {
			return false, fmt.Errorf("invalid CPU owner process identity")
		}
		if pgid != group {
			continue
		}
		// A zombie/dead process cannot resume, run threads or fork new producers.
		if fields[2][0] != 'Z' && fields[2][0] != 'X' && fields[2][0] != 'x' {
			return false, nil
		}
	}
	if rows == 0 {
		return false, fmt.Errorf("empty CPU owner process observation")
	}
	return true, nil
}
