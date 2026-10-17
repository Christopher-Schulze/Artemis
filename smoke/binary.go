package smoke

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const smokeBinaryBytesLimit = 256 << 20

// BinaryEvidence describes the actual immutable bytes executed by one runner.
type BinaryEvidence struct {
	SHA256        string `json:"sha256"`
	Bytes         int64  `json:"bytes"`
	Version       string `json:"version"`
	GoVersion     string `json:"go_version"`
	Commit        string `json:"commit,omitempty"`
	CommitKnown   bool   `json:"commit_known"`
	Modified      bool   `json:"modified"`
	ModifiedKnown bool   `json:"modified_known"`
}

func (r *Runner) prepareBinary(ctx context.Context) (prepareErr error) {
	if ctx == nil {
		return fmt.Errorf("binary preparation context required")
	}
	if r.binaryDir != "" {
		return fmt.Errorf("binary already prepared; close the existing owner first")
	}
	if r.cfg.ArtemisBinPath == "" && r.cfg.BuildDir == "" {
		return fmt.Errorf("BuildDir required without a pre-built binary")
	}
	root, err := os.MkdirTemp("", "artemis-smoke-bin-")
	if err != nil {
		return err
	}
	r.binaryDir = root
	r.binaryInfo, err = os.Lstat(root)
	if err != nil {
		return fmt.Errorf("identify owned binary directory: %w", err)
	}
	defer func() {
		if prepareErr != nil {
			prepareErr = errors.Join(prepareErr, r.Close())
		}
	}()
	name := "artemis"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	r.binPath = filepath.Join(root, name)
	if r.cfg.ArtemisBinPath != "" {
		if err := copySmokeBinary(r.cfg.ArtemisBinPath, r.binPath); err != nil {
			return err
		}
	} else {
		buildCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		command := newSmokeCommand(buildCtx, "go", "build", "-o", r.binPath, "./cmd/artemis")
		command.Dir = r.cfg.BuildDir
		command.Env = append(os.Environ(), "GOTMPDIR="+root)
		var logs boundedSmokeLog
		command.Stdout, command.Stderr = &logs, &logs
		runErr := command.Run()
		output, logErr := logs.snapshot()
		if err := errors.Join(runErr, logErr); err != nil {
			return fmt.Errorf("build actual Artemis: %w; output: %s", err, output)
		}
	}
	evidence, err := inspectSmokeBinary(ctx, r.binPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(r.binPath, 0o500); err != nil {
		return err
	}
	r.binary = evidence
	return nil
}

func copySmokeBinary(source, target string) (returnErr error) {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("stat pre-built Artemis: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("pre-built Artemis must be a regular binary")
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open pre-built Artemis: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, input.Close()) }()
	info, err = input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > smokeBinaryBytesLimit {
		return fmt.Errorf("Artemis binary must be a nonempty regular file within %d bytes", smokeBinaryBytesLimit)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("pre-built Artemis is not executable")
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, output.Sync(), output.Close()) }()
	size, err := io.Copy(output, io.LimitReader(input, smokeBinaryBytesLimit+1))
	if err != nil {
		return err
	}
	if size > smokeBinaryBytesLimit || size != info.Size() {
		return fmt.Errorf("pre-built binary changed or exceeded its byte limit during snapshot")
	}
	return nil
}

func hashSmokeBinary(path string) (digest string, size int64, returnErr error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, file.Close())
		if returnErr != nil {
			digest = ""
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > smokeBinaryBytesLimit {
		return "", 0, fmt.Errorf("invalid or oversized smoke binary")
	}
	hash := sha256.New()
	size, err = io.Copy(hash, io.LimitReader(file, smokeBinaryBytesLimit+1))
	if err != nil {
		return "", 0, err
	}
	if size != info.Size() {
		return "", 0, fmt.Errorf("smoke binary changed during hashing")
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func inspectSmokeBinary(ctx context.Context, path string) (BinaryEvidence, error) {
	digest, size, err := hashSmokeBinary(path)
	if err != nil {
		return BinaryEvidence{}, err
	}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return BinaryEvidence{}, fmt.Errorf("read actual Go binary metadata: %w", err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, smokeReadinessTimeout)
	defer cancel()
	command := newSmokeCommand(probeCtx, path, "version")
	var output, diagnostics boundedSmokeLog
	command.Stdout, command.Stderr = &output, &diagnostics
	runErr := command.Run()
	version, outputErr := output.snapshot()
	stderr, stderrErr := diagnostics.snapshot()
	if err := errors.Join(runErr, outputErr, stderrErr); err != nil {
		return BinaryEvidence{}, fmt.Errorf("probe actual binary version: %w; stderr: %s", err, stderr)
	}
	value := strings.TrimSpace(string(version))
	if value == "" || len(value) > 256 || strings.ContainsAny(value, "\r\n") {
		return BinaryEvidence{}, fmt.Errorf("binary version must be one bounded nonempty line")
	}
	verified, verifiedSize, err := hashSmokeBinary(path)
	if err != nil {
		return BinaryEvidence{}, err
	}
	if verified != digest || verifiedSize != size {
		return BinaryEvidence{}, fmt.Errorf("binary changed during version probe")
	}
	evidence := BinaryEvidence{SHA256: digest, Bytes: size, Version: value, GoVersion: info.GoVersion}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			evidence.Commit, evidence.CommitKnown = setting.Value, setting.Value != ""
		case "vcs.modified":
			evidence.Modified = setting.Value == "true"
			evidence.ModifiedKnown = setting.Value == "true" || setting.Value == "false"
		}
	}
	return evidence, nil
}

func (r *Runner) verifyBinary() error {
	if r.binary.SHA256 == "" {
		return fmt.Errorf("prepare the actual smoke binary before starting a process")
	}
	info, err := os.Lstat(r.binaryDir)
	if err != nil || r.binaryInfo == nil || !info.IsDir() || !os.SameFile(info, r.binaryInfo) {
		return errors.Join(fmt.Errorf("prepared binary directory changed"), err)
	}
	digest, size, err := hashSmokeBinary(r.binPath)
	if err != nil {
		return err
	}
	if digest != r.binary.SHA256 || size != r.binary.Bytes {
		return fmt.Errorf("prepared smoke binary changed")
	}
	return nil
}

// Close removes only joined runner-owned temporary binary state.
func (r *Runner) Close() error {
	for _, child := range r.children {
		select {
		case <-child.done:
		default:
			return fmt.Errorf("cannot remove binary while an owned child remains unjoined")
		}
		select {
		case <-child.readyDone:
		default:
			return fmt.Errorf("cannot remove binary while an owned reader remains unjoined")
		}
	}
	if r.binaryDir == "" {
		return nil
	}
	info, err := os.Lstat(r.binaryDir)
	if errors.Is(err, os.ErrNotExist) {
		r.binaryDir, r.binPath, r.children, r.binaryInfo = "", "", nil, nil
		r.binary = BinaryEvidence{}
		return nil
	}
	if err != nil || r.binaryInfo == nil || !info.IsDir() || !os.SameFile(info, r.binaryInfo) {
		return errors.Join(fmt.Errorf("unexpected binary directory owner retained"), err)
	}
	if err := os.RemoveAll(r.binaryDir); err != nil {
		return fmt.Errorf("remove owned smoke binary directory: %w", err)
	}
	r.binaryDir, r.binPath, r.children, r.binaryInfo = "", "", nil, nil
	r.binary = BinaryEvidence{}
	return nil
}
