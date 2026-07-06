// Package profile implements the enterprise browser profile system
// (spec ss28.17): encrypted credential store, multi-profile manager,
// auto-login, session health, cookie + storage management.
package profile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// LoginSelectors are CSS selectors / ARIA refs for auto-login (spec L4553).
type LoginSelectors struct {
	UsernameField string `json:"username_field"`
	PasswordField string `json:"password_field"`
	SubmitButton  string `json:"submit_button"`
	SuccessCheck  string `json:"success_check"`
	MFAField      string `json:"mfa_field"`
}

// StoredCredential is one encrypted credential entry (spec L4553).
type StoredCredential struct {
	ID          string         `json:"id"`
	ProfileName string         `json:"profile_name"`
	Domain      string         `json:"domain"`
	Username    string         `json:"username"`
	Password    []byte         `json:"password"` // AES-256-GCM ciphertext
	Nonce       []byte         `json:"nonce"`
	Selectors   LoginSelectors `json:"selectors"`
	CreatedAt   time.Time      `json:"created_at"`
	LastUsedAt  time.Time      `json:"last_used_at"`
	LastLoginOK bool           `json:"last_login_ok"`
}

// CredentialSummary is the password-less projection for ListCredentials.
type CredentialSummary struct {
	ID          string    `json:"id"`
	ProfileName string    `json:"profile_name"`
	Domain      string    `json:"domain"`
	Username    string    `json:"username"`
	CreatedAt   time.Time `json:"created_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
	LastLoginOK bool      `json:"last_login_ok"`
}

// CredentialStore is the AES-256-GCM encrypted credential store backed by
// a JSON file in the browser data dir.
//
// The master key is 32 bytes (AES-256). In production it is resolved via
// the host secret management; here it is
// injected via NewCredentialStore so the store is testable without a
// Keychain/TPM dependency.
type CredentialStore struct {
	mu      sync.Mutex
	key     []byte
	path    string
	records map[string]*StoredCredential // keyed by ID
}

// NewCredentialStore creates a store at path with the given 32-byte master
// key. The file is created empty if it does not exist.
func NewCredentialStore(path string, key []byte) (*CredentialStore, error) {
	if len(key) != 32 {
		return nil, errors.New("credential store: key must be 32 bytes (AES-256)")
	}
	s := &CredentialStore{
		key:     key,
		path:    path,
		records: make(map[string]*StoredCredential),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// DefaultCredentialPath returns the browser credentials file path.
func DefaultCredentialPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".omnimus", "browser", "credentials.enc")
}

func (s *CredentialStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("credential store: read: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var records []*StoredCredential
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("credential store: parse: %w", err)
	}
	for _, r := range records {
		s.records[r.ID] = r
	}
	return nil
}

func (s *CredentialStore) save() error {
	records := make([]*StoredCredential, 0, len(s.records))
	for _, r := range s.records {
		records = append(records, r)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("credential store: marshal: %w", err)
	}
	dir := filepath.Dir(s.path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("credential store: mkdir: %w", err)
		}
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("credential store: write: %w", err)
	}
	return nil
}

// encryptPassword seals the plaintext password with AES-256-GCM.
func (s *CredentialStore) encryptPassword(plaintext string) ([]byte, []byte, error) {
	if len(plaintext) == 0 {
		return nil, nil, errors.New("credential store: empty password")
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, nil, fmt.Errorf("credential store: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("credential store: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("credential store: nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return ct, nonce, nil
}

// decryptPassword opens an AES-256-GCM ciphertext.
func (s *CredentialStore) decryptPassword(ct, nonce []byte) (string, error) {
	if len(ct) == 0 || len(nonce) == 0 {
		return "", errors.New("credential store: missing ciphertext or nonce")
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("credential store: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("credential store: gcm: %w", err)
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("credential store: decrypt: %w", err)
	}
	return string(pt), nil
}

// StoreCredential encrypts and persists a new credential, returning its ID
// (spec L4553 API).
func (s *CredentialStore) StoreCredential(profileName, domain, username, password string, selectors LoginSelectors) (string, error) {
	if strings.TrimSpace(profileName) == "" || strings.TrimSpace(domain) == "" || strings.TrimSpace(username) == "" {
		return "", errors.New("credential store: profile, domain, username required")
	}
	ct, nonce, err := s.encryptPassword(password)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	rec := &StoredCredential{
		ID:          id,
		ProfileName: profileName,
		Domain:      domain,
		Username:    username,
		Password:    ct,
		Nonce:       nonce,
		Selectors:   selectors,
		CreatedAt:   now,
		LastUsedAt:  now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[id] = rec
	if err := s.save(); err != nil {
		delete(s.records, id)
		return "", err
	}
	return id, nil
}

// GetCredential returns the decrypted credential for (profile, domain)
// (spec L4553 API).
func (s *CredentialStore) GetCredential(profileName, domain string) (*StoredCredential, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		if r.ProfileName == profileName && r.Domain == domain {
			pw, err := s.decryptPassword(r.Password, r.Nonce)
			if err != nil {
				return nil, "", err
			}
			return r, pw, nil
		}
	}
	return nil, "", errors.New("credential store: not found")
}

// ListCredentials returns password-less summaries, optionally filtered by
// profile (spec L4553 API: NO password).
func (s *CredentialStore) ListCredentials(profileName string) []CredentialSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CredentialSummary, 0, len(s.records))
	for _, r := range s.records {
		if profileName != "" && r.ProfileName != profileName {
			continue
		}
		out = append(out, CredentialSummary{
			ID:          r.ID,
			ProfileName: r.ProfileName,
			Domain:      r.Domain,
			Username:    r.Username,
			CreatedAt:   r.CreatedAt,
			LastUsedAt:  r.LastUsedAt,
			LastLoginOK: r.LastLoginOK,
		})
	}
	return out
}

// DeleteCredential removes a credential by ID (spec L4553 API).
func (s *CredentialStore) DeleteCredential(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return errors.New("credential store: not found")
	}
	delete(s.records, id)
	return s.save()
}

// UpdateLastUsed marks a credential as used at now, auto on login success
// (spec L4553 API).
func (s *CredentialStore) UpdateLastUsed(id string, loginOK bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return errors.New("credential store: not found")
	}
	r.LastUsedAt = time.Now().UTC()
	r.LastLoginOK = loginOK
	return s.save()
}

// DecryptPassword exposes the decrypted password for a record (used by
// auto-login; never logged).
func (s *CredentialStore) DecryptPassword(r *StoredCredential) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decryptPassword(r.Password, r.Nonce)
}
