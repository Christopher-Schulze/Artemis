// Package profile: identity.go implements Profile-Scoped Persistent
// Identity (spec L4282): deterministic UUID5 generation so that the same
// logical profile name always maps to the same stable UserID across
// sessions and processes, with a pluggable Persistence layer for
// durability.
//
// UUID5 is a name-based UUID built from a namespace UUID + name via
// SHA-1 (RFC 4122 §4.3). We use github.com/google/uuid for the canonical
// implementation; GenerateUUID5 is exposed for callers that need to
// derive deterministic IDs outside an IdentityManager.
package profile

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DefaultNamespace is the fixed namespace UUID used by IdentityManager
// when no explicit namespace is supplied. It is a v4 UUID generated once
// and pinned here so UserIDs are stable across releases.
var DefaultNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8") // DNS namespace per RFC 4122

// ProfileIdentity is the persistent, profile-scoped identity record
// (spec L4282). UserID is a deterministic UUID5 of (namespace, name).
type ProfileIdentity struct {
	UserID      uuid.UUID `json:"user_id"`
	Namespace   uuid.UUID `json:"namespace"`
	ProfileName string    `json:"profile_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// GenerateUUID5 returns the deterministic UUID5 for (namespace, name)
// per RFC 4122 §4.3: SHA-1 over namespace.Bytes()||name, with version
// and variant bits set. This mirrors github.com/google/uuid's
// uuid.NewSHA1 but is exported here as a stable, documented surface for
// the identity subsystem.
func GenerateUUID5(namespace uuid.UUID, name string) uuid.UUID {
	h := sha1.New()
	h.Write(namespace[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)

	var id uuid.UUID
	copy(id[:], sum[:16])

	// Version 5 (name-based with SHA-1).
	id[6] = (id[6] & 0x0f) | 0x50
	// Variant RFC 4122.
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}

// Persistence durably stores and retrieves ProfileIdentity records.
// Implementations may back this with a file, a DB, or an in-memory map
// for tests.
type Persistence interface {
	Save(identity ProfileIdentity) error
	Load(profileName string) (ProfileIdentity, error)
}

// InMemoryPersistence is a non-durable Persistence backed by a map,
// intended for tests and ephemeral sessions. It is safe for concurrent
// use.
type InMemoryPersistence struct {
	mu    sync.RWMutex
	store map[string]ProfileIdentity
}

// NewInMemoryPersistence returns an empty in-memory persistence.
func NewInMemoryPersistence() *InMemoryPersistence {
	return &InMemoryPersistence{store: make(map[string]ProfileIdentity)}
}

// Save stores identity keyed by its ProfileName, overwriting any prior.
func (p *InMemoryPersistence) Save(identity ProfileIdentity) error {
	if identity.ProfileName == "" {
		return errors.New("identity persistence: empty profile name")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// uuid.UUID is an array value, so the struct copy above already
	// captures UserID; we store the value as-is to avoid external
	// mutation after save.
	p.store[identity.ProfileName] = identity
	return nil
}

// Load returns the stored identity for profileName, or an error if none.
func (p *InMemoryPersistence) Load(profileName string) (ProfileIdentity, error) {
	if profileName == "" {
		return ProfileIdentity{}, errors.New("identity persistence: empty profile name")
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	id, ok := p.store[profileName]
	if !ok {
		return ProfileIdentity{}, fmt.Errorf("identity persistence: not found: %q", profileName)
	}
	return id, nil
}

// IdentityManager creates and persists deterministic ProfileIdentity
// records (spec L4282). Same profileName always yields the same UserID
// for a given namespace. It is safe for concurrent use.
type IdentityManager struct {
	mu        sync.RWMutex
	namespace uuid.UUID
	persist   Persistence
	cache     map[string]ProfileIdentity
}

// NewIdentityManager builds a manager using the given namespace and
// persistence. If persist is nil, identities are kept in-process only
// (still deterministic across calls within the manager lifetime).
func NewIdentityManager(namespace uuid.UUID, persist Persistence) *IdentityManager {
	if namespace == uuid.Nil {
		namespace = DefaultNamespace
	}
	return &IdentityManager{
		namespace: namespace,
		persist:   persist,
		cache:     make(map[string]ProfileIdentity),
	}
}

// GetOrCreateIdentity returns the identity for profileName, creating and
// persisting it on first access. Deterministic: same (namespace, name)
// always produces the same UserID.
func (m *IdentityManager) GetOrCreateIdentity(profileName string) ProfileIdentity {
	if profileName == "" {
		return ProfileIdentity{}
	}

	// Fast path: cached.
	m.mu.RLock()
	if id, ok := m.cache[profileName]; ok {
		m.mu.RUnlock()
		return id
	}
	m.mu.RUnlock()

	// Try persistence before generating a fresh one, so a prior process
	// session's identity is preserved.
	if m.persist != nil {
		if id, err := m.persist.Load(profileName); err == nil {
			m.mu.Lock()
			m.cache[profileName] = id
			m.mu.Unlock()
			return id
		}
	}

	id := ProfileIdentity{
		UserID:      GenerateUUID5(m.namespace, profileName),
		Namespace:   m.namespace,
		ProfileName: profileName,
		CreatedAt:   time.Now().UTC(),
	}

	m.mu.Lock()
	m.cache[profileName] = id
	m.mu.Unlock()

	if m.persist != nil {
		// Persist best-effort; the in-memory cache is already authoritative
		// for this process. A save failure is logged but does not fail the
		// call, matching the spec's "get or create" semantics. Callers
		// needing strict durability should check Persist via a dedicated
		// method.
		if err := m.persist.Save(id); err != nil {
			slog.Warn("identity: failed to persist identity", "profile", profileName, "err", err)
		}
	}
	return id
}

// Lookup returns a previously created identity without creating one.
// Returns an error if no identity exists for profileName.
func (m *IdentityManager) Lookup(profileName string) (ProfileIdentity, error) {
	if profileName == "" {
		return ProfileIdentity{}, errors.New("identity manager: empty profile name")
	}
	m.mu.RLock()
	if id, ok := m.cache[profileName]; ok {
		m.mu.RUnlock()
		return id, nil
	}
	m.mu.RUnlock()
	if m.persist != nil {
		id, err := m.persist.Load(profileName)
		if err != nil {
			return ProfileIdentity{}, fmt.Errorf("identity manager: %w", err)
		}
		m.mu.Lock()
		m.cache[profileName] = id
		m.mu.Unlock()
		return id, nil
	}
	return ProfileIdentity{}, fmt.Errorf("identity manager: not found: %q", profileName)
}

// Namespace returns the namespace UUID used by this manager.
func (m *IdentityManager) Namespace() uuid.UUID {
	return m.namespace
}
