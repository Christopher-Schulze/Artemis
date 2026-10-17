package artemis

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Christopher-Schulze/Artemis/playwright"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
)

// signedPlaywrightArtifact writes a complete signed lane artifact.
func signedPlaywrightArtifact(t *testing.T, dir string, key ed25519.PrivateKey) {
	t.Helper()
	manifest := playwright.Manifest{
		SchemaVersion: 1, ArtifactVersion: "1.0.0", PlaywrightCoreVersion: "1.58.0", RuntimeVersion: "24.4.0",
		AdmissionMode: playwright.AdmissionInstaller, OfflineReady: true, SessionABIVersion: playwright.SessionABIVersion,
		Browsers: []playwright.BrowserRevision{{Browser: "chromium", Revision: "1200"}}, Capabilities: []playwright.Capability{playwright.CapNavigation, playwright.CapVideo},
	}
	for name, role := range map[string]playwright.Role{"core.js": playwright.RoleCore, "node": playwright.RoleRuntime, "sbom.json": playwright.RoleSBOM, "LICENSES": playwright.RoleLicenses, "provenance.json": playwright.RoleProvenance} {
		content := []byte("content " + name)
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		manifest.Files = append(manifest.Files, playwright.File{Path: name, Role: role, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	public := key.Public().(ed25519.PublicKey)
	signature, err := json.Marshal(playwright.Signature{KeyID: playwright.KeyID(public), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, data))})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, playwright.ManifestFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, playwright.SignatureFile), signature, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAgentPlaywrightLaneProvisionsTruthfullyAndNeverSubstitutes(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := key.Public().(ed25519.PublicKey)
	agent, err := NewAgent(AgentConfig{Playwright: &PlaywrightLaneConfig{CacheDir: t.TempDir(), TrustedKeys: playwright.TrustedKeys{playwright.KeyID(public): public}, MinFreeBytes: 1, PolicyRef: "policy.browser"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if lane := agent.PlaywrightLane(ctx); lane.Availability != playwright.AvailabilityUnavailable || lane.ArtifactDigest != "" || lane.Remediation == "" {
		t.Fatalf("unprovisioned lane = %+v", lane)
	}
	artifact := t.TempDir()
	signedPlaywrightArtifact(t, artifact, key)
	lane, taskErr := agent.InstallPlaywrightArtifact(ctx, artifact)
	if taskErr != nil {
		t.Fatalf("install: %v", taskErr)
	}
	if lane.ArtifactDigest == "" || lane.Availability != playwright.AvailabilityUnavailable || lane.PolicyRef != "policy.browser" {
		t.Fatalf("provisioned lane without execution runtime = %+v", lane)
	}
	tampered := t.TempDir()
	signedPlaywrightArtifact(t, tampered, key)
	if err := os.WriteFile(filepath.Join(tampered, "node"), []byte("swapped runtime"), 0o600); err != nil {
		t.Fatal(err)
	}
	if after, taskErr := agent.InstallPlaywrightArtifact(ctx, tampered); taskErr == nil || taskErr.Code != TaskErrorPolicyDenied || after.ArtifactDigest != lane.ArtifactDigest {
		t.Fatalf("tampered install = %+v, %v", after, taskErr)
	}

	_, taskErr = agent.ResolveBrowserTool(ctx, "", artemisrouter.IntentPlaywright, nil)
	var unavailable *artemisrouter.ToolUnavailableError
	if taskErr == nil || taskErr.Code != TaskErrorCapabilityUnavailable || !errors.As(taskErr, &unavailable) || unavailable.Engine != artemisrouter.EnginePlaywright {
		t.Fatalf("explicit playwright = %v", taskErr)
	}
	if _, taskErr := agent.ResolveBrowserTool(ctx, "", artemisrouter.IntentAuto, []artemisrouter.ToolRequirement{"capability:video"}); taskErr == nil || taskErr.Code != TaskErrorCapabilityUnavailable {
		t.Fatalf("auto video without execution runtime = %v", taskErr)
	}
	decision, taskErr := agent.ResolveBrowserTool(ctx, "", artemisrouter.IntentAuto, []artemisrouter.ToolRequirement{"capability:screenshot"})
	if taskErr != nil || decision.Engine != artemisrouter.EngineArtemis {
		t.Fatalf("auto screenshot = %+v, %v", decision, taskErr)
	}
	if _, taskErr := agent.ResolveBrowserTool(ctx, "", artemisrouter.IntentAuto, []artemisrouter.ToolRequirement{"capability:telepathy"}); taskErr == nil || taskErr.Code != TaskErrorInvalidInput {
		t.Fatalf("invalid requirement = %v", taskErr)
	}

	restarted, err := NewAgent(AgentConfig{Playwright: &PlaywrightLaneConfig{CacheDir: agent.Config().Playwright.CacheDir, TrustedKeys: playwright.TrustedKeys{playwright.KeyID(public): public}, MinFreeBytes: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if again := restarted.PlaywrightLane(ctx); again.ArtifactDigest != lane.ArtifactDigest {
		t.Fatalf("restart lost the last-known-good artifact: %+v", again)
	}
}
