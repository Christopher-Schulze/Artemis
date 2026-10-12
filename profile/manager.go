package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var profileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
var ownerRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@-]{0,127}$`)

// ShareScope controls profile sharing (spec L4585).
type ShareScope string

const (
	SharePrivate         ShareScope = "private"
	ShareOperatorShared  ShareScope = "operator_shared"
	ShareWorkspaceShared ShareScope = "workspace_shared"
)

// BrowserProfile is one enterprise browser profile (spec L4583).
type BrowserProfile struct {
	ID                  ProfileID  `json:"id"`
	Name                string     `json:"name"`
	DisplayName         string     `json:"display_name"`
	Color               string     `json:"color"` // hex for UI
	OwnerUserRef        string     `json:"owner_user_ref"`
	ShareScope          ShareScope `json:"share_scope"`
	SharedWithUserRefs  []string   `json:"shared_with_user_refs"`
	DataDir             string     `json:"data_dir"`
	Domains             []string   `json:"domains"`
	Credentials         []string   `json:"credentials"`   // credential IDs
	StealthLevel        string     `json:"stealth_level"` // default/stealth/paranoid
	ConsentMode         string     `json:"consent_mode"`  // manual/auto_accept/reject_nonessential
	DefaultPurpose      string     `json:"default_purpose"`
	AllowedDomains      []string   `json:"allowed_domains"`
	StealthAckExpiresAt time.Time  `json:"stealth_ack_expires_at"`
	IsActive            bool       `json:"is_active"`
	LastUsedAt          time.Time  `json:"last_used_at"`
	CreatedAt           time.Time  `json:"created_at"`
}

// AccessDecision is the outcome of BrowserProfileAccessGate (spec L4585).
type AccessDecision struct {
	Allowed bool
	Reason  string
}

// BrowserProfileAccessGate enforces caller permission before profile use
// (spec L4585: private only owner, operator_shared owner+operators,
// workspace_shared listed users).
type BrowserProfileAccessGate struct {
	// IsOperator reports whether the caller user ref is an operator for the
	// owner partition. Real impl resolves via ScopeRef/EffectiveAccessProfile;
	// tests inject a function.
	IsOperator func(userRef string) bool
}

// Check authorizes callerUserRef to use profile.
func (g *BrowserProfileAccessGate) Check(profile *BrowserProfile, callerUserRef string) AccessDecision {
	if profile == nil {
		return AccessDecision{Allowed: false, Reason: "nil_profile"}
	}
	if strings.TrimSpace(callerUserRef) == "" {
		return AccessDecision{Allowed: false, Reason: "empty_caller"}
	}
	switch profile.ShareScope {
	case SharePrivate:
		if profile.OwnerUserRef == callerUserRef {
			return AccessDecision{Allowed: true, Reason: "private_owner"}
		}
		return AccessDecision{Allowed: false, Reason: "private_owner_only"}
	case ShareOperatorShared:
		if profile.OwnerUserRef == callerUserRef {
			return AccessDecision{Allowed: true, Reason: "operator_shared_owner"}
		}
		if g.IsOperator != nil && g.IsOperator(callerUserRef) {
			return AccessDecision{Allowed: true, Reason: "operator_shared_operator"}
		}
		return AccessDecision{Allowed: false, Reason: "operator_shared_not_authorized"}
	case ShareWorkspaceShared:
		if profile.OwnerUserRef == callerUserRef {
			return AccessDecision{Allowed: true, Reason: "workspace_shared_owner"}
		}
		for _, u := range profile.SharedWithUserRefs {
			if u == callerUserRef {
				return AccessDecision{Allowed: true, Reason: "workspace_shared_listed"}
			}
		}
		return AccessDecision{Allowed: false, Reason: "workspace_shared_not_listed"}
	default:
		return AccessDecision{Allowed: false, Reason: "invalid_share_scope"}
	}
}

// ProfileManager is the multi-profile CRUD manager (spec L4583).
type ProfileManager struct {
	mu           sync.Mutex
	profiles     map[string]*BrowserProfile // keyed by Name
	gate         *BrowserProfileAccessGate
	baseDir      string // browser profiles dir
	metadataPath string
	loadErr      error
}

// NewProfileManager creates a manager. baseDir is the profiles root
// (the browser profiles dir). If empty, DefaultProfileBaseDir is used.
func NewProfileManager(baseDir string, gate *BrowserProfileAccessGate) *ProfileManager {
	if baseDir == "" {
		baseDir = DefaultProfileBaseDir()
	}
	if gate == nil {
		gate = &BrowserProfileAccessGate{}
	}
	m := &ProfileManager{
		profiles:     make(map[string]*BrowserProfile),
		gate:         gate,
		baseDir:      baseDir,
		metadataPath: filepath.Join(baseDir, "profiles.json"),
	}
	m.loadErr = m.load()
	return m
}

// DefaultProfileBaseDir returns the browser profiles dir.
func DefaultProfileBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".artemis", "browser", "profiles")
}

// ProfileDataDir returns the per-profile data dir
// browser profiles dir layout: {owner_user_ref}/{name}/
func (m *ProfileManager) ProfileDataDir(ownerUserRef, name string) string {
	if m == nil || m.baseDir == "" {
		return ""
	}
	return filepath.Join(m.baseDir, ownerUserRef, name)
}

// Create adds a new profile with profile isolation (own user-data-dir).
// Returns error on duplicate name or invalid input.
func (m *ProfileManager) Create(p *BrowserProfile) error {
	if m == nil {
		return errors.New("profile manager: nil")
	}
	if p == nil {
		return errors.New("profile manager: nil profile")
	}
	if m.loadErr != nil {
		return m.loadErr
	}
	if !profileNamePattern.MatchString(p.Name) {
		return errors.New("profile manager: name must use lowercase letters, numbers, and hyphens")
	}
	if !ownerRefPattern.MatchString(p.OwnerUserRef) {
		return errors.New("profile manager: invalid owner_user_ref")
	}
	if p.ShareScope == "" {
		p.ShareScope = SharePrivate
	}
	if p.ID == "" {
		value, err := newOpaqueID("pro")
		if err != nil {
			return err
		}
		p.ID = ProfileID(value)
	}
	if p.StealthLevel == "" {
		p.StealthLevel = "default"
	}
	if p.ConsentMode == "" {
		p.ConsentMode = "manual"
	}
	if p.DataDir == "" {
		p.DataDir = m.ProfileDataDir(p.OwnerUserRef, p.Name)
	}
	p.CreatedAt = time.Now().UTC()
	p.LastUsedAt = p.CreatedAt

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.profiles[p.Name]; ok {
		return fmt.Errorf("profile manager: %s already exists", p.Name)
	}
	// Ensure data dir exists for profile isolation.
	if p.DataDir != "" {
		if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
			return fmt.Errorf("profile manager: mkdir data dir: %w", err)
		}
	}
	m.profiles[p.Name] = cloneProfile(p)
	if err := m.persistLocked(); err != nil {
		delete(m.profiles, p.Name)
		return err
	}
	return nil
}

// Get returns a profile by name, after access-gate check.
func (m *ProfileManager) Get(name, callerUserRef string) (*BrowserProfile, error) {
	if m == nil {
		return nil, errors.New("profile manager: nil")
	}
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	m.mu.Lock()
	p, ok := m.profiles[name]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("profile manager: %s not found", name)
	}
	if m.gate != nil {
		dec := m.gate.Check(p, callerUserRef)
		if !dec.Allowed {
			return nil, fmt.Errorf("profile manager: access denied: %s", dec.Reason)
		}
	}
	return cloneProfile(p), nil
}

func (m *ProfileManager) GetByID(id ProfileID, callerUserRef string) (*BrowserProfile, error) {
	if m == nil {
		return nil, errors.New("profile manager: nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	for _, profile := range m.profiles {
		if profile.ID != id {
			continue
		}
		if m.gate != nil {
			decision := m.gate.Check(profile, callerUserRef)
			if !decision.Allowed {
				return nil, fmt.Errorf("profile manager: access denied: %s", decision.Reason)
			}
		}
		return cloneProfile(profile), nil
	}
	return nil, fmt.Errorf("profile manager: id %s not found", id)
}

// List returns all profiles the caller is allowed to see.
func (m *ProfileManager) List(callerUserRef string) []*BrowserProfile {
	if m == nil {
		return nil
	}
	if m.loadErr != nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*BrowserProfile, 0, len(m.profiles))
	for _, p := range m.profiles {
		if m.gate != nil {
			dec := m.gate.Check(p, callerUserRef)
			if !dec.Allowed {
				continue
			}
		}
		out = append(out, cloneProfile(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cloneProfile(profile *BrowserProfile) *BrowserProfile {
	copy := *profile
	copy.SharedWithUserRefs = append([]string(nil), profile.SharedWithUserRefs...)
	copy.Domains = append([]string(nil), profile.Domains...)
	copy.Credentials = append([]string(nil), profile.Credentials...)
	copy.AllowedDomains = append([]string(nil), profile.AllowedDomains...)
	return &copy
}

// Delete removes a profile by name, after access-gate check. Does NOT
// remove the on-disk data dir (caller must explicitly purge).
func (m *ProfileManager) Delete(name, callerUserRef string) error {
	if m == nil {
		return errors.New("profile manager: nil")
	}
	if m.loadErr != nil {
		return m.loadErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profiles[name]
	if !ok {
		return fmt.Errorf("profile manager: %s not found", name)
	}
	if m.gate != nil {
		dec := m.gate.Check(p, callerUserRef)
		if !dec.Allowed {
			return fmt.Errorf("profile manager: access denied: %s", dec.Reason)
		}
	}
	delete(m.profiles, name)
	if err := m.persistLocked(); err != nil {
		m.profiles[name] = p
		return err
	}
	return nil
}

// SwitchProfile activates one profile and deactivates all others owned by
// the same user (spec L4587: user-context switch closes/evicts active
// tabs for the previous user).
func (m *ProfileManager) SwitchProfile(name, callerUserRef string) (*BrowserProfile, error) {
	if m == nil {
		return nil, errors.New("profile manager: nil")
	}
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	target, ok := m.profiles[name]
	if !ok {
		return nil, fmt.Errorf("profile manager: %s not found", name)
	}
	if m.gate != nil {
		dec := m.gate.Check(target, callerUserRef)
		if !dec.Allowed {
			return nil, fmt.Errorf("profile manager: access denied: %s", dec.Reason)
		}
	}
	// Deactivate all profiles owned by the same user.
	previous := make(map[string]bool)
	for _, p := range m.profiles {
		if p.OwnerUserRef == target.OwnerUserRef {
			previous[p.Name] = p.IsActive
			p.IsActive = false
		}
	}
	target.IsActive = true
	target.LastUsedAt = time.Now().UTC()
	if err := m.persistLocked(); err != nil {
		for name, active := range previous {
			m.profiles[name].IsActive = active
		}
		return nil, err
	}
	return cloneProfile(target), nil
}

func (m *ProfileManager) load() error {
	data, err := os.ReadFile(m.metadataPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("profile manager: load metadata: %w", err)
	}
	var profiles []*BrowserProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return fmt.Errorf("profile manager: parse metadata: %w", err)
	}
	migrated := false
	for _, profile := range profiles {
		if profile == nil || profile.Name == "" || profile.OwnerUserRef == "" {
			return errors.New("profile manager: invalid metadata")
		}
		if _, exists := m.profiles[profile.Name]; exists {
			return fmt.Errorf("profile manager: duplicate metadata %s", profile.Name)
		}
		if profile.ID == "" {
			value, err := newOpaqueID("pro")
			if err != nil {
				return err
			}
			profile.ID = ProfileID(value)
			migrated = true
		}
		m.profiles[profile.Name] = profile
	}
	if migrated {
		return m.persistLocked()
	}
	return nil
}

func (m *ProfileManager) persistLocked() (returnErr error) {
	profiles := make([]*BrowserProfile, 0, len(m.profiles))
	for _, profile := range m.profiles {
		copy := *profile
		profiles = append(profiles, &copy)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	if mkdirErr := os.MkdirAll(m.baseDir, 0o700); mkdirErr != nil {
		return mkdirErr
	}
	tmp, err := os.CreateTemp(m.baseDir, ".profiles-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if cleanupErr := removeTemporaryFile(name); cleanupErr != nil {
			returnErr = errors.Join(returnErr, cleanupErr)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return closeTemporaryFile(tmp, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return closeTemporaryFile(tmp, err)
	}
	if err := tmp.Sync(); err != nil {
		return closeTemporaryFile(tmp, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, m.metadataPath); err != nil {
		return err
	}
	return nil
}

// PurgeDataDir removes the on-disk data dir for a profile. Caller must
// have already passed the access gate (e.g. via Get or Delete).
func (m *ProfileManager) PurgeDataDir(p *BrowserProfile) error {
	if m == nil || p == nil || p.DataDir == "" {
		return errors.New("profile manager: missing data dir")
	}
	if err := os.RemoveAll(p.DataDir); err != nil {
		return fmt.Errorf("profile manager: purge: %w", err)
	}
	return nil
}
