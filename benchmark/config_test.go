package benchmark

import (
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCanonicalOptions(t *testing.T) {
	dir := t.TempDir()
	options, err := ParseOptions([]string{
		"--skip-competitor=false", "--require-head-to-head", "--iterations=7",
		"--output=" + dir, "--benchmark-tag=measured", "--timeout=42s", "--format=summary",
		"--cpu-profile=" + filepath.Join(dir, "cpu.pprof"), "--mem-profile=" + filepath.Join(dir, "heap.pprof"),
		"--competitor-dir=" + filepath.Join(dir, "bin"), "--competitor-sha256=" + strings.Repeat("a", 64),
		"--download-url=https://example.com/pinned", "--regression-budget=wall_ms:10:0.05",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.Config.Iterations != 7 || options.Config.SkipCompetitor || !options.Config.RequireHeadToHead ||
		options.Config.Timeout != 42*time.Second || options.Config.BenchmarkTag != "measured" || options.Format != "summary" {
		t.Fatalf("options lost: %+v", options)
	}
	if len(options.Config.RegressionBudgets) != 1 || options.Config.RegressionBudgets[0].MaxPercent != 0.05 ||
		options.Config.Competitor.ExpectedSHA256 != strings.Repeat("a", 64) || options.Config.Profile.MemProfilePath == "" {
		t.Fatalf("provenance, budget or profile lost: %+v", options)
	}
}

func TestCanonicalOptionsRejectInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"zero iterations", []string{"--iterations=0"}},
		{"negative iterations", []string{"--iterations=-1"}},
		{"excessive iterations", []string{"--iterations=1001"}},
		{"zero timeout", []string{"--timeout=0s"}},
		{"excessive timeout", []string{"--timeout=25h"}},
		{"empty output", []string{"--output="}},
		{"contradictory modes", []string{"--require-head-to-head"}},
		{"unverified download", []string{"--download-url=https://example.com/binary"}},
		{"malformed digest", []string{"--competitor-sha256=abc"}},
		{"unrecognized format", []string{"--format=xml"}},
		{"positional", []string{"stray"}},
		{"unknown option", []string{"--unknown"}},
		{"unknown metric", []string{"--skip-competitor=false", "--regression-budget=unknown:1:0"}},
		{"NaN budget", []string{"--skip-competitor=false", "--regression-budget=wall_ms:NaN:0"}},
		{"negative budget", []string{"--skip-competitor=false", "--regression-budget=wall_ms:-1:0"}},
		{"infinite budget", []string{"--skip-competitor=false", "--regression-budget=wall_ms:0:Inf"}},
		{"zero budget", []string{"--skip-competitor=false", "--regression-budget=wall_ms:0:0"}},
		{"missing baseline", []string{"--regression-budget=wall_ms:1:0"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--output=" + t.TempDir()}, tc.args...)
			if _, err := ParseOptions(args, io.Discard); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestHarnessPreservesCallerConfiguration(t *testing.T) {
	cfg := HarnessConfig{Iterations: 9, SkipCompetitor: true, BenchmarkTag: "caller", Competitor: CompetitorConfig{ExpectedSHA256: strings.Repeat("b", 64)}}
	h := NewHarness(cfg)
	if h.cfg.OutputDir != DefaultHarnessConfig().OutputDir || h.cfg.Timeout != DefaultTimeout {
		t.Fatalf("missing field defaults: %+v", h.cfg)
	}
	h.cfg.OutputDir, h.cfg.Timeout = cfg.OutputDir, cfg.Timeout
	if !reflect.DeepEqual(h.cfg, cfg) {
		t.Fatalf("caller configuration replaced: %+v", h.cfg)
	}
	for _, count := range []int{0, -1} {
		if NewHarness(HarnessConfig{Iterations: count}).cfg.Iterations != count {
			t.Fatal("invalid count was silently normalized")
		}
	}
}

func TestWorkloadValidationBeforeSideEffects(t *testing.T) {
	cases := []struct {
		name string
		edit func(*HarnessConfig)
	}{
		{"unknown scenario", func(c *HarnessConfig) { c.Workloads[0].ScenarioID = "missing" }},
		{"unknown mode", func(c *HarnessConfig) { c.Workloads[0].EngineMode = "made-up" }},
		{"unsupported chromium", func(c *HarnessConfig) { c.Workloads[0].EngineMode = ModeChromium }},
		{"unsupported hybrid", func(c *HarnessConfig) { c.Workloads[0].EngineMode = ModeHybrid }},
		{"unsupported network", func(c *HarnessConfig) { c.Workloads[0].Locality = LocalityNetwork }},
		{"unknown warmth", func(c *HarnessConfig) { c.Workloads[0].Warmth = "lukewarm" }},
		{"invalid samples", func(c *HarnessConfig) { c.Workloads[0].Iterations = 0 }},
		{"duplicate workload", func(c *HarnessConfig) { c.Workloads = append(c.Workloads, c.Workloads[0]) }},
		{"NaN budget", func(c *HarnessConfig) {
			c.RegressionBudgets = []RegressionBudget{{MetricKind: MetricWallMs, MaxDelta: math.NaN()}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			cfg := DefaultHarnessConfig()
			cfg.OutputDir = filepath.Join(parent, "not-created")
			cfg.Profile.CPUProfilePath = filepath.Join(cfg.OutputDir, "cpu.pprof")
			cfg.Workloads = WorkloadsWithIterations(DefaultScenarios()[:1], 2)
			tc.edit(&cfg)
			if _, err := NewHarness(cfg).Run(context.Background()); err == nil {
				t.Fatal("invalid workload accepted")
			}
			if _, err := os.Stat(cfg.OutputDir); !os.IsNotExist(err) {
				t.Fatalf("invalid input caused side effects: %v", err)
			}
		})
	}
}

func TestArtifactPathsPreserveExistingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scorecard.json")
	original := []byte("operator-owned evidence")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultHarnessConfig()
	cfg.OutputDir = dir
	if _, err := NewHarness(cfg).Run(context.Background()); err == nil {
		t.Fatal("existing evidence accepted as output")
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(original) {
		t.Fatalf("evidence changed: %q, %v", actual, err)
	}
}

func TestProfilePathConfinement(t *testing.T) {
	cases := []struct {
		name string
		edit func(*HarnessConfig, string)
	}{
		{"outside output", func(c *HarnessConfig, parent string) {
			c.Profile.CPUProfilePath = filepath.Join(parent, "outside.pprof")
		}},
		{"artifact collision", func(c *HarnessConfig, parent string) {
			c.Profile.CPUProfilePath = filepath.Join(c.OutputDir, "scorecard.json")
		}},
		{"profile collision", func(c *HarnessConfig, parent string) {
			c.Profile.CPUProfilePath = filepath.Join(c.OutputDir, "profile.pprof")
			c.Profile.MemProfilePath = c.Profile.CPUProfilePath
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			cfg := DefaultHarnessConfig()
			cfg.OutputDir = filepath.Join(parent, "output")
			tc.edit(&cfg, parent)
			if err := ValidateConfig(cfg, DefaultScenarios()); err == nil {
				t.Fatal("unconfined or colliding path accepted")
			}
		})
	}
	parent := t.TempDir()
	linked := filepath.Join(parent, "linked")
	if err := os.Symlink(t.TempDir(), linked); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultHarnessConfig()
	cfg.OutputDir = linked
	if err := ValidateConfig(cfg, DefaultScenarios()); err == nil {
		t.Fatal("symlinked output accepted")
	}
}

func TestWorkloadSamplesAndIdentity(t *testing.T) {
	for _, count := range []int{1, 7, 13} {
		workloads := WorkloadsWithIterations(DefaultScenarios(), count)
		if len(workloads) != 2*len(DefaultScenarios()) {
			t.Fatalf("matrix size = %d", len(workloads))
		}
		for _, workload := range workloads {
			if workload.Iterations != count || !workload.IsValid() {
				t.Fatalf("sample count lost: %+v", workload)
			}
			other := workload
			other.Iterations++
			if other.Identity() == workload.Identity() {
				t.Fatal("sample policy missing from identity")
			}
		}
	}
	scenario := Scenario{ID: "explicit", EngineMode: ModeChromium}
	if NewWorkload(scenario, ModeRenderless, LocalityLocal, WarmthCold, 3).EngineMode != ModeRenderless {
		t.Fatal("explicit engine mode overwritten by scenario")
	}
}
