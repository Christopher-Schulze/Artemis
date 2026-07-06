package bridge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExtensionLoader configures browser extension loading via Chromium flags
// (spec L4563): --load-extension + --disable-extensions-except, sources
// from the browser extensions dir (per-profile configurable), explicit
// allowlist only.
type ExtensionLoader struct {
	BaseDir    string   // root extensions dir
	Allowlist  []string // explicit allowlist of extension names/paths
	ProfileDir string   // per-profile override
}

// DefaultExtensionBaseDir returns the default extensions root.
func DefaultExtensionBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".omnimus", "browser", "extensions")
}

// NewExtensionLoader creates a loader with the default base dir and an
// explicit allowlist. Empty allowlist means NO extensions load (spec L4563:
// explicit allowlist only).
func NewExtensionLoader(allowlist []string) *ExtensionLoader {
	return &ExtensionLoader{
		BaseDir:   DefaultExtensionBaseDir(),
		Allowlist: allowlist,
	}
}

// WithProfileDir overrides the base dir per-profile.
func (l *ExtensionLoader) WithProfileDir(profileDir string) *ExtensionLoader {
	if l == nil {
		return nil
	}
	l.ProfileDir = profileDir
	return l
}

// ResolvedDir returns the effective extensions directory.
func (l *ExtensionLoader) ResolvedDir() string {
	if l == nil {
		return ""
	}
	if l.ProfileDir != "" {
		return l.ProfileDir
	}
	return l.BaseDir
}

// ResolvedExtensions returns the absolute paths of allowlisted extension
// directories that exist on disk. An extension in the allowlist that does
// not exist on disk is skipped with a recorded error per entry.
func (l *ExtensionLoader) ResolvedExtensions() ([]string, []error) {
	if l == nil {
		return nil, []error{errors.New("extension loader: nil")}
	}
	if len(l.Allowlist) == 0 {
		return nil, nil
	}
	base := l.ResolvedDir()
	if base == "" {
		return nil, []error{errors.New("extension loader: no base dir")}
	}
	var paths []string
	var errs []error
	for _, name := range l.Allowlist {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		// Allowlist entries may be absolute paths or names relative to base.
		var candidate string
		if filepath.IsAbs(name) {
			candidate = name
		} else {
			candidate = filepath.Join(base, name)
		}
		info, err := os.Stat(candidate)
		if err != nil {
			errs = append(errs, fmt.Errorf("extension loader: %s: %w", name, err))
			continue
		}
		if !info.IsDir() {
			errs = append(errs, fmt.Errorf("extension loader: %s: not a directory", name))
			continue
		}
		paths = append(paths, candidate)
	}
	return paths, errs
}

// ChromiumArgs builds the Chromium flags for loading the resolved
// extensions (spec L4563): --load-extension=<paths> and
// --disable-extensions-except=<paths>. Returns empty slice when no
// extensions resolve (allowlist empty or none on disk).
func (l *ExtensionLoader) ChromiumArgs() ([]string, error) {
	if l == nil {
		return nil, errors.New("extension loader: nil")
	}
	paths, errs := l.ResolvedExtensions()
	if len(paths) == 0 {
		// Allowlist empty is not an error; missing-on-disk is.
		if len(errs) > 0 {
			return nil, errs[0]
		}
		return nil, nil
	}
	joined := strings.Join(paths, ",")
	return []string{
		"--load-extension=" + joined,
		"--disable-extensions-except=" + joined,
	}, nil
}

// IsAllowed reports whether an extension name/path is in the allowlist.
func (l *ExtensionLoader) IsAllowed(name string) bool {
	if l == nil || name == "" {
		return false
	}
	name = strings.TrimSpace(name)
	for _, a := range l.Allowlist {
		if strings.TrimSpace(a) == name {
			return true
		}
	}
	return false
}
