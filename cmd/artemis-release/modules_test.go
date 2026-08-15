package main

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseModuleGraphMapsRequirementsToSelectedVersions(t *testing.T) {
	modules := []goModule{
		{Path: "example.com/main", Version: "v1.0.0", Main: true},
		{Path: "example.com/dependency", Version: "v1.5.0"},
	}
	graph := strings.Join([]string{
		"example.com/main example.com/dependency@v1.0.0",
		"example.com/main go@1.26",
		"go@1.26 toolchain@go1.26",
		"example.com/dependency@v1.0.0 example.com/transitive@v0.1.0",
	}, "\n")
	edges, err := parseModuleGraph([]byte(graph), modules)
	if err != nil {
		t.Fatalf("parseModuleGraph: %v", err)
	}
	want := []string{"example.com/dependency@v1.5.0"}
	if strings.Join(edges["example.com/main"], ",") != strings.Join(want, ",") {
		t.Fatalf("main edges = %v, want %v", edges["example.com/main"], want)
	}
}

func TestParseModuleGraphRejectsIncompleteSelectedInventory(t *testing.T) {
	modules := []goModule{{Path: "example.com/main", Version: "v1.0.0", Main: true}}
	_, err := parseModuleGraph([]byte("example.com/main example.com/missing@v1.0.0\n"), modules)
	if err == nil || !strings.Contains(err.Error(), "unselected dependency") {
		t.Fatalf("incomplete graph error = %v", err)
	}
}

func TestParseModuleGraphRejectsMalformedAndEmptyGraphs(t *testing.T) {
	modules := []goModule{{Path: "example.com/main", Version: "v1.0.0", Main: true}}
	for _, graph := range []string{"", "example.com/main", "example.com/main invalid"} {
		if _, err := parseModuleGraph([]byte(graph), modules); err == nil {
			t.Fatalf("graph %q passed validation", graph)
		}
	}
}

func TestGoChecksumHex(t *testing.T) {
	raw := strings.Repeat("a", 32)
	checksum := "h1:" + base64.StdEncoding.EncodeToString([]byte(raw))
	digest, err := goChecksumHex(checksum)
	if err != nil {
		t.Fatalf("goChecksumHex: %v", err)
	}
	if digest != "sha256:"+strings.Repeat("61", 32) {
		t.Fatalf("digest = %s", digest)
	}
	for _, invalid := range []string{"", "sha256:abc", "h1:invalid", "h1:" + base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := goChecksumHex(invalid); err == nil {
			t.Fatalf("invalid checksum %q passed", invalid)
		}
	}
}

func TestHydrateModuleRequiresExactCompleteDownloadEvidence(t *testing.T) {
	module := goModule{Path: "example.com/dependency", Version: "v1.2.3"}
	download := goModuleDownloadWire{
		Path: module.Path, Version: module.Version, Dir: "/module-cache/dependency",
		Sum:      "h1:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))),
		GoModSum: "h1:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("b", 32))),
	}
	if err := hydrateModule(&module, map[string]goModuleDownloadWire{module.Path + "@" + module.Version: download}); err != nil {
		t.Fatalf("hydrate exact selected module: %v", err)
	}
	if module.Dir != download.Dir || module.Sum != download.Sum || module.GoModSum != download.GoModSum {
		t.Fatalf("hydrated module does not match download evidence: %+v", module)
	}
	missing := goModule{Path: "example.com/missing", Version: "v1.0.0"}
	if err := hydrateModule(&missing, nil); err == nil {
		t.Fatal("missing selected module download evidence passed")
	}
}

func TestDecodeModuleDownloadsRejectsIncompleteAndDuplicateEvidence(t *testing.T) {
	complete := `{"Path":"example.com/dependency","Version":"v1.2.3","Dir":"/cache/dependency","Sum":"h1:sum","GoModSum":"h1:mod"}`
	for _, document := range []string{
		`{"Path":"example.com/dependency","Version":"v1.2.3"}`,
		complete + "\n" + complete,
		`{"Path":"example.com/dependency","Version":"v1.2.3","Error":"download failed"}`,
	} {
		if _, err := decodeModuleDownloads([]byte(document)); err == nil {
			t.Fatalf("invalid module download evidence passed: %s", document)
		}
	}
}

func TestLicenseClassification(t *testing.T) {
	tests := []struct {
		name string
		text string
		spdx string
	}{
		{name: "MIT", text: "Permission is hereby granted, free of charge", spdx: "MIT"},
		{name: "Apache", text: "Apache License Version 2.0", spdx: "Apache-2.0"},
		{name: "BSD three clause", text: "Redistribution and use. Neither the name may be used. Disclaimer", spdx: "BSD-3-Clause"},
		{name: "BSD two clause", text: "Redistribution and use. Disclaimer", spdx: "BSD-2-Clause"},
		{name: "MPL", text: "Mozilla Public License Version 2.0", spdx: "MPL-2.0"},
		{name: "unknown", text: "Custom License\nterms", spdx: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spdx, name := classifyLicense([]byte(test.text))
			if spdx != test.spdx || name == "" {
				t.Fatalf("classifyLicense = (%q, %q), want SPDX %q", spdx, name, test.spdx)
			}
		})
	}
}

func TestCurrentModuleGraphAndLicenseEvidenceAreComplete(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := loadModuleInventory(context.Background(), root, "v1.2.3", execCommandRunner{})
	if err != nil {
		t.Fatalf("load current Artemis module inventory: %v", err)
	}
	if len(inventory.Modules) < 2 || inventory.MainPath != "github.com/Christopher-Schulze/Artemis" || inventory.GraphDigest == "" {
		t.Fatalf("incomplete current module inventory: %+v", inventory)
	}
	stage := t.TempDir()
	report, err := collectLicenses(stage, inventory, "2024-03-09T16:00:00Z", writeDurableFile)
	if err != nil {
		t.Fatalf("collect current module licenses: %v", err)
	}
	if len(report.Components) != len(inventory.Modules) {
		t.Fatalf("license components=%d, modules=%d", len(report.Components), len(inventory.Modules))
	}
	inputs := releaseInputs{
		Version: "v1.2.3", Commit: strings.Repeat("a", 40), BuildProfile: buildProfileRelease,
		SourceDateEpoch: 1_710_000_000, Target: targetPlatform{OS: "darwin", Arch: "arm64"},
		ToolchainDigest: "sha256:" + strings.Repeat("b", 64),
	}
	sbomPath := filepath.Join(stage, sbomFile)
	if err := writeSBOM(sbomPath, inputs, inventory, report, writeDurableFile); err != nil {
		t.Fatalf("write current module SBOM: %v", err)
	}
	if err := validateSBOMFile(sbomPath, inputs, inventory, report); err != nil {
		t.Fatalf("validate current module SBOM: %v", err)
	}
	if info, err := os.Stat(sbomPath); err != nil || info.Size() == 0 {
		t.Fatalf("current module SBOM missing or empty: info=%v err=%v", info, err)
	}
}
