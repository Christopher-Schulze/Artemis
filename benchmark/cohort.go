package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ArtifactReceipt is the final seal of a complete benchmark artifact cohort.
type ArtifactReceipt struct {
	Version     string           `json:"version"`
	RunDate     time.Time        `json:"runDate"`
	ClaimStatus string           `json:"claimStatus"`
	Artifacts   []ArtifactDigest `json:"artifacts"`
}

// ArtifactDigest binds a relative artifact path to its exact installed bytes.
type ArtifactDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

func sealArtifacts(ctx context.Context, cfg HarnessConfig, sc *Scorecard) (returnErr error) {
	root, identity, err := openCohortRoot(ctx, cfg.OutputDir)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	receipt, err := collectCohortReceipt(ctx, root, cfg, sc)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > int(maxArtifactManifestBytes) {
		return fmt.Errorf("artifact manifest exceeds limit")
	}
	var validated ArtifactReceipt
	if err := decodeCohortReceipt(data, &validated); err != nil {
		return err
	}
	if err := verifyCohortRoot(cfg.OutputDir, identity); err != nil {
		return err
	}
	if err := writeArtifactInRoot(root, "manifest.json", data); err != nil {
		return err
	}
	return errors.Join(ctx.Err(), verifyCohortRoot(cfg.OutputDir, identity))
}

func collectCohortReceipt(ctx context.Context, root *os.Root, cfg HarnessConfig, sc *Scorecard) (ArtifactReceipt, error) {
	receipt := ArtifactReceipt{Version: "1.0.0", RunDate: sc.Date, ClaimStatus: sc.Claim.Status}
	paths := []string{"scorecard.json", "scorecard.md", "report.md"}
	outputRoot, err := filepath.Abs(cfg.OutputDir)
	if err != nil {
		return receipt, err
	}
	for _, profile := range []string{cfg.Profile.CPUProfilePath, cfg.Profile.MemProfilePath} {
		if profile == "" {
			continue
		}
		profilePath, err := filepath.Abs(profile)
		if err != nil {
			return receipt, err
		}
		relative, err := filepath.Rel(outputRoot, profilePath)
		if err != nil {
			return receipt, err
		}
		paths = append(paths, relative)
	}
	for _, path := range paths {
		artifact, err := digestArtifact(ctx, root, path, MaxArtifactFileBytes, nil)
		if err != nil {
			return receipt, err
		}
		receipt.Artifacts = append(receipt.Artifacts, artifact)
	}
	return receipt, nil
}

func readSealedArtifacts(outputDir string) (RawArtifacts, error) {
	var scorecard, markdown strings.Builder
	capture := func(artifact ArtifactDigest) io.Writer {
		switch artifact.Path {
		case "scorecard.json":
			return &scorecard
		case "scorecard.md":
			return &markdown
		default:
			return nil
		}
	}
	if _, _, err := readArtifactCohort(context.Background(), outputDir, capture); err != nil {
		return RawArtifacts{}, err
	}
	return RawArtifacts{ScorecardPath: outputDir, ScorecardJSON: scorecard.String(), ScorecardMD: markdown.String()}, nil
}
