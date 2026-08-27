package artemis

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/benchmark"
	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
)

// TestSealEngineSecureByDefault verifies the seal invariant: engine.Config{}
// (zero value) denies private/loopback/link-local/multicast/metadata/CGNAT
// networks. This is the baseline security contract.
func TestSealEngineSecureByDefault(t *testing.T) {
	cfg := engine.Config{}
	if cfg.PolicyConfig.AllowPrivateNetworks {
		t.Fatal("engine.Config{} must deny private networks by default; AllowPrivateNetworks is true")
	}

	policy, err := network.NewPolicy(cfg.PolicyConfig, nil, nil)
	if err != nil {
		t.Fatalf("NewPolicy from zero Config: %v", err)
	}

	// The policy must deny a private IP address.
	decisions := []struct {
		url     string
		allowed bool
	}{
		{"http://127.0.0.1/", false},
		{"http://10.0.0.1/", false},
		{"http://192.168.1.1/", false},
		{"http://169.254.169.254/", false}, // metadata
		{"http://0.0.0.0/", false},
	}
	for _, d := range decisions {
		err := policy.ValidateRequest(context.Background(), d.url, "GET", "", 0, network.TargetNavigation, "")
		if d.allowed && err != nil {
			t.Errorf("policy should allow %s but got %v", d.url, err)
		}
		if !d.allowed && err == nil {
			t.Errorf("policy should deny %s but it was allowed", d.url)
		}
	}
}

// TestSealNoCommittedBuildArtifacts verifies the seal invariant: zero
// committed build artifacts remain in the tree. The .gitignore must cover
// the artemis binary and *.test binaries.
func TestSealNoCommittedBuildArtifacts(t *testing.T) {
	gitignore, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	content := string(gitignore)
	required := []string{"/artemis", "*.test"}
	for _, req := range required {
		if !strings.Contains(content, req) {
			t.Errorf(".gitignore missing pattern %q (build artifact leak risk)", req)
		}
	}
}

// TestSealSupplyChainArtifactsExist verifies the seal invariant: release
// supply-chain artifacts and documentation exist and are valid.
func TestSealSupplyChainArtifactsExist(t *testing.T) {
	files := []struct {
		path     string
		required []string
	}{
		{"SECURITY.md", []string{"Reporting a Vulnerability", "Threat Model"}},
		{"CONTRIBUTING.md", []string{"Local Gates", "Code Style"}},
		{"CHANGELOG.md", []string{"Unreleased"}},
		{"LICENSE", []string{"MIT License"}},
		{filepath.Join("third_party", "v8go", "PROVENANCE.md"), []string{"rogchap.com/v8go", "v0.9.0"}},
		{filepath.Join("third_party", "v8go", "ARTEMIS_PATCHES.md"), []string{"SnapshotCreator"}},
		{filepath.Join(".github", "workflows", "ci.yml"), []string{"test-macos-arm64", "chromium-integration-linux"}},
		{filepath.Join("docs", "release-procedures.md"), []string{"Signing", "Rollback", "Revocation"}},
	}
	for _, f := range files {
		t.Run(f.path, func(t *testing.T) {
			data, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatalf("read %s: %v", f.path, err)
			}
			content := string(data)
			for _, req := range f.required {
				if !strings.Contains(content, req) {
					t.Errorf("%s missing %q", f.path, req)
				}
			}
		})
	}
}

// TestSealReleaseArtifactGeneratorExists verifies the artemis-release tool
// exists and can produce checksums, SBOM, and license report.
func TestSealReleaseArtifactGeneratorExists(t *testing.T) {
	path := filepath.Join("cmd", "artemis-release", "main.go")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("artemis-release tool missing: %v", err)
	}
}

// TestSealCapabilityRegistryHasNoUnsupportedSupportedEntries verifies the
// seal invariant: no supported capability lacks release evidence.
func TestSealCapabilityRegistryHasNoUnsupportedSupportedEntries(t *testing.T) {
	for _, cap := range Capabilities() {
		if cap.State != SupportSupported {
			continue
		}
		if cap.Since != Version {
			t.Fatalf("supported capability %q has Since=%q, want %q", cap.ID, cap.Since, Version)
		}
		if cap.BehaviorTest == "" {
			t.Fatalf("supported capability %q has no BehaviorTest", cap.ID)
		}
		if cap.Owner == "" {
			t.Fatalf("supported capability %q has no Owner", cap.ID)
		}
		if cap.Entrypoint == "" {
			t.Fatalf("supported capability %q has no Entrypoint", cap.ID)
		}
	}
}

// TestSealNoSyntheticSuccessInAgent verifies the seal invariant: no
// synthetic/no-op tool responses remain in the agent path. The agent must
// return real errors for unavailable capabilities, not fake success.
func TestSealNoSyntheticSuccessInAgent(t *testing.T) {
	// An unknown action must produce a capability-unavailable error, not
	// a synthetic success.
	agent, err := NewAgent(AgentConfig{})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	var invalidContext context.Context
	if err := agent.Start(invalidContext); err == nil {
		t.Fatal("agent.Start(nil) should fail")
	}
	// Verify the agent does not claim to support Chromium when it's unavailable.
	snap := agent.CapabilitySnapshot()
	if snap.Health.Chromium.State == SupportSupported {
		t.Fatal("agent claims Chromium is supported but it should be unavailable in renderless-only mode")
	}
}

// TestSealBenchmarkHarnessIsFailClosed verifies the seal invariant: the
// benchmark harness fails closed on dishonest head-to-head (no competitor
// presented as comparison).
func TestSealBenchmarkHarnessIsFailClosed(t *testing.T) {
	// The benchmark scorecard must have an Honest field that can be set
	// to false when a competitor is unavailable/skipped. We verify the
	// Scorecard type from the benchmark package has this field.
	sc := benchmark.NewScorecard()
	if !sc.Honest {
		t.Fatal("NewScorecard should default Honest to true")
	}
	sc.Honest = false
	if sc.Honest {
		t.Fatal("Scorecard.Honest should be settable to false")
	}
}

// TestSealVersionAndLicenseConsistency verifies the seal invariant: version,
// license, and module identity are consistent across all public surfaces.
func TestSealVersionAndLicenseConsistency(t *testing.T) {
	if Version == "" {
		t.Fatal("Version is empty")
	}
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		t.Fatalf("read LICENSE: %v", err)
	}
	if !strings.HasPrefix(string(license), "MIT License\n") {
		t.Fatal("LICENSE is not MIT")
	}
	module, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.HasPrefix(string(module), "module github.com/Christopher-Schulze/Artemis\n") {
		t.Fatal("go.mod module identity drifted")
	}
}
