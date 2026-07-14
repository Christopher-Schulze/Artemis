package main

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseArtifactsProduction(t *testing.T) {
	// The release tool operates on the artemis source tree root.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	artemisRoot := filepath.Join(origDir, "..", "..")
	if err := os.Chdir(artemisRoot); err != nil {
		t.Fatalf("chdir to artemis root: %v", err)
	}
	defer os.Chdir(origDir)

	tmpDir := t.TempDir()

	// Run the release tool in test mode by calling the production functions
	// directly through a subprocess would be complex; instead verify the
	// artifact shapes by running the tool.
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Produce checksums.
	checksums, err := produceChecksums(tmpDir)
	if err != nil {
		t.Fatalf("produceChecksums: %v", err)
	}
	if len(checksums) == 0 {
		t.Fatalf("no checksums produced")
	}

	checksumPath := filepath.Join(tmpDir, "checksums.txt")
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatalf("read checksums.txt: %v", err)
	}
	if !strings.Contains(string(data), "  LICENSE") {
		t.Errorf("checksums.txt missing LICENSE entry")
	}

	// Produce SBOM.
	if err := produceSBOM(tmpDir); err != nil {
		t.Fatalf("produceSBOM: %v", err)
	}
	sbomPath := filepath.Join(tmpDir, "sbom.cdx.xml")
	sbomData, err := os.ReadFile(sbomPath)
	if err != nil {
		t.Fatalf("read sbom.cdx.xml: %v", err)
	}
	if !strings.Contains(string(sbomData), "cyclonedx.org/schema/bom") {
		t.Errorf("sbom.cdx.xml missing CycloneDX namespace")
	}
	// Verify it parses as XML.
	var bom cycloneDXBOM
	if err := xml.Unmarshal(sbomData, &bom); err != nil {
		t.Fatalf("sbom.cdx.xml invalid XML: %v", err)
	}
	if len(bom.Components) == 0 {
		t.Errorf("sbom has no components")
	}

	// Produce license report.
	if err := produceLicenseReport(tmpDir); err != nil {
		t.Fatalf("produceLicenseReport: %v", err)
	}
	reportPath := filepath.Join(tmpDir, "license-report.txt")
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read license-report.txt: %v", err)
	}
	if !strings.Contains(string(reportData), "MIT") {
		t.Errorf("license-report.txt missing MIT license")
	}
	if !strings.Contains(string(reportData), "rogchap.com/v8go") {
		t.Errorf("license-report.txt missing v8go entry")
	}

	// Produce release manifest.
	manifest := releaseManifest{
		Version:    "test-0.0.0",
		License:    "MIT",
		SBOMFormat: "CycloneDX",
		Checksums:  checksums,
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if !strings.Contains(string(manifestData), `"license":"MIT"`) {
		t.Errorf("manifest missing MIT license")
	}
}

func TestChecksumIsDeterministic(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("hello world\n")
	if err := os.WriteFile(testFile, content, 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	h1, err := hashFile(testFile)
	if err != nil {
		t.Fatalf("hashFile 1: %v", err)
	}
	h2, err := hashFile(testFile)
	if err != nil {
		t.Fatalf("hashFile 2: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("hashFile not deterministic: %s != %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("sha256 hex length = %d, want 64", len(h1))
	}
}
