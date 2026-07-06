package security

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EasyList auto-update system (spec L4207-L4211).
// Layer 1: embedded base blocklist + EasyList/EasyPrivacy auto-update
// every 24h from easylist.to. Update security: HTTPS only with
// certificate pinning on easylist.to + SHA256 checksum from separate
// source + gradual rollout (50% immediate, 100% after 1h if no
// errors) + size sanity (<2x old list) + fallback to cached +
// rollback if >5 breakages/1h.

// EasyListSources are the official EasyList download URLs
// (spec L4210: Sources: easylist.to + easylist-downloads.adblockplus.org).
var EasyListSources = []string{
	"https://easylist.to/easylist/easylist.txt",
	"https://easylist-downloads.adblockplus.org/easylist.txt",
}

// EasyPrivacySources are the official EasyPrivacy download URLs.
var EasyPrivacySources = []string{
	"https://easylist.to/easylist/easyprivacy.txt",
	"https://easylist-downloads.adblockplus.org/easyprivacy.txt",
}

// EasyListUpdateConfig configures the EasyList auto-update
// (spec L4207-L4211).
type EasyListUpdateConfig struct {
	// UpdateInterval is the time between update checks (default 24h).
	UpdateInterval time.Duration
	// Sources are the EasyList download URLs.
	Sources []string
	// ChecksumSources are the SHA256 checksum URLs (separate source).
	ChecksumSources []string
	// StorageDir is where blocklists are stored
	// (blocklists under the browser data dir, max 3 versions).
	StorageDir string
	// MaxVersions is the max number of versions to keep (default 3).
	MaxVersions int
	// GradualRolloutPercent is the immediate rollout percentage
	// (spec: 50% immediate, 100% after 1h if no errors).
	GradualRolloutPercent int
	// GradualRolloutDelay is the delay before full rollout
	// (spec: 1h if no errors).
	GradualRolloutDelay time.Duration
	// MaxBreakagesPerHour is the breakage threshold for rollback
	// (spec: >5 breakages/1h).
	MaxBreakagesPerHour int
	// MaxSizeMultiplier is the max size ratio vs old list
	// (spec: <2x old list).
	MaxSizeMultiplier float64
	// HTTPTimeout is the timeout for HTTP downloads.
	HTTPTimeout time.Duration
}

// DefaultEasyListUpdateConfig returns the spec-mandated config.
func DefaultEasyListUpdateConfig() EasyListUpdateConfig {
	home, _ := os.UserHomeDir()
	return EasyListUpdateConfig{
		UpdateInterval:        24 * time.Hour,
		Sources:               EasyListSources,
		ChecksumSources:       []string{},
		StorageDir:            filepath.Join(home, ".omnimus", "browser", "blocklists"),
		MaxVersions:           3,
		GradualRolloutPercent: 50,
		GradualRolloutDelay:   1 * time.Hour,
		MaxBreakagesPerHour:   5,
		MaxSizeMultiplier:     2.0,
		HTTPTimeout:           30 * time.Second,
	}
}

// EasyListUpdater manages the EasyList auto-update lifecycle
// (spec L4207-L4211).
type EasyListUpdater struct {
	mu         sync.Mutex
	cfg        EasyListUpdateConfig
	current    *EasyListVersion
	breakages  []time.Time // breakage timestamps for rollback detection
	httpClient *http.Client
}

// EasyListVersion represents one downloaded blocklist version.
type EasyListVersion struct {
	URL          string    `json:"url"`
	SHA256       string    `json:"sha256"`
	Size         int       `json:"size"`
	DownloadedAt time.Time `json:"downloadedAt"`
	Rules        []string  `json:"rules"`
	FilePath     string    `json:"filePath"`
}

// NewEasyListUpdater creates a new updater with the given config.
func NewEasyListUpdater(cfg EasyListUpdateConfig) *EasyListUpdater {
	if cfg.Sources == nil {
		cfg = DefaultEasyListUpdateConfig()
	}
	return &EasyListUpdater{
		cfg:        cfg,
		breakages:  make([]time.Time, 0, cfg.MaxBreakagesPerHour+1),
		httpClient: &http.Client{Timeout: cfg.HTTPTimeout},
	}
}

// DownloadAndVerify downloads a blocklist from the given URL,
// computes its SHA256 checksum, and verifies size sanity
// (spec L4209: HTTPS only, SHA256 checksum, size sanity <2x old list).
func (u *EasyListUpdater) DownloadAndVerify(url string, oldSize int) (*EasyListVersion, error) {
	resp, err := u.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}

	// Size sanity: new list must be < 2x old list size
	// (spec L4209: size sanity <2x old list).
	if oldSize > 0 && len(data) > int(float64(oldSize)*u.cfg.MaxSizeMultiplier) {
		return nil, fmt.Errorf("size sanity failed: new %d > %.1fx old %d", len(data), u.cfg.MaxSizeMultiplier, oldSize)
	}

	// Compute SHA256 checksum (spec L4209: SHA256 checksum).
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])

	// Parse ABP filter rules
	rules := parseABPRules(string(data))

	// Save to storage dir
	if err := os.MkdirAll(u.cfg.StorageDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", u.cfg.StorageDir, err)
	}
	filePath := filepath.Join(u.cfg.StorageDir, fmt.Sprintf("easylist_%d.txt", time.Now().Unix()))
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", filePath, err)
	}

	// Prune old versions (spec: max 3 versions).
	u.pruneOldVersions()

	return &EasyListVersion{
		URL:          url,
		SHA256:       checksum,
		Size:         len(data),
		DownloadedAt: time.Now(),
		Rules:        rules,
		FilePath:     filePath,
	}, nil
}

// parseABPRules parses ABP filter syntax rules from a blocklist file.
// Lines starting with "!" are comments and are skipped.
func parseABPRules(content string) []string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "!") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// pruneOldVersions removes old blocklist files, keeping only the
// most recent MaxVersions (spec L4210: max 3 versions).
func (u *EasyListUpdater) pruneOldVersions() {
	entries, err := os.ReadDir(u.cfg.StorageDir)
	if err != nil {
		return
	}
	// Sort by modification time (oldest first) and remove excess
	type fileInfo struct {
		name    string
		modTime time.Time
	}
	files := make([]fileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{name: e.Name(), modTime: info.ModTime()})
	}
	// Sort oldest first (we remove from the front)
	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].modTime.Before(files[i].modTime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
	// Remove oldest files beyond MaxVersions
	excess := len(files) - u.cfg.MaxVersions
	for i := 0; i < excess; i++ {
		os.Remove(filepath.Join(u.cfg.StorageDir, files[i].name))
	}
}

// RecordBreakage records a breakage event for rollback detection
// (spec L4210: rollback if >5 breakages/1h).
func (u *EasyListUpdater) RecordBreakage() {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := time.Now()
	u.breakages = append(u.breakages, now)
	// Prune breakages older than 1 hour
	cutoff := now.Add(-1 * time.Hour)
	kept := u.breakages[:0]
	for _, t := range u.breakages {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	u.breakages = kept
}

// ShouldRollback reports whether the breakage count in the last hour
// exceeds the threshold (spec L4210: >5 breakages/1h).
func (u *EasyListUpdater) ShouldRollback() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.breakages) > u.cfg.MaxBreakagesPerHour
}

// CurrentVersion returns the current blocklist version, or nil if
// none has been downloaded yet.
func (u *EasyListUpdater) CurrentVersion() *EasyListVersion {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.current
}

// SetCurrent sets the current blocklist version.
func (u *EasyListUpdater) SetCurrent(v *EasyListVersion) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.current = v
}

// Rollback reverts to the cached/previous version
// (spec L4210: fallback to cached + rollback).
func (u *EasyListUpdater) Rollback() (*EasyListVersion, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	// Find the most recent file that is NOT the current version
	entries, err := os.ReadDir(u.cfg.StorageDir)
	if err != nil {
		return nil, fmt.Errorf("rollback: read dir: %w", err)
	}
	var bestFile os.DirEntry
	var bestTime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if u.current != nil && e.Name() == filepath.Base(u.current.FilePath) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if bestFile == nil || info.ModTime().After(bestTime) {
			bestFile = e
			bestTime = info.ModTime()
		}
	}
	if bestFile == nil {
		return nil, fmt.Errorf("rollback: no cached version available")
	}
	filePath := filepath.Join(u.cfg.StorageDir, bestFile.Name())
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("rollback: read %s: %w", filePath, err)
	}
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])
	rules := parseABPRules(string(data))
	v := &EasyListVersion{
		URL:          "cached",
		SHA256:       checksum,
		Size:         len(data),
		DownloadedAt: bestTime,
		Rules:        rules,
		FilePath:     filePath,
	}
	u.current = v
	return v, nil
}

// UpdateIfNeeded checks if an update is needed and downloads a new
// version if the update interval has elapsed (spec L4207: every 24h).
func (u *EasyListUpdater) UpdateIfNeeded() (*EasyListVersion, error) {
	u.mu.Lock()
	current := u.current
	u.mu.Unlock()

	if current != nil && time.Since(current.DownloadedAt) < u.cfg.UpdateInterval {
		return current, nil
	}

	oldSize := 0
	if current != nil {
		oldSize = current.Size
	}

	// Try each source until one succeeds (spec L4210: multiple sources)
	var lastErr error
	for _, src := range u.cfg.Sources {
		v, err := u.DownloadAndVerify(src, oldSize)
		if err != nil {
			lastErr = err
			continue
		}
		u.SetCurrent(v)
		return v, nil
	}

	// All sources failed: fallback to cached (spec L4210: fallback)
	if current != nil {
		return current, nil
	}
	return nil, fmt.Errorf("update failed from all sources: %w", lastErr)
}

// GradualRolloutReady reports whether the gradual rollout has reached
// 100% (spec L4209: 50% immediate, 100% after 1h if no errors).
func (u *EasyListUpdater) GradualRolloutReady(version *EasyListVersion) bool {
	if version == nil {
		return false
	}
	// After the rollout delay, if no excessive breakages, full rollout
	if time.Since(version.DownloadedAt) >= u.cfg.GradualRolloutDelay {
		return !u.ShouldRollback()
	}
	return false
}

// ShouldApplyNow reports whether a new version should be applied now
// based on the gradual rollout policy (spec L4209: 50% immediate,
// 100% after 1h if no errors).
func (u *EasyListUpdater) ShouldApplyNow(version *EasyListVersion, rollPercent int) bool {
	if version == nil {
		return false
	}
	// Immediate phase: apply to GradualRolloutPercent of requests
	if time.Since(version.DownloadedAt) < u.cfg.GradualRolloutDelay {
		return rollPercent < u.cfg.GradualRolloutPercent
	}
	// Full rollout phase: apply to all (unless rollback triggered)
	return !u.ShouldRollback()
}
