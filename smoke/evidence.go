package smoke

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

const (
	smokeArtifactBytesLimit      = 8 << 20
	smokeOutputBytesLimit        = 64 << 20
	smokeResultPreviewBytesLimit = 64 << 10
)

// EvidenceArtifact names only persisted bytes, relative to the published run.
type EvidenceArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type InputEvidence struct {
	FixtureManifest fixtureasset.Manifest `json:"fixture_manifest"`
	ScenarioSHA256  string                `json:"scenario_sha256"`
	MatrixSHA256    string                `json:"matrix_sha256"`
	ConfigSHA256    string                `json:"config_sha256"`
}

// ConfigurationDigest binds the same non-secret settings recorded by RunAll.
// Callers supply normalized settings, as stored by NewRunner.
func ConfigurationDigest(host string, port int, stepTimeout time.Duration) (string, error) {
	configuration := struct {
		Host             string `json:"host"`
		Port             int    `json:"port"`
		StepTimeoutNanos int64  `json:"step_timeout_ns"`
	}{Host: host, Port: port, StepTimeoutNanos: int64(stepTimeout)}
	raw, err := json.Marshal(configuration)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func (r *Runner) beginEvidence() error {
	base, err := filepath.Abs(r.cfg.OutDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return err
	}
	final, err := os.MkdirTemp(base, "run-")
	if err != nil {
		return err
	}
	r.publishedDir = final
	r.reservedInfo, err = os.Stat(final)
	if err != nil {
		return errors.Join(err, r.cleanupEvidence())
	}
	r.stageDir, err = os.MkdirTemp(base, ".smoke-stage-")
	if err != nil {
		return errors.Join(err, r.cleanupEvidence())
	}
	r.stageInfo, err = os.Lstat(r.stageDir)
	if err != nil {
		return errors.Join(err, r.cleanupEvidence())
	}
	r.outputBytes = 0
	return nil
}

func (r *Runner) outputRoot() string {
	if r.stageDir != "" {
		return r.stageDir
	}
	return r.cfg.OutDir
}

func (r *Runner) persistArtifact(relative string, data []byte) (EvidenceArtifact, error) {
	if !filepath.IsLocal(relative) || len(data) > smokeArtifactBytesLimit || int64(len(data)) > smokeOutputBytesLimit-r.outputBytes {
		return EvidenceArtifact{}, fmt.Errorf("artifact path, entry or aggregate byte limit exceeded")
	}
	path := filepath.Join(r.outputRoot(), relative)
	if err := writeExclusiveSmokeFile(path, data); err != nil {
		return EvidenceArtifact{}, err
	}
	r.outputBytes += int64(len(data))
	digest := sha256.Sum256(data)
	return EvidenceArtifact{Path: filepath.ToSlash(relative), SHA256: hex.EncodeToString(digest[:]), Bytes: int64(len(data))}, nil
}

func writeExclusiveSmokeFile(path string, data []byte) (writeErr error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		writeErr = errors.Join(writeErr, file.Sync(), file.Close())
		if writeErr != nil {
			writeErr = errors.Join(writeErr, os.Remove(path))
		}
	}()
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func (r *Runner) verifyArtifact(artifact EvidenceArtifact) (verifyErr error) {
	if !filepath.IsLocal(artifact.Path) {
		return fmt.Errorf("nonlocal evidence reference")
	}
	path := filepath.Join(r.stageDir, filepath.FromSlash(artifact.Path))
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != artifact.Bytes || info.Size() > smokeArtifactBytesLimit {
		return fmt.Errorf("artifact identity or size changed")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { verifyErr = errors.Join(verifyErr, file.Close()) }()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, smokeArtifactBytesLimit+1))
	if err != nil {
		return err
	}
	if size != artifact.Bytes || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
		return fmt.Errorf("artifact bytes changed before publication")
	}
	return nil
}

func (r *Runner) writeScorecard(sc *Scorecard) error {
	info, err := os.Lstat(r.stageDir)
	if err != nil || r.stageInfo == nil || !info.IsDir() || !os.SameFile(info, r.stageInfo) {
		return errors.Join(fmt.Errorf("staging directory changed before publication"), err)
	}
	for _, result := range sc.Results {
		references := make(map[string]EvidenceArtifact, len(result.Artifacts))
		for _, artifact := range result.Artifacts {
			if _, duplicate := references[artifact.Path]; duplicate {
				return fmt.Errorf("duplicate scenario artifact reference")
			}
			if err := r.verifyArtifact(artifact); err != nil {
				return err
			}
			references[artifact.Path] = artifact
		}
		for _, step := range result.Steps {
			if step.ValueArtifact != nil && references[step.ValueArtifact.Path] != *step.ValueArtifact {
				return fmt.Errorf("step references an unverified artifact")
			}
		}
	}
	raw, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	if _, err := r.persistArtifact("scorecard.json", raw); err != nil {
		return err
	}
	if err := syncSmokeDirectory(r.stageDir); err != nil {
		return err
	}
	info, err = os.Lstat(r.publishedDir)
	if err != nil || !os.SameFile(info, r.reservedInfo) {
		return errors.Join(fmt.Errorf("reserved publication directory changed"), err)
	}
	entries, err := os.ReadDir(r.publishedDir)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("reserved publication directory acquired unexpected contents")
	}
	// Rename to a new child of the exclusive reservation. Replacing even an
	// empty directory is not portable on macOS; the reservation stays private.
	if sc.OutputDir != filepath.Join(r.publishedDir, "evidence") {
		return fmt.Errorf("scorecard publication path differs from its owner")
	}
	if err := os.Rename(r.stageDir, sc.OutputDir); err != nil {
		return err
	}
	reserved := r.publishedDir
	r.stageDir = ""
	r.stageInfo = nil
	r.publishedDir, r.reservedInfo = "", nil
	if err := errors.Join(syncSmokeDirectory(reserved), syncSmokeDirectory(filepath.Dir(reserved))); err != nil {
		return fmt.Errorf("published evidence but directory sync failed: %w", err)
	}
	return nil
}

func syncSmokeDirectory(path string) (syncErr error) {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { syncErr = errors.Join(syncErr, directory.Close()) }()
	return directory.Sync()
}

func (r *Runner) cleanupEvidence() error {
	var cleanupErr error
	if r.stageDir != "" {
		info, err := os.Lstat(r.stageDir)
		if errors.Is(err, os.ErrNotExist) {
			r.stageDir, r.stageInfo = "", nil
		} else if err != nil || r.stageInfo == nil || !info.IsDir() || !os.SameFile(info, r.stageInfo) {
			cleanupErr = errors.Join(fmt.Errorf("unexpected staging owner retained"), err)
		} else {
			cleanupErr = os.RemoveAll(r.stageDir)
			if cleanupErr == nil {
				r.stageDir, r.stageInfo = "", nil
			}
		}
	}
	if r.publishedDir != "" {
		info, err := os.Lstat(r.publishedDir)
		if err == nil && r.reservedInfo != nil && !os.SameFile(info, r.reservedInfo) {
			return errors.Join(cleanupErr, fmt.Errorf("unexpected publication owner retained"))
		}
		if err == nil {
			err = os.Remove(r.publishedDir)
		}
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		cleanupErr = errors.Join(cleanupErr, err)
		if err == nil {
			r.publishedDir, r.reservedInfo = "", nil
		}
	}
	return cleanupErr
}
