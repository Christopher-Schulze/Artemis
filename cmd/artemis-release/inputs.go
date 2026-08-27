package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type commandRunner interface {
	Output(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name)
	command.Args = append(command.Args, args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func normalizeInputs(ctx context.Context, in releaseInputs, runner commandRunner) (releaseInputs, error) {
	var err error
	in.SourceRoot, err = canonicalDirectory(in.SourceRoot)
	if err != nil {
		return releaseInputs{}, fmt.Errorf("source root: %w", err)
	}
	in.OutputRoot, err = canonicalFreshOutput(in.OutputRoot, in.SourceRoot)
	if err != nil {
		return releaseInputs{}, fmt.Errorf("output root: %w", err)
	}
	if err = validateScalarInputs(in); err != nil {
		return releaseInputs{}, err
	}
	in.Artifacts, err = normalizeArtifacts(in.Artifacts, in.OutputRoot)
	if err != nil {
		return releaseInputs{}, err
	}
	gitRoot, err := validateGitSource(ctx, in, runner)
	if err != nil {
		return releaseInputs{}, err
	}
	if withinPath(gitRoot, in.OutputRoot) {
		return releaseInputs{}, errors.New("output root must be outside the source Git worktree")
	}
	return in, nil
}

func canonicalDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("must be a directory")
	}
	return filepath.Clean(resolved), nil
}

func canonicalFreshOutput(path, sourceRoot string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err = os.Lstat(abs); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return "", err
		}
		return "", errors.New("destination already exists")
	}
	parent, err := canonicalDirectory(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("parent: %w", err)
	}
	resolved := filepath.Join(parent, filepath.Base(abs))
	if withinPath(sourceRoot, resolved) {
		return "", errors.New("destination must be outside the source root")
	}
	return resolved, nil
}

func validateScalarInputs(in releaseInputs) error {
	if !validReleaseVersion(in.Version) {
		return fmt.Errorf("version %q is not a release semantic version", in.Version)
	}
	if (len(in.Commit) != 40 && len(in.Commit) != 64) || !lowerHex(in.Commit) {
		return fmt.Errorf("commit must be a full lowercase Git object ID")
	}
	if in.BuildProfile != buildProfileRelease {
		return fmt.Errorf("build profile must be %q", buildProfileRelease)
	}
	if in.SourceDateEpoch < 0 || in.SourceDateEpoch > 253_402_300_799 {
		return errors.New("source-date-epoch must produce an RFC 3339 UTC timestamp")
	}
	if !validTarget(in.Target) {
		return fmt.Errorf("unsupported target %s/%s", in.Target.OS, in.Target.Arch)
	}
	toolchainHash := strings.TrimPrefix(in.ToolchainDigest, "sha256:")
	if len(toolchainHash) != 64 || !lowerHex(toolchainHash) || toolchainHash == in.ToolchainDigest {
		return errors.New("toolchain digest must be sha256:<64 lowercase hex>")
	}
	return nil
}

func validReleaseVersion(version string) bool {
	version = strings.TrimPrefix(version, "v")
	if version == "" || strings.Count(version, "+") > 1 {
		return false
	}
	coreAndPre, metadata, hasMetadata := strings.Cut(version, "+")
	if hasMetadata && !validIdentifierList(metadata, false) {
		return false
	}
	core, prerelease, hasPrerelease := strings.Cut(coreAndPre, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if !validNumericIdentifier(part) {
			return false
		}
	}
	return !hasPrerelease || validIdentifierList(prerelease, true)
}

func validIdentifierList(value string, rejectNumericLeadingZero bool) bool {
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" || !asciiIdentifier(identifier) {
			return false
		}
		if rejectNumericLeadingZero && len(identifier) > 1 && identifier[0] == '0' && numeric(identifier) {
			return false
		}
	}
	return true
}

func validNumericIdentifier(value string) bool {
	return value != "" && (len(value) == 1 || value[0] != '0') && numeric(value)
}

func numeric(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func asciiIdentifier(value string) bool {
	for _, char := range value {
		if char != '-' && (char < '0' || char > '9') && (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') {
			return false
		}
	}
	return true
}

func lowerHex(value string) bool {
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validTarget(target targetPlatform) bool {
	return target == (targetPlatform{OS: "darwin", Arch: "arm64"}) || target == (targetPlatform{OS: "linux", Arch: "amd64"})
}

func normalizeArtifacts(artifacts []artifactSpec, outputRoot string) ([]artifactSpec, error) {
	if len(artifacts) == 0 {
		return nil, errors.New("at least one --artifact name=path is required")
	}
	seen := make(map[string]struct{}, len(artifacts))
	normalized := make([]artifactSpec, 0, len(artifacts))
	for _, artifact := range artifacts {
		resolved, err := normalizeArtifact(artifact, outputRoot, seen)
		if err != nil {
			return nil, err
		}
		seen[resolved.Name] = struct{}{}
		normalized = append(normalized, resolved)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Name < normalized[j].Name })
	return normalized, nil
}

func normalizeArtifact(artifact artifactSpec, outputRoot string, seen map[string]struct{}) (artifactSpec, error) {
	if !validArtifactName(artifact.Name) {
		return artifactSpec{}, fmt.Errorf("artifact name %q is invalid", artifact.Name)
	}
	if _, exists := seen[artifact.Name]; exists || reservedReleaseName(artifact.Name) {
		return artifactSpec{}, fmt.Errorf("artifact name %q is duplicate or reserved", artifact.Name)
	}
	abs, err := filepath.Abs(artifact.SourcePath)
	if err != nil {
		return artifactSpec{}, fmt.Errorf("artifact %s: %w", artifact.Name, err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return artifactSpec{}, fmt.Errorf("artifact %s: %w", artifact.Name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 {
		return artifactSpec{}, fmt.Errorf("artifact %s must be a non-empty regular non-symlink file", artifact.Name)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return artifactSpec{}, fmt.Errorf("artifact %s: resolve path: %w", artifact.Name, err)
	}
	if withinPath(outputRoot, resolved) {
		return artifactSpec{}, fmt.Errorf("artifact %s must be outside the output root", artifact.Name)
	}
	digest, err := hashFile(resolved)
	if err != nil {
		return artifactSpec{}, fmt.Errorf("artifact %s: hash source: %w", artifact.Name, err)
	}
	return artifactSpec{Name: artifact.Name, SourcePath: filepath.Clean(resolved), SourceSHA256: digest}, nil
}

func validArtifactName(name string) bool {
	if len(name) == 0 || len(name) > 128 || !asciiAlphaNumeric(rune(name[0])) {
		return false
	}
	for _, char := range name[1:] {
		if char != '.' && char != '_' && char != '-' && !asciiAlphaNumeric(char) {
			return false
		}
	}
	return true
}

func asciiAlphaNumeric(char rune) bool {
	return char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
}

func reservedReleaseName(name string) bool {
	return name == checksumsFile || name == licenseReportFile || name == releaseManifestFile || name == sbomFile || name == "licenses"
}

func validateGitSource(ctx context.Context, in releaseInputs, runner commandRunner) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	canonicalTop, err := gitRootForSource(ctx, in.SourceRoot, runner)
	if err != nil {
		return "", fmt.Errorf("source Git root: %w", err)
	}
	if !withinPath(canonicalTop, in.SourceRoot) {
		return "", errors.New("source root must be inside the Git worktree")
	}
	head, err := runner.Output(ctx, in.SourceRoot, "git", "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != in.Commit {
		return "", errors.New("declared commit does not match Git HEAD")
	}
	epoch, err := runner.Output(ctx, in.SourceRoot, "git", "show", "-s", "--format=%ct", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read source commit epoch: %w", err)
	}
	commitEpoch, err := strconv.ParseInt(strings.TrimSpace(string(epoch)), 10, 64)
	if err != nil || commitEpoch != in.SourceDateEpoch {
		return "", errors.New("source-date-epoch does not match the Git HEAD committer timestamp")
	}
	status, err := runner.Output(ctx, in.SourceRoot, "git", "status", "--porcelain=v1", "--untracked-files=all", "--", ".")
	if err != nil {
		return "", fmt.Errorf("source Git status: %w", err)
	}
	if len(status) != 0 {
		return "", errors.New("source Git worktree is dirty")
	}
	return canonicalTop, nil
}

func gitRootForSource(ctx context.Context, sourceRoot string, runner commandRunner) (string, error) {
	top, err := runner.Output(ctx, sourceRoot, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	canonicalTop, err := canonicalDirectory(strings.TrimSpace(string(top)))
	if err != nil {
		return "", err
	}
	return ancestorWithIdentity(sourceRoot, canonicalTop)
}

func ancestorWithIdentity(path, expected string) (string, error) {
	expectedInfo, err := os.Stat(expected)
	if err != nil {
		return "", err
	}
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil {
			return "", err
		}
		if os.SameFile(info, expectedInfo) {
			return current, nil
		}
		if parent := filepath.Dir(current); parent == current {
			return "", errors.New("git root is not an ancestor of the source root")
		}
	}
}

func withinPath(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	for current := filepath.Clean(candidate); ; current = filepath.Dir(current) {
		info, statErr := os.Stat(current)
		if statErr == nil && os.SameFile(rootInfo, info) {
			return true
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return true
		}
		if parent := filepath.Dir(current); parent == current {
			return false
		}
	}
}

func stableDigest(parts ...string) string {
	digest := stableDigestRaw(parts...)
	return hex.EncodeToString(digest[:])
}

func stableDigestRaw(parts ...string) [sha256.Size]byte {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}
