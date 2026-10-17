package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/process"
)

// CompetitorConfig controls the competitor binary download and execution.
type CompetitorConfig struct {
	// BinaryDir is the local directory where the competitor binary
	// is stored. Default: benchmark/bin (gitignored).
	BinaryDir string
	// DownloadURL is the URL to fetch the competitor binary from.
	// If empty, the competitor is considered unavailable and the
	// harness reports honestly.
	DownloadURL string
	// ExpectedSHA256 is the hex-encoded SHA256 of the expected binary.
	// Required before downloading or executing any competitor binary.
	ExpectedSHA256 string
	// VersionArgs are the command-line arguments used to retrieve the version.
	// Default: ["version"].
	VersionArgs []string
	// Port is the port the competitor binary listens on.
	Port int
	// Timeout is the maximum time to wait for the competitor to start.
	Timeout time.Duration
	// MaxDownloadBytes bounds downloaded executables. Default: 256 MiB.
	MaxDownloadBytes int64
}

// DefaultCompetitorConfig returns the default competitor configuration.
// The binary is stored under benchmark/bin/ which is gitignored.
func DefaultCompetitorConfig() CompetitorConfig {
	return CompetitorConfig{
		BinaryDir:        filepath.Join("benchmark", "bin"),
		Port:             9223,
		Timeout:          30 * time.Second,
		MaxDownloadBytes: 256 * 1024 * 1024,
	}
}

// CompetitorRunner manages the competitor binary lifecycle: download,
// start, run scenarios, stop.
type CompetitorRunner struct {
	cfg            CompetitorConfig
	cmd            *exec.Cmd
	waitDone       chan struct{}
	waitErr        error
	transport      *bridge.CDPTransport
	verifiedSHA256 string
	verifiedFile   os.FileInfo
	contextStopped atomic.Bool
}

// NewCompetitorRunner creates a runner with the given config.
func NewCompetitorRunner(cfg CompetitorConfig) *CompetitorRunner {
	if cfg.BinaryDir == "" {
		cfg.BinaryDir = DefaultCompetitorConfig().BinaryDir
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if len(cfg.VersionArgs) == 0 {
		cfg.VersionArgs = []string{"version"}
	}
	if cfg.MaxDownloadBytes == 0 {
		cfg.MaxDownloadBytes = DefaultCompetitorConfig().MaxDownloadBytes
	}
	return &CompetitorRunner{cfg: cfg}
}

// BinaryPath returns the expected path of the competitor binary.
func (r *CompetitorRunner) BinaryPath() string {
	name := "lightpanda"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(r.cfg.BinaryDir, name)
}

// IsAvailable reports whether the competitor binary is already
// downloaded and executable.
func (r *CompetitorRunner) IsAvailable() bool {
	info, err := os.Lstat(r.BinaryPath())
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode()&0o111 != 0)
}

// EnsureBinary downloads the competitor binary if not already present.
// If DownloadURL is empty or the download fails, it returns an error
// explaining the situation honestly.
func (r *CompetitorRunner) EnsureBinary(ctx context.Context) (returnErr error) {
	if ctx == nil {
		return fmt.Errorf("competitor context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateCompetitorConfig(r.cfg); err != nil {
		return err
	}
	if r.IsAvailable() {
		if err := r.verifyChecksum(); err != nil {
			return err
		}
		return nil
	}
	if r.cfg.DownloadURL == "" {
		return fmt.Errorf("competitor binary not available: no download URL configured")
	}

	if err := r.downloadBinary(ctx); err != nil {
		return err
	}
	return r.verifyChecksum()
}

func newCompetitorCommand(ctx context.Context, executable string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, executable)
	command.Args = append(command.Args, args...)
	command.WaitDelay = time.Second
	process.ConfigureProcessGroup(command)
	command.Cancel = func() error { return process.SignalProcessGroup(command.Process, syscall.SIGKILL) }
	return command
}

func (r *CompetitorRunner) verifyChecksum() error {
	r.verifiedSHA256 = ""
	r.verifiedFile = nil
	digest, err := hex.DecodeString(r.cfg.ExpectedSHA256)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("competitor execution requires a pinned SHA256 digest")
	}
	if !r.IsAvailable() {
		return fmt.Errorf("competitor binary must be a regular executable file")
	}
	before, err := os.Lstat(r.BinaryPath())
	if err != nil {
		return err
	}
	got, err := fileSHA256(r.BinaryPath())
	if err != nil {
		return fmt.Errorf("competitor checksum: %w", err)
	}
	if !strings.EqualFold(got, r.cfg.ExpectedSHA256) {
		return fmt.Errorf("competitor checksum mismatch: got %s, want %s", got, r.cfg.ExpectedSHA256)
	}
	after, err := os.Lstat(r.BinaryPath())
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return fmt.Errorf("competitor changed during verification")
	}
	r.verifiedSHA256 = got
	r.verifiedFile = after
	return nil
}

func (r *CompetitorRunner) verifyExecutedFile() error {
	info, err := os.Lstat(r.BinaryPath())
	if err != nil {
		return err
	}
	if r.verifiedFile == nil || !info.Mode().IsRegular() || !os.SameFile(info, r.verifiedFile) || info.Size() != r.verifiedFile.Size() || info.ModTime() != r.verifiedFile.ModTime() {
		return fmt.Errorf("competitor executable identity changed during execution")
	}
	return nil
}

// VerifiedSHA256 returns the digest verified immediately before execution.
func (r *CompetitorRunner) VerifiedSHA256() string { return r.verifiedSHA256 }

func fileSHA256(path string) (sum string, returnErr error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close checksum file: %w", closeErr))
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Version runs the binary's version command and returns the captured output.
func (r *CompetitorRunner) Version(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("competitor version context required")
	}
	if err := r.verifyChecksum(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out := &boundedCommandOutput{limit: 64 * 1024}
	command := newCompetitorCommand(ctx, r.BinaryPath(), r.cfg.VersionArgs...)
	command.Stdout, command.Stderr = out, out
	err := command.Run()
	if command.Process != nil {
		err = errors.Join(err, process.SignalProcessGroup(command.Process, syscall.SIGKILL))
	}
	err = errors.Join(err, r.verifyExecutedFile())
	if err != nil {
		return "", fmt.Errorf("competitor version: %w", err)
	}
	version := strings.TrimSpace(out.buffer.String())
	if version == "" {
		return "", fmt.Errorf("competitor version returned empty output")
	}
	return version, nil
}

// Start launches the competitor binary as a subprocess listening on
// a reserved loopback port. Readiness requires a working CDP handshake.
func (r *CompetitorRunner) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("competitor start context required")
	}
	if r.cmd != nil {
		return fmt.Errorf("competitor is already started")
	}
	if err := validateCompetitorConfig(r.cfg); err != nil {
		return err
	}
	if err := r.verifyChecksum(); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", r.cfg.Port))
	if err != nil {
		return fmt.Errorf("reserve competitor port: %w", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}

	r.cmd = newCompetitorCommand(ctx, r.BinaryPath(),
		"serve", "--host", "127.0.0.1", "--port", port,
	)
	r.contextStopped.Store(false)
	cancelCommand := r.cmd.Cancel
	r.cmd.Cancel = func() error {
		err := cancelCommand()
		if err == nil {
			r.contextStopped.Store(true)
		}
		return err
	}
	output := &boundedCommandOutput{limit: 64 * 1024}
	r.cmd.Stdout, r.cmd.Stderr = output, output

	if err := r.cmd.Start(); err != nil {
		r.cmd = nil
		return fmt.Errorf("competitor start: %w", err)
	}
	command := r.cmd
	r.waitDone = make(chan struct{})
	go func() {
		r.waitErr = command.Wait()
		close(r.waitDone)
	}()
	if err := r.connect(ctx, address); err != nil {
		return err
	}
	if err := r.verifyExecutedFile(); err != nil {
		return errors.Join(err, r.Stop())
	}
	return nil
}

// Stop terminates the competitor subprocess.
func (r *CompetitorRunner) Stop() error {
	var stopErr error
	if r.transport != nil {
		stopErr = r.transport.Close()
		r.transport = nil
	}
	if r.cmd != nil && r.cmd.Process != nil {
		cmd := r.cmd
		alreadyExited := false
		select {
		case <-r.waitDone:
			alreadyExited = true
		default:
		}
		interruptErr := process.SignalProcessGroup(cmd.Process, syscall.SIGINT)
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-r.waitDone:
		case <-timer.C:
			if err := process.SignalProcessGroup(cmd.Process, syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
				stopErr = errors.Join(stopErr, err)
			}
			<-r.waitDone
		}
		stopErr = errors.Join(stopErr, process.SignalProcessGroup(cmd.Process, syscall.SIGKILL))
		stopErr = errors.Join(stopErr, r.verifyExecutedFile())
		// Wait joins exec's cancellation watcher before these flags are read.
		if !r.contextStopped.Load() {
			if alreadyExited {
				stopErr = errors.Join(stopErr, fmt.Errorf("competitor exited before owned stop"), r.waitErr)
			}
			if interruptErr != nil && !errors.Is(interruptErr, os.ErrProcessDone) {
				stopErr = errors.Join(stopErr, fmt.Errorf("competitor interrupt: %w", interruptErr))
			}
		}
		var exitError *exec.ExitError
		if r.waitErr != nil && !errors.As(r.waitErr, &exitError) && !errors.Is(r.waitErr, context.Canceled) {
			stopErr = errors.Join(stopErr, r.waitErr)
		}
		r.cmd = nil
		r.waitDone = nil
		return stopErr
	}
	return stopErr
}

// RunScenario measures a real CDP navigation and canonical Go extraction.
func (r *CompetitorRunner) RunScenario(ctx context.Context, s Scenario, scenarioURL string) ScenarioResult {
	result := ScenarioResult{
		ScenarioID: s.ID,
		Engine:     EngineCompetitor,
		EngineMode: string(s.EngineMode),
		Timestamp:  time.Now().UTC(),
	}
	if r.transport == nil || r.cmd == nil {
		result.Error = "competitor not started"
		return result
	}
	start := time.Now()
	evidence, err := r.runCDPScenario(ctx, s, scenarioURL)
	result.WallMs = float64(time.Since(start)) / float64(time.Millisecond)
	result.Throughput = result.ToMetricSet().ThroughputForPages(1)
	result.Validation = evidence
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Validated = evidence.Passed
	result.OK = evidence.Passed
	if !result.OK {
		result.Error = "scenario semantic assertions failed"
	}
	return result
}

// Close stops the competitor if running.
func (r *CompetitorRunner) Close() error {
	return r.Stop()
}
