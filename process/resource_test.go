package process

import "testing"

func TestResourceBudgetEnforcesEveryMeasuredDimension(t *testing.T) {
	budget := ResourceBudget{MaxCPUPercent: 100, MaxMemoryBytes: 200, MaxProfileDiskBytes: 300}
	for _, test := range []struct {
		name  string
		usage ResourceUsage
	}{
		{name: "cpu", usage: ResourceUsage{CPUPercent: 100.01}},
		{name: "memory", usage: ResourceUsage{MemoryBytes: 201}},
		{name: "profile", usage: ResourceUsage{ProfileDiskBytes: 301}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := budget.exceeded(test.usage); err == nil {
				t.Fatalf("usage %+v accepted", test.usage)
			}
		})
	}
	if err := budget.exceeded(ResourceUsage{CPUPercent: 100, MemoryBytes: 200, ProfileDiskBytes: 300}); err != nil {
		t.Fatalf("exact limits rejected: %v", err)
	}
}
