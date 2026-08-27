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

func platformResourceDefaults() ResourceBudget {
	return ResourceBudget{
		MaxCPUPercent:       DefaultMaxCPUPercent,
		MaxMemoryBytes:      DefaultMaxMemoryBytes,
		MaxProfileDiskBytes: DefaultMaxProfileDiskBytes,
	}
}

func sampleProcessResources(processGroupID int, profileDir string) (ResourceUsage, error) {
	cmd := exec.CommandContext(context.Background(), "ps", "-axo", "pgid=,%cpu=,rss=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	output, err := cmd.Output()
	if err != nil {
		return ResourceUsage{}, fmt.Errorf("sample process group: %w", err)
	}
	var usage ResourceUsage
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		pgid, parseErr := strconv.Atoi(fields[0])
		if parseErr != nil || pgid != processGroupID {
			continue
		}
		cpu, parseErr := strconv.ParseFloat(fields[1], 64)
		if parseErr != nil {
			return ResourceUsage{}, fmt.Errorf("parse process cpu %q: %w", fields[1], parseErr)
		}
		rssKiB, parseErr := strconv.ParseInt(fields[2], 10, 64)
		if parseErr != nil {
			return ResourceUsage{}, fmt.Errorf("parse process rss %q: %w", fields[2], parseErr)
		}
		usage.CPUPercent += cpu
		usage.MemoryBytes += rssKiB * 1024
	}
	profileBytes, err := directorySize(profileDir)
	if err != nil {
		return ResourceUsage{}, fmt.Errorf("sample profile disk: %w", err)
	}
	usage.ProfileDiskBytes = profileBytes
	return usage, nil
}
