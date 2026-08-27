package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func verifyArtifactSet(root string, inputs releaseInputs, inventory moduleInventory) error {
	manifest, err := readReleaseManifest(filepath.Join(root, releaseManifestFile))
	if err != nil {
		return err
	}
	if err = verifyManifestIdentity(manifest, inputs, inventory); err != nil {
		return err
	}
	files, err := scanReleaseFiles(root)
	if err != nil {
		return err
	}
	if err = compareReleaseFiles(manifest.Files, files); err != nil {
		return err
	}
	if manifest.ArtifactSetDigest != releaseSetDigest(files) {
		return errors.New("artifact-set digest mismatch")
	}
	if err = verifyChecksums(root, files); err != nil {
		return err
	}
	report, err := readLicenseReport(filepath.Join(root, licenseReportFile))
	if err != nil {
		return err
	}
	if err := verifyLicenseReport(root, report, inventory, files, inputs.generatedAt()); err != nil {
		return err
	}
	if err := validateSBOMFile(filepath.Join(root, sbomFile), inputs, inventory, report); err != nil {
		return err
	}
	return verifyManifestReferences(manifest, files)
}

func readReleaseManifest(path string) (releaseManifest, error) {
	var manifest releaseManifest
	if err := decodeStrictJSONFile(path, &manifest); err != nil {
		return releaseManifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	return manifest, nil
}

func verifyManifestIdentity(manifest releaseManifest, inputs releaseInputs, inventory moduleInventory) error {
	if manifest.SchemaVersion != releaseManifestSchema || manifest.Version != inputs.Version || manifest.Commit != inputs.Commit {
		return errors.New("release manifest source identity mismatch")
	}
	if manifest.BuildProfile != buildProfileRelease || manifest.SourceDateEpoch != inputs.SourceDateEpoch || manifest.GeneratedAt != inputs.generatedAt() {
		return errors.New("release manifest build identity mismatch")
	}
	if manifest.Target != inputs.Target || manifest.Toolchain.GoVersion != runtime.Version() || manifest.Toolchain.Digest != inputs.ToolchainDigest {
		return errors.New("release manifest target or toolchain mismatch")
	}
	if manifest.GoModule != inventory.MainPath || manifest.ModuleGraphDigest != inventory.GraphDigest {
		return errors.New("release manifest module graph mismatch")
	}
	if len(manifest.Deliverables) != len(inputs.Artifacts) {
		return errors.New("release manifest deliverable count mismatch")
	}
	for index, artifact := range inputs.Artifacts {
		expected := filepath.ToSlash(filepath.Join("artifacts", artifact.Name))
		if manifest.Deliverables[index] != expected {
			return fmt.Errorf("release manifest deliverable %d mismatch", index)
		}
	}
	return nil
}

func verifyManifestReferences(manifest releaseManifest, files []releaseFile) error {
	if manifest.Checksums.Path != checksumsFile || manifest.SBOM.Path != sbomFile || manifest.LicenseReport.Path != licenseReportFile {
		return errors.New("release manifest canonical references mismatch")
	}
	for _, reference := range []releaseFileRef{manifest.Checksums, manifest.SBOM, manifest.LicenseReport} {
		file, ok := findReleaseFile(files, reference.Path)
		if !ok || file.SHA256 != reference.SHA256 {
			return fmt.Errorf("manifest reference %s does not match release files", reference.Path)
		}
	}
	for _, deliverable := range manifest.Deliverables {
		file, ok := findReleaseFile(files, deliverable)
		if !ok || file.Kind != "deliverable" {
			return fmt.Errorf("manifest deliverable %s is missing or misclassified", deliverable)
		}
	}
	return nil
}

func scanReleaseFiles(root string) ([]releaseFile, error) {
	var files []releaseFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("release set contains non-regular path %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == releaseManifestFile {
			return nil
		}
		digest, err := hashFile(path)
		if err != nil {
			return err
		}
		files = append(files, releaseFile{Path: relative, Kind: releaseFileKind(relative), SHA256: digest, Size: info.Size(), Mode: uint32(info.Mode().Perm())})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan release set: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func releaseFileKind(path string) string {
	switch {
	case strings.HasPrefix(path, "artifacts/"):
		return "deliverable"
	case strings.HasPrefix(path, "licenses/"):
		return "license-text"
	case path == sbomFile:
		return "sbom"
	case path == licenseReportFile:
		return "license-report"
	case path == checksumsFile:
		return "checksums"
	case path == releaseManifestFile:
		return "manifest"
	default:
		return "unknown"
	}
}

func compareReleaseFiles(expected, actual []releaseFile) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("release file count mismatch: manifest=%d actual=%d", len(expected), len(actual))
	}
	for index := range expected {
		if expected[index] != actual[index] || expected[index].Kind == "unknown" {
			return fmt.Errorf("release file mismatch at %d: manifest=%+v actual=%+v", index, expected[index], actual[index])
		}
	}
	return nil
}

func verifyChecksums(root string, files []releaseFile) (returnErr error) {
	checksumPath := filepath.Join(filepath.Clean(root), checksumsFile)
	file, err := os.Open(checksumPath)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	actual := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "  ", 2)
		if len(parts) != 2 || !validRelativeReleasePath(parts[1]) {
			return fmt.Errorf("invalid checksum line %q", scanner.Text())
		}
		if _, duplicate := actual[parts[1]]; duplicate {
			return fmt.Errorf("duplicate checksum path %s", parts[1])
		}
		actual[parts[1]] = parts[0]
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for _, entry := range files {
		if entry.Path == checksumsFile {
			continue
		}
		if actual[entry.Path] != entry.SHA256 {
			return fmt.Errorf("checksum mismatch for %s", entry.Path)
		}
		delete(actual, entry.Path)
	}
	if len(actual) != 0 {
		return errors.New("checksums contain paths outside the release manifest")
	}
	return nil
}

func readLicenseReport(path string) (licenseReport, error) {
	var report licenseReport
	if err := decodeStrictJSONFile(path, &report); err != nil {
		return licenseReport{}, fmt.Errorf("decode license report: %w", err)
	}
	return report, nil
}

func verifyLicenseReport(root string, report licenseReport, inventory moduleInventory, files []releaseFile, generatedAt string) error {
	if report.SchemaVersion != licenseReportSchema || report.GeneratedAt != generatedAt || len(report.Components) != len(inventory.Modules) {
		return errors.New("license report identity or component count mismatch")
	}
	referenced := make(map[string]struct{})
	for index, component := range report.Components {
		module := inventory.Modules[index]
		if component.BOMRef != moduleBOMRef(module) || component.ModulePath != module.Path || component.Version != module.Version || component.Main != module.Main || component.Indirect != module.Indirect || len(component.Licenses) == 0 {
			return fmt.Errorf("license report component %s mismatch", module.Path)
		}
		if err := verifyReplacementEvidence(component.Replacement, module.Replace); err != nil {
			return fmt.Errorf("license report component %s: %w", module.Path, err)
		}
		for _, license := range component.Licenses {
			if !validRelativeReleasePath(license.Path) || !strings.HasPrefix(license.Path, "licenses/") || license.Name == "" {
				return fmt.Errorf("module %s has invalid license evidence", module.Path)
			}
			if _, duplicate := referenced[license.Path]; duplicate {
				return fmt.Errorf("license evidence path %s is duplicated", license.Path)
			}
			referenced[license.Path] = struct{}{}
			digest, err := hashFile(filepath.Join(root, filepath.FromSlash(license.Path)))
			if err != nil || digest != license.SHA256 {
				return fmt.Errorf("module %s license digest mismatch", module.Path)
			}
		}
	}
	return verifyReportedLicenseFiles(files, referenced)
}

func verifyReportedLicenseFiles(files []releaseFile, referenced map[string]struct{}) error {
	for _, file := range files {
		if file.Kind == "license-text" {
			if _, ok := referenced[file.Path]; !ok {
				return fmt.Errorf("license text %s is not referenced by the report", file.Path)
			}
			delete(referenced, file.Path)
		}
	}
	if len(referenced) != 0 {
		return errors.New("license report references files outside the sealed release set")
	}
	return nil
}

func verifyReplacementEvidence(actual *moduleReplacement, expected *goModule) error {
	if expected == nil {
		if actual != nil {
			return errors.New("unexpected replacement evidence")
		}
		return nil
	}
	if actual == nil || actual.Path != expected.Path || actual.Version != expected.Version || actual.Digest != expected.Sum {
		return errors.New("replacement evidence mismatch")
	}
	return nil
}

func decodeStrictJSONFile(path string, destination any) (returnErr error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON has trailing content")
	}
	return nil
}

func findReleaseFile(files []releaseFile, path string) (releaseFile, bool) {
	index := sort.Search(len(files), func(index int) bool { return files[index].Path >= path })
	if index == len(files) || files[index].Path != path {
		return releaseFile{}, false
	}
	return files[index], true
}

func validRelativeReleasePath(path string) bool {
	return path != "" && path == filepath.ToSlash(filepath.Clean(path)) && path != "." && !strings.HasPrefix(path, "../") && !filepath.IsAbs(path)
}
