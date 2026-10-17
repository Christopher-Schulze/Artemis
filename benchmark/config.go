package benchmark

import (
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout bounds a complete benchmark invocation.
const DefaultTimeout = 5 * time.Minute

// ValidateConfig rejects unsupported execution paths before starting engines,
// profiling, downloads or artifact writes.
func ValidateConfig(cfg HarnessConfig, scenarios []Scenario) error {
	if cfg.Iterations < 1 || cfg.Iterations > 1000 {
		return fmt.Errorf("iterations must be between 1 and 1000")
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 24*time.Hour {
		return fmt.Errorf("timeout must be positive and at most 24h")
	}
	if cfg.SkipCompetitor && cfg.RequireHeadToHead {
		return fmt.Errorf("require-head-to-head cannot be combined with skip-competitor")
	}
	if cfg.OutputFormat != "" && cfg.OutputFormat != "json" && cfg.OutputFormat != "summary" {
		return fmt.Errorf("output format must be json or summary")
	}
	if len(cfg.BenchmarkTag) > 128 || strings.ContainsAny(cfg.BenchmarkTag, "\r\n\x00|") {
		return fmt.Errorf("benchmark-tag must be a single line of at most 128 bytes without table delimiters")
	}
	if err := validateArtifactPaths(cfg); err != nil {
		return err
	}
	if err := validateWorkloads(cfg, scenarios); err != nil {
		return err
	}
	for _, budget := range cfg.RegressionBudgets {
		if _, err := MetricKindFromString(string(budget.MetricKind)); err != nil {
			return err
		}
		if !finiteNonnegative(budget.MaxDelta) || !finiteNonnegative(budget.MaxPercent) ||
			(budget.MaxDelta == 0 && budget.MaxPercent == 0) {
			return fmt.Errorf("regression budget %s requires a finite positive bound", budget.MetricKind)
		}
	}
	if len(cfg.RegressionBudgets) > 0 && cfg.SkipCompetitor {
		return fmt.Errorf("regression budgets require a competitor baseline")
	}
	return validateCompetitorConfig(cfg.Competitor)
}

func finiteNonnegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validateWorkloads(cfg HarnessConfig, scenarios []Scenario) error {
	workloads := cfg.Workloads
	if len(workloads) == 0 {
		workloads = WorkloadsWithIterations(scenarios, cfg.Iterations)
	}
	if len(workloads) == 0 {
		return fmt.Errorf("at least one scenario is required")
	}
	seen := make(map[string]bool, len(workloads))
	for _, workload := range workloads {
		if !workload.IsValid() || workload.Iterations > 1000 {
			return fmt.Errorf("invalid workload %q", workload.Identity())
		}
		if workload.EngineMode != ModeRenderless || workload.Locality != LocalityLocal {
			return fmt.Errorf("unsupported workload %q: harness executes only local renderless workloads", workload.Identity())
		}
		if seen[workload.Key()] {
			return fmt.Errorf("duplicate workload %q", workload.Key())
		}
		seen[workload.Key()] = true
		scenario := scenarioByID(scenarios, workload.ScenarioID)
		if scenario == nil || scenario.HTML == "" || scenario.ExpectTitle == "" {
			return fmt.Errorf("workload %q requires a known substantive scenario", workload.Identity())
		}
	}
	return nil
}

func validateCompetitorConfig(cfg CompetitorConfig) error {
	if cfg.Port < 0 || cfg.Port > 65535 || cfg.Timeout < 0 || cfg.Timeout > 24*time.Hour {
		return fmt.Errorf("invalid competitor port or startup timeout")
	}
	if cfg.MaxDownloadBytes < 0 || cfg.MaxDownloadBytes > 1024*1024*1024 {
		return fmt.Errorf("competitor download limit must be at most 1 GiB")
	}
	if cfg.ExpectedSHA256 != "" {
		digest, err := hex.DecodeString(cfg.ExpectedSHA256)
		if err != nil || len(digest) != 32 {
			return fmt.Errorf("competitor-sha256 must be a 64-character SHA256 digest")
		}
	}
	if cfg.DownloadURL == "" {
		return nil
	}
	parsed, err := url.Parse(cfg.DownloadURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("invalid competitor download URL")
	}
	ip := net.ParseIP(parsed.Hostname())
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return fmt.Errorf("competitor download requires HTTPS or an explicit loopback HTTP fixture")
	}
	if cfg.ExpectedSHA256 == "" {
		return fmt.Errorf("competitor downloads require competitor-sha256")
	}
	return nil
}

func validateArtifactPaths(cfg HarnessConfig) error {
	if strings.TrimSpace(cfg.OutputDir) == "" {
		return fmt.Errorf("output must not be empty")
	}
	root, err := filepath.Abs(cfg.OutputDir)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	paths := []string{filepath.Join(root, "scorecard.json"), filepath.Join(root, "scorecard.md"), filepath.Join(root, "report.md"), filepath.Join(root, "manifest.json")}
	for _, profile := range []string{cfg.Profile.CPUProfilePath, cfg.Profile.MemProfilePath} {
		if profile == "" {
			continue
		}
		absolute, err := filepath.Abs(profile)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, absolute)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("profile path must be confined to output directory: %s", profile)
		}
		paths = append(paths, absolute)
	}
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if seen[path] {
			return fmt.Errorf("artifact paths must be distinct: %s", path)
		}
		seen[path] = true
		if err := validateNewArtifact(path, root); err != nil {
			return err
		}
	}
	return nil
}

func validateNewArtifact(path, root string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("artifact already exists; use a fresh output directory: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("artifact ancestor must be a real directory: %s", parent)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		// System ancestors such as macOS /var may be symlinks. Confinement
		// applies to the explicitly selected root and paths inside it.
		if parent == root {
			return nil
		}
	}
}
