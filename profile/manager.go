package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ShareScope controls profile sharing (spec L4583).
type ShareScope string

const (
	SharePrivate         ShareScope = "private"
	ShareOperatorShared  ShareScope = "operator_shared"
	ShareWorkspaceShared ShareScope = "workspace_shared"
)

// BrowserProfile is one enterprise browser profile (spec L4583).
type BrowserProfile struct {
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

// AccessDecision is the outcome of BrowserProfileAccessGate (spec L4583).
type AccessDecision struct {
	Allowed bool
	Reason  string
}

// BrowserProfileAccessGate enforces caller permission before profile use
// (spec L4583: private only owner, operator_shared owner+operators,
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
	mu       sync.Mutex
	profiles map[string]*BrowserProfile // keyed by Name
	gate     *BrowserProfileAccessGate
	baseDir  string // browser profiles dir
}

// NewProfileManager creates a manager. baseDir is the profiles root
// (the browser profiles dir). If empty, DefaultProfileBaseDir is used.
func NewProfileManager(baseDir string, gate *BrowserProfileAccessGate) *ProfileManager {
	if baseDir == "" {
		baseDir = DefaultProfileBaseDir()
	}
	return &ProfileManager{
		profiles: make(map[string]*BrowserProfile),
		gate:     gate,
		baseDir:  baseDir,
	}
}

// DefaultProfileBaseDir returns the browser profiles dir.
func DefaultProfileBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".omnimus", "browser", "profiles")
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
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("profile manager: name required")
	}
	if strings.TrimSpace(p.OwnerUserRef) == "" {
		return errors.New("profile manager: owner_user_ref required")
	}
	if p.ShareScope == "" {
		p.ShareScope = SharePrivate
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
	m.profiles[p.Name] = p
	return nil
}

// Get returns a profile by name, after access-gate check.
func (m *ProfileManager) Get(name, callerUserRef string) (*BrowserProfile, error) {
	if m == nil {
		return nil, errors.New("profile manager: nil")
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
	return p, nil
}

// List returns all profiles the caller is allowed to see.
func (m *ProfileManager) List(callerUserRef string) []*BrowserProfile {
	if m == nil {
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
		out = append(out, p)
	}
	return out
}

// Delete removes a profile by name, after access-gate check. Does NOT
// remove the on-disk data dir (caller must explicitly purge).
func (m *ProfileManager) Delete(name, callerUserRef string) error {
	if m == nil {
		return errors.New("profile manager: nil")
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
	return nil
}

// SwitchProfile activates one profile and deactivates all others owned by
// the same user (spec L4583: user-context switch closes/evicts active
// tabs for the previous user).
func (m *ProfileManager) SwitchProfile(name, callerUserRef string) (*BrowserProfile, error) {
	if m == nil {
		return nil, errors.New("profile manager: nil")
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
	for _, p := range m.profiles {
		if p.OwnerUserRef == target.OwnerUserRef {
			p.IsActive = false
		}
	}
	target.IsActive = true
	target.LastUsedAt = time.Now().UTC()
	return target, nil
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
