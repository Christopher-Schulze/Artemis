package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type releaseOperations struct {
	beforeWrite func(path string) error
	rename      func(oldPath, newPath string) error
	postPublish func(outputRoot string) error
}

func productionReleaseOperations() releaseOperations {
	return releaseOperations{
		beforeWrite: func(string) error { return nil },
		rename:      renameExclusive,
		postPublish: func(string) error { return nil },
	}
}

func buildReleaseArtifactSet(ctx context.Context, inputs releaseInputs, runner commandRunner, operations releaseOperations) (returnErr error) {
	normalized, err := normalizeInputs(ctx, inputs, runner)
	if err != nil {
		return err
	}
	inventory, err := loadModuleInventory(ctx, normalized.SourceRoot, normalized.Version, runner)
	if err != nil {
		return err
	}
	stageRoot, err := os.MkdirTemp(filepath.Dir(normalized.OutputRoot), "."+filepath.Base(normalized.OutputRoot)+".stage-")
	if err != nil {
		return fmt.Errorf("create release stage: %w", err)
	}
	defer func() {
		if stageRoot != "" {
			returnErr = errors.Join(returnErr, removeReleaseStage(stageRoot))
		}
	}()
	if err := populateReleaseStage(stageRoot, normalized, inventory, operations); err != nil {
		return err
	}
	if _, err := validateGitSource(ctx, normalized, runner); err != nil {
		return fmt.Errorf("source changed during release generation: %w", err)
	}
	if err := syncDirectoryTree(stageRoot); err != nil {
		return fmt.Errorf("sync release stage: %w", err)
	}
	if err := verifyArtifactSet(stageRoot, normalized, inventory); err != nil {
		return fmt.Errorf("verify staged release set: %w", err)
	}
	if err := publishReleaseSet(stageRoot, normalized.OutputRoot, normalized, inventory, operations); err != nil {
		return err
	}
	stageRoot = ""
	return nil
}

func populateReleaseStage(stageRoot string, inputs releaseInputs, inventory moduleInventory, operations releaseOperations) error {
	writer := releaseWriter(operations)
	deliverables, err := copyDeliverables(stageRoot, inputs.Artifacts, operations)
	if err != nil {
		return err
	}
	report, err := collectLicenses(stageRoot, inventory, inputs.generatedAt(), writer)
	if err != nil {
		return err
	}
	if err = writeJSONFile(filepath.Join(stageRoot, licenseReportFile), report, writer); err != nil {
		return fmt.Errorf("write license report: %w", err)
	}
	if err = writeSBOM(filepath.Join(stageRoot, sbomFile), inputs, inventory, report, writer); err != nil {
		return err
	}
	files, err := scanReleaseFiles(stageRoot)
	if err != nil {
		return err
	}
	if err = writeChecksums(filepath.Join(stageRoot, checksumsFile), files, writer); err != nil {
		return err
	}
	files, err = scanReleaseFiles(stageRoot)
	if err != nil {
		return err
	}
	manifest, err := buildManifest(inputs, inventory, deliverables, files)
	if err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(stageRoot, releaseManifestFile), manifest, writer); err != nil {
		return fmt.Errorf("write release manifest: %w", err)
	}
	return nil
}

func releaseWriter(operations releaseOperations) func(string, []byte, os.FileMode) error {
	return func(path string, data []byte, mode os.FileMode) error {
		if err := operations.beforeWrite(path); err != nil {
			return err
		}
		return writeDurableFile(path, data, mode)
	}
}

func copyDeliverables(stageRoot string, artifacts []artifactSpec, operations releaseOperations) ([]string, error) {
	deliverables := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		relative := filepath.ToSlash(filepath.Join("artifacts", artifact.Name))
		destination := filepath.Join(stageRoot, filepath.FromSlash(relative))
		if err := operations.beforeWrite(destination); err != nil {
			return nil, err
		}
		if err := copyDurableFile(artifact.SourcePath, destination); err != nil {
			return nil, fmt.Errorf("copy artifact %s: %w", artifact.Name, err)
		}
		if err := verifyCopiedArtifact(artifact, destination); err != nil {
			return nil, err
		}
		deliverables = append(deliverables, relative)
	}
	return deliverables, nil
}

func verifyCopiedArtifact(artifact artifactSpec, destination string) error {
	sourceDigest, err := hashFile(artifact.SourcePath)
	if err != nil {
		return fmt.Errorf("verify artifact %s source: %w", artifact.Name, err)
	}
	destinationDigest, err := hashFile(destination)
	if err != nil {
		return fmt.Errorf("verify artifact %s copy: %w", artifact.Name, err)
	}
	if sourceDigest != artifact.SourceSHA256 || destinationDigest != artifact.SourceSHA256 {
		return fmt.Errorf("artifact %s changed during release generation", artifact.Name)
	}
	return nil
}

func writeJSONFile(path string, value any, writeFile func(string, []byte, os.FileMode) error) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'), 0o644)
}

func writeChecksums(path string, files []releaseFile, writeFile func(string, []byte, os.FileMode) error) error {
	var builder strings.Builder
	for _, file := range files {
		if file.Path == checksumsFile || file.Path == releaseManifestFile {
			continue
		}
		builder.WriteString(file.SHA256)
		builder.WriteString("  ")
		builder.WriteString(file.Path)
		builder.WriteByte('\n')
	}
	if err := writeFile(path, []byte(builder.String()), 0o644); err != nil {
		return fmt.Errorf("write checksums: %w", err)
	}
	return nil
}

func buildManifest(inputs releaseInputs, inventory moduleInventory, deliverables []string, files []releaseFile) (releaseManifest, error) {
	checksums, ok := findReleaseFile(files, checksumsFile)
	if !ok {
		return releaseManifest{}, errors.New("checksums artifact missing")
	}
	sbom, ok := findReleaseFile(files, sbomFile)
	if !ok {
		return releaseManifest{}, errors.New("SBOM artifact missing")
	}
	licenses, ok := findReleaseFile(files, licenseReportFile)
	if !ok {
		return releaseManifest{}, errors.New("license report artifact missing")
	}
	return releaseManifest{
		SchemaVersion: releaseManifestSchema, Version: inputs.Version, Commit: inputs.Commit,
		BuildProfile: inputs.BuildProfile, SourceDateEpoch: inputs.SourceDateEpoch, GeneratedAt: inputs.generatedAt(),
		Target: inputs.Target, Toolchain: releaseToolchain{GoVersion: runtime.Version(), Digest: inputs.ToolchainDigest},
		GoModule: inventory.MainPath, ModuleGraphDigest: inventory.GraphDigest, Deliverables: append([]string(nil), deliverables...),
		Files: files, Checksums: fileRef(checksums), SBOM: fileRef(sbom), LicenseReport: fileRef(licenses),
		ArtifactSetDigest: releaseSetDigest(files),
	}, nil
}

func fileRef(file releaseFile) releaseFileRef {
	return releaseFileRef{Path: file.Path, SHA256: file.SHA256}
}

func releaseSetDigest(files []releaseFile) string {
	parts := make([]string, 0, len(files))
	for _, file := range files {
		parts = append(parts, fmt.Sprintf("%s|%s|%d|%o|%s", file.Path, file.SHA256, file.Size, file.Mode, file.Kind))
	}
	return "sha256:" + stableDigest(parts...)
}

func publishReleaseSet(stageRoot, outputRoot string, inputs releaseInputs, inventory moduleInventory, operations releaseOperations) (returnErr error) {
	if _, err := os.Lstat(outputRoot); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return errors.New("release destination appeared before publication")
	}
	if err := operations.rename(stageRoot, outputRoot); err != nil {
		return fmt.Errorf("publish release set: %w", err)
	}
	published := true
	defer func() {
		if !published {
			if rollbackErr := operations.rename(outputRoot, stageRoot); rollbackErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("rollback published release set: %w", rollbackErr))
			} else if syncErr := syncDirectory(filepath.Dir(outputRoot)); syncErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("sync release rollback: %w", syncErr))
			}
		}
	}()
	if err := syncDirectory(filepath.Dir(outputRoot)); err != nil {
		published = false
		return fmt.Errorf("sync release destination parent: %w", err)
	}
	if err := operations.postPublish(outputRoot); err != nil {
		published = false
		return fmt.Errorf("post-publish release verification hook: %w", err)
	}
	if err := verifyArtifactSet(outputRoot, inputs, inventory); err != nil {
		published = false
		return fmt.Errorf("verify published release set: %w", err)
	}
	return nil
}
