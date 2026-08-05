package process

// dependency-authority: owned Chromium launch requires the boundary adapter's
// canonical AcquisitionAuthority approval before process start.

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

	"github.com/Christopher-Schulze/Artemis/network"
)

const (
	defaultStartupTimeout  = 15 * time.Second
	defaultShutdownTimeout = 5 * time.Second
	defaultOutputLimit     = 256 * 1024
	profileLeaseName       = ".artemis-profile.lock"
	policyHostResolverRule = "MAP * ~NOTFOUND, EXCLUDE 127.0.0.1"
)

// DependencyAuthorizer is the boundary adapter supplied by the Omnimus
// runtime. Artemis deliberately does not import Omnimus internals.
type DependencyAuthorizer interface {
	Authorize(ctx context.Context, artifact, version, path string) error
}

// ChildProcessRegistrar is the narrow boundary used to attach the owned
// Chromium child to the host runtime's external-process governor.
type ChildProcessRegistrar interface {
	RegisterChild(pid int, name string) (func(), error)
}

// SandboxPolicy controls whether Chromium's OS sandbox must remain enabled.
type SandboxPolicy string

const (
	SandboxRequired SandboxPolicy = "required"
	SandboxDisabled SandboxPolicy = "disabled"
)

// LaunchConfig configures one owned Chromium process.
type LaunchConfig struct {
	BinaryPath                     string
	UserDataDir                    string
	Headless                       bool
	ExtraArgs                      []string
	StartupTimeout                 time.Duration
	ShutdownTimeout                time.Duration
	OutputLimit                    int
	AllowPrivateNetworks           bool
	AllowedPorts                   []int
	PolicyProxyURL                 string
	Sandbox                        SandboxPolicy
	ResourceBudget                 ResourceBudget
	PolicyDecisionSink             network.DecisionSink
	ResourceSink                   ResourceSink
	DependencyAuthorizer           DependencyAuthorizer
	Artifact                       string
	ArtifactVersion                string
	RequireDependencyAuthorization bool
	ChildProcessRegistrar          ChildProcessRegistrar
	resourceSampler                resourceSampler
}

// Browser owns a launched Chromium process and its disposable profile.
type Browser struct {
	mu              sync.RWMutex
	cmd             *exec.Cmd
	endpoint        string
	profileDir      string
	profileLease    string
	removeProfile   bool
	shutdown        time.Duration
	output          *cappedOutput
	done            chan struct{}
	processDone     chan struct{}
	ready           chan struct{}
	waitErr         error
	terminalErr     error
	closing         bool
	closeOnce       sync.Once
	closeErr        error
	cleanupOnce     sync.Once
	cleanupErr      error
	unregisterChild func()
	warnings        []string
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
	if normalized.RequireDependencyAuthorization {
		artifact := normalized.Artifact
		if artifact == "" {
			artifact = "chromium"
		}
		if err := normalized.DependencyAuthorizer.Authorize(ctx, artifact, normalized.ArtifactVersion, binary.Path); err != nil {
			return nil, &Error{Code: ErrorLaunchFailed, Op: "authorize Chromium", Err: err}
		}
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
	if config.Sandbox == "" {
		config.Sandbox = SandboxRequired
	}
	if config.RequireDependencyAuthorization && (config.DependencyAuthorizer == nil || strings.TrimSpace(config.ArtifactVersion) == "") {
		return LaunchConfig{}, Binary{}, invalidConfig("dependency authorizer and artifact version are required")
	}
	if config.Sandbox != SandboxRequired && config.Sandbox != SandboxDisabled {
		return LaunchConfig{}, Binary{}, invalidConfig(fmt.Sprintf("invalid sandbox policy %q", config.Sandbox))
	}
	config.ResourceBudget.applyDefaults()
	if err := config.ResourceBudget.validate(); err != nil {
		return LaunchConfig{}, Binary{}, invalidConfig(err.Error())
	}
	if config.resourceSampler == nil {
		config.resourceSampler = sampleProcessResources
	}
	if config.StartupTimeout < 0 || config.ShutdownTimeout < 0 || config.OutputLimit < 1024 {
		return LaunchConfig{}, Binary{}, invalidConfig("timeouts must be positive and output limit must be at least 1024 bytes")
	}
	for _, arg := range config.ExtraArgs {
		name := strings.SplitN(arg, "=", 2)[0]
		switch name {
		case "--remote-debugging-port", "--remote-debugging-address", "--remote-debugging-pipe", "--user-data-dir",
			"--proxy-server", "--proxy-bypass-list", "--proxy-pac-url", "--proxy-auto-detect", "--no-proxy-server",
			"--host-resolver-rules", "--enable-quic", "--disable-quic", "--no-sandbox", "--disable-setuid-sandbox":
			return LaunchConfig{}, Binary{}, invalidConfig(fmt.Sprintf("reserved Chromium flag %q", name))
		}
	}
	for _, port := range config.AllowedPorts {
		if port < 1 || port > 65535 {
			return LaunchConfig{}, Binary{}, invalidConfig(fmt.Sprintf("invalid allowed port %d", port))
		}
	}
	config.AllowedPorts = append([]int(nil), config.AllowedPorts...)
	if err := validatePolicyProxyURL(config.PolicyProxyURL); err != nil {
		return LaunchConfig{}, Binary{}, err
	}
	binary, err := DiscoverBinary(config.BinaryPath)
	if err != nil {
		return LaunchConfig{}, Binary{}, err
	}
	return config, binary, nil
}

func validatePolicyProxyURL(raw string) error {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return invalidConfig("policy proxy must be an origin-only loopback HTTP URL")
	}
	host := parsed.Hostname()
	port, portErr := strconv.Atoi(parsed.Port())
	if (host != "127.0.0.1" && host != "::1") || portErr != nil || port < 1 || port > 65535 {
		return invalidConfig("policy proxy must use a loopback IP and explicit valid port")
	}
	return nil
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

// dependency-authority-flow: Launch authorizes binary.Path before delegating
// the actual process start to this boundary helper.
func startProcess(ctx context.Context, config LaunchConfig, binary Binary, profileDir, profileLease string, removeProfile bool) (*Browser, error) {
	args := chromiumArgs(config, profileDir)
	cmd := newProcessCommand(binary.Path, profileDir, removeProfile, args)
	cmd.WaitDelay = config.ShutdownTimeout
	configureProcessGroup(cmd)
	output := newCappedOutput(config.OutputLimit)
	cmd.Stdout = output
	cmd.Stderr = output
	browser := &Browser{
		cmd: cmd, profileDir: profileDir, profileLease: profileLease, removeProfile: removeProfile,
		shutdown: config.ShutdownTimeout, output: output, done: make(chan struct{}), processDone: make(chan struct{}), ready: make(chan struct{}),
	}
	if config.Sandbox == SandboxDisabled {
		browser.warnings = []string{"Chromium sandbox is disabled by explicit policy"}
	}
	if err := cmd.Start(); err != nil {
		return nil, &Error{Code: ErrorLaunchFailed, Op: "start", Err: err}
	}
	unregisterChild := func() {}
	if config.ChildProcessRegistrar != nil {
		cleanup, err := config.ChildProcessRegistrar.RegisterChild(cmd.Process.Pid, filepath.Base(binary.Path))
		if err != nil {
			killErr := cmd.Process.Kill()
			waitErr := cmd.Wait()
			return nil, &Error{Code: ErrorLaunchFailed, Op: "register Chromium process", Err: errors.Join(err, killErr, waitErr)}
		}
		if cleanup != nil {
			unregisterChild = cleanup
		}
	}
	browser.unregisterChild = unregisterChild
	go browser.wait()
	go browser.supervise(ctx, config)
	endpoint, err := browser.waitForEndpoint(ctx, config.StartupTimeout)
	if err != nil {
		_ = browser.Close()
		return nil, err
	}
	browser.mu.Lock()
	browser.endpoint = endpoint
	browser.mu.Unlock()
	close(browser.ready)
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
	if config.Sandbox == SandboxDisabled {
		args = append(args, "--no-sandbox")
	}
	if config.PolicyProxyURL != "" {
		args = append(args,
			"--proxy-server="+config.PolicyProxyURL,
			"--proxy-bypass-list=<-loopback>",
			"--host-resolver-rules="+policyHostResolverRule,
			"--disable-quic",
		)
	}
	args = append(args, config.ExtraArgs...)
	return append(args, "about:blank")
}

func (b *Browser) wait() {
	err := b.cmd.Wait()
	b.mu.Lock()
	b.waitErr = err
	unregisterChild := b.unregisterChild
	b.mu.Unlock()
	unregisterChild()
	close(b.processDone)
}

func (b *Browser) supervise(ctx context.Context, config LaunchConfig) {
	ticker := time.NewTicker(config.ResourceBudget.SampleInterval)
	defer ticker.Stop()
	timeout := time.NewTimer(config.ResourceBudget.SessionTimeout)
	defer timeout.Stop()
	ctxDone := ctx.Done()
	timeoutC := timeout.C
	readyC := (<-chan struct{})(b.ready)
	sampleC := ticker.C
	for {
		select {
		case <-b.processDone:
			b.reapHelpers()
			b.cleanup()
			close(b.done)
			return
		case <-ctxDone:
			b.setTerminalError(&Error{Code: ErrorCancelled, Op: "supervise running browser", Err: context.Cause(ctx)})
			b.terminateAfterFailure()
			ctxDone = nil
			timeoutC = nil
			readyC = nil
			sampleC = nil
		case <-timeoutC:
			b.setTerminalError(&Error{Code: ErrorResourceBudget, Op: "supervise running browser", Err: fmt.Errorf("session timeout %s exceeded", config.ResourceBudget.SessionTimeout)})
			b.terminateAfterFailure()
			ctxDone = nil
			timeoutC = nil
			readyC = nil
			sampleC = nil
		case <-readyC:
			readyC = nil
			if !b.sampleAndEnforce(config, false) {
				ctxDone = nil
				timeoutC = nil
				sampleC = nil
			}
		case <-sampleC:
			if readyC != nil {
				continue
			}
			if !b.sampleAndEnforce(config, true) {
				ctxDone = nil
				timeoutC = nil
				sampleC = nil
			}
		}
	}
}

func (b *Browser) sampleAndEnforce(config LaunchConfig, enforceBudget bool) bool {
	usage, err := config.resourceSampler(b.cmd.Process.Pid, b.profileDir)
	if err != nil {
		select {
		case <-b.processDone:
			return true
		default:
		}
		b.setTerminalError(&Error{Code: ErrorResourceBudget, Op: "sample browser resources", Err: err})
		b.terminateAfterFailure()
		return false
	}
	if config.ResourceSink != nil {
		if err := config.ResourceSink(usage); err != nil {
			b.setTerminalError(&Error{Code: ErrorDiagnostics, Op: "record browser resources", Err: err})
			b.terminateAfterFailure()
			return false
		}
	}
	if !enforceBudget {
		return true
	}
	if err := config.ResourceBudget.exceeded(usage); err != nil {
		b.setTerminalError(&Error{Code: ErrorResourceBudget, Op: "enforce browser resources", Err: err})
		b.terminateAfterFailure()
		return false
	}
	return true
}

func (b *Browser) setTerminalError(err error) {
	b.mu.Lock()
	if b.terminalErr == nil && !b.closing {
		b.terminalErr = err
	}
	b.mu.Unlock()
}

func (b *Browser) terminateAfterFailure() {
	if err := b.terminate(); err != nil {
		b.mu.Lock()
		b.terminalErr = errors.Join(b.terminalErr, err)
		b.mu.Unlock()
	}
}

func (b *Browser) reapHelpers() {
	b.mu.RLock()
	closing := b.closing
	b.mu.RUnlock()
	if !closing {
		_ = signalProcessGroup(b.cmd.Process, syscall.SIGKILL)
	}
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
		case <-b.processDone:
			if terminal := b.terminalError(); terminal != nil {
				return "", terminal
			}
			return "", &Error{Code: ErrorBrowserCrash, Op: "await readiness", Err: fmt.Errorf("process exited: %v; output: %s", b.WaitError(), b.Output())}
		case <-waitCtx.Done():
			select {
			case <-b.processDone:
				if terminal := b.terminalError(); terminal != nil {
					return "", terminal
				}
				return "", &Error{Code: ErrorBrowserCrash, Op: "await readiness", Err: fmt.Errorf("process exited: %v; output: %s", b.WaitError(), b.Output())}
			default:
			}
			code := ErrorLaunchTimeout
			if ctx.Err() != nil {
				code = ErrorCancelled
			}
			return "", &Error{Code: code, Op: "await readiness", Err: fmt.Errorf("%w; output: %s", waitCtx.Err(), b.Output())}
		case <-ticker.C:
		}
	}
}

func (b *Browser) terminalError() error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.terminalErr
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

// Warnings returns immutable launch-policy warnings requiring operator visibility.
func (b *Browser) Warnings() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]string(nil), b.warnings...)
}

// Done closes when Chromium exits.
func (b *Browser) Done() <-chan struct{} {
	return b.done
}

// Signal forwards a signal to the owned Chromium process. Signal(0) is the
// non-destructive liveness probe used by the Omnimus browser circuit.
func (b *Browser) Signal(signal os.Signal) error {
	if b == nil {
		return errors.New("browser process is nil")
	}
	if signal == nil {
		return errors.New("browser process signal is nil")
	}
	b.mu.RLock()
	cmd := b.cmd
	b.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return errors.New("browser process is not running")
	}
	return cmd.Process.Signal(signal)
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
	terminalErr := b.terminalErr
	closing := b.closing
	b.mu.RUnlock()
	if terminalErr != nil {
		return terminalErr
	}
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
	select {
	case <-b.done:
		result = errors.Join(result, b.cleanupErr)
	case <-time.After(b.shutdown):
		result = errors.Join(result, fmt.Errorf("wait for Chromium cleanup: timeout"))
	}
	return result
}

func (b *Browser) cleanup() {
	b.cleanupOnce.Do(func() {
		var result error
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
		b.cleanupErr = result
	})
}

func (b *Browser) terminate() error {
	select {
	case <-b.processDone:
		return nil
	default:
		if err := signalProcessGroup(b.cmd.Process, syscall.SIGTERM); err != nil {
			return fmt.Errorf("terminate Chromium process group: %w", err)
		}
		timer := time.NewTimer(b.shutdown)
		select {
		case <-b.processDone:
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

	timer := time.NewTimer(b.shutdown)
	defer timer.Stop()
	select {
	case <-b.processDone:
		return nil
	case <-timer.C:
		if err := b.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill Chromium process: %w", err)
		}
	}

	timer2 := time.NewTimer(b.shutdown)
	defer timer2.Stop()
	select {
	case <-b.processDone:
		return nil
	case <-timer2.C:
		return fmt.Errorf("Chromium process %d did not exit after SIGKILL", b.cmd.Process.Pid)
	}
}
