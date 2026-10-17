package security

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// EasyList auto-update (spec L4206). Every feed is downloaded over pinned
// HTTPS from its primary source and verified against an independent mirror:
// both must yield the same SHA-256 over the filter lines (comment headers
// differ per mirror). A list larger than twice its predecessor is refused.
// Verified versions are stored with a manifest (rule count, size and gzip
// size as proof per shipped version), at most MaxVersions are kept, the
// newest valid cached version is loaded at start and every new version
// enters the AdBlocker gradual rollout. Failures keep the cached version.

// EasyListSources are the official EasyList download URLs (primary first).
var EasyListSources = []string{
	"https://easylist.to/easylist/easylist.txt",
	"https://easylist-downloads.adblockplus.org/easylist.txt",
}

// EasyPrivacySources are the official EasyPrivacy download URLs.
var EasyPrivacySources = []string{
	"https://easylist.to/easylist/easyprivacy.txt",
	"https://easylist-downloads.adblockplus.org/easyprivacy.txt",
}

// EasyListPins are SHA-256 SPKI pins (base64) of the root CAs that issue the
// EasyList hosts: Google Trust Services R1-R4 and GlobalSign Root CA for
// easylist.to, ISRG Root X1/X2 and ISRG Root YR for
// easylist-downloads.adblockplus.org. Roots, not leaves, are pinned so
// routine certificate renewal does not stop updates.
var EasyListPins = []string{
	"hxqRlPTu1bMS/0DITB1SSu0vd4u/8l8TjPgfaAp63Gc=", // GTS Root R1
	"Vfd95BwDeSQo+NUYxVEEIlvkOlWY2SalKK1lPhzOx78=", // GTS Root R2
	"QXnt2YHvdHR3tJYmQIr0Paosp6t/nggsEGD4QJZ3Q0g=", // GTS Root R3
	"mEflZT5enoR1FuXLgYYGqnVEoZvmf9c2bVBpiOjYQ0c=", // GTS Root R4
	"K87oWBWM9UZfyddvDfoxL+8lpNyoUB2ptGtn0fv6G2Q=", // GlobalSign Root CA
	"C5+lpZ7tcVwmwQIMcRtPbsQtWLABXhQzejna0wHFr8M=", // ISRG Root X1
	"diGVwiVYbubAI3RW4hB9xU8e/CH2GnkuvVFZE8zmgzI=", // ISRG Root X2
	"fk6IOKit1ild5647BH06ujSIq5XbCgqlbYl6ANhhi88=", // ISRG Root YR
}

// ErrEasyListPin is returned when a source's certificate chain carries no
// pinned key.
var ErrEasyListPin = errors.New("easylist: certificate chain does not match a pinned key")

// EasyListFeed is one list with its primary source and verification mirror.
type EasyListFeed struct {
	Name     string          `json:"name"`
	Category AdBlockCategory `json:"category"`
	Primary  string          `json:"primary"`
	Mirror   string          `json:"mirror"`
}

// DefaultEasyListFeeds returns EasyList (ads) and EasyPrivacy (trackers).
func DefaultEasyListFeeds() []EasyListFeed {
	return []EasyListFeed{
		{Name: "easylist", Category: AdBlockCategoryAd, Primary: EasyListSources[0], Mirror: EasyListSources[1]},
		{Name: "easyprivacy", Category: AdBlockCategoryTracker, Primary: EasyPrivacySources[0], Mirror: EasyPrivacySources[1]},
	}
}

// EasyListUpdateConfig configures the update pipeline.
type EasyListUpdateConfig struct {
	UpdateInterval        time.Duration // default 24h
	Feeds                 []EasyListFeed
	StorageDir            string // versions and manifest.json
	MaxVersions           int    // default 3
	GradualRolloutPercent int    // default 50
	GradualRolloutDelay   time.Duration
	MaxBreakagesPerHour   int
	MaxSizeMultiplier     float64 // default 2.0
	MaxListBytes          int64   // per download, default 16 MiB
	HTTPTimeout           time.Duration
	Pins                  []string       // SPKI SHA-256 pins, default EasyListPins
	RootCAs               *x509.CertPool // nil selects the system roots
}

// DefaultEasyListUpdateConfig returns the spec-mandated config with storage
// under ~/.artemis/browser/blocklists; embedding hosts pass their own home.
func DefaultEasyListUpdateConfig() EasyListUpdateConfig {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return EasyListUpdateConfig{
		UpdateInterval:        24 * time.Hour,
		Feeds:                 DefaultEasyListFeeds(),
		StorageDir:            filepath.Join(home, ".artemis", "browser", "blocklists"),
		MaxVersions:           3,
		GradualRolloutPercent: 50,
		GradualRolloutDelay:   time.Hour,
		MaxBreakagesPerHour:   5,
		MaxSizeMultiplier:     2.0,
		MaxListBytes:          16 << 20,
		HTTPTimeout:           60 * time.Second,
		Pins:                  EasyListPins,
	}
}

// RolloutConfig returns the AdBlocker rollout settings of this config.
func (c EasyListUpdateConfig) RolloutConfig() AdBlockRolloutConfig {
	return AdBlockRolloutConfig{Percent: c.GradualRolloutPercent, Delay: c.GradualRolloutDelay, MaxBreakages: c.MaxBreakagesPerHour, BreakageWindow: time.Hour}
}

// AdBlockFeedManifest is the proof record of one feed in a version.
type AdBlockFeedManifest struct {
	Name            string           `json:"name"`
	Category        AdBlockCategory  `json:"category"`
	Primary         string           `json:"primary"`
	Mirror          string           `json:"mirror"`
	RulesSHA256     string           `json:"rulesSha256"`
	Bytes           int              `json:"bytes"`
	CompressedBytes int              `json:"compressedBytes"`
	Stats           AdBlockRuleStats `json:"stats"`
	File            string           `json:"file"`
}

// AdBlockListManifest is the proof record of one stored list version.
type AdBlockListManifest struct {
	ID           string                `json:"id"`
	SHA256       string                `json:"sha256"`
	Rules        int                   `json:"rules"`
	Bytes        int                   `json:"bytes"`
	DownloadedAt time.Time             `json:"downloadedAt"`
	Feeds        []AdBlockFeedManifest `json:"feeds"`
}

type easyListStore struct {
	Versions []AdBlockListManifest `json:"versions"`
}

// EasyListUpdater downloads, verifies, stores and installs list versions.
type EasyListUpdater struct {
	cfg     EasyListUpdateConfig
	blocker *AdBlocker
	client  *http.Client
	now     func() time.Time

	mu        sync.Mutex
	current   *AdBlockListManifest
	nextCheck time.Time
}

// easyListRetryInterval spaces retries after a failed update.
const easyListRetryInterval = time.Hour

// NewEasyListUpdater validates cfg and returns an updater installing into
// blocker.
func NewEasyListUpdater(cfg EasyListUpdateConfig, blocker *AdBlocker) (*EasyListUpdater, error) {
	switch {
	case blocker == nil:
		return nil, errors.New("easylist: blocker required")
	case cfg.StorageDir == "":
		return nil, errors.New("easylist: storage directory required")
	case len(cfg.Feeds) == 0:
		return nil, errors.New("easylist: at least one feed required")
	case len(cfg.Pins) == 0:
		return nil, errors.New("easylist: certificate pins required")
	}
	for _, feed := range cfg.Feeds {
		if !strings.HasPrefix(feed.Primary, "https://") || !strings.HasPrefix(feed.Mirror, "https://") || feed.Primary == feed.Mirror || feed.Name == "" || strings.ContainsAny(feed.Name, `/\.`) {
			return nil, fmt.Errorf("easylist: feed %q needs a name and distinct HTTPS primary and mirror sources", feed.Name)
		}
	}
	if cfg.UpdateInterval <= 0 {
		cfg.UpdateInterval = 24 * time.Hour
	}
	if cfg.MaxVersions <= 0 {
		cfg.MaxVersions = 3
	}
	if cfg.MaxSizeMultiplier <= 1 {
		cfg.MaxSizeMultiplier = 2
	}
	if cfg.MaxListBytes <= 0 {
		cfg.MaxListBytes = 16 << 20
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 60 * time.Second
	}
	return &EasyListUpdater{cfg: cfg, blocker: blocker, client: pinnedHTTPClient(cfg), now: time.Now}, nil
}

func pinnedHTTPClient(cfg EasyListUpdateConfig) *http.Client {
	pins := make(map[string]bool, len(cfg.Pins))
	for _, pin := range cfg.Pins {
		pins[pin] = true
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    cfg.RootCAs,
			VerifyConnection: func(state tls.ConnectionState) error {
				for _, chain := range state.VerifiedChains {
					for _, cert := range chain {
						sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
						if pins[base64.StdEncoding.EncodeToString(sum[:])] {
							return nil
						}
					}
				}
				return ErrEasyListPin
			},
		},
		ForceAttemptHTTP2: true,
	}
	return &http.Client{
		Timeout:   cfg.HTTPTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 3 {
				return errors.New("easylist: redirects must stay on HTTPS (max 3)")
			}
			return nil
		},
	}
}

// Current returns the manifest of the newest installed version.
func (u *EasyListUpdater) Current() *AdBlockListManifest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.current
}

// LoadCached installs the newest stored version whose files still match
// their recorded checksums. A version still inside its rollout delay
// resumes as candidate on top of the newest older valid version, so a
// restart never promotes an unproven list.
func (u *EasyListUpdater) LoadCached() error {
	store, err := u.readStore()
	if err != nil {
		return err
	}
	var errs []error
	var loaded []*AdBlockList
	for index := len(store.Versions) - 1; index >= 0 && len(loaded) < 2; index-- {
		manifest := store.Versions[index]
		if u.blocker.IsRejected(manifest.SHA256) {
			continue
		}
		list, err := u.loadStoredVersion(manifest)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		loaded = append(loaded, list)
		if u.now().Sub(manifest.DownloadedAt) >= u.cfg.GradualRolloutDelay {
			break
		}
	}
	if len(loaded) == 0 {
		return errors.Join(errs...)
	}
	newest := loaded[0]
	newest.activated = newest.Manifest.DownloadedAt
	if len(loaded) == 2 {
		if err := u.blocker.Install(loaded[1]); err != nil {
			return errors.Join(append(errs, err)...)
		}
	}
	if err := u.blocker.Install(newest); err != nil {
		return errors.Join(append(errs, err)...)
	}
	u.setCurrent(newest.Manifest)
	return nil
}

func (u *EasyListUpdater) loadStoredVersion(manifest AdBlockListManifest) (*AdBlockList, error) {
	list := &AdBlockList{Manifest: manifest}
	for _, feed := range manifest.Feeds {
		path := filepath.Join(u.cfg.StorageDir, manifest.ID, filepath.Base(feed.File))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("easylist cache %s: %w", manifest.ID, err)
		}
		rules, sha := filterLines(data)
		if sha != feed.RulesSHA256 {
			return nil, fmt.Errorf("easylist cache %s/%s: checksum mismatch", manifest.ID, feed.Name)
		}
		set, err := ParseAdBlockRules(strings.NewReader(rules))
		if err != nil {
			return nil, fmt.Errorf("easylist cache %s/%s: %w", manifest.ID, feed.Name, err)
		}
		list.sets = append(list.sets, categorizedRules{category: feed.Category, rules: set})
	}
	return list, nil
}

// UpdateIfDue updates when no version is installed or the interval passed.
func (u *EasyListUpdater) UpdateIfDue(ctx context.Context) (*AdBlockListManifest, error) {
	u.mu.Lock()
	current, next := u.current, u.nextCheck
	if next.IsZero() && current != nil {
		next = current.DownloadedAt.Add(u.cfg.UpdateInterval)
	}
	u.mu.Unlock()
	if u.now().Before(next) {
		return current, nil
	}
	manifest, err := u.Update(ctx)
	delay := u.cfg.UpdateInterval
	if err != nil {
		delay = easyListRetryInterval
	}
	u.mu.Lock()
	u.nextCheck = u.now().Add(delay)
	u.mu.Unlock()
	return manifest, err
}

// Update downloads and verifies every feed, stores the version and installs
// it into the blocker. On any failure the installed version stays active.
func (u *EasyListUpdater) Update(ctx context.Context) (*AdBlockListManifest, error) {
	if ctx == nil {
		return nil, errors.New("easylist update: context required")
	}
	previous := map[string]int{}
	if current := u.Current(); current != nil {
		for _, feed := range current.Feeds {
			previous[feed.Name] = feed.Bytes
		}
	}
	now := u.now().UTC()
	manifest := AdBlockListManifest{ID: now.Format("20060102T150405Z"), DownloadedAt: now}
	list := &AdBlockList{}
	texts := make(map[string]string, len(u.cfg.Feeds))
	combined := sha256.New()
	for _, feed := range u.cfg.Feeds {
		record, rules, err := u.fetchFeed(ctx, feed, previous[feed.Name])
		if err != nil {
			return nil, err
		}
		set, err := ParseAdBlockRules(strings.NewReader(rules))
		if err != nil {
			return nil, fmt.Errorf("easylist %s: %w", feed.Name, err)
		}
		record.Stats = set.Stats()
		manifest.Rules += record.Stats.Blocking + record.Stats.Exceptions
		manifest.Bytes += record.Bytes
		manifest.Feeds = append(manifest.Feeds, record)
		list.sets = append(list.sets, categorizedRules{category: feed.Category, rules: set})
		texts[feed.Name] = rules
		combined.Write([]byte(feed.Name + ":" + record.RulesSHA256 + "\n"))
	}
	manifest.SHA256 = hex.EncodeToString(combined.Sum(nil))
	if u.blocker.IsRejected(manifest.SHA256) {
		return nil, fmt.Errorf("easylist: version %s was rolled back for breakage", manifest.SHA256)
	}
	if current := u.Current(); current != nil && current.SHA256 == manifest.SHA256 {
		return current, nil
	}
	if err := u.persist(&manifest, texts); err != nil {
		return nil, err
	}
	list.Manifest = manifest
	if err := u.blocker.Install(list); err != nil {
		return nil, err
	}
	u.setCurrent(manifest)
	return &manifest, nil
}

func (u *EasyListUpdater) fetchFeed(ctx context.Context, feed EasyListFeed, previousBytes int) (AdBlockFeedManifest, string, error) {
	primary, err := u.download(ctx, feed.Primary)
	if err != nil {
		return AdBlockFeedManifest{}, "", fmt.Errorf("easylist %s primary: %w", feed.Name, err)
	}
	mirror, err := u.download(ctx, feed.Mirror)
	if err != nil {
		return AdBlockFeedManifest{}, "", fmt.Errorf("easylist %s mirror: %w", feed.Name, err)
	}
	rules, sha := filterLines(primary)
	if _, mirrorSHA := filterLines(mirror); mirrorSHA != sha {
		return AdBlockFeedManifest{}, "", fmt.Errorf("easylist %s: primary and mirror filter checksums differ", feed.Name)
	}
	if previousBytes > 0 && float64(len(rules)) > float64(previousBytes)*u.cfg.MaxSizeMultiplier {
		return AdBlockFeedManifest{}, "", fmt.Errorf("easylist %s: size %d exceeds %.1fx previous %d", feed.Name, len(rules), u.cfg.MaxSizeMultiplier, previousBytes)
	}
	compressed, err := gzipSize(rules)
	if err != nil {
		return AdBlockFeedManifest{}, "", fmt.Errorf("easylist %s: %w", feed.Name, err)
	}
	return AdBlockFeedManifest{
		Name: feed.Name, Category: feed.Category, Primary: feed.Primary, Mirror: feed.Mirror,
		RulesSHA256: sha, Bytes: len(rules), CompressedBytes: compressed, File: feed.Name + ".txt",
	}, rules, nil
}

func (u *EasyListUpdater) download(ctx context.Context, source string) (body []byte, returnErr error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	response, err := u.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil && returnErr == nil {
			body, returnErr = nil, closeErr
		}
	}()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(response.Body, u.cfg.MaxListBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > u.cfg.MaxListBytes {
		return nil, fmt.Errorf("list exceeds %d bytes", u.cfg.MaxListBytes)
	}
	return body, nil
}

// filterLines keeps the filter lines (no comments, headers or blanks) and
// returns them with their SHA-256; mirrors differ only in comment headers.
func filterLines(data []byte) (string, string) {
	var rules strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '!' || line[0] == '[' {
			continue
		}
		rules.WriteString(line)
		rules.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(rules.String()))
	return rules.String(), hex.EncodeToString(sum[:])
}

func gzipSize(text string) (int, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write([]byte(text)); err != nil {
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}
	return buffer.Len(), nil
}

func (u *EasyListUpdater) setCurrent(manifest AdBlockListManifest) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.current = &manifest
}

func (u *EasyListUpdater) persist(manifest *AdBlockListManifest, texts map[string]string) error {
	dir := filepath.Join(u.cfg.StorageDir, manifest.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("easylist store: %w", err)
	}
	for _, feed := range manifest.Feeds {
		if err := writeFileAtomic(filepath.Join(dir, feed.File), []byte(texts[feed.Name])); err != nil {
			return fmt.Errorf("easylist store: %w", err)
		}
	}
	store, err := u.readStore()
	if err != nil {
		return err
	}
	store.Versions = append(store.Versions, *manifest)
	sort.SliceStable(store.Versions, func(i, j int) bool { return store.Versions[i].DownloadedAt.Before(store.Versions[j].DownloadedAt) })
	for len(store.Versions) > u.cfg.MaxVersions {
		oldest := store.Versions[0]
		if err := os.RemoveAll(filepath.Join(u.cfg.StorageDir, filepath.Base(oldest.ID))); err != nil {
			return fmt.Errorf("easylist store: prune %s: %w", oldest.ID, err)
		}
		store.Versions = store.Versions[1:]
	}
	encoded, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("easylist store: %w", err)
	}
	return writeFileAtomic(filepath.Join(u.cfg.StorageDir, "manifest.json"), encoded)
}

func (u *EasyListUpdater) readStore() (easyListStore, error) {
	var store easyListStore
	data, err := os.ReadFile(filepath.Join(u.cfg.StorageDir, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return store, fmt.Errorf("easylist store: %w", err)
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return store, fmt.Errorf("easylist store: manifest: %w", err)
	}
	return store, nil
}

// Run loads the cached version, then updates when due and advances the
// rollout every minute until ctx ends. Update failures keep the cached
// version and are logged; they never stop the loop.
func (u *EasyListUpdater) Run(ctx context.Context, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	if err := u.LoadCached(); err != nil {
		logger.Warn("easylist cache unusable", slog.String("error", err.Error()))
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if _, err := u.UpdateIfDue(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("easylist update failed; keeping cached list", slog.String("error", err.Error()))
		}
		u.blocker.Advance()
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func writeFileAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	_, writeErr := temp.Write(data)
	closeErr := temp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return errors.Join(err, os.Remove(name))
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return errors.Join(err, os.Remove(name))
	}
	return os.Rename(name, path)
}
