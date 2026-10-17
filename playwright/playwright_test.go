package playwright

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type artifactSpec struct {
	files        map[string]Role
	capabilities []Capability
	abi          string
	mutate       func(*Manifest)
}

func defaultSpec() artifactSpec {
	return artifactSpec{
		files: map[string]Role{
			"core/playwright-core.js": RoleCore, "runtime/node": RoleRuntime, "browsers/chromium/chrome": RoleBrowser,
			"compiler/tsc.js": RoleCompiler, "types/context.d.ts": RoleDeclarations, "sbom.spdx.json": RoleSBOM,
			"LICENSES.txt": RoleLicenses, "provenance.intoto.json": RoleProvenance,
		},
		capabilities: []Capability{CapNavigation, CapLocators, CapScreenshot, CapVideo, CapHAR, CapBrowserScript},
		abi:          SessionABIVersion,
	}
}

// writeArtifact builds a signed artifact directory and returns its trust map.
func writeArtifact(t *testing.T, dir string, spec artifactSpec, key ed25519.PrivateKey) TrustedKeys {
	t.Helper()
	manifest := Manifest{
		SchemaVersion: schemaVersion, ArtifactVersion: "1.0.0", PlaywrightCoreVersion: "1.58.0", RuntimeVersion: "24.4.0",
		AdmissionMode: AdmissionInstaller, OfflineReady: true, SessionABIVersion: spec.abi,
		Browsers: []BrowserRevision{{Browser: "chromium", Revision: "1200"}, {Browser: "firefox", Revision: "1500"}}, Capabilities: spec.capabilities,
	}
	for name, role := range spec.files {
		content := []byte("bytes of " + name)
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
		sum := sha256.Sum256(content)
		manifest.Files = append(manifest.Files, File{Path: name, Role: role, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))})
	}
	if spec.mutate != nil {
		spec.mutate(&manifest)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ManifestFile), data)
	public := key.Public().(ed25519.PublicKey)
	signature, err := json.Marshal(Signature{KeyID: KeyID(public), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, data))})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, SignatureFile), signature)
	return TrustedKeys{KeyID(public): public}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestAdmitAcceptsSignedArtifact(t *testing.T) {
	dir := t.TempDir()
	trusted := writeArtifact(t, dir, defaultSpec(), newKey(t))
	admission, err := Admit(context.Background(), dir, trusted)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(manifest)
	if admission.ArtifactDigest != "sha256:"+hex.EncodeToString(sum[:]) || !admission.ScriptCompiler || len(admission.Manifest.Files) != 8 {
		t.Fatalf("admission = %+v", admission)
	}
}

func TestAdmitRejectsTamperedArtifacts(t *testing.T) {
	cases := []struct {
		name   string
		build  func(t *testing.T, dir string) TrustedKeys
		target error
	}{
		{"tampered file", func(t *testing.T, dir string) TrustedKeys {
			trusted := writeArtifact(t, dir, defaultSpec(), newKey(t))
			writeFile(t, filepath.Join(dir, "runtime", "node"), []byte("bytes of runtime/nodX"))
			return trusted
		}, ErrArtifactCorrupt},
		{"unlisted file", func(t *testing.T, dir string) TrustedKeys {
			trusted := writeArtifact(t, dir, defaultSpec(), newKey(t))
			writeFile(t, filepath.Join(dir, "core", "injected.js"), []byte("payload"))
			return trusted
		}, ErrArtifactCorrupt},
		{"missing file", func(t *testing.T, dir string) TrustedKeys {
			trusted := writeArtifact(t, dir, defaultSpec(), newKey(t))
			if err := os.Remove(filepath.Join(dir, "LICENSES.txt")); err != nil {
				t.Fatal(err)
			}
			return trusted
		}, ErrArtifactCorrupt},
		{"symlink escape", func(t *testing.T, dir string) TrustedKeys {
			trusted := writeArtifact(t, dir, defaultSpec(), newKey(t))
			outside := filepath.Join(t.TempDir(), "outside")
			writeFile(t, outside, []byte("bytes of sbom.spdx.json"))
			if err := os.Remove(filepath.Join(dir, "sbom.spdx.json")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, "sbom.spdx.json")); err != nil {
				t.Fatal(err)
			}
			return trusted
		}, ErrArtifactCorrupt},
		{"untrusted signer", func(t *testing.T, dir string) TrustedKeys {
			writeArtifact(t, dir, defaultSpec(), newKey(t))
			other := newKey(t).Public().(ed25519.PublicKey)
			return TrustedKeys{KeyID(other): other}
		}, ErrSignatureInvalid},
		{"manifest edited after signing", func(t *testing.T, dir string) TrustedKeys {
			trusted := writeArtifact(t, dir, defaultSpec(), newKey(t))
			data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(dir, ManifestFile), append(data, ' '))
			return trusted
		}, ErrSignatureInvalid},
		{"session ABI mismatch", func(t *testing.T, dir string) TrustedKeys {
			spec := defaultSpec()
			spec.abi = "2"
			return writeArtifact(t, dir, spec, newKey(t))
		}, ErrABIIncompatible},
		{"missing provenance role", func(t *testing.T, dir string) TrustedKeys {
			spec := defaultSpec()
			delete(spec.files, "provenance.intoto.json")
			return writeArtifact(t, dir, spec, newKey(t))
		}, ErrManifestInvalid},
		{"unknown capability", func(t *testing.T, dir string) TrustedKeys {
			spec := defaultSpec()
			spec.capabilities = append(spec.capabilities, "kernel_access")
			return writeArtifact(t, dir, spec, newKey(t))
		}, ErrManifestInvalid},
		{"path traversal", func(t *testing.T, dir string) TrustedKeys {
			spec := defaultSpec()
			spec.mutate = func(m *Manifest) { m.Files[0].Path = "../escape.js" }
			return writeArtifact(t, dir, spec, newKey(t))
		}, ErrManifestInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			trusted := tc.build(t, dir)
			if _, err := Admit(context.Background(), dir, trusted); !errors.Is(err, tc.target) {
				t.Fatalf("Admit error = %v, want %v", err, tc.target)
			}
		})
	}
}

func TestCacheKeepsLastKnownGoodAcrossFailuresAndRestart(t *testing.T) {
	key := newKey(t)
	good := t.TempDir()
	trusted := writeArtifact(t, good, defaultSpec(), key)
	cacheDir := t.TempDir()
	cache, err := OpenCache(cacheDir, trusted, 1)
	if err != nil {
		t.Fatal(err)
	}
	if status := cache.Load(context.Background()); status.Admitted || status.Remediation == "" {
		t.Fatalf("empty cache status = %+v", status)
	}
	installed, err := cache.Install(context.Background(), good)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	bad := t.TempDir()
	spec := defaultSpec()
	spec.files["core/extra.js"] = RoleCore
	writeArtifact(t, bad, spec, key)
	writeFile(t, filepath.Join(bad, "core", "extra.js"), []byte("tampered"))
	if _, err := cache.Install(context.Background(), bad); !errors.Is(err, ErrArtifactCorrupt) {
		t.Fatalf("tampered candidate Install = %v", err)
	}
	restarted, err := OpenCache(cacheDir, trusted, 1)
	if err != nil {
		t.Fatal(err)
	}
	status := restarted.Load(context.Background())
	if !status.Admitted || status.Admission.ArtifactDigest != installed.ArtifactDigest {
		t.Fatalf("restart did not serve the last-known-good artifact: %+v", status)
	}
	huge, err := OpenCache(cacheDir, trusted, math.MaxInt64/2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := huge.Install(context.Background(), good); !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("Install without headroom = %v", err)
	}
	if status := restarted.Load(context.Background()); status.Admission.ArtifactDigest != installed.ArtifactDigest {
		t.Fatal("failed install moved the last-known-good pointer")
	}
}

func TestCacheReportsCorruptionWithoutDeletingOrSubstituting(t *testing.T) {
	source := t.TempDir()
	trusted := writeArtifact(t, source, defaultSpec(), newKey(t))
	cacheDir := t.TempDir()
	cache, err := OpenCache(cacheDir, trusted, 1)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := cache.Install(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := filepath.Join(cache.artifactDir(installed.ArtifactDigest), "runtime", "node")
	writeFile(t, corrupted, []byte("bytes of runtime/nodX"))
	status := cache.Load(context.Background())
	if status.Admitted || len(status.Diagnostics) == 0 || status.Remediation == "" {
		t.Fatalf("corrupted cache status = %+v", status)
	}
	if _, err := os.Stat(corrupted); err != nil {
		t.Fatalf("corrupted evidence was deleted: %v", err)
	}
	if projection := Project(status, true, ""); projection.Availability != AvailabilityUnavailable || len(projection.Capabilities) != 0 {
		t.Fatalf("corrupt cache projected as usable: %+v", projection)
	}
}

func TestProjectionIsTruthful(t *testing.T) {
	dir := t.TempDir()
	trusted := writeArtifact(t, dir, defaultSpec(), newKey(t))
	admission, err := Admit(context.Background(), dir, trusted)
	if err != nil {
		t.Fatal(err)
	}
	status := Status{Admission: admission, Admitted: true}
	if p := Project(Status{Diagnostics: []string{"no artifact"}, Remediation: "install"}, true, ""); p.Availability != AvailabilityUnavailable || p.Remediation != "install" {
		t.Fatalf("unprovisioned projection = %+v", p)
	}
	if p := Project(status, false, ""); p.Availability != AvailabilityUnavailable || p.ArtifactDigest != admission.ArtifactDigest || len(p.Capabilities) != 0 {
		t.Fatalf("projection without execution runtime = %+v", p)
	}
	ready := Project(status, true, "policy.browser")
	if ready.Availability != AvailabilityReady || !ready.Supports(CapBrowserScript) || !ready.SupportsBrowser("firefox") || ready.Limits.SessionsPerProcess != 4 || ready.PolicyRef != "policy.browser" {
		t.Fatalf("ready projection = %+v", ready)
	}
	admission.ScriptCompiler = false
	degraded := Project(Status{Admission: admission, Admitted: true}, true, "")
	if degraded.Availability != AvailabilityDegraded || degraded.Supports(CapBrowserScript) || !degraded.Supports(CapVideo) {
		t.Fatalf("projection without compiler = %+v", degraded)
	}
}
