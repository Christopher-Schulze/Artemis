//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestDiscoverBinaryExplicitPrecedence(t *testing.T) {
	path := writeBrowserScript(t, browserReadyScript)
	binary, err := DiscoverBinary(path)
	if err != nil {
		t.Fatal(err)
	}
	if binary.Path != path || binary.Source != "configured" {
		t.Fatalf("binary=%+v", binary)
	}
}

func TestDiscoverBinaryMissingIsTyped(t *testing.T) {
	_, err := DiscoverBinary(filepath.Join(t.TempDir(), "missing"))
	if !IsCode(err, ErrorBinaryNotFound) {
		t.Fatalf("expected binary_not_found, got %v", err)
	}
}

func TestDiscoverBinaryPlatformDefault(t *testing.T) {
	binary, err := DiscoverBinary("")
	if err != nil {
		t.Fatalf("platform Chromium unavailable: %v", err)
	}
	if binary.Path == "" || binary.Source != "platform_default" {
		t.Fatalf("binary=%+v", binary)
	}
}

func TestReadEndpointValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "DevToolsActivePort")
	if err := os.WriteFile(path, []byte("43210\n/devtools/browser/test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint, err := readEndpoint(path)
	if err != nil || endpoint != "ws://127.0.0.1:43210/devtools/browser/test" {
		t.Fatalf("endpoint=%q err=%v", endpoint, err)
	}
	for _, invalid := range []string{"", "0\n/devtools/browser/x", "9222\n/devtools/page/x", "x\n/devtools/browser/x"} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readEndpoint(path); err == nil {
			t.Fatalf("expected invalid endpoint for %q", invalid)
		}
	}
}

func TestLaunchOwnsAndRemovesDisposableProfile(t *testing.T) {
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserReadyScript), Headless: true,
		StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := browser.ProfileDir()
	if browser.Endpoint() != "ws://127.0.0.1:43210/devtools/browser/test" {
		t.Fatalf("endpoint=%q", browser.Endpoint())
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("profile missing before close: %v", err)
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned profile survived close: %v", err)
	}
	if err := browser.Close(); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
}

func TestLaunchRegistersAndUnregistersOwnedChromiumProcess(t *testing.T) {
	registrar := &recordingChildRegistrar{}
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath:            writeBrowserScript(t, browserReadyScript),
		StartupTimeout:        5 * time.Second,
		ShutdownTimeout:       time.Second,
		ChildProcessRegistrar: registrar,
	})
	if err != nil {
		t.Fatal(err)
	}
	registrar.mu.Lock()
	pid, name := registrar.pid, registrar.name
	registrar.mu.Unlock()
	if pid <= 0 || name == "" {
		t.Fatalf("registration pid=%d name=%q", pid, name)
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
	registrar.mu.Lock()
	unregistered := registrar.unregistered
	registrar.mu.Unlock()
	if !unregistered {
		t.Fatal("owned Chromium process remained registered after close")
	}
}

func TestLaunchFailsClosedWhenChildRegistrationFails(t *testing.T) {
	_, err := Launch(context.Background(), LaunchConfig{
		BinaryPath:            writeBrowserScript(t, browserReadyScript),
		StartupTimeout:        5 * time.Second,
		ShutdownTimeout:       time.Second,
		ChildProcessRegistrar: rejectingChildRegistrar{},
	})
	if !IsCode(err, ErrorLaunchFailed) {
		t.Fatalf("registration failure=%v", err)
	}
}

type recordingChildRegistrar struct {
	mu           sync.Mutex
	pid          int
	name         string
	unregistered bool
}

func (r *recordingChildRegistrar) RegisterChild(pid int, name string) (func(), error) {
	r.mu.Lock()
	r.pid = pid
	r.name = name
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		r.unregistered = true
		r.mu.Unlock()
	}, nil
}

type rejectingChildRegistrar struct{}

func (rejectingChildRegistrar) RegisterChild(int, string) (func(), error) {
	return nil, errors.New("governor unavailable")
}

func TestBrowserSignalZeroChecksOwnedChromium(t *testing.T) {
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserReadyScript), Headless: true,
		StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := browser.Close(); err != nil {
			t.Errorf("close browser: %v", err)
		}
	}()
	if err := browser.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("Signal(0): %v", err)
	}
}

func TestLaunchPreservesConfiguredProfile(t *testing.T) {
	profile := t.TempDir()
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserReadyScript), UserDataDir: profile,
		StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if closeErr := browser.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if _, statErr := os.Stat(profile); statErr != nil {
		t.Fatalf("configured profile removed: %v", statErr)
	}
	if _, endpointErr := os.Stat(filepath.Join(profile, "DevToolsActivePort")); !errors.Is(endpointErr, os.ErrNotExist) {
		t.Fatalf("stale DevTools endpoint survived close: %v", endpointErr)
	}
	restarted, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserReadyScript), UserDataDir: profile,
		StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("configured profile did not restart: %v", err)
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchRejectsReservedFlagsAndActiveProfile(t *testing.T) {
	script := writeBrowserScript(t, browserReadyScript)
	for _, arg := range []string{
		"--remote-debugging-port=9222",
		"--remote-debugging-address=0.0.0.0",
		"--remote-debugging-pipe",
		"--user-data-dir=/tmp/shared-profile",
		"--proxy-server=http://127.0.0.1:8080",
		"--proxy-bypass-list=*",
		"--proxy-pac-url=http://127.0.0.1/proxy.pac",
		"--proxy-auto-detect",
		"--no-proxy-server",
		"--host-resolver-rules=MAP * 127.0.0.1",
		"--enable-quic",
		"--disable-quic",
		"--no-sandbox",
		"--disable-setuid-sandbox",
	} {
		_, err := Launch(context.Background(), LaunchConfig{BinaryPath: script, ExtraArgs: []string{arg}})
		if !IsCode(err, ErrorInvalidConfig) {
			t.Fatalf("reserved flag %q error=%v", arg, err)
		}
	}
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, "DevToolsActivePort"), []byte("9222\n/devtools/browser/live"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Launch(context.Background(), LaunchConfig{BinaryPath: script, UserDataDir: profile})
	if !IsCode(err, ErrorInvalidConfig) {
		t.Fatalf("active profile error=%v", err)
	}
}

func TestSandboxPolicyRequiresExplicitDisableAndWarns(t *testing.T) {
	script := writeBrowserScript(t, browserReadyScript)
	normalized, _, err := normalizeLaunchConfig(LaunchConfig{BinaryPath: script})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Sandbox != SandboxRequired || containsArg(chromiumArgs(normalized, "/tmp/profile"), "--no-sandbox") {
		t.Fatalf("secure sandbox defaults not applied: %+v", normalized)
	}
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: script, Sandbox: SandboxDisabled, StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if warnings := browser.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "disabled") {
		t.Fatalf("warnings=%v", warnings)
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestChromiumArgsPinPolicyProxyAndDNS(t *testing.T) {
	config := LaunchConfig{Headless: true, PolicyProxyURL: "http://127.0.0.1:43123"}
	args := chromiumArgs(config, "/tmp/artemis-profile")
	for _, want := range []string{
		"--proxy-server=http://127.0.0.1:43123",
		"--proxy-bypass-list=<-loopback>",
		"--host-resolver-rules=" + policyHostResolverRule,
		"--disable-quic",
	} {
		if !containsArg(args, want) {
			t.Fatalf("Chromium args missing %q: %v", want, args)
		}
	}
}

func TestChromiumArgsDisableMacAppCodeSignClone(t *testing.T) {
	tests := []struct {
		name  string
		extra []string
		want  string
	}{
		{name: "adds mandatory feature", extra: []string{"--site-per-process"}, want: "--disable-features=MacAppCodeSignClone"},
		{name: "merges inline values", extra: []string{"--disable-features=Foo,Bar", "--site-per-process", "--disable-features=Bar,Baz"}, want: "--disable-features=Foo,Bar,Baz,MacAppCodeSignClone"},
		{name: "merges separate value", extra: []string{"--site-per-process", "--disable-features", "Foo, Baz"}, want: "--disable-features=Foo,Baz,MacAppCodeSignClone"},
		{name: "repairs bare flag", extra: []string{"--disable-features"}, want: "--disable-features=MacAppCodeSignClone"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := chromiumArgs(LaunchConfig{ExtraArgs: test.extra}, "/tmp/artemis-profile")
			var disableArgs []string
			for _, arg := range args {
				if strings.HasPrefix(arg, "--disable-features=") {
					disableArgs = append(disableArgs, arg)
				}
			}
			if len(disableArgs) != 1 || disableArgs[0] != test.want {
				t.Fatalf("disable-features args=%v, want [%q]", disableArgs, test.want)
			}
		})
	}
}

func TestLaunchPassesMacAppCodeSignCloneProtection(t *testing.T) {
	profile := t.TempDir()
	script := writeBrowserScript(t, browserRecordsArgsScript)
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: script, UserDataDir: profile, ExtraArgs: []string{"--disable-features=ExistingFeature"},
		StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := browser.Close(); closeErr != nil {
			t.Errorf("close browser: %v", closeErr)
		}
	}()
	argsData, err := os.ReadFile(filepath.Join(profile, "launch-args"))
	if err != nil {
		t.Fatal(err)
	}
	var disableArgs []string
	for _, arg := range strings.Split(strings.TrimSpace(string(argsData)), "\n") {
		if strings.HasPrefix(arg, "--disable-features=") {
			disableArgs = append(disableArgs, arg)
		}
	}
	if len(disableArgs) != 1 || disableArgs[0] != "--disable-features=ExistingFeature,MacAppCodeSignClone" {
		t.Fatalf("launched disable-features args=%v", disableArgs)
	}
}

func TestNormalizeLaunchConfigRejectsInvalidPolicyProxyAndPorts(t *testing.T) {
	script := writeBrowserScript(t, browserReadyScript)
	for _, config := range []LaunchConfig{
		{BinaryPath: script, PolicyProxyURL: "https://127.0.0.1:443"},
		{BinaryPath: script, PolicyProxyURL: "http://example.com:8080"},
		{BinaryPath: script, PolicyProxyURL: "http://127.0.0.1"},
		{BinaryPath: script, AllowedPorts: []int{0}},
		{BinaryPath: script, AllowedPorts: []int{65536}},
	} {
		if _, _, err := normalizeLaunchConfig(config); !IsCode(err, ErrorInvalidConfig) {
			t.Fatalf("config=%+v error=%v", config, err)
		}
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestConfiguredProfileLeasePreventsParallelOwnership(t *testing.T) {
	profile := t.TempDir()
	script := writeBrowserScript(t, browserReadyScript)
	first, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: script, UserDataDir: profile, StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Launch(context.Background(), LaunchConfig{
		BinaryPath: script, UserDataDir: profile, StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if !IsCode(err, ErrorInvalidConfig) {
		t.Fatalf("parallel profile ownership error=%v", err)
	}
	if closeErr := first.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if _, statErr := os.Stat(filepath.Join(profile, profileLeaseName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("profile lease survived close: %v", statErr)
	}
}

func TestLaunchTimeoutAndCrashAreTyped(t *testing.T) {
	_, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserIdleScript), StartupTimeout: 30 * time.Millisecond, ShutdownTimeout: time.Second,
	})
	if !IsCode(err, ErrorLaunchTimeout) {
		t.Fatalf("timeout error=%v", err)
	}
	_, err = Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, "#!/bin/sh\nexit 7\n"), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if !IsCode(err, ErrorBrowserCrash) {
		t.Fatalf("crash error=%v", err)
	}
}

func TestLaunchFailureReleasesConfiguredProfileLease(t *testing.T) {
	profile := t.TempDir()
	_, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserIdleScript), UserDataDir: profile,
		StartupTimeout: 30 * time.Millisecond, ShutdownTimeout: time.Second,
	})
	if !IsCode(err, ErrorLaunchTimeout) {
		t.Fatalf("timeout error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(profile, profileLeaseName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed launch retained profile lease: %v", err)
	}
}

func TestRunningBrowserCrashIsObservable(t *testing.T) {
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserCrashAfterReadyScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-browser.Done()
	if err := browser.Err(); !IsCode(err, ErrorBrowserCrash) {
		t.Fatalf("running crash error=%v", err)
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUnexpectedCrashAutomaticallyCleansDisposableProfile(t *testing.T) {
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserCrashAfterReadyScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := browser.ProfileDir()
	<-browser.Done()
	if _, statErr := os.Stat(profile); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("crashed browser retained disposable profile: %v", statErr)
	}
}

func TestRunningBrowserHonorsOwnerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	browser, err := Launch(ctx, LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserReadyScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-browser.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("browser survived owner cancellation")
	}
	if err := browser.Err(); !IsCode(err, ErrorCancelled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestResourceBudgetBreachTerminatesProcessGroup(t *testing.T) {
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserReadyScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
		ResourceBudget: ResourceBudget{MaxMemoryBytes: 1, SampleInterval: 5 * time.Millisecond},
		resourceSampler: func(int, string) (ResourceUsage, error) {
			return ResourceUsage{MemoryBytes: 2}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-browser.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("browser survived resource breach")
	}
	if err := browser.Err(); !IsCode(err, ErrorResourceBudget) {
		t.Fatalf("resource error=%v", err)
	}
}

func TestResourceDiagnosticsEmitImmediatelyAndFailClosed(t *testing.T) {
	samples := make(chan ResourceUsage, 1)
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserReadyScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
		ResourceBudget: ResourceBudget{SampleInterval: time.Hour},
		resourceSampler: func(int, string) (ResourceUsage, error) {
			return ResourceUsage{CPUPercent: 12.5, MemoryBytes: 64, ProfileDiskBytes: 32}, nil
		},
		ResourceSink: func(usage ResourceUsage) error {
			samples <- usage
			return errors.New("audit disk unavailable")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case usage := <-samples:
		if usage.CPUPercent != 12.5 || usage.MemoryBytes != 64 || usage.ProfileDiskBytes != 32 {
			t.Fatalf("usage=%+v", usage)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("initial resource diagnostic was not emitted")
	}
	select {
	case <-browser.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("browser survived diagnostics failure")
	}
	if err := browser.Err(); !IsCode(err, ErrorDiagnostics) {
		t.Fatalf("diagnostics error=%v", err)
	}
}

func TestInitialResourceDiagnosticDoesNotEnforceColdStartBurst(t *testing.T) {
	samples := make(chan ResourceUsage, 1)
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserReadyScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
		ResourceBudget: ResourceBudget{MaxMemoryBytes: 1, SampleInterval: time.Hour},
		resourceSampler: func(int, string) (ResourceUsage, error) {
			return ResourceUsage{MemoryBytes: 2}, nil
		},
		ResourceSink: func(usage ResourceUsage) error {
			samples <- usage
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-samples:
	case <-time.After(3 * time.Second):
		t.Fatal("initial resource diagnostic was not emitted")
	}
	select {
	case <-browser.Done():
		t.Fatalf("cold-start sample enforced before interval: %v", browser.Err())
	case <-time.After(100 * time.Millisecond):
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionTimeoutDuringStartupIsResourceFailure(t *testing.T) {
	_, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserIdleScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: 100 * time.Millisecond,
		ResourceBudget: ResourceBudget{SessionTimeout: 20 * time.Millisecond},
	})
	if !IsCode(err, ErrorResourceBudget) {
		t.Fatalf("session timeout error=%v", err)
	}
}

func TestProfileDiskBudgetUsesRealOwnedProfileBytes(t *testing.T) {
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserWritesProfileScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
		ResourceBudget: ResourceBudget{MaxProfileDiskBytes: 1024, SampleInterval: 5 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-browser.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("browser survived profile disk breach")
	}
	if err := browser.Err(); !IsCode(err, ErrorResourceBudget) {
		t.Fatalf("profile budget error=%v", err)
	}
}

func TestUnexpectedLeaderExitReapsProcessGroupHelper(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	script := strings.ReplaceAll(browserLeaderExitWithHelperScript, "HELPER_PID_FILE", pidFile)
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, script), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-browser.Done()
	data, err := os.ReadFile(filepath.Clean(pidFile))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("helper process %d survived leader exit", pid)
	}
}

func TestProcessGuardianReapsBrowserAfterOwnerDeath(t *testing.T) {
	if os.Getenv("ARTEMIS_PROCESS_GUARDIAN_HELPER") == "1" {
		script := os.Getenv("ARTEMIS_PROCESS_GUARDIAN_SCRIPT")
		state := os.Getenv("ARTEMIS_PROCESS_GUARDIAN_STATE")
		browser, err := Launch(context.Background(), LaunchConfig{
			BinaryPath: script, StartupTimeout: 5 * time.Second, ShutdownTimeout: 100 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Clean(state), []byte(browser.ProfileDir()+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "browser.pid")
	stateFile := filepath.Join(dir, "state")
	script := strings.ReplaceAll(browserRecordsPIDScript, "BROWSER_PID_FILE", pidFile)
	scriptPath := writeBrowserScript(t, script)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := newBrowserTestCommand(t.Context(), executable, "-test.run=TestProcessGuardianReapsBrowserAfterOwnerDeath")
	cmd.Env = append(os.Environ(),
		"ARTEMIS_PROCESS_GUARDIAN_HELPER=1",
		"ARTEMIS_PROCESS_GUARDIAN_SCRIPT="+scriptPath,
		"ARTEMIS_PROCESS_GUARDIAN_STATE="+stateFile,
	)
	if output, commandErr := cmd.CombinedOutput(); commandErr != nil {
		t.Fatalf("owner helper: %v: %s", commandErr, output)
	}
	pidData, err := os.ReadFile(filepath.Clean(pidFile))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatal(err)
	}
	profileData, err := os.ReadFile(filepath.Clean(stateFile))
	if err != nil {
		t.Fatal(err)
	}
	profile := strings.TrimSpace(string(profileData))
	deadline := time.Now().Add(4 * time.Second)
	for (processAlive(pid) || pathExists(profile)) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("browser process %d survived owner death", pid)
	}
	if pathExists(profile) {
		t.Fatalf("disposable profile %q survived owner death", profile)
	}
}

func TestUnexpectedCleanBrowserExitIsCrash(t *testing.T) {
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserCleanExitAfterReadyScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-browser.Done()
	if err := browser.Err(); !IsCode(err, ErrorBrowserCrash) {
		t.Fatalf("unexpected clean exit error=%v", err)
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Launch(ctx, LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserIdleScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if !IsCode(err, ErrorCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestCloseForcesUnresponsiveProcessGroup(t *testing.T) {
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserIgnoresTermScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("forced shutdown exceeded bound")
	}
}

func TestCappedOutputRetainsTail(t *testing.T) {
	output := newCappedOutput(5)
	if _, err := output.Write([]byte("abc")); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	if _, err := output.Write([]byte("defg")); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}
	if got := output.String(); got != "cdefg" {
		t.Fatalf("output=%q", got)
	}
}

const browserReadyScript = `#!/bin/sh
profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*) profile="${arg#*=}" ;;
  esac
done
mkdir -p "$profile"
printf '43210\n/devtools/browser/test\n' > "$profile/DevToolsActivePort"
trap 'exit 0' TERM INT
while :; do sleep 1; done
`

const browserRecordsArgsScript = `#!/bin/sh
profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*) profile="${arg#*=}" ;;
  esac
done
mkdir -p "$profile"
printf '%s\n' "$@" > "$profile/launch-args"
printf '43210\n/devtools/browser/test\n' > "$profile/DevToolsActivePort"
trap 'exit 0' TERM INT
while :; do sleep 1; done
`

const browserIdleScript = `#!/bin/sh
trap 'exit 0' TERM INT
while :; do sleep 1; done
`

const browserCrashAfterReadyScript = `#!/bin/sh
profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*) profile="${arg#*=}" ;;
  esac
done
mkdir -p "$profile"
printf '43210\n/devtools/browser/test\n' > "$profile/DevToolsActivePort"
sleep 0.1
exit 9
`

const browserCleanExitAfterReadyScript = `#!/bin/sh
profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*) profile="${arg#*=}" ;;
  esac
done
mkdir -p "$profile"
printf '43210\n/devtools/browser/test\n' > "$profile/DevToolsActivePort"
sleep 0.1
exit 0
`

const browserIgnoresTermScript = `#!/bin/sh
profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*) profile="${arg#*=}" ;;
  esac
done
mkdir -p "$profile"
printf '43210\n/devtools/browser/test\n' > "$profile/DevToolsActivePort"
trap '' TERM INT
while :; do sleep 1; done
`

const browserLeaderExitWithHelperScript = `#!/bin/sh
profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*) profile="${arg#*=}" ;;
  esac
done
mkdir -p "$profile"
sleep 300 &
printf '%s\n' "$!" > "HELPER_PID_FILE"
printf '43210\n/devtools/browser/test\n' > "$profile/DevToolsActivePort"
sleep 0.05
exit 9
`

const browserWritesProfileScript = `#!/bin/sh
profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*) profile="${arg#*=}" ;;
  esac
done
mkdir -p "$profile"
printf '43210\n/devtools/browser/test\n' > "$profile/DevToolsActivePort"
dd if=/dev/zero of="$profile/large.bin" bs=2048 count=1 2>/dev/null
trap 'exit 0' TERM INT
while :; do sleep 1; done
`

const browserRecordsPIDScript = `#!/bin/sh
profile=""
for arg in "$@"; do
  case "$arg" in
    --user-data-dir=*) profile="${arg#*=}" ;;
  esac
done
mkdir -p "$profile"
printf '%s\n' "$$" > "BROWSER_PID_FILE"
printf '43210\n/devtools/browser/test\n' > "$profile/DevToolsActivePort"
trap '' TERM INT
while :; do sleep 1; done
`

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func pathExists(path string) bool {
	_, err := os.Stat(filepath.Clean(path))
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func writeBrowserScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "browser-fixture")
	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Chmod(0o700); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(strings.TrimSpace(body) + "\n")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func newBrowserTestCommand(ctx context.Context, executable string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, executable)
	command.Args = append(command.Args, args...)
	return command
}
