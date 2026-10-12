package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const (
	testCommitName  = "Artemis Release Test"
	testCommitEmail = "artemis-release@example.invalid"
)

type releaseFixture struct {
	base            string
	sourceRoot      string
	artifact        string
	commit          string
	sourceDateEpoch int64
}

type fileSnapshot struct {
	Mode uint32
	Data string
}

type selectiveFailureRunner struct {
	delegate commandRunner
	name     string
	args     string
}

type invalidReleaseInputCase struct {
	name  string
	setup func(t *testing.T, fixture releaseFixture, inputs *releaseInputs)
}

type pipelineFailureCase struct {
	name       string
	runner     func() commandRunner
	operations func() releaseOperations
}

func (runner selectiveFailureRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	if name == runner.name && strings.Contains(strings.Join(args, " "), runner.args) {
		return nil, errors.New("injected command failure")
	}
	return runner.delegate.Output(ctx, dir, name, args...)
}

func TestBuildReleaseArtifactSetIsByteReproducible(t *testing.T) {
	fixture := newReleaseFixture(t, true)
	first := filepath.Join(fixture.base, "release-a")
	second := filepath.Join(fixture.base, "release-b")
	buildFixtureRelease(t, fixture.inputs(first), execCommandRunner{}, productionReleaseOperations())
	buildFixtureRelease(t, fixture.inputs(second), execCommandRunner{}, productionReleaseOperations())

	firstFiles := snapshotTree(t, first)
	secondFiles := snapshotTree(t, second)
	if !reflect.DeepEqual(firstFiles, secondFiles) {
		t.Fatalf("release trees differ:\nfirst=%v\nsecond=%v", firstFiles, secondFiles)
	}
	manifest, err := readReleaseManifest(filepath.Join(first, releaseManifestFile))
	if err != nil {
		t.Fatalf("read release manifest: %v", err)
	}
	if manifest.SchemaVersion != releaseManifestSchema || manifest.Commit != fixture.commit || manifest.BuildProfile != buildProfileRelease {
		t.Fatalf("unexpected manifest identity: %+v", manifest)
	}
	wantDeliverables := []string{"artifacts/artemis"}
	if !reflect.DeepEqual(manifest.Deliverables, wantDeliverables) {
		t.Fatalf("deliverables = %v, want %v", manifest.Deliverables, wantDeliverables)
	}
	for _, required := range []string{checksumsFile, licenseReportFile, releaseManifestFile, sbomFile, "artifacts/artemis"} {
		if _, ok := firstFiles[required]; !ok {
			t.Fatalf("release set missing %s", required)
		}
	}
}

func TestBuildReleaseArtifactSetRejectsInvalidInputsWithoutPublication(t *testing.T) {
	for _, test := range invalidReleaseInputCases() {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReleaseFixture(t, true)
			output := filepath.Join(fixture.base, "release")
			inputs := fixture.inputs(output)
			test.setup(t, fixture, &inputs)
			if err := buildReleaseArtifactSet(context.Background(), inputs, execCommandRunner{}, productionReleaseOperations()); err == nil {
				t.Fatal("expected invalid release inputs to fail")
			}
			if test.name == "existing destination" {
				assertSentinelUnchanged(t, output)
				return
			}
			assertPathAbsent(t, output)
			assertNoReleaseStage(t, fixture.base)
		})
	}
}

func invalidReleaseInputCases() []invalidReleaseInputCase {
	return []invalidReleaseInputCase{
		{
			name: "commit mismatch",
			setup: func(_ *testing.T, _ releaseFixture, inputs *releaseInputs) {
				inputs.Commit = strings.Repeat("0", 40)
			},
		},
		{
			name: "dirty source",
			setup: func(t *testing.T, fixture releaseFixture, _ *releaseInputs) {
				writeTestFile(t, filepath.Join(fixture.sourceRoot, "dirty.txt"), "dirty\n", 0o644)
			},
		},
		{
			name: "source epoch mismatch",
			setup: func(_ *testing.T, _ releaseFixture, inputs *releaseInputs) {
				inputs.SourceDateEpoch++
			},
		},
		{
			name: "existing destination",
			setup: func(t *testing.T, _ releaseFixture, inputs *releaseInputs) {
				if err := os.Mkdir(inputs.OutputRoot, 0o700); err != nil {
					t.Fatalf("create existing destination: %v", err)
				}
				writeTestFile(t, filepath.Join(inputs.OutputRoot, "sentinel"), "keep\n", 0o644)
			},
		},
	}
}

func assertSentinelUnchanged(t *testing.T, output string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Clean(output), "sentinel"))
	if err != nil || string(data) != "keep\n" {
		t.Fatalf("existing destination changed: data=%q err=%v", data, err)
	}
}

func TestBuildReleaseArtifactSetRejectsMissingLicenseEvidence(t *testing.T) {
	fixture := newReleaseFixture(t, false)
	output := filepath.Join(fixture.base, "release")
	err := buildReleaseArtifactSet(context.Background(), fixture.inputs(output), execCommandRunner{}, productionReleaseOperations())
	if err == nil || !strings.Contains(err.Error(), "no top-level LICENSE") {
		t.Fatalf("missing license error = %v", err)
	}
	assertPathAbsent(t, output)
	assertNoReleaseStage(t, fixture.base)
}

func TestBuildReleaseArtifactSetFailsClosedAcrossPipelineBoundaries(t *testing.T) {
	for _, test := range pipelineFailureCases() {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReleaseFixture(t, true)
			output := filepath.Join(fixture.base, "release")
			if err := buildReleaseArtifactSet(context.Background(), fixture.inputs(output), test.runner(), test.operations()); err == nil {
				t.Fatal("expected injected failure")
			}
			assertPathAbsent(t, output)
			assertNoReleaseStage(t, fixture.base)
		})
	}
}

func pipelineFailureCases() []pipelineFailureCase {
	return []pipelineFailureCase{
		{
			name: "module enumeration",
			runner: func() commandRunner {
				return selectiveFailureRunner{delegate: execCommandRunner{}, name: "go", args: "list -m"}
			},
			operations: productionReleaseOperations,
		},
		{
			name:       "artifact write",
			runner:     func() commandRunner { return execCommandRunner{} },
			operations: writeFailureOperations,
		},
		{
			name:       "atomic rename",
			runner:     func() commandRunner { return execCommandRunner{} },
			operations: renameFailureOperations,
		},
		{
			name:       "post-publish verification",
			runner:     func() commandRunner { return execCommandRunner{} },
			operations: postPublishFailureOperations,
		},
	}
}

func writeFailureOperations() releaseOperations {
	operations := productionReleaseOperations()
	operations.beforeWrite = func(path string) error {
		if filepath.Base(path) == sbomFile {
			return errors.New("injected write failure")
		}
		return nil
	}
	return operations
}

func renameFailureOperations() releaseOperations {
	operations := productionReleaseOperations()
	operations.rename = func(string, string) error { return errors.New("injected rename failure") }
	return operations
}

func postPublishFailureOperations() releaseOperations {
	operations := productionReleaseOperations()
	operations.postPublish = func(string) error { return errors.New("injected verification failure") }
	return operations
}

func TestBuildReleaseArtifactSetRejectsArtifactMutation(t *testing.T) {
	fixture := newReleaseFixture(t, true)
	output := filepath.Join(fixture.base, "release")
	operations := productionReleaseOperations()
	mutated := false
	operations.beforeWrite = func(path string) error {
		if !mutated && filepath.Base(filepath.Dir(path)) == "artifacts" {
			mutated = true
			writeTestFile(t, fixture.artifact, "mutated artifact\n", 0o755)
		}
		return nil
	}
	if err := buildReleaseArtifactSet(context.Background(), fixture.inputs(output), execCommandRunner{}, operations); err == nil || !strings.Contains(err.Error(), "changed during release generation") {
		t.Fatalf("artifact mutation error = %v", err)
	}
	assertPathAbsent(t, output)
	assertNoReleaseStage(t, fixture.base)
}

func TestBuildReleaseArtifactSetRejectsSourceMutation(t *testing.T) {
	fixture := newReleaseFixture(t, true)
	output := filepath.Join(fixture.base, "release")
	operations := productionReleaseOperations()
	mutated := false
	operations.beforeWrite = func(string) error {
		if !mutated {
			mutated = true
			writeTestFile(t, filepath.Join(fixture.sourceRoot, "main.go"), "package artemisfixture\n\nvar Changed = true\n", 0o644)
		}
		return nil
	}
	if err := buildReleaseArtifactSet(context.Background(), fixture.inputs(output), execCommandRunner{}, operations); err == nil || !strings.Contains(err.Error(), "source changed during release generation") {
		t.Fatalf("source mutation error = %v", err)
	}
	assertPathAbsent(t, output)
	assertNoReleaseStage(t, fixture.base)
}

func TestReleaseArtifactVerificationRejectsTamperingAndUnknownJSON(t *testing.T) {
	fixture := newReleaseFixture(t, true)
	output := filepath.Join(fixture.base, "release")
	inputs := fixture.inputs(output)
	buildFixtureRelease(t, inputs, execCommandRunner{}, productionReleaseOperations())
	normalized, err := normalizeInputsForPublishedSet(inputs)
	if err != nil {
		t.Fatalf("normalize published inputs: %v", err)
	}
	inventory, err := loadModuleInventory(context.Background(), normalized.SourceRoot, normalized.Version, execCommandRunner{})
	if err != nil {
		t.Fatalf("load module inventory: %v", err)
	}
	report, err := readLicenseReport(filepath.Join(output, licenseReportFile))
	if err != nil {
		t.Fatalf("read license report: %v", err)
	}
	assertNonCanonicalSBOMRejected(t, output, normalized, inventory, report)
	writeTestFile(t, filepath.Join(output, "artifacts", "artemis"), "tampered\n", 0o755)
	if err := verifyArtifactSet(output, normalized, inventory); err == nil {
		t.Fatal("tampered release set passed verification")
	}
	assertStrictManifestJSON(t, fixture.base)
}

func assertNonCanonicalSBOMRejected(t *testing.T, output string, inputs releaseInputs, inventory moduleInventory, report licenseReport) {
	t.Helper()
	sbomPath := filepath.Join(filepath.Clean(output), sbomFile)
	sbom, err := os.ReadFile(filepath.Clean(sbomPath))
	if err != nil {
		t.Fatalf("read SBOM: %v", err)
	}
	tamperedSBOM := strings.Replace(string(sbom), `"artemis-release"`, `"artemis_release"`, 1)
	if tamperedSBOM == string(sbom) {
		t.Fatal("SBOM tamper fixture did not change canonical bytes")
	}
	writeExistingTestFile(t, sbomPath, tamperedSBOM)
	if err := validateSBOMFile(sbomPath, inputs, inventory, report); err == nil || !strings.Contains(err.Error(), "canonical release inputs") {
		t.Fatalf("schema-valid non-canonical SBOM error = %v", err)
	}
	writeExistingTestFile(t, sbomPath, string(sbom))
}

func assertStrictManifestJSON(t *testing.T, base string) {
	t.Helper()
	unknown := filepath.Join(base, "unknown.json")
	writeTestFile(t, unknown, "{\"schema_version\":\"artemis.release-set/v1\",\"unknown\":true}\n", 0o644)
	if _, err := readReleaseManifest(unknown); err == nil {
		t.Fatal("unknown manifest field passed strict decoding")
	}
	trailing := filepath.Join(base, "trailing.json")
	writeTestFile(t, trailing, "{} {}\n", 0o644)
	if _, err := readReleaseManifest(trailing); err == nil {
		t.Fatal("trailing manifest JSON passed strict decoding")
	}
}

func TestNormalizeArtifactsRejectsUnsafeSources(t *testing.T) {
	base := t.TempDir()
	artifact := filepath.Join(base, "artifact")
	writeTestFile(t, artifact, "artifact\n", 0o755)
	symlink := filepath.Join(base, "artifact-link")
	if err := os.Symlink(artifact, symlink); err != nil {
		t.Fatalf("create artifact symlink: %v", err)
	}
	tests := []struct {
		name      string
		artifacts []artifactSpec
	}{
		{name: "symlink", artifacts: []artifactSpec{{Name: "artemis", SourcePath: symlink}}},
		{name: "duplicate", artifacts: []artifactSpec{{Name: "artemis", SourcePath: artifact}, {Name: "artemis", SourcePath: artifact}}},
		{name: "reserved", artifacts: []artifactSpec{{Name: sbomFile, SourcePath: artifact}}},
		{name: "invalid name", artifacts: []artifactSpec{{Name: "../artemis", SourcePath: artifact}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := normalizeArtifacts(test.artifacts, filepath.Join(base, "release")); err == nil {
				t.Fatal("unsafe artifact source passed validation")
			}
		})
	}
}

func TestRenameExclusiveRejectsExistingDestination(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	destination := filepath.Join(base, "destination")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(destination, "sentinel"), "keep\n", 0o644)
	if err := renameExclusive(source, destination); err == nil {
		t.Fatal("exclusive publication replaced an existing destination")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("failed exclusive rename changed source: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Clean(destination), "sentinel"))
	if err != nil || string(data) != "keep\n" {
		t.Fatalf("failed exclusive rename changed destination: data=%q err=%v", data, err)
	}
}

func newReleaseFixture(t *testing.T, localLicense bool) releaseFixture {
	t.Helper()
	base := t.TempDir()
	sourceRoot := filepath.Join(base, "source")
	writeReleaseFixtureModule(t, sourceRoot, localLicense)
	commit, sourceDateEpoch := commitReleaseFixture(t, sourceRoot)
	artifact := filepath.Join(base, "build", "artemis")
	writeTestFile(t, artifact, "artemis release binary\n", 0o755)
	return releaseFixture{base: base, sourceRoot: sourceRoot, artifact: artifact, commit: commit, sourceDateEpoch: sourceDateEpoch}
}

func writeReleaseFixtureModule(t *testing.T, sourceRoot string, localLicense bool) {
	t.Helper()
	localRoot := filepath.Join(sourceRoot, "local")
	writeTestFile(t, filepath.Join(sourceRoot, "go.mod"), "module example.com/artemisfixture\n\ngo 1.27\n\nrequire example.com/local v0.0.0\n\nreplace example.com/local => ./local\n", 0o644)
	writeTestFile(t, filepath.Join(sourceRoot, "main.go"), "package artemisfixture\n\nimport _ \"example.com/local\"\n", 0o644)
	writeTestFile(t, filepath.Join(sourceRoot, "LICENSE"), "MIT License\n\nPermission is hereby granted, free of charge, to any person obtaining a copy.\n", 0o644)
	writeTestFile(t, filepath.Join(localRoot, "go.mod"), "module example.com/local\n\ngo 1.27\n", 0o644)
	writeTestFile(t, filepath.Join(localRoot, "local.go"), "package local\n", 0o644)
	if localLicense {
		writeTestFile(t, filepath.Join(localRoot, "LICENSE"), "MIT License\n\nPermission is hereby granted, free of charge, to any person obtaining a copy.\n", 0o644)
	}
}

func commitReleaseFixture(t *testing.T, sourceRoot string) (string, int64) {
	t.Helper()
	ctx := context.Background()
	runner := execCommandRunner{}
	for _, command := range [][]string{
		{"init"},
		{"config", "user.name", testCommitName},
		{"config", "user.email", testCommitEmail},
		{"add", "."},
		{"commit", "-m", "fixture"},
	} {
		if _, err := runner.Output(ctx, sourceRoot, "git", command...); err != nil {
			t.Fatalf("git %s: %v", strings.Join(command, " "), err)
		}
	}
	commitBytes, err := runner.Output(ctx, sourceRoot, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("read fixture commit: %v", err)
	}
	epochBytes, err := runner.Output(ctx, sourceRoot, "git", "show", "-s", "--format=%ct", "HEAD")
	if err != nil {
		t.Fatalf("read fixture commit epoch: %v", err)
	}
	sourceDateEpoch, err := strconv.ParseInt(strings.TrimSpace(string(epochBytes)), 10, 64)
	if err != nil {
		t.Fatalf("parse fixture commit epoch: %v", err)
	}
	return strings.TrimSpace(string(commitBytes)), sourceDateEpoch
}

func (fixture releaseFixture) inputs(output string) releaseInputs {
	return releaseInputs{
		SourceRoot: fixture.sourceRoot, OutputRoot: output, Version: "v1.2.3", Commit: fixture.commit,
		BuildProfile: buildProfileRelease, SourceDateEpoch: fixture.sourceDateEpoch,
		Target:          targetPlatform{OS: "darwin", Arch: "arm64"},
		ToolchainDigest: "sha256:" + strings.Repeat("a", 64),
		Artifacts:       []artifactSpec{{Name: "artemis", SourcePath: fixture.artifact}},
	}
}

func buildFixtureRelease(t *testing.T, inputs releaseInputs, runner commandRunner, operations releaseOperations) {
	t.Helper()
	if err := buildReleaseArtifactSet(context.Background(), inputs, runner, operations); err != nil {
		t.Fatalf("build release artifact set: %v", err)
	}
}

func normalizeInputsForPublishedSet(inputs releaseInputs) (releaseInputs, error) {
	output := inputs.OutputRoot
	inputs.OutputRoot = filepath.Join(filepath.Dir(output), "unused-verification-output")
	normalized, err := normalizeInputs(context.Background(), inputs, execCommandRunner{})
	if err != nil {
		return releaseInputs{}, err
	}
	normalized.OutputRoot = output
	return normalized, nil
}

func snapshotTree(t *testing.T, root string) map[string]fileSnapshot {
	t.Helper()
	files := make(map[string]fileSnapshot)
	releaseRoot, err := os.OpenRoot(filepath.Clean(root))
	if err != nil {
		t.Fatalf("open release root: %v", err)
	}
	walkErr := fs.WalkDir(releaseRoot.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := releaseRoot.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = fileSnapshot{Mode: uint32(info.Mode().Perm()), Data: string(data)}
		return nil
	})
	closeErr := releaseRoot.Close()
	if walkErr != nil || closeErr != nil {
		t.Fatalf("snapshot release tree: walk=%v close=%v", walkErr, closeErr)
	}
	return files
}

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %s exists after failed publication: %v", path, err)
	}
}

func assertNoReleaseStage(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read stage parent: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".stage-") {
			t.Fatalf("release stage leaked after failure: %s", entry.Name())
		}
	}
}

func TestSelectiveFailureRunner(t *testing.T) {
	runner := selectiveFailureRunner{delegate: execCommandRunner{}, name: "go", args: "list -m"}
	_, err := runner.Output(context.Background(), t.TempDir(), "go", "list", "-m")
	if err == nil || err.Error() != "injected command failure" {
		t.Fatalf("unexpected injected error: %v", err)
	}
}
