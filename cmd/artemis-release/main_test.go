package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseCLIInputs(t *testing.T) {
	var stderr bytes.Buffer
	inputs, err := parseCLIInputs([]string{
		"--source-root", "/source",
		"--output", "/release",
		"--version", "v1.2.3",
		"--commit", "0123456789012345678901234567890123456789",
		"--source-date-epoch", "1710000000",
		"--target", "darwin/arm64",
		"--toolchain-digest", "sha256:0123456789012345678901234567890123456789012345678901234567890123",
		"--artifact", "artemis=/build/artemis",
	}, &stderr)
	if err != nil {
		t.Fatalf("parseCLIInputs: %v", err)
	}
	if inputs.Target != (targetPlatform{OS: "darwin", Arch: "arm64"}) || len(inputs.Artifacts) != 1 {
		t.Fatalf("unexpected parsed inputs: %+v", inputs)
	}
}

func TestParseCLIInputsRejectsMalformedContract(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "target", args: []string{"--target", "darwin"}},
		{name: "artifact", args: []string{"--target", "darwin/arm64", "--artifact", "artemis"}},
		{name: "positional", args: []string{"--target", "darwin/arm64", "unexpected"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseCLIInputs(test.args, &bytes.Buffer{}); err == nil {
				t.Fatal("expected malformed CLI contract to fail")
			}
		})
	}
}

func TestRunCLIPublishesReleaseSet(t *testing.T) {
	fixture := newReleaseFixture(t, true)
	output := filepath.Join(fixture.base, "release-cli")
	inputs := fixture.inputs(output)
	args := []string{
		"--source-root", inputs.SourceRoot,
		"--output", inputs.OutputRoot,
		"--version", inputs.Version,
		"--commit", inputs.Commit,
		"--source-date-epoch", strconv.FormatInt(inputs.SourceDateEpoch, 10),
		"--target", inputs.Target.OS + "/" + inputs.Target.Arch,
		"--toolchain-digest", inputs.ToolchainDigest,
		"--artifact", inputs.Artifacts[0].Name + "=" + inputs.Artifacts[0].SourcePath,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := runCLI(args, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("runCLI exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "published Artemis release set") {
		t.Fatalf("runCLI stdout=%q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(output, releaseManifestFile)); err != nil {
		t.Fatalf("runCLI did not publish manifest: %v", err)
	}
}

func TestRunCLIRejectsInvalidArgumentsAndBuildFailure(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "invalid arguments", args: []string{"--target", "invalid"}, want: 2},
		{name: "invalid build inputs", args: []string{"--target", "darwin/arm64"}, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if exitCode := runCLI(test.args, &bytes.Buffer{}, &bytes.Buffer{}); exitCode != test.want {
				t.Fatalf("runCLI exit=%d, want %d", exitCode, test.want)
			}
		})
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
