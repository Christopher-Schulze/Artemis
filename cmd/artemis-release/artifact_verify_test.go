package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyManifestIdentityRejectsEveryAuthorityDrift(t *testing.T) {
	fixture, root, inputs, inventory := verifiedReleaseFixture(t)
	manifest, err := readReleaseManifest(filepath.Join(root, releaseManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*releaseManifest)
	}{
		{name: "source", mutate: func(value *releaseManifest) { value.Commit = strings.Repeat("0", len(fixture.commit)) }},
		{name: "build", mutate: func(value *releaseManifest) { value.SourceDateEpoch++ }},
		{name: "target", mutate: func(value *releaseManifest) { value.Target.Arch = "amd64" }},
		{name: "module graph", mutate: func(value *releaseManifest) { value.ModuleGraphDigest = "sha256:" + strings.Repeat("0", 64) }},
		{name: "deliverables", mutate: func(value *releaseManifest) { value.Deliverables = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := manifest
			test.mutate(&changed)
			if err := verifyManifestIdentity(changed, inputs, inventory); err == nil {
				t.Fatal("drifted release authority passed manifest verification")
			}
		})
	}
}

func TestVerifyChecksumsRejectsCorrelatedManifestTampering(t *testing.T) {
	_, root, inputs, inventory := verifiedReleaseFixture(t)
	checksumPath := filepath.Join(root, checksumsFile)
	checksums, err := os.ReadFile(filepath.Clean(checksumPath))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.SplitN(string(checksums), "  ", 2)
	if len(fields) != 2 {
		t.Fatalf("unexpected checksum fixture: %q", checksums)
	}
	writeExistingTestFile(t, checksumPath, strings.Repeat("0", 64)+"  "+fields[1])
	resealManifestFileEntry(t, root, checksumsFile)
	if err := verifyArtifactSet(root, inputs, inventory); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("correlated checksum tampering error = %v", err)
	}
}

func TestVerifyLicenseReportRejectsDigestAndReplacementDrift(t *testing.T) {
	_, root, inputs, inventory := verifiedReleaseFixture(t)
	report, err := readLicenseReport(filepath.Join(root, licenseReportFile))
	if err != nil {
		t.Fatal(err)
	}
	files, err := scanReleaseFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	changedDigest := cloneLicenseReport(t, report)
	changedDigest.Components[0].Licenses[0].SHA256 = strings.Repeat("0", 64)
	assertLicenseReportRejected(t, root, changedDigest, inventory, files, inputs.generatedAt())
	changedReplacement := cloneLicenseReport(t, report)
	component := replacementComponent(t, changedReplacement.Components)
	component.Replacement.Digest = "git:" + strings.Repeat("0", 40)
	assertLicenseReportRejected(t, root, changedReplacement, inventory, files, inputs.generatedAt())
}

func verifiedReleaseFixture(t *testing.T) (releaseFixture, string, releaseInputs, moduleInventory) {
	t.Helper()
	fixture := newReleaseFixture(t, true)
	root := filepath.Join(fixture.base, "release-verified")
	inputs := fixture.inputs(root)
	buildFixtureRelease(t, inputs, execCommandRunner{}, productionReleaseOperations())
	normalized, err := normalizeInputsForPublishedSet(inputs)
	if err != nil {
		t.Fatalf("normalize published inputs: %v", err)
	}
	inventory, err := loadModuleInventory(context.Background(), normalized.SourceRoot, normalized.Version, execCommandRunner{})
	if err != nil {
		t.Fatalf("load module inventory: %v", err)
	}
	return fixture, root, normalized, inventory
}

func resealManifestFileEntry(t *testing.T, root, path string) {
	t.Helper()
	manifest, err := readReleaseManifest(filepath.Join(root, releaseManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := hashFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	for index := range manifest.Files {
		if manifest.Files[index].Path == path {
			manifest.Files[index].SHA256 = digest
			manifest.Checksums.SHA256 = digest
		}
	}
	manifest.ArtifactSetDigest = releaseSetDigest(manifest.Files)
	writeExistingJSON(t, filepath.Join(root, releaseManifestFile), manifest)
}

func cloneLicenseReport(t *testing.T, report licenseReport) licenseReport {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var clone licenseReport
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func replacementComponent(t *testing.T, components []componentLicenseEvidence) *componentLicenseEvidence {
	t.Helper()
	for index := range components {
		if components[index].Replacement != nil {
			return &components[index]
		}
	}
	t.Fatal("release fixture has no replacement component")
	return nil
}

func assertLicenseReportRejected(t *testing.T, root string, report licenseReport, inventory moduleInventory, files []releaseFile, generatedAt string) {
	t.Helper()
	if err := verifyLicenseReport(root, report, inventory, files, generatedAt); err == nil {
		t.Fatal("drifted license report passed verification")
	}
}

func writeExistingJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeExistingTestFile(t, path, string(append(data, '\n')))
}

func writeExistingTestFile(t *testing.T, path, content string) {
	t.Helper()
	cleanPath := filepath.Clean(path)
	root, err := os.OpenRoot(filepath.Dir(cleanPath))
	if err != nil {
		t.Fatalf("open parent for %s: %v", path, err)
	}
	writeErr := root.WriteFile(filepath.Base(cleanPath), []byte(content), 0o600)
	closeErr := root.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("write %s: write=%v close=%v", path, writeErr, closeErr)
	}
}
