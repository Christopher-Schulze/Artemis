package main

import "time"

const (
	releaseManifestSchema = "artemis.release-set/v1"
	licenseReportSchema   = "artemis.license-report/v1"
	buildProfileRelease   = "release_hardened"
	sbomFormat            = "CycloneDX"
	sbomSpecVersion       = "1.7"

	checksumsFile       = "checksums.txt"
	licenseReportFile   = "license-report.json"
	releaseManifestFile = "release-manifest.json"
	sbomFile            = "sbom.cdx.json"
)

type targetPlatform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type artifactSpec struct {
	Name         string
	SourcePath   string
	SourceSHA256 string
}

type releaseInputs struct {
	SourceRoot      string
	OutputRoot      string
	Version         string
	Commit          string
	BuildProfile    string
	SourceDateEpoch int64
	Target          targetPlatform
	ToolchainDigest string
	Artifacts       []artifactSpec
}

func (in releaseInputs) generatedAt() string {
	return time.Unix(in.SourceDateEpoch, 0).UTC().Format(time.RFC3339)
}

type releaseFile struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
}

type releaseFileRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type releaseToolchain struct {
	GoVersion string `json:"go_version"`
	Digest    string `json:"digest"`
}

type releaseManifest struct {
	SchemaVersion     string           `json:"schema_version"`
	Version           string           `json:"version"`
	Commit            string           `json:"commit"`
	BuildProfile      string           `json:"build_profile"`
	SourceDateEpoch   int64            `json:"source_date_epoch"`
	GeneratedAt       string           `json:"generated_at"`
	Target            targetPlatform   `json:"target"`
	Toolchain         releaseToolchain `json:"toolchain"`
	GoModule          string           `json:"go_module"`
	ModuleGraphDigest string           `json:"module_graph_digest"`
	Deliverables      []string         `json:"deliverables"`
	Files             []releaseFile    `json:"files"`
	Checksums         releaseFileRef   `json:"checksums"`
	SBOM              releaseFileRef   `json:"sbom"`
	LicenseReport     releaseFileRef   `json:"license_report"`
	ArtifactSetDigest string           `json:"artifact_set_digest"`
}

type moduleReplacement struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
	Digest  string `json:"digest"`
}

type licenseEvidence struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	SPDXID string `json:"spdx_id,omitempty"`
	Name   string `json:"name"`
}

type componentLicenseEvidence struct {
	BOMRef      string             `json:"bom_ref"`
	ModulePath  string             `json:"module_path"`
	Version     string             `json:"version"`
	Main        bool               `json:"main"`
	Indirect    bool               `json:"indirect"`
	Replacement *moduleReplacement `json:"replacement,omitempty"`
	Licenses    []licenseEvidence  `json:"licenses"`
}

type licenseReport struct {
	SchemaVersion string                     `json:"schema_version"`
	GeneratedAt   string                     `json:"generated_at"`
	Components    []componentLicenseEvidence `json:"components"`
}

type goModule struct {
	Path     string
	Version  string
	Main     bool
	Indirect bool
	Dir      string
	Sum      string
	GoModSum string
	Replace  *goModule
	Error    string
}

type moduleInventory struct {
	MainPath    string
	Modules     []goModule
	Edges       map[string][]string
	GraphDigest string
}
