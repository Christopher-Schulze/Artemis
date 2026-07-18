//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestLaunchPreservesConfiguredProfile(t *testing.T) {
	profile := t.TempDir()
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserReadyScript), UserDataDir: profile,
		StartupTimeout: 5 * time.Second, ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("configured profile removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(profile, "DevToolsActivePort")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale DevTools endpoint survived close: %v", err)
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
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(profile, profileLeaseName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile lease survived close: %v", err)
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
		BinaryPath: writeBrowserScript(t, "#!/bin/sh\nexit 7\n"), StartupTimeout: time.Second, ShutdownTimeout: time.Second,
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
		BinaryPath: writeBrowserScript(t, browserIdleScript), StartupTimeout: time.Second, ShutdownTimeout: time.Second,
	})
	if !IsCode(err, ErrorCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestCloseForcesUnresponsiveProcessGroup(t *testing.T) {
	browser, err := Launch(context.Background(), LaunchConfig{
		BinaryPath: writeBrowserScript(t, browserIgnoresTermScript), StartupTimeout: 5 * time.Second, ShutdownTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := browser.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("forced shutdown exceeded bound")
	}
}

func TestCappedOutputRetainsTail(t *testing.T) {
	output := newCappedOutput(5)
	_, _ = output.Write([]byte("abc"))
	_, _ = output.Write([]byte("defg"))
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

func writeBrowserScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "browser-fixture")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
