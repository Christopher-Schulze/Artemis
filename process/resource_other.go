//go:build !darwin && !linux

package process

import "fmt"

func platformResourceDefaults() ResourceBudget { return ResourceBudget{} }

func sampleProcessResources(_ int, profileDir string) (ResourceUsage, error) {
	profileBytes, err := directorySize(profileDir)
	if err != nil {
		return ResourceUsage{}, fmt.Errorf("sample profile disk: %w", err)
	}
	return ResourceUsage{ProfileDiskBytes: profileBytes}, nil
}
