// Package download owns policy-checked per-session browser download storage.
package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

const sniffBytes = 512

const downloadCompatibilityTimeout = 30 * time.Second
const downloadCleanupTimeout = 5 * time.Second

func downloadCompatibilityContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), downloadCompatibilityTimeout)
}

func downloadCleanupContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		return context.WithTimeout(context.Background(), downloadCleanupTimeout)
	}
	return context.WithTimeout(context.WithoutCancel(parent), downloadCleanupTimeout)
}

const (
	DefaultMaxDiskBytes = int64(1024 * 1024 * 1024)
	DefaultMinFreeBytes = int64(512 * 1024 * 1024)
)

var downloadSessionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ReclaimPolicy is deliberately dependency-free so the Artemis module can be
// composed with Omnimus' supervisor without importing the Omnimus module.
type ReclaimPolicy string

const ReclaimNone ReclaimPolicy = "none"

// StorageReservationSpec is the cross-process admission request for one
// download publication.
type StorageReservationSpec struct {
	EstimatedBytes     int64
	RollbackBytes      int64
	MinFreeAfter       int64
	AuditHeadroomBytes int64
	ReclaimPolicy      ReclaimPolicy
	AbortCleanupRefs   []string
}

// AdmissionResult returns an exact deficit instead of allowing mid-write
// ENOSPC.
type AdmissionResult struct {
	Admitted    bool
	ByteDeficit int64
	Reason      string
	Runbook     string
}

// StorageReservationAuthority is implemented by the durable Omnimus
// supervisor adapter and may also be used by standalone Artemis hosts.
type StorageReservationAuthority interface {
	Admit(ctx context.Context, reservationID, owner string, spec StorageReservationSpec) (AdmissionResult, error)
	Release(ctx context.Context, reservationID string) error
}

// DownloadConfig defines one session-owned download store.
type DownloadConfig struct {
	RootDir            string
	SessionID          string
	MaxDiskBytes       int64
	MinFreeBytes       int64
	Policy             *network.Policy
	Reservation        StorageReservationAuthority
	RequireReservation bool
}

// Download is the verified metadata for a committed session download.
type Download struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// DownloadManager owns validation, quota, and atomic writes for one session.
type DownloadManager struct {
	mu                 sync.Mutex
	dir                string
	sessionDir         string
	sessionID          string
	maxDiskBytes       int64
	minFreeBytes       int64
	policy             *network.Policy
	reservation        StorageReservationAuthority
	requireReservation bool
}

// BrowserStage isolates one Chromium download attempt from committed files
// and from every concurrent attempt in the same session.
type BrowserStage struct {
	manager *DownloadManager
	dir     string
	close   sync.Once
}

// NewDownloadManager creates the canonical per-session download directory.
func NewDownloadManager(config DownloadConfig) (*DownloadManager, error) {
	if config.Policy == nil {
		return nil, errors.New("download manager: network policy required")
	}
	if config.RequireReservation && config.Reservation == nil {
		return nil, errors.New("download manager: storage reservation authority required")
	}
	if !downloadSessionPattern.MatchString(config.SessionID) || config.SessionID == "." || config.SessionID == ".." {
		return nil, errors.New("download manager: invalid session ID")
	}
	if config.MaxDiskBytes <= 0 {
		if config.MaxDiskBytes < 0 {
			return nil, errors.New("download manager: maximum disk bytes must not be negative")
		}
		config.MaxDiskBytes = DefaultMaxDiskBytes
	}
	if config.MinFreeBytes == 0 {
		config.MinFreeBytes = DefaultMinFreeBytes
	}
	if config.MinFreeBytes < 0 {
		return nil, errors.New("download manager: minimum free bytes must not be negative")
	}
	root, err := canonicalDownloadRoot(config.RootDir)
	if err != nil {
		return nil, err
	}
	sessionDir := filepath.Join(root, config.SessionID)
	if sessionDirErr := ensurePrivateDirectory(sessionDir); sessionDirErr != nil {
		return nil, fmt.Errorf("download manager: create session directory: %w", sessionDirErr)
	}
	dir := filepath.Join(sessionDir, "downloads")
	if downloadDirErr := ensurePrivateDirectory(dir); downloadDirErr != nil {
		return nil, fmt.Errorf("download manager: create download directory: %w", downloadDirErr)
	}
	canonicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("download manager: canonicalize session directory: %w", err)
	}
	if err := ensureDownloadContained(root, canonicalDir); err != nil {
		return nil, err
	}
	return &DownloadManager{
		dir:                canonicalDir,
		sessionDir:         sessionDir,
		sessionID:          config.SessionID,
		maxDiskBytes:       config.MaxDiskBytes,
		minFreeBytes:       config.MinFreeBytes,
		policy:             config.Policy,
		reservation:        config.Reservation,
		requireReservation: config.RequireReservation,
	}, nil
}

// NewBrowserStage creates a private operation directory owned by this session.
func (m *DownloadManager) NewBrowserStage() (*BrowserStage, error) {
	if m == nil {
		return nil, errors.New("download manager unavailable")
	}
	dir, err := os.MkdirTemp(m.sessionDir, ".download-stage-")
	if err != nil {
		return nil, fmt.Errorf("download stage: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		cleanupErr := os.RemoveAll(dir)
		return nil, errors.Join(fmt.Errorf("download stage permissions: %w", err), cleanupErr)
	}
	return &BrowserStage{manager: m, dir: dir}, nil
}

// Directory returns the only path Chromium may use for this attempt.
func (s *BrowserStage) Directory() string { return s.dir }

// Adopt verifies the staged file and atomically links it into committed state.
func (s *BrowserStage) Adopt(filename, declaredType string) (*Download, error) {
	ctx, cancel := downloadCompatibilityContext()
	defer cancel()
	return s.AdoptContext(ctx, filename, declaredType)
}

// AdoptContext verifies and commits one staged browser file under a caller
// deadline, including the shared storage reservation.
func (s *BrowserStage) AdoptContext(ctx context.Context, filename, declaredType string) (*Download, error) {
	if s == nil || s.manager == nil {
		return nil, errors.New("download stage unavailable")
	}
	if ctx == nil {
		return nil, errors.New("download stage context required")
	}
	if filepath.Base(filename) != filename || filename == "." || filename == ".." || strings.TrimSpace(filename) == "" {
		return nil, errors.New("download stage filename invalid")
	}
	source := filepath.Join(s.dir, filename)
	if err := ensureDownloadContained(s.dir, source); err != nil {
		return nil, err
	}
	target, err := s.manager.ResolveTarget(filename)
	if err != nil {
		return nil, err
	}
	s.manager.mu.Lock()
	defer s.manager.mu.Unlock()
	if _, targetStatErr := os.Lstat(target); targetStatErr == nil {
		return nil, errors.New("download target already exists")
	} else if !errors.Is(targetStatErr, os.ErrNotExist) {
		return nil, fmt.Errorf("download target: %w", targetStatErr)
	}
	if linkErr := os.Link(source, target); linkErr != nil {
		return nil, fmt.Errorf("download stage commit: %w", linkErr)
	}
	download, err := s.manager.adoptLocked(ctx, target, declaredType)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(source); err != nil {
		finalizeErr := fmt.Errorf("download stage finalize: %w", err)
		if cleanupErr := removeCommittedDownload(target); cleanupErr != nil {
			finalizeErr = errors.Join(finalizeErr, fmt.Errorf("download stage rollback: %w", cleanupErr))
		}
		return nil, finalizeErr
	}
	if err := syncDownloadDirectory(s.manager.dir); err != nil {
		cleanupErr := removeCommittedDownload(target)
		if cleanupErr != nil {
			return nil, errors.Join(fmt.Errorf("download stage sync: %w", err), fmt.Errorf("download stage rollback: %w", cleanupErr))
		}
		return nil, fmt.Errorf("download stage sync: %w", err)
	}
	return download, nil
}

// Close removes this attempt's partial and completed staging files only.
func (s *BrowserStage) Close() error {
	if s == nil {
		return nil
	}
	var err error
	s.close.Do(func() { err = os.RemoveAll(s.dir) })
	return err
}

// Directory returns the absolute owned directory accepted by Chromium.
func (m *DownloadManager) Directory() string { return m.dir }

// DiskUsage returns committed bytes in the session-owned download directory.
func (m *DownloadManager) DiskUsage() (int64, error) {
	if m == nil {
		return 0, errors.New("download manager unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.diskUsageLocked("")
}

// ResolveTarget accepts a basename or an absolute path inside Directory.
func (m *DownloadManager) ResolveTarget(target string) (string, error) {
	if m == nil {
		return "", errors.New("download manager unavailable")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("download target required")
	}
	if !filepath.IsAbs(target) {
		if filepath.Base(target) != target || target == "." || target == ".." {
			return "", errors.New("download target must be a filename")
		}
		target = filepath.Join(m.dir, target)
	}
	target = filepath.Clean(target)
	if err := ensureDownloadContained(m.dir, target); err != nil {
		return "", err
	}
	if target == m.dir {
		return "", errors.New("download target must be a file")
	}
	return target, nil
}

// Store atomically commits bytes after path, MIME, size, quota, disk and
// shared-reservation checks.
func (m *DownloadManager) Store(filename, declaredType string, content []byte) (download *Download, resultErr error) {
	ctx, cancel := downloadCompatibilityContext()
	defer cancel()
	return m.StoreContext(ctx, filename, declaredType, content)
}

// StoreContext is the cancellation-aware download publication path.
func (m *DownloadManager) StoreContext(ctx context.Context, filename, declaredType string, content []byte) (download *Download, resultErr error) {
	if m == nil {
		return nil, errors.New("download manager unavailable")
	}
	if ctx == nil {
		return nil, errors.New("download context required")
	}
	target, err := m.ResolveTarget(filename)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mimeType := sniffContentType(content, declaredType)
	if validateErr := m.validateLocked(int64(len(content)), mimeType, ""); validateErr != nil {
		return nil, validateErr
	}
	if _, targetStatErr := os.Lstat(target); targetStatErr == nil {
		return nil, errors.New("download target already exists")
	} else if !errors.Is(targetStatErr, os.ErrNotExist) {
		return nil, fmt.Errorf("download target: %w", targetStatErr)
	}
	hashSum := sha256.Sum256(content)
	hashHex := hex.EncodeToString(hashSum[:])
	reservationID, reserved, err := m.admit(ctx, int64(len(content)), target, hashHex)
	if err != nil {
		return nil, err
	}
	if reserved {
		defer func() {
			releaseCtx, releaseCancel := downloadCleanupContext(ctx)
			releaseErr := m.reservation.Release(releaseCtx, reservationID)
			releaseCancel()
			if releaseErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("download reservation release: %w", releaseErr))
			}
		}()
	}
	temporary, err := os.CreateTemp(m.dir, ".artemis-download-*.partial")
	if err != nil {
		return nil, fmt.Errorf("download temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	targetLinked := false
	temporaryClosed := false
	defer func() {
		if !temporaryClosed {
			if closeErr := temporary.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrInvalid) {
				resultErr = errors.Join(resultErr, fmt.Errorf("download temporary close: %w", closeErr))
			}
		}
		if !committed && temporaryPath != "" {
			if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				resultErr = errors.Join(resultErr, fmt.Errorf("download temporary cleanup: %w", removeErr))
			}
		}
		if !committed && targetLinked {
			if err := removeCommittedDownload(target); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("download rollback: %w", err))
			}
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("download temporary permissions: %w", err)
	}
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hash), bytes.NewReader(content)); err != nil {
		return nil, fmt.Errorf("download write: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return nil, fmt.Errorf("download sync: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("download close: %w", err)
	}
	temporaryClosed = true
	if err := os.Link(temporaryPath, target); err != nil {
		return nil, fmt.Errorf("download commit: %w", err)
	}
	targetLinked = true
	if err := os.Remove(temporaryPath); err != nil {
		return nil, fmt.Errorf("download finalize: %w", err)
	}
	temporaryPath = ""
	if err := syncDownloadDirectory(m.dir); err != nil {
		return nil, fmt.Errorf("download directory sync: %w", err)
	}
	committed = true
	return &Download{Path: target, Filename: filepath.Base(target), MIME: mimeType, Size: int64(len(content)), SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

// Adopt verifies a completed browser download already written in Directory.
// Rejected files are removed so policy failures never leave untracked content.
func (m *DownloadManager) Adopt(path, declaredType string) (*Download, error) {
	ctx, cancel := downloadCompatibilityContext()
	defer cancel()
	return m.AdoptContext(ctx, path, declaredType)
}

// AdoptContext verifies a completed browser file and reserves its committed
// size before accepting it into the session store.
func (m *DownloadManager) AdoptContext(ctx context.Context, path, declaredType string) (*Download, error) {
	if m == nil {
		return nil, errors.New("download manager unavailable")
	}
	if ctx == nil {
		return nil, errors.New("download context required")
	}
	target, err := m.ResolveTarget(path)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.adoptLocked(ctx, target, declaredType)
}

func (m *DownloadManager) adoptLocked(ctx context.Context, target, declaredType string) (download *Download, resultErr error) {
	accepted := false
	defer func() {
		if accepted {
			return
		}
		if err := removeCommittedDownload(target); err != nil {
			cleanupErr := fmt.Errorf("download cleanup rejected target: %w", err)
			resultErr = errors.Join(resultErr, cleanupErr)
		}
	}()
	info, err := os.Lstat(target)
	if err != nil {
		return nil, fmt.Errorf("download inspect: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("download target is not a regular file")
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, fmt.Errorf("download open: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("download close: %w", closeErr))
			accepted = false
		}
		if accepted {
			return
		}
		download = nil
		if cleanupErr := removeCommittedDownload(target); cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("download cleanup rejected target: %w", cleanupErr))
		}
	}()
	prefix := make([]byte, sniffBytes)
	read, readErr := io.ReadFull(file, prefix)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
		return nil, fmt.Errorf("download sniff: %w", readErr)
	}
	mimeType := sniffContentType(prefix[:read], declaredType)
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return nil, fmt.Errorf("download rewind: %w", seekErr)
	}
	if validateErr := m.validateLocked(info.Size(), mimeType, target); validateErr != nil {
		return nil, validateErr
	}
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return nil, fmt.Errorf("download hash: %w", err)
	}
	if written != info.Size() {
		return nil, errors.New("download changed while being verified")
	}
	current, err := os.Lstat(target)
	if err != nil || !os.SameFile(info, current) || current.Size() != info.Size() || current.ModTime() != info.ModTime() {
		return nil, errors.New("download changed while being verified")
	}
	hashHex := hex.EncodeToString(hash.Sum(nil))
	reservationID, reserved, err := m.admit(ctx, info.Size(), target, hashHex)
	if err != nil {
		return nil, err
	}
	if reserved {
		defer func() {
			releaseCtx, releaseCancel := downloadCleanupContext(ctx)
			releaseErr := m.reservation.Release(releaseCtx, reservationID)
			releaseCancel()
			if releaseErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("download reservation release: %w", releaseErr))
			}
		}()
	}
	accepted = true
	return &Download{Path: target, Filename: filepath.Base(target), MIME: mimeType, Size: info.Size(), SHA256: hashHex}, nil
}

func (m *DownloadManager) admit(ctx context.Context, size int64, target, hash string) (string, bool, error) {
	if m.reservation == nil {
		if m.requireReservation {
			return "", false, errors.New("download storage reservation authority unavailable")
		}
		return "", false, nil
	}
	estimate := size
	if estimate == 0 {
		estimate = 1
	}
	reservationID := fmt.Sprintf("artemis-download:%s:%s:%s", m.sessionID, filepath.Base(target), hash[:16])
	result, err := m.reservation.Admit(ctx, reservationID, m.sessionID, StorageReservationSpec{
		EstimatedBytes: estimate, RollbackBytes: estimate, MinFreeAfter: m.minFreeBytes,
		ReclaimPolicy: ReclaimNone, AbortCleanupRefs: []string{target},
	})
	if err != nil {
		return reservationID, false, fmt.Errorf("download storage reservation: %w", err)
	}
	if !result.Admitted {
		return reservationID, false, fmt.Errorf("download rejected by shared storage reservation: %s (deficit=%d)", result.Reason, result.ByteDeficit)
	}
	return reservationID, true, nil
}

// ValidatePending rejects an in-progress browser download before completion.
func (m *DownloadManager) ValidatePending(size int64) error {
	if m == nil {
		return errors.New("download manager unavailable")
	}
	if size < 0 {
		return errors.New("download pending size must not be negative")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if size == 0 {
		return nil
	}
	if err := m.policy.ValidateDownloadSize(size, m.sessionID); err != nil {
		return err
	}
	return m.validateCapacityLocked(size, "")
}

func (m *DownloadManager) validateLocked(size int64, mimeType, exclude string) error {
	if err := m.policy.ValidateDownload(mimeType, size, m.sessionID); err != nil {
		return err
	}
	return m.validateCapacityLocked(size, exclude)
}

func (m *DownloadManager) validateCapacityLocked(size int64, exclude string) error {
	usage, err := m.diskUsageLocked(exclude)
	if err != nil {
		return err
	}
	if size > m.maxDiskBytes-usage {
		return errors.New("download session disk quota exceeded")
	}
	free, err := downloadFreeBytes(m.dir)
	if err != nil {
		return err
	}
	if size > free-m.minFreeBytes {
		return errors.New("download rejected: insufficient free disk headroom")
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a private directory")
	}
	return os.Chmod(path, 0o700)
}

func (m *DownloadManager) diskUsageLocked(exclude string) (int64, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return 0, fmt.Errorf("download quota scan: %w", err)
	}
	var total int64
	for _, entry := range entries {
		path := filepath.Join(m.dir, entry.Name())
		if path == exclude {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, fmt.Errorf("download quota inspect: %w", err)
		}
		if info.Mode().IsRegular() {
			total += info.Size()
			continue
		}
		return 0, errors.New("download quota directory contains non-regular entry")
	}
	return total, nil
}

func canonicalDownloadRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("download manager: resolve home: %w", err)
		}
		root = filepath.Join(home, ".omnimus", "tmp", "browser")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("download manager: resolve root: %w", err)
	}
	if mkdirErr := os.MkdirAll(abs, 0o700); mkdirErr != nil {
		return "", fmt.Errorf("download manager: create root: %w", mkdirErr)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("download manager: canonicalize root: %w", err)
	}
	return canonical, nil
}

func ensureDownloadContained(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("download target escapes owned directory")
	}
	return nil
}

func sniffContentType(content []byte, declared string) string {
	if len(content) > 0 {
		return strings.ToLower(strings.TrimSpace(strings.SplitN(http.DetectContentType(content), ";", 2)[0]))
	}
	declared = strings.ToLower(strings.TrimSpace(strings.SplitN(declared, ";", 2)[0]))
	if declared != "" {
		return declared
	}
	return "application/octet-stream"
}

func downloadFreeBytes(path string) (int64, error) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return 0, fmt.Errorf("download free-space check: %w", err)
	}
	return int64(stats.Bavail) * int64(stats.Bsize), nil
}

func syncDownloadDirectory(path string) (returnErr error) {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory: %w", err)
	}
	if err := directory.Sync(); err != nil {
		returnErr = fmt.Errorf("sync directory: %w", err)
	}
	if err := directory.Close(); err != nil {
		returnErr = errors.Join(returnErr, fmt.Errorf("close directory: %w", err))
	}
	return returnErr
}

func removeCommittedDownload(path string) error {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncDownloadDirectory(filepath.Dir(path))
}

// SuggestedFilename derives a safe basename from response metadata.
func SuggestedFilename(rawURL, disposition string) string {
	if _, params, err := mime.ParseMediaType(disposition); err == nil {
		if name := filepath.Base(strings.TrimSpace(params["filename"])); name != "" && name != "." {
			return name
		}
	}
	parsed, err := url.Parse(rawURL)
	if err == nil {
		if name := filepath.Base(parsed.Path); name != "" && name != "." && name != "/" {
			return name
		}
	}
	return "download.bin"
}
