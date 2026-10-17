package platform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCgroupVisibleAncestorsConstrainActualFiles(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range []string{"", "team", "team/worker"} {
				if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
					t.Fatal(err)
				}
			}
			maxName, currentName, cpuName := "memory.max", "memory.current", "cpu.max"
			membership := "0::/team/worker\n"
			mount := fmt.Sprintf("29 1 0:2 / %s rw - cgroup2 cgroup rw\n", root)
			if version == 1 {
				maxName, currentName, cpuName = "memory.limit_in_bytes", "memory.usage_in_bytes", "cpu.cfs_quota_us"
				membership = "4:memory,cpu:/team/worker\n"
				mount = fmt.Sprintf("29 1 0:2 / %s rw - cgroup cgroup rw,memory,cpu\n", root)
			}
			for _, item := range []struct{ dir, limit, used, cpu string }{
				{"", "1000", "500", "300000"}, {"team", "300", "250", "150000"}, {"team/worker", "800", "100", "400000"},
			} {
				writeLimitFixture(t, root, item.dir, maxName, item.limit)
				writeLimitFixture(t, root, item.dir, currentName, item.used)
				cpu := item.cpu
				if version == 2 {
					cpu += " 100000"
				} else {
					writeLimitFixture(t, root, item.dir, "cpu.cfs_period_us", "100000")
				}
				writeLimitFixture(t, root, item.dir, cpuName, cpu)
			}
			result := observeCgroupLimits(t.Context(), []byte(membership), []byte(mount), readProbeFile)
			if result.MemoryCeiling.State != StateAvailable || result.MemoryCeiling.Value != 300 || result.MemoryHeadroom.Value != 50 || result.CPUQuota.Value != 2 {
				t.Fatalf("ancestor constraints lost: %+v", result)
			}
			writeLimitFixture(t, root, "team", currentName, "400")
			result = observeCgroupLimits(t.Context(), []byte(membership), []byte(mount), readProbeFile)
			if result.MemoryHeadroom.State != StateAvailable || result.MemoryHeadroom.Value != 0 {
				t.Fatalf("exhaustion wrapped or became unknown: %+v", result)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			result = observeCgroupLimits(ctx, []byte(membership), []byte(mount), readProbeFile)
			if result.MemoryCeiling.State != StateUnknown || result.CPUQuota.State != StateUnknown {
				t.Fatal("canceled observation invented capacity")
			}
		})
	}
}

func writeLimitFixture(t *testing.T, root, dir, name, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, dir, name), []byte(value+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCgroupPathsAndQuotaRejectUnobservableBounds(t *testing.T) {
	for _, value := range []string{"../outside", "/../../outside", "/safe/../outside", "/safe\\777path", "/safe\x00path"} {
		if _, err := cgroupPath(value); err == nil {
			t.Fatalf("unsafe path admitted: %q", value)
		}
	}
	if got, err := cgroupPath(`/safe\040directory`); err != nil || got != "/safe directory" {
		t.Fatalf("kernel escaped path: %q %v", got, err)
	}
	for _, value := range []string{"0 100000", "1 0", "max 0", "1", "-2 100000", "18446744073709551616 1"} {
		if _, _, err := parseCPUCeiling([]byte(value), 2); err == nil {
			t.Fatalf("bad quota admitted: %q", value)
		}
	}
	if _, _, err := parseCPUCeiling([]byte("-1 100000"), 2); err == nil {
		t.Fatal("v1 unlimited sentinel accepted as v2 quota")
	}
	if _, unlimited, err := parseCPUCeiling([]byte("-1 100000"), 1); err != nil || !unlimited {
		t.Fatal("v1 unlimited quota rejected")
	}
	location, err := cgroupLocationFor([]byte("0::/team/worker"), []byte("1 0 0:1 /team /mounted\\040cg rw - cgroup2 cgroup rw"), "memory")
	if err != nil || location.leaf != "/mounted cg/worker" {
		t.Fatalf("mount root projection: %+v %v", location, err)
	}
	if _, err := cgroupLocationFor([]byte("0::/teamX/worker"), []byte("1 0 0:1 /team /mounted rw - cgroup2 cgroup rw"), "memory"); err == nil {
		t.Fatal("partial root prefix crossed cgroup boundary")
	}
	result := observeCgroupLimits(t.Context(), []byte("0::/"), nil, readProbeFile)
	if result.MemoryCeiling.State != StateUnknown || result.MemoryCeiling.Value != 0 {
		t.Fatal("hidden controller became unlimited capacity")
	}
}

func TestResourceConstraintsOnlyNarrowCanonicalCeilings(t *testing.T) {
	caps := PlatformCapabilities{NumCPU: 8, TotalMemoryMB: 4, Evidence: CapabilityEvidence{CPU: ProbeResult{State: StateAvailable}, Memory: ProbeResult{State: StateAvailable, Value: 4 << 20}}}
	for _, tc := range []struct {
		name              string
		limits            ResourceLimits
		cpu               int
		memory, transient int64
	}{
		{name: "physical", cpu: 8, memory: 4 << 20, transient: 4 << 20},
		{name: "cpuset and container", limits: ResourceLimits{AllowedCPUs: ProbeResult{State: StateAvailable, Value: 3}, CPUQuota: ProbeResult{State: StateAvailable, Value: 2}, MemoryCeiling: ProbeResult{State: StateAvailable, Value: 2 << 20}, MemoryHeadroom: ProbeResult{State: StateAvailable, Value: 1 << 20}}, cpu: 2, memory: 2 << 20, transient: 1 << 20},
		{name: "unknown", limits: unknownResourceLimits(fmt.Errorf("denied")), cpu: 1, memory: 4 << 20, transient: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cpu, memory, transient, err := ConstrainResources(caps, tc.limits, 16, 8<<20)
			if err != nil || cpu != tc.cpu || memory != tc.memory || transient != tc.transient {
				t.Fatalf("resolved %d/%d/%d %v", cpu, memory, transient, err)
			}
		})
	}
	if _, _, _, err := ConstrainResources(caps, ResourceLimits{}, 0, 1); err == nil {
		t.Fatal("invalid canonical ceiling admitted")
	}
	if ObserveResourceLimits(nil).MemoryCeiling.State != StateUnknown {
		t.Fatal("nil context invented capacity")
	}
	caps.IsContainerized = true
	if _, _, transient, err := ConstrainResources(caps, ResourceLimits{MemoryCeiling: ProbeResult{State: StateUnavailable}}, 8, 8<<20); err != nil || transient != 0 {
		t.Fatal("unobservable container boundary admitted transient capacity")
	}
}
