package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Binary describes a discovered Chromium-family executable.
type Binary struct {
	Path   string
	Source string
}

// DiscoverBinary resolves an explicit executable before platform defaults.
func DiscoverBinary(configuredPath string) (Binary, error) {
	if strings.TrimSpace(configuredPath) != "" {
		path, err := resolveExecutable(configuredPath)
		if err != nil {
			return Binary{}, &Error{Code: ErrorBinaryNotFound, Op: "discover explicit binary", Err: err}
		}
		return Binary{Path: path, Source: "configured"}, nil
	}

	attempted := make([]string, 0, 12)
	for _, candidate := range platformCandidates() {
		attempted = append(attempted, candidate)
		path, err := resolveExecutable(candidate)
		if err == nil {
			return Binary{Path: path, Source: "platform_default"}, nil
		}
	}
	return Binary{}, &Error{
		Code: ErrorBinaryNotFound,
		Op:   "discover binary",
		Err:  fmt.Errorf("no Chromium-family executable found; checked %s", strings.Join(attempted, ", ")),
	}
}

func resolveExecutable(candidate string) (string, error) {
	path := candidate
	if !strings.ContainsRune(candidate, os.PathSeparator) {
		resolved, err := exec.LookPath(candidate)
		if err != nil {
			return "", err
		}
		path = resolved
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("absolute path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%q is not an executable regular file", abs)
	}
	return filepath.Clean(abs), nil
}

// platformCandidates lists only real Google Chrome installs. Chromium,
// Canary and distro-bundled clones are deliberately excluded: the fallback
// engine must orchestrate the operator's installed Chrome and never a
// downloaded or rotting clone.
func platformCandidates() []string {
	pathNames := []string{"google-chrome", "google-chrome-stable"}
	switch runtime.GOOS {
	case "darwin":
		return append([]string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		}, pathNames...)
	case "linux":
		return pathNames
	default:
		return pathNames
	}
}
