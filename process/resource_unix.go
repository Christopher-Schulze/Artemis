//go:build darwin || linux

package process

func platformResourceDefaults() ResourceBudget {
	return ResourceBudget{
		MaxCPUPercent:       DefaultMaxCPUPercent,
		MaxMemoryBytes:      DefaultMaxMemoryBytes,
		MaxProfileDiskBytes: DefaultMaxProfileDiskBytes,
	}
}
