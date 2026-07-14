package process

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultStartupTimeout  = 15 * time.Second
	defaultShutdownTimeout = 5 * time.Second
	defaultOutputLimit     = 256 * 1024
	profileLeaseName       = ".artemis-profile.lock"
)

// LaunchConfig configures one owned Chromium process.
type LaunchConfig struct {
	BinaryPath      string
	UserDataDir     string
	Headless        bool
	ExtraArgs       []string
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
	OutputLimit     int
}

// Browser owns a launched Chromium process and its disposable profile.
type Browser struct {
	mu            sync.RWMutex
	cmd           *exec.Cmd
	endpoint      string
	profileDir    string
	profileLease  string
	removeProfile bool
	shutdown      time.Duration
	output        *cappedOutput
	done          chan struct{}
	waitErr       error
	closing       bool
	closeOnce     sync.Once
	closeErr      error
}

// Launch starts Chromium with an isolated loopback CDP endpoint.
func Launch(ctx context.Context, config LaunchConfig) (*Browser, error) {
	if ctx == nil {
		return nil, invalidConfig("context required")
	}
	normalized, binary, err := normalizeLaunchConfig(config)
	if err != nil {
		return nil, err
	}
	profileDir, removeProfile, profileLease, err := prepareProfile(normalized.UserDataDir)
	if err != nil {
		return nil, err
	}
	browser, err := startProcess(ctx, normalized, binary, profileDir, profileLease, removeProfile)
	if err != nil {
		_ = releaseProfileLease(profileLease)
		if removeProfile {
			_ = os.RemoveAll(profileDir)
		}
	}
	return browser, err
}

func normalizeLaunchConfig(config LaunchConfig) (LaunchConfig, Binary, error) {
	if config.StartupTimeout == 0 {
		config.StartupTimeout = defaultStartupTimeout
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = defaultShutdownTimeout
	}
	if config.OutputLimit == 0 {
		config.OutputLimit = defaultOutputLimit
	}
	if config.StartupTimeout < 0 || config.ShutdownTimeout < 0 || config.OutputLimit < 1024 {
		return LaunchConfig{}, Binary{}, invalidConfig("timeouts must be positive and output limit must be at least 1024 bytes")
	}
	for _, arg := range config.ExtraArgs {
		name := strings.SplitN(arg, "=", 2)[0]
		switch name {
		case "--remote-debugging-port", "--remote-debugging-address", "--remote-debugging-pipe", "--user-data-dir":
			return LaunchConfig{}, Binary{}, invalidConfig(fmt.Sprintf("reserved Chromium flag %q", name))
		}
	}
	binary, err := DiscoverBinary(config.BinaryPath)
	if err != nil {
		return LaunchConfig{}, Binary{}, err
	}
	return config, binary, nil
}

func invalidConfig(message string) error {
	return &Error{Code: ErrorInvalidConfig, Op: "validate launch config", Err: errors.New(message)}
}

func prepareProfile(configured string) (string, bool, string, error) {
	if configured != "" {
		return prepareConfiguredProfile(configured)
	}
	dir, err := os.MkdirTemp("", "artemis-chromium-")
	if err != nil {
		return "", false, "", &Error{Code: ErrorLaunchFailed, Op: "create profile", Err: err}
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return "", false, "", &Error{Code: ErrorLaunchFailed, Op: "secure profile", Err: err}
	}
	lease, err := acquireProfileLease(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", false, "", err
	}
	return dir, true, lease, nil
}

func prepareConfiguredProfile(configured string) (string, bool, string, error) {
	abs, err := filepath.Abs(configured)
	if err != nil {
		return "", false, "", invalidConfig(fmt.Sprintf("profile path: %v", err))
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return "", false, "", &Error{Code: ErrorLaunchFailed, Op: "create configured profile", Err: err}
	}
	lease, err := acquireProfileLease(abs)
	if err != nil {
		return "", false, "", err
	}
	if _, err := os.Stat(filepath.Join(abs, "DevToolsActivePort")); err == nil {
		_ = releaseProfileLease(lease)
		return "", false, "", invalidConfig("configured profile already exposes an active DevTools endpoint")
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = releaseProfileLease(lease)
		return "", false, "", &Error{Code: ErrorLaunchFailed, Op: "inspect configured profile", Err: err}
	}
	return abs, false, lease, nil
}

func acquireProfileLease(profileDir string) (string, error) {
	lease := filepath.Join(profileDir, profileLeaseName)
	file, err := os.OpenFile(lease, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", invalidConfig(fmt.Sprintf("profile %q is already leased", profileDir))
		}
		return "", &Error{Code: ErrorLaunchFailed, Op: "acquire profile lease", Err: err}
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		_ = file.Close()
		_ = os.Remove(lease)
		return "", &Error{Code: ErrorLaunchFailed, Op: "write profile lease", Err: err}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(lease)
		return "", &Error{Code: ErrorLaunchFailed, Op: "close profile lease", Err: err}
	}
	return lease, nil
}

func releaseProfileLease(lease string) error {
	if lease == "" {
		return nil
	}
	err := os.Remove(lease)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func startProcess(ctx context.Context, config LaunchConfig, binary Binary, profileDir, profileLease string, removeProfile bool) (*Browser, error) {
	args := chromiumArgs(config, profileDir)
	cmd := exec.Command(binary.Path, args...)
	configureProcessGroup(cmd)
	output := newCappedOutput(config.OutputLimit)
	cmd.Stdout = output
	cmd.Stderr = output
	browser := &Browser{
		cmd: cmd, profileDir: profileDir, profileLease: profileLease, removeProfile: removeProfile,
		shutdown: config.ShutdownTimeout, output: output, done: make(chan struct{}),
	}
	if err := cmd.Start(); err != nil {
		return nil, &Error{Code: ErrorLaunchFailed, Op: "start", Err: err}
	}
	go browser.wait()
	endpoint, err := browser.waitForEndpoint(ctx, config.StartupTimeout)
	if err != nil {
		_ = browser.Close()
		return nil, err
	}
	browser.mu.Lock()
	browser.endpoint = endpoint
	browser.mu.Unlock()
	return browser, nil
}

func chromiumArgs(config LaunchConfig, profileDir string) []string {
	args := []string{
		"--remote-debugging-port=0",
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-sync",
		"--metrics-recording-only",
		"--password-store=basic",
		"--use-mock-keychain",
	}
	if config.Headless {
		args = append(args, "--headless=new", "--disable-gpu")
	}
	args = append(args, config.ExtraArgs...)
	return append(args, "about:blank")
}

func (b *Browser) wait() {
	err := b.cmd.Wait()
	b.mu.Lock()
	b.waitErr = err
	b.mu.Unlock()
	close(b.done)
}

func (b *Browser) waitForEndpoint(ctx context.Context, timeout time.Duration) (string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	activePort := filepath.Join(b.profileDir, "DevToolsActivePort")
	for {
		endpoint, err := readEndpoint(activePort)
		if err == nil {
			return endpoint, nil
		}
		select {
		case <-b.done:
			return "", &Error{Code: ErrorBrowserCrash, Op: "await readiness", Err: fmt.Errorf("process exited: %v; output: %s", b.WaitError(), b.Output())}
		case <-waitCtx.Done():
			code := ErrorLaunchTimeout
			if ctx.Err() != nil {
				code = ErrorCancelled
			}
			return "", &Error{Code: code, Op: "await readiness", Err: fmt.Errorf("%w; output: %s", waitCtx.Err(), b.Output())}
		case <-ticker.C:
		}
	}
}

func readEndpoint(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("malformed DevToolsActivePort")
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid DevTools port %q", lines[0])
	}
	pathPart := strings.TrimSpace(lines[1])
	if !strings.HasPrefix(pathPart, "/devtools/browser/") {
		return "", fmt.Errorf("invalid browser endpoint path %q", pathPart)
	}
	endpoint := url.URL{Scheme: "ws", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), Path: pathPart}
	return endpoint.String(), nil
}

// Endpoint returns the loopback browser WebSocket endpoint.
func (b *Browser) Endpoint() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.endpoint
}

// ProfileDir returns the process profile directory.
func (b *Browser) ProfileDir() string {
	return b.profileDir
}

// Output returns bounded recent Chromium diagnostics.
func (b *Browser) Output() string {
	return b.output.String()
}

// Done closes when Chromium exits.
func (b *Browser) Done() <-chan struct{} {
	return b.done
}

// WaitError returns Chromium's terminal process result after Done closes.
func (b *Browser) WaitError() error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.waitErr
}

// Err classifies an unexpected terminal process exit after startup.
func (b *Browser) Err() error {
	select {
	case <-b.done:
	default:
		return nil
	}
	b.mu.RLock()
	err := b.waitErr
	closing := b.closing
	b.mu.RUnlock()
	if closing {
		return nil
	}
	if err == nil {
		err = errors.New("process exited unexpectedly")
	}
	return &Error{Code: ErrorBrowserCrash, Op: "supervise running browser", Err: fmt.Errorf("%w; output: %s", err, b.Output())}
}

// Close terminates the owned process group and removes owned profile state.
func (b *Browser) Close() error {
	b.closeOnce.Do(func() {
		b.closeErr = b.close()
	})
	return b.closeErr
}

func (b *Browser) close() error {
	b.mu.Lock()
	b.closing = true
	b.mu.Unlock()
	result := b.terminate()
	activePort := filepath.Join(b.profileDir, "DevToolsActivePort")
	if err := os.Remove(activePort); err != nil && !errors.Is(err, os.ErrNotExist) {
		result = errors.Join(result, fmt.Errorf("remove DevTools endpoint file: %w", err))
	}
	if err := releaseProfileLease(b.profileLease); err != nil {
		result = errors.Join(result, fmt.Errorf("release profile lease: %w", err))
	}
	if b.removeProfile {
		if err := os.RemoveAll(b.profileDir); err != nil {
			result = errors.Join(result, fmt.Errorf("remove Chromium profile: %w", err))
		}
	}
	return result
}

func (b *Browser) terminate() error {
	select {
	case <-b.done:
		return nil
	default:
		if err := signalProcessGroup(b.cmd.Process, syscall.SIGTERM); err != nil {
			return fmt.Errorf("terminate Chromium process group: %w", err)
		}
		timer := time.NewTimer(b.shutdown)
		select {
		case <-b.done:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil
		case <-timer.C:
			if err := signalProcessGroup(b.cmd.Process, syscall.SIGKILL); err != nil {
				return fmt.Errorf("kill Chromium process group: %w", err)
			}
		}
	}

	// The process group signal may not reach the leader if the Chromium
	// main process has changed its process group. Use a direct SIGKILL to
	// the recorded pid as a fallback, and bound the total close time so
	// the caller never waits indefinitely.
	timer := time.NewTimer(b.shutdown)
	defer timer.Stop()
	select {
	case <-b.done:
		return nil
	case <-timer.C:
		if err := b.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill Chromium process: %w", err)
		}
	}

	timer2 := time.NewTimer(b.shutdown)
	defer timer2.Stop()
	select {
	case <-b.done:
		return nil
	case <-timer2.C:
		return fmt.Errorf("Chromium process %d did not exit after SIGKILL", b.cmd.Process.Pid)
	}
}
