package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

type artifactFlags []artifactSpec

func (values *artifactFlags) String() string {
	items := make([]string, 0, len(*values))
	for _, value := range *values {
		items = append(items, value.Name+"="+value.SourcePath)
	}
	return strings.Join(items, ",")
}

func (values *artifactFlags) Set(value string) error {
	name, path, found := strings.Cut(value, "=")
	if !found || name == "" || path == "" {
		return errors.New("artifact must be name=path")
	}
	*values = append(*values, artifactSpec{Name: name, SourcePath: path})
	return nil
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	inputs, err := parseCLIInputs(args, stderr)
	if err != nil {
		if _, reportErr := fmt.Fprintf(stderr, "artemis-release: %v\n", err); reportErr != nil {
			return 1
		}
		return 2
	}
	if err := buildReleaseArtifactSet(context.Background(), inputs, execCommandRunner{}, productionReleaseOperations()); err != nil {
		if _, reportErr := fmt.Fprintf(stderr, "artemis-release: %v\n", err); reportErr != nil {
			return 1
		}
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "published Artemis release set: %s\n", inputs.OutputRoot); err != nil {
		if _, reportErr := fmt.Fprintf(stderr, "artemis-release: report success: %v\n", err); reportErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

func parseCLIInputs(args []string, stderr io.Writer) (releaseInputs, error) {
	flags := flag.NewFlagSet("artemis-release", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var inputs releaseInputs
	var target string
	var artifacts artifactFlags
	flags.StringVar(&inputs.SourceRoot, "source-root", ".", "clean Artemis module root inside a Git worktree")
	flags.StringVar(&inputs.OutputRoot, "output", "", "fresh release-set destination")
	flags.StringVar(&inputs.Version, "version", "", "release semantic version")
	flags.StringVar(&inputs.Commit, "commit", "", "exact full Git HEAD object ID")
	flags.StringVar(&inputs.BuildProfile, "build-profile", buildProfileRelease, "canonical build profile")
	flags.Int64Var(&inputs.SourceDateEpoch, "source-date-epoch", -1, "reproducible Unix timestamp")
	flags.StringVar(&target, "target", "", "target os/arch")
	flags.StringVar(&inputs.ToolchainDigest, "toolchain-digest", "", "sha256 digest of the pinned Go toolchain")
	flags.Var(&artifacts, "artifact", "release deliverable as name=path; repeatable")
	if err := flags.Parse(args); err != nil {
		return releaseInputs{}, err
	}
	if flags.NArg() != 0 {
		return releaseInputs{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	osName, architecture, found := strings.Cut(target, "/")
	if !found || osName == "" || architecture == "" || strings.Contains(architecture, "/") {
		return releaseInputs{}, errors.New("target must be os/arch")
	}
	inputs.Target = targetPlatform{OS: osName, Arch: architecture}
	inputs.Artifacts = artifacts
	return inputs, nil
}
