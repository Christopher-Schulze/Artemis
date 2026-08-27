package benchmark

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
	// If non-empty, the downloaded binary is verified against this.
	ExpectedSHA256 string
	// VersionArgs are the command-line arguments used to retrieve the version.
	// Default: ["--version"].
	VersionArgs []string
	// Port is the port the competitor binary listens on.
	Port int
	// Timeout is the maximum time to wait for the competitor to start.
	Timeout time.Duration
}

// DefaultCompetitorConfig returns the default competitor configuration.
// The binary is stored under benchmark/bin/ which is gitignored.
func DefaultCompetitorConfig() CompetitorConfig {
	return CompetitorConfig{
		BinaryDir: filepath.Join("benchmark", "bin"),
		Port:      9223,
		Timeout:   30 * time.Second,
	}
}

// CompetitorRunner manages the competitor binary lifecycle: download,
// start, run scenarios, stop.
type CompetitorRunner struct {
	cfg CompetitorConfig
	cmd *exec.Cmd
}

// NewCompetitorRunner creates a runner with the given config.
func NewCompetitorRunner(cfg CompetitorConfig) *CompetitorRunner {
	if cfg.BinaryDir == "" {
		cfg = DefaultCompetitorConfig()
	}
	if cfg.Port == 0 {
		cfg.Port = 9223
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if len(cfg.VersionArgs) == 0 {
		cfg.VersionArgs = []string{"--version"}
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
	info, err := os.Stat(r.BinaryPath())
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// EnsureBinary downloads the competitor binary if not already present.
// If DownloadURL is empty or the download fails, it returns an error
// explaining the situation honestly.
func (r *CompetitorRunner) EnsureBinary(ctx context.Context) error {
	if r.IsAvailable() {
		if err := r.verifyChecksum(); err != nil {
			return err
		}
		return nil
	}
	if r.cfg.DownloadURL == "" {
		return fmt.Errorf("competitor binary not available: no download URL configured")
	}

	if err := os.MkdirAll(r.cfg.BinaryDir, 0o755); err != nil {
		return fmt.Errorf("competitor binary mkdir: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.cfg.DownloadURL, nil)
	if err != nil {
		return fmt.Errorf("competitor download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("competitor download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("competitor download: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(r.BinaryPath())
	if err != nil {
		return fmt.Errorf("competitor binary create: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		os.Remove(r.BinaryPath())
		return fmt.Errorf("competitor binary write: %w", err)
	}

	if err := r.verifyChecksum(); err != nil {
		os.Remove(r.BinaryPath())
		return err
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(r.BinaryPath(), 0o755); err != nil {
			return fmt.Errorf("competitor binary chmod: %w", err)
		}
	}

	return nil
}

func (r *CompetitorRunner) verifyChecksum() error {
	if r.cfg.ExpectedSHA256 == "" {
		return nil
	}
	got, err := fileSHA256(r.BinaryPath())
	if err != nil {
		return fmt.Errorf("competitor checksum: %w", err)
	}
	if !strings.EqualFold(got, r.cfg.ExpectedSHA256) {
		return fmt.Errorf("competitor checksum mismatch: got %s, want %s", got, r.cfg.ExpectedSHA256)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Version runs the binary's version command and returns the captured output.
func (r *CompetitorRunner) Version() (string, error) {
	if !r.IsAvailable() {
		return "", fmt.Errorf("competitor binary not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, r.BinaryPath(), r.cfg.VersionArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("competitor version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Start launches the competitor binary as a subprocess listening on
// the configured port. It waits for the HTTP endpoint to become
// reachable before returning.
func (r *CompetitorRunner) Start(ctx context.Context) error {
	if !r.IsAvailable() {
		return fmt.Errorf("competitor binary not available: run EnsureBinary first")
	}

	r.cmd = exec.CommandContext(ctx, r.BinaryPath(),
		"--port", fmt.Sprintf("%d", r.cfg.Port),
		"--headless",
	)
	r.cmd.Stdout = os.Stderr
	r.cmd.Stderr = os.Stderr

	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("competitor start: %w", err)
	}

	// Wait for the HTTP endpoint to become reachable
	addr := fmt.Sprintf("127.0.0.1:%d", r.cfg.Port)
	deadline := time.Now().Add(r.cfg.Timeout)
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			r.Stop()
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	r.Stop()
	return fmt.Errorf("competitor did not become reachable within %s", r.cfg.Timeout)
}

// Stop terminates the competitor subprocess.
func (r *CompetitorRunner) Stop() {
	if r.cmd != nil && r.cmd.Process != nil {
		r.cmd.Process.Signal(os.Interrupt)
		r.cmd.Wait()
		r.cmd = nil
	}
}

// RunScenario runs the competitor against a single scenario by
// fetching the URL via the competitor's HTTP API. The scenarioURL
// must be reachable from the competitor process.
func (r *CompetitorRunner) RunScenario(ctx context.Context, s Scenario, scenarioURL string) ScenarioResult {
	result := ScenarioResult{
		ScenarioID: s.ID,
		Engine:     EngineCompetitor,
		Timestamp:  time.Now().UTC(),
	}

	if r.cmd == nil {
		result.Error = "competitor not started"
		return result
	}

	// Fetch via the competitor's HTTP API
	apiURL := fmt.Sprintf("http://127.0.0.1:%d/fetch?url=%s", r.cfg.Port, urlEncode(scenarioURL))
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		result.WallMs = float64(time.Since(start).Microseconds()) / 1000.0
		result.Error = fmt.Sprintf("competitor request: %v", err)
		return result
	}
	resp, err := http.DefaultClient.Do(req)
	wallMs := float64(time.Since(start).Microseconds()) / 1000.0

	if err != nil {
		result.WallMs = wallMs
		result.Error = fmt.Sprintf("competitor fetch: %v", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		result.WallMs = wallMs
		result.Error = fmt.Sprintf("competitor HTTP %d", resp.StatusCode)
		return result
	}

	result.WallMs = wallMs
	result.OK = true
	return result
}

// Close stops the competitor if running.
func (r *CompetitorRunner) Close() {
	r.Stop()
}

// urlEncode is a minimal URL encoder for query parameters.
func urlEncode(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' || c == ':' || c == '/' || c == '?' {
			b = append(b, c)
		} else {
			b = append(b, '%')
			b = append(b, hexNib(c>>4))
			b = append(b, hexNib(c&0xf))
		}
	}
	return string(b)
}

func hexNib(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'a' + (n - 10)
}
