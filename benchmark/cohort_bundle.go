package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxArtifactFileBytes bounds each sealed output before file I/O or capture.
const MaxArtifactFileBytes int64 = 16 << 20

// MaxArtifactBundleBytes bounds all sealed outputs and their manifest together.
const MaxArtifactBundleBytes int64 = 64 << 20
const maxArtifactManifestBytes int64 = 64 << 10

// ArtifactFile is one complete member of an admitted sealed cohort.
type ArtifactFile struct {
	Digest  ArtifactDigest
	Content []byte
}

// ArtifactBundle preserves every sealed output, including opaque profile bytes.
// These are integrity-bound data, not authenticated production or execution authority.
type ArtifactBundle struct {
	Receipt  ArtifactReceipt
	Manifest []byte
	Files    []ArtifactFile
}

// ReadArtifactBundle captures a complete bounded cohort through the same owner
// used by ReadRawArtifacts. Cancellation is cooperative during regular-file I/O.
func ReadArtifactBundle(ctx context.Context, outputDir string) (ArtifactBundle, error) {
	var files []ArtifactFile
	var buffers []*bytes.Buffer
	capture := func(digest ArtifactDigest) io.Writer {
		files = append(files, ArtifactFile{Digest: digest})
		buffer := bytes.NewBuffer(make([]byte, 0, int(digest.Bytes)))
		buffers = append(buffers, buffer)
		return buffer
	}
	receipt, manifest, err := readArtifactCohort(ctx, outputDir, capture)
	if err != nil {
		return ArtifactBundle{}, err
	}
	for index := range files {
		files[index].Content = buffers[index].Bytes()
	}
	return ArtifactBundle{Receipt: receipt, Manifest: manifest, Files: files}, nil
}

func readArtifactCohort(ctx context.Context, outputDir string, capture func(ArtifactDigest) io.Writer) (receipt ArtifactReceipt, manifest []byte, returnErr error) {
	root, identity, err := openCohortRoot(ctx, outputDir)
	if err != nil {
		return receipt, nil, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	var data bytes.Buffer
	if _, err := digestArtifact(ctx, root, "manifest.json", maxArtifactManifestBytes, &data); err != nil {
		return receipt, nil, fmt.Errorf("complete benchmark artifact manifest required: %w", err)
	}
	manifest = data.Bytes()
	if err := decodeCohortReceipt(manifest, &receipt); err != nil {
		return ArtifactReceipt{}, nil, err
	}
	for _, expected := range receipt.Artifacts {
		var destination io.Writer
		if capture != nil {
			destination = capture(expected)
		}
		actual, err := digestArtifact(ctx, root, expected.Path, expected.Bytes, destination)
		if err != nil {
			return ArtifactReceipt{}, nil, fmt.Errorf("artifact receipt mismatch: %s: %w", expected.Path, err)
		}
		if actual != expected {
			return ArtifactReceipt{}, nil, fmt.Errorf("artifact receipt mismatch: %s", expected.Path)
		}
	}
	if err := verifyCohortRoot(outputDir, identity); err != nil {
		return ArtifactReceipt{}, nil, err
	}
	return receipt, manifest, ctx.Err()
}

func decodeCohortReceipt(data []byte, receipt *ArtifactReceipt) error {
	if err := json.Unmarshal(data, receipt); err != nil {
		return err
	}
	for _, artifact := range receipt.Artifacts {
		if !filepath.IsLocal(artifact.Path) || filepath.Clean(artifact.Path) != artifact.Path {
			return fmt.Errorf("artifact path escapes or aliases output directory: %s", artifact.Path)
		}
	}
	canonical, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil || !bytes.Equal(canonical, data) || receipt.Version != "1.0.0" || receipt.RunDate.IsZero() ||
		(receipt.ClaimStatus != "observed" && receipt.ClaimStatus != "benchmarked" && receipt.ClaimStatus != "invalid") ||
		len(receipt.Artifacts) < 3 || len(receipt.Artifacts) > 5 {
		return fmt.Errorf("invalid or noncanonical artifact manifest")
	}
	seen := make(map[string]bool, len(receipt.Artifacts))
	total := int64(len(data))
	for _, artifact := range receipt.Artifacts {
		if !filepath.IsLocal(artifact.Path) || filepath.Clean(artifact.Path) != artifact.Path || len(artifact.Path) > 4096 || strings.ContainsAny(artifact.Path, "\x00\r\n") ||
			artifact.Path == "manifest.json" || seen[artifact.Path] || !validSHA256(artifact.SHA256) || artifact.SHA256 != strings.ToLower(artifact.SHA256) || artifact.Bytes < 0 || artifact.Bytes > MaxArtifactFileBytes {
			return fmt.Errorf("invalid, duplicate or oversized artifact receipt")
		}
		if artifact.Bytes > MaxArtifactBundleBytes-total {
			return fmt.Errorf("artifact cohort aggregate byte limit exceeded")
		}
		total += artifact.Bytes
		seen[artifact.Path] = true
	}
	for _, required := range []string{"scorecard.json", "scorecard.md", "report.md"} {
		if !seen[required] {
			return fmt.Errorf("missing artifact receipt: %s", required)
		}
	}
	return nil
}

func openCohortRoot(ctx context.Context, path string) (*os.Root, os.FileInfo, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("artifact cohort: owning context required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	identity, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !identity.IsDir() || identity.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("artifact cohort: real output directory required")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(identity, opened) {
		return nil, nil, errors.Join(fmt.Errorf("artifact cohort: output directory changed"), err, root.Close())
	}
	return root, identity, nil
}

func verifyCohortRoot(path string, identity os.FileInfo) error {
	current, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(current, identity) {
		return fmt.Errorf("artifact cohort: output directory changed")
	}
	return nil
}
