package main

import (
	"strings"
	"testing"
)

func TestValidReleaseVersion(t *testing.T) {
	for _, version := range []string{"0.0.0", "v1.2.3", "1.2.3-rc.1", "1.2.3-alpha-beta+build.001"} {
		if !validReleaseVersion(version) {
			t.Fatalf("valid release version %q was rejected", version)
		}
	}
	for _, version := range []string{"", "v", "01.2.3", "1.02.3", "1.2.03", "1.2", "1.2.3-", "1.2.3-01", "1.2.3+", "1.2.3+a..b", "1.2.3+ä"} {
		if validReleaseVersion(version) {
			t.Fatalf("invalid release version %q passed", version)
		}
	}
}

func TestValidateScalarInputsRejectsInvalidIdentity(t *testing.T) {
	valid := releaseInputs{
		Version: "v1.2.3", Commit: strings.Repeat("a", 40), BuildProfile: buildProfileRelease,
		SourceDateEpoch: 1_710_000_000, Target: targetPlatform{OS: "darwin", Arch: "arm64"},
		ToolchainDigest: "sha256:" + strings.Repeat("b", 64),
	}
	tests := []struct {
		name   string
		mutate func(*releaseInputs)
	}{
		{name: "version", mutate: func(input *releaseInputs) { input.Version = "1.2" }},
		{name: "commit", mutate: func(input *releaseInputs) { input.Commit = strings.Repeat("A", 40) }},
		{name: "profile", mutate: func(input *releaseInputs) { input.BuildProfile = "debug" }},
		{name: "epoch", mutate: func(input *releaseInputs) { input.SourceDateEpoch = 253_402_300_800 }},
		{name: "target", mutate: func(input *releaseInputs) { input.Target = targetPlatform{OS: "windows", Arch: "amd64"} }},
		{name: "toolchain", mutate: func(input *releaseInputs) { input.ToolchainDigest = "sha256:" + strings.Repeat("B", 64) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if err := validateScalarInputs(input); err == nil {
				t.Fatal("invalid release identity passed validation")
			}
		})
	}
}

func TestValidArtifactName(t *testing.T) {
	for _, name := range []string{"artemis", "artemis-darwin_arm64.v1"} {
		if !validArtifactName(name) {
			t.Fatalf("valid artifact name %q was rejected", name)
		}
	}
	for _, name := range []string{"", ".artemis", "../artemis", "artemis/path", "artemis ä", strings.Repeat("a", 129)} {
		if validArtifactName(name) {
			t.Fatalf("invalid artifact name %q passed", name)
		}
	}
}
