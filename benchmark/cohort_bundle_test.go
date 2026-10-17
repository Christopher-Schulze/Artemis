package benchmark

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactBundleCapturesEveryActualHarnessOutputAndProfile(t *testing.T) {
	config := harnessFixtureConfigForTest(t)
	config.Profile = ProfileConfig{CPUProfilePath: filepath.Join(config.OutputDir, "cpu.pprof"), MemProfilePath: filepath.Join(config.OutputDir, "heap.pprof")}
	card, err := NewHarness(config).Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := ReadArtifactBundle(t.Context(), config.OutputDir)
	if err != nil || len(bundle.Files) != 5 || len(bundle.Receipt.Artifacts) != 5 || !bundle.Receipt.RunDate.Equal(card.Date) || bundle.Receipt.ClaimStatus != card.Claim.Status {
		t.Fatalf("actual complete cohort unavailable: members=%d err=%v", len(bundle.Files), err)
	}
	manifest, err := os.ReadFile(filepath.Join(config.OutputDir, "manifest.json"))
	if err != nil || !bytes.Equal(manifest, bundle.Manifest) {
		t.Fatalf("manifest bytes changed: %v", err)
	}
	for index, member := range bundle.Files {
		actual, err := os.ReadFile(filepath.Join(config.OutputDir, member.Digest.Path))
		if err != nil || member.Digest != bundle.Receipt.Artifacts[index] || !bytes.Equal(actual, member.Content) || int64(len(member.Content)) != member.Digest.Bytes || fmt.Sprintf("%x", sha256.Sum256(member.Content)) != member.Digest.SHA256 {
			t.Fatalf("member %q lost exact actual bytes/digest: %v", member.Digest.Path, err)
		}
		if strings.HasSuffix(member.Digest.Path, ".pprof") {
			profile, err := gzip.NewReader(bytes.NewReader(member.Content))
			if err != nil {
				t.Fatal(err)
			}
			read, readErr := io.Copy(io.Discard, profile)
			if err := errors.Join(readErr, profile.Close()); err != nil || read == 0 {
				t.Fatalf("actual profile is incomplete: %v", err)
			}
		}
	}
	raw, err := ReadRawArtifacts(config.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range bundle.Files {
		if member.Digest.Path == "scorecard.json" && string(member.Content) != raw.ScorecardJSON || member.Digest.Path == "scorecard.md" && string(member.Content) != raw.ScorecardMD {
			t.Fatal("legacy capture differs from complete capture")
		}
	}
}

func TestArtifactBundleRejectsChangedReceiptMetadataBeforeReadingMembers(t *testing.T) {
	config := harnessFixtureConfigForTest(t)
	if _, err := NewHarness(config).Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	bundle, err := ReadArtifactBundle(t.Context(), config.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"version", "claim", "omitted", "duplicate", "missing-required", "traversal", "alias", "self-reference", "digest", "negative-size", "file-limit", "aggregate-limit"} {
		t.Run(change, func(t *testing.T) {
			receipt := bundle.Receipt
			receipt.Artifacts = append([]ArtifactDigest{}, receipt.Artifacts...)
			switch change {
			case "version":
				receipt.Version = "2.0.0"
			case "claim":
				receipt.ClaimStatus = "success"
			case "omitted":
				receipt.Artifacts = receipt.Artifacts[:2]
			case "duplicate":
				receipt.Artifacts[1] = receipt.Artifacts[0]
			case "missing-required":
				receipt.Artifacts[2].Path = "unknown.md"
			case "traversal":
				receipt.Artifacts[0].Path = "../scorecard.json"
			case "alias":
				receipt.Artifacts[0].Path = "./scorecard.json"
			case "self-reference":
				receipt.Artifacts[0].Path = "manifest.json"
			case "digest":
				receipt.Artifacts[0].SHA256 = strings.Repeat("z", 64)
			case "negative-size":
				receipt.Artifacts[0].Bytes = -1
			case "file-limit":
				receipt.Artifacts[0].Bytes = MaxArtifactFileBytes + 1
			case "aggregate-limit":
				receipt.Artifacts = append(receipt.Artifacts, ArtifactDigest{Path: "cpu.pprof", SHA256: receipt.Artifacts[0].SHA256}, ArtifactDigest{Path: "heap.pprof", SHA256: receipt.Artifacts[0].SHA256})
				for index := range receipt.Artifacts {
					receipt.Artifacts[index].Bytes = MaxArtifactFileBytes
				}
			}
			content, err := json.MarshalIndent(receipt, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(config.OutputDir, "manifest.json"), content, 0o600); err != nil {
				t.Fatal(err)
			}
			if read, err := ReadArtifactBundle(t.Context(), config.OutputDir); err == nil || len(read.Files) != 0 || len(read.Manifest) != 0 {
				t.Fatal("changed receipt acquired complete artifact identity")
			} else if change == "aggregate-limit" && !strings.Contains(err.Error(), "aggregate") {
				t.Fatalf("aggregate bound was not checked before file I/O: %v", err)
			}
		})
	}
}

func TestArtifactBundleRejectsChangedBytesAndSymlinkedRoots(t *testing.T) {
	config := harnessFixtureConfigForTest(t)
	if _, err := NewHarness(config).Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(config.OutputDir, alias); err != nil {
		t.Fatal(err)
	}
	if read, err := ReadArtifactBundle(t.Context(), alias); err == nil || len(read.Files) != 0 {
		t.Fatal("symlinked output root acquired proof")
	}
	if _, err := ReadRawArtifacts(alias); err == nil {
		t.Fatal("legacy reader bypassed root admission")
	}
	file, err := os.OpenFile(filepath.Join(config.OutputDir, "scorecard.md"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteAt([]byte("["), 0)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if read, err := ReadArtifactBundle(t.Context(), config.OutputDir); err == nil || len(read.Files) != 0 {
		t.Fatal("changed actual output bytes acquired proof")
	}
}

type cohortActionWriter struct {
	action func() error
	done   bool
}

func (w *cohortActionWriter) Write(data []byte) (int, error) {
	if !w.done {
		w.done = true
		if err := w.action(); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}

func TestArtifactCohortObservesCancellationAndRootReplacement(t *testing.T) {
	for _, change := range []string{"cancelled-before", "cancelled-during", "root-moved", "file-grown"} {
		t.Run(change, func(t *testing.T) {
			config := harnessFixtureConfigForTest(t)
			if _, err := NewHarness(config).Run(t.Context()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if change == "cancelled-before" {
				cancel()
			}
			acted := false
			capture := func(digest ArtifactDigest) io.Writer {
				return &cohortActionWriter{action: func() error {
					if acted {
						return nil
					}
					acted = true
					switch change {
					case "cancelled-during":
						cancel()
					case "root-moved":
						destination := filepath.Join(filepath.Dir(config.OutputDir), "moved")
						if _, err := os.Lstat(destination); !os.IsNotExist(err) {
							return fmt.Errorf("unexpected move destination: %v", err)
						}
						return os.Rename(config.OutputDir, destination)
					case "file-grown":
						file, err := os.OpenFile(filepath.Join(config.OutputDir, digest.Path), os.O_WRONLY|os.O_APPEND, 0)
						if err != nil {
							return err
						}
						_, writeErr := file.Write([]byte("x"))
						return errors.Join(writeErr, file.Close())
					}
					return nil
				}}
			}
			_, _, err := readArtifactCohort(ctx, config.OutputDir, capture)
			if err == nil || change != "cancelled-before" && !acted {
				t.Fatalf("changed/cancelled actual read acquired proof: acted=%t err=%v", acted, err)
			}
			if strings.HasPrefix(change, "cancelled") && !errors.Is(err, context.Canceled) {
				t.Fatalf("owner cancellation lost: %v", err)
			}
		})
	}
}
