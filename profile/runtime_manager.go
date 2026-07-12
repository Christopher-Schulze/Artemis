package profile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const runtimeSchemaVersion = 1

type SessionID string
type ProfileID string
type ContextID string
type PageID string

type ProfileClass string

const (
	ProfileEphemeral  ProfileClass = "ephemeral"
	ProfilePersistent ProfileClass = "persistent"
	ProfileAttached   ProfileClass = "attached"
)

type RuntimeSessionState string

const (
	SessionOpening RuntimeSessionState = "opening"
	SessionActive  RuntimeSessionState = "active"
	SessionClosing RuntimeSessionState = "closing"
	SessionClosed  RuntimeSessionState = "closed"
	SessionExpired RuntimeSessionState = "expired"
	SessionCrashed RuntimeSessionState = "crashed"
)

type ResourceLimits struct {
	MaxPages         int           `json:"max_pages"`
	MaxMemoryBytes   int64         `json:"max_memory_bytes"`
	MaxDiskBytes     int64         `json:"max_disk_bytes"`
	MaxDownloadBytes int64         `json:"max_download_bytes"`
	MaxLifetime      time.Duration `json:"max_lifetime"`
}

func DefaultResourceLimits() ResourceLimits {
	return ResourceLimits{MaxPages: 16, MaxMemoryBytes: 2 << 30, MaxDiskBytes: 5 << 30, MaxDownloadBytes: 1 << 30, MaxLifetime: 24 * time.Hour}
}

type ResourceUsage struct {
	Pages         int   `json:"pages"`
	MemoryBytes   int64 `json:"memory_bytes"`
	DiskBytes     int64 `json:"disk_bytes"`
	DownloadBytes int64 `json:"download_bytes"`
}

type RuntimeSession struct {
	ID           SessionID           `json:"id"`
	ProfileID    ProfileID           `json:"profile_id"`
	ContextID    ContextID           `json:"context_id"`
	OwnerUserRef string              `json:"owner_user_ref"`
	Class        ProfileClass        `json:"class"`
	State        RuntimeSessionState `json:"state"`
	DataDir      string              `json:"data_dir"`
	CreatedAt    time.Time           `json:"created_at"`
	ExpiresAt    time.Time           `json:"expires_at"`
	ClosedAt     time.Time           `json:"closed_at,omitempty"`
	Usage        ResourceUsage       `json:"usage"`
	Limits       ResourceLimits      `json:"limits"`
	Pages        map[PageID]string   `json:"pages"`
	Recovered    bool                `json:"recovered"`
	CrashMarker  bool                `json:"crash_marker"`
}

type runtimeManifest struct {
	Version  int                           `json:"version"`
	Sessions map[SessionID]*RuntimeSession `json:"sessions"`
}

type RuntimeManager struct {
	mu       sync.RWMutex
	root     string
	manifest string
	sessions map[SessionID]*RuntimeSession
	locks    map[ProfileID]*os.File
	now      func() time.Time
}

type OpenSessionRequest struct {
	ProfileID    ProfileID
	OwnerUserRef string
	Class        ProfileClass
	DataDir      string
	Limits       ResourceLimits
	Lifetime     time.Duration
}

type ProfileArchive struct {
	Version      int          `json:"version"`
	ProfileID    ProfileID    `json:"profile_id"`
	OwnerUserRef string       `json:"owner_user_ref"`
	ExportedAt   time.Time    `json:"exported_at"`
	StorageState StorageState `json:"storage_state"`
}

type RuntimeFailure string

const (
	FailureInvalid  RuntimeFailure = "invalid"
	FailureDenied   RuntimeFailure = "denied"
	FailureNotFound RuntimeFailure = "not_found"
	FailureConflict RuntimeFailure = "conflict"
	FailureLimit    RuntimeFailure = "resource_limit"
	FailureCorrupt  RuntimeFailure = "corrupt_state"
)

type RuntimeError struct {
	Class RuntimeFailure
	Op    string
	Msg   string
}

func (e *RuntimeError) Error() string { return "profile runtime " + e.Op + ": " + e.Msg }

func NewRuntimeManager(root string) (*RuntimeManager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, &RuntimeError{Class: FailureInvalid, Op: "new", Msg: "root required"}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("profile runtime: create root: %w", err)
	}
	m := &RuntimeManager{root: root, manifest: filepath.Join(root, "sessions.json"), sessions: make(map[SessionID]*RuntimeSession), locks: make(map[ProfileID]*os.File), now: time.Now}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *RuntimeManager) Open(ctx context.Context, request OpenSessionRequest) (*RuntimeSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.ProfileID == "" || strings.TrimSpace(request.OwnerUserRef) == "" {
		return nil, &RuntimeError{Class: FailureInvalid, Op: "open", Msg: "profile and owner required"}
	}
	if request.Class != ProfileEphemeral && request.Class != ProfilePersistent && request.Class != ProfileAttached {
		return nil, &RuntimeError{Class: FailureInvalid, Op: "open", Msg: "invalid profile class"}
	}
	limits := normalizeLimits(request.Limits)
	lifetime := request.Lifetime
	if lifetime <= 0 || lifetime > limits.MaxLifetime {
		lifetime = limits.MaxLifetime
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if request.Class == ProfilePersistent {
		if err := m.lockProfile(request.ProfileID); err != nil {
			return nil, err
		}
	}
	id, err := newOpaqueID("ses")
	if err != nil {
		m.unlockProfile(request.ProfileID)
		return nil, err
	}
	contextID, err := newOpaqueID("ctx")
	if err != nil {
		m.unlockProfile(request.ProfileID)
		return nil, err
	}
	dataDir := request.DataDir
	if dataDir == "" {
		base := "persistent"
		if request.Class == ProfileEphemeral {
			base = "ephemeral"
		}
		dataDir = filepath.Join(m.root, base, string(request.ProfileID))
	}
	if err := ensureContained(m.root, dataDir); err != nil {
		m.unlockProfile(request.ProfileID)
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		m.unlockProfile(request.ProfileID)
		return nil, fmt.Errorf("profile runtime open: data dir: %w", err)
	}
	created := m.now().UTC()
	s := &RuntimeSession{ID: SessionID(id), ProfileID: request.ProfileID, ContextID: ContextID(contextID), OwnerUserRef: request.OwnerUserRef, Class: request.Class, State: SessionActive, DataDir: dataDir, CreatedAt: created, ExpiresAt: created.Add(lifetime), Limits: limits, Pages: make(map[PageID]string), CrashMarker: true}
	m.sessions[s.ID] = s
	if err := m.persistLocked(); err != nil {
		delete(m.sessions, s.ID)
		m.unlockProfile(request.ProfileID)
		return nil, err
	}
	return cloneSession(s), nil
}

func (m *RuntimeManager) Get(id SessionID, owner string) (*RuntimeSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, &RuntimeError{Class: FailureNotFound, Op: "get", Msg: "session not found"}
	}
	if s.OwnerUserRef != owner {
		return nil, &RuntimeError{Class: FailureDenied, Op: "get", Msg: "owner mismatch"}
	}
	return cloneSession(s), nil
}

func (m *RuntimeManager) List(owner string) []*RuntimeSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*RuntimeSession, 0)
	for _, session := range m.sessions {
		if session.OwnerUserRef == owner {
			result = append(result, cloneSession(session))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (m *RuntimeManager) ExportProfile(id ProfileID, owner string, cookies *CookieStore, storage *StorageManager) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	found := false
	for _, session := range m.sessions {
		if session.ProfileID != id {
			continue
		}
		if session.OwnerUserRef != owner {
			return nil, &RuntimeError{Class: FailureDenied, Op: "export", Msg: "owner mismatch"}
		}
		found = true
	}
	if !found {
		return nil, &RuntimeError{Class: FailureNotFound, Op: "export", Msg: "profile not found"}
	}
	state, err := ExportStorageState(cookies, storage, nil)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(ProfileArchive{Version: runtimeSchemaVersion, ProfileID: id, OwnerUserRef: owner, ExportedAt: m.now().UTC(), StorageState: *state}, "", "  ")
}

func (m *RuntimeManager) ImportProfile(data []byte, owner string, cookies *CookieStore, storage *StorageManager) (ProfileID, error) {
	var archive ProfileArchive
	if err := json.Unmarshal(data, &archive); err != nil {
		return "", &RuntimeError{Class: FailureCorrupt, Op: "import", Msg: err.Error()}
	}
	if archive.Version != runtimeSchemaVersion || archive.ProfileID == "" || archive.OwnerUserRef != owner {
		return "", &RuntimeError{Class: FailureDenied, Op: "import", Msg: "archive identity or version rejected"}
	}
	if _, err := ImportStorageState(&archive.StorageState, cookies, storage); err != nil {
		return "", err
	}
	return archive.ProfileID, nil
}

func (m *RuntimeManager) ResetProfile(ctx context.Context, id ProfileID, owner string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	dataDir := ""
	for _, session := range m.sessions {
		if session.ProfileID != id {
			continue
		}
		if session.OwnerUserRef != owner {
			return &RuntimeError{Class: FailureDenied, Op: "reset", Msg: "owner mismatch"}
		}
		if session.State == SessionActive {
			return &RuntimeError{Class: FailureConflict, Op: "reset", Msg: "profile has active session"}
		}
		dataDir = session.DataDir
	}
	if dataDir == "" {
		return &RuntimeError{Class: FailureNotFound, Op: "reset", Msg: "profile not found"}
	}
	if err := ensureContained(m.root, dataDir); err != nil {
		return err
	}
	if err := os.RemoveAll(dataDir); err != nil {
		return err
	}
	return os.MkdirAll(dataDir, 0o700)
}

func (m *RuntimeManager) RegisterPage(id SessionID, owner, targetID string) (PageID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.authorizeLocked(id, owner, "register page")
	if err != nil {
		return "", err
	}
	if s.State != SessionActive {
		return "", &RuntimeError{Class: FailureConflict, Op: "register page", Msg: "session is not active"}
	}
	if s.Usage.Pages >= s.Limits.MaxPages {
		return "", &RuntimeError{Class: FailureLimit, Op: "register page", Msg: "page limit reached"}
	}
	value, err := newOpaqueID("pag")
	if err != nil {
		return "", err
	}
	pageID := PageID(value)
	s.Pages[pageID] = targetID
	s.Usage.Pages = len(s.Pages)
	if err := m.persistLocked(); err != nil {
		delete(s.Pages, pageID)
		s.Usage.Pages = len(s.Pages)
		return "", err
	}
	return pageID, nil
}

func (m *RuntimeManager) ResolvePage(sessionID SessionID, pageID PageID, owner string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, err := m.authorizeLocked(sessionID, owner, "resolve page")
	if err != nil {
		return "", err
	}
	target, ok := s.Pages[pageID]
	if !ok {
		return "", &RuntimeError{Class: FailureNotFound, Op: "resolve page", Msg: "page not found in session"}
	}
	return target, nil
}

func (m *RuntimeManager) Account(id SessionID, owner string, usage ResourceUsage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.authorizeLocked(id, owner, "account")
	if err != nil {
		return err
	}
	if usage.Pages > s.Limits.MaxPages || usage.MemoryBytes > s.Limits.MaxMemoryBytes || usage.DiskBytes > s.Limits.MaxDiskBytes || usage.DownloadBytes > s.Limits.MaxDownloadBytes {
		return &RuntimeError{Class: FailureLimit, Op: "account", Msg: "resource limit exceeded"}
	}
	s.Usage = usage
	return m.persistLocked()
}

func (m *RuntimeManager) Close(ctx context.Context, id SessionID, owner string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.authorizeLocked(id, owner, "close")
	if err != nil {
		return err
	}
	s.State = SessionClosing
	s.Pages = make(map[PageID]string)
	s.Usage.Pages = 0
	if s.Class == ProfileEphemeral {
		if err := os.RemoveAll(s.DataDir); err != nil {
			return fmt.Errorf("profile runtime close: remove ephemeral data: %w", err)
		}
	}
	s.State = SessionClosed
	s.ClosedAt = m.now().UTC()
	s.CrashMarker = false
	m.unlockProfile(s.ProfileID)
	return m.persistLocked()
}

func (m *RuntimeManager) Expire(ctx context.Context) ([]SessionID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	var expired []SessionID
	for _, s := range m.sessions {
		if s.State != SessionActive || now.Before(s.ExpiresAt) {
			continue
		}
		s.State = SessionExpired
		s.ClosedAt = now
		s.CrashMarker = false
		s.Pages = make(map[PageID]string)
		s.Usage.Pages = 0
		if s.Class == ProfileEphemeral {
			if err := os.RemoveAll(s.DataDir); err != nil {
				return nil, err
			}
		}
		m.unlockProfile(s.ProfileID)
		expired = append(expired, s.ID)
	}
	sort.Slice(expired, func(i, j int) bool { return expired[i] < expired[j] })
	return expired, m.persistLocked()
}

func (m *RuntimeManager) DeleteProfile(ctx context.Context, profileID ProfileID, owner string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.ProfileID == profileID && s.OwnerUserRef != owner {
			return &RuntimeError{Class: FailureDenied, Op: "delete profile", Msg: "owner mismatch"}
		}
		if s.ProfileID == profileID && s.State == SessionActive {
			return &RuntimeError{Class: FailureConflict, Op: "delete profile", Msg: "profile has active session"}
		}
	}
	path := filepath.Join(m.root, "persistent", string(profileID))
	if err := ensureContained(m.root, path); err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	for id, s := range m.sessions {
		if s.ProfileID == profileID {
			delete(m.sessions, id)
		}
	}
	return m.persistLocked()
}

func (m *RuntimeManager) load() error {
	data, err := os.ReadFile(m.manifest)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("profile runtime load: %w", err)
	}
	var manifest runtimeManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return &RuntimeError{Class: FailureCorrupt, Op: "load", Msg: err.Error()}
	}
	if manifest.Version != runtimeSchemaVersion {
		return &RuntimeError{Class: FailureCorrupt, Op: "load", Msg: fmt.Sprintf("unsupported schema %d", manifest.Version)}
	}
	for id, session := range manifest.Sessions {
		if session == nil || session.ID != id || session.ProfileID == "" || session.OwnerUserRef == "" {
			return &RuntimeError{Class: FailureCorrupt, Op: "load", Msg: "invalid session record"}
		}
		if session.Pages == nil {
			session.Pages = make(map[PageID]string)
		}
		if session.CrashMarker && session.State == SessionActive {
			session.State = SessionCrashed
			session.Recovered = true
			session.Pages = make(map[PageID]string)
			session.Usage.Pages = 0
		}
		m.sessions[id] = session
	}
	return nil
}

func (m *RuntimeManager) persistLocked() error {
	data, err := json.MarshalIndent(runtimeManifest{Version: runtimeSchemaVersion, Sessions: m.sessions}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.root, ".sessions-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, m.manifest)
}

func (m *RuntimeManager) authorizeLocked(id SessionID, owner, op string) (*RuntimeSession, error) {
	s, ok := m.sessions[id]
	if !ok {
		return nil, &RuntimeError{Class: FailureNotFound, Op: op, Msg: "session not found"}
	}
	if owner == "" || s.OwnerUserRef != owner {
		return nil, &RuntimeError{Class: FailureDenied, Op: op, Msg: "owner mismatch"}
	}
	return s, nil
}

func (m *RuntimeManager) lockProfile(id ProfileID) error {
	if _, exists := m.locks[id]; exists {
		return &RuntimeError{Class: FailureConflict, Op: "lock", Msg: "profile already open"}
	}
	lockDir := filepath.Join(m.root, "locks")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(lockDir, string(id)+".lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return &RuntimeError{Class: FailureConflict, Op: "lock", Msg: "profile locked by another process"}
	}
	if err != nil {
		return err
	}
	m.locks[id] = file
	return nil
}

func (m *RuntimeManager) unlockProfile(id ProfileID) {
	file, ok := m.locks[id]
	if !ok {
		return
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	delete(m.locks, id)
}

func normalizeLimits(limits ResourceLimits) ResourceLimits {
	defaults := DefaultResourceLimits()
	if limits.MaxPages <= 0 {
		limits.MaxPages = defaults.MaxPages
	}
	if limits.MaxMemoryBytes <= 0 {
		limits.MaxMemoryBytes = defaults.MaxMemoryBytes
	}
	if limits.MaxDiskBytes <= 0 {
		limits.MaxDiskBytes = defaults.MaxDiskBytes
	}
	if limits.MaxDownloadBytes <= 0 {
		limits.MaxDownloadBytes = defaults.MaxDownloadBytes
	}
	if limits.MaxLifetime <= 0 {
		limits.MaxLifetime = defaults.MaxLifetime
	}
	return limits
}

func newOpaqueID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("profile runtime id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}

func ensureContained(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return &RuntimeError{Class: FailureDenied, Op: "path", Msg: "data directory escapes runtime root"}
	}
	return nil
}

func cloneSession(session *RuntimeSession) *RuntimeSession {
	copy := *session
	copy.Pages = make(map[PageID]string, len(session.Pages))
	for id, target := range session.Pages {
		copy.Pages[id] = target
	}
	return &copy
}
