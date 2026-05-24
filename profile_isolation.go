package artemis

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// ProfileSession binds a browser profile to an isolated stealth seed.
type ProfileSession struct {
	ProfileID string
	Seed      uint64
}

// ProfileRegistry keeps isolated Artemis browser profiles (spec L4551).
type ProfileRegistry struct {
	sessions map[string]ProfileSession
}

// NewProfileRegistry creates a registry.
func NewProfileRegistry() *ProfileRegistry {
	return &ProfileRegistry{sessions: make(map[string]ProfileSession)}
}

// Create allocates a new isolated profile session.
func (r *ProfileRegistry) Create(profileID string) (ProfileSession, error) {
	if profileID == "" {
		return ProfileSession{}, fmt.Errorf("profile_id required")
	}
	if _, ok := r.sessions[profileID]; ok {
		return ProfileSession{}, fmt.Errorf("profile already exists")
	}
	seed := seedFromProfile(profileID)
	sess := ProfileSession{ProfileID: profileID, Seed: seed}
	r.sessions[profileID] = sess
	return sess, nil
}

// AutoLoginSession records a persistent login context reference per profile.
type AutoLoginSession struct {
	ProfileID string
	ContextID string
}

// AutoLoginRegistry stores auto-login sessions without sharing cookies across profiles.
type AutoLoginRegistry struct {
	byProfile map[string]AutoLoginSession
}

// NewAutoLoginRegistry creates an auto-login registry.
func NewAutoLoginRegistry() *AutoLoginRegistry {
	return &AutoLoginRegistry{byProfile: make(map[string]AutoLoginSession)}
}

// Put stores a profile-bound login session.
func (r *AutoLoginRegistry) Put(sess AutoLoginSession) error {
	if sess.ProfileID == "" || sess.ContextID == "" {
		return fmt.Errorf("profile_id and context_id required")
	}
	r.byProfile[sess.ProfileID] = sess
	return nil
}

// Get returns the session for a profile.
func (r *AutoLoginRegistry) Get(profileID string) (AutoLoginSession, bool) {
	s, ok := r.byProfile[profileID]
	return s, ok
}

func seedFromProfile(profileID string) uint64 {
	sum := sha256.Sum256([]byte(profileID))
	return binary.LittleEndian.Uint64(sum[:8])
}
