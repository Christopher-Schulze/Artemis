// Package profile: auth_encrypt.go implements Auth Profile Encryption
// (spec L4260): AES-256-GCM authenticated encryption of sensitive auth
// profile material with a pluggable SecretProvider/keychain abstraction.
//
// The encryptor is intentionally decoupled from any concrete OS keychain:
// production wires a Keychain/TPM-backed SecretProvider, while tests use
// KeychainProvider (an in-memory key store). The crypto core uses the
// standard library crypto/aes + crypto/cipher GCM, never a hand-rolled
// primitive.
package profile

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
)

// AlgorithmAES256GCM is the only supported algorithm identifier.
const AlgorithmAES256GCM = "AES-256-GCM"

// keyIDSize is the number of hex chars used to fingerprint a derived key.
const keyIDSize = 16

// AuthEncryptConfig configures the AuthEncryptor (spec L4260).
type AuthEncryptConfig struct {
	// Enabled gates encryption. When false, Encrypt returns plaintext
	// wrapped in an EncryptedProfile with Algorithm="" and no ciphertext,
	// and Decrypt returns the stored plaintext verbatim. This lets callers
	// run the same code path in dev/test without key material.
	Enabled bool
	// KeyProvider resolves the master key bytes. Required when Enabled.
	KeyProvider SecretProvider
	// Salt is mixed into the key derivation (HKDF-style SHA-256 expand)
	// so that the same provider key yields different per-deployment keys.
	// May be nil; an empty salt still produces a valid derived key.
	Salt []byte
}

// DefaultAuthEncryptConfig returns a disabled config with no provider.
// Callers must set Enabled=true and supply a KeyProvider before use.
func DefaultAuthEncryptConfig() AuthEncryptConfig {
	return AuthEncryptConfig{
		Enabled:     false,
		KeyProvider: nil,
		Salt:        nil,
	}
}

// SecretProvider resolves the raw master key used to derive the AES-256
// key. Implementations may back this with an OS keychain, a TPM, a KMS,
// or an in-memory store for tests.
type SecretProvider interface {
	// GetKey returns the raw key material. Length is not constrained here;
	// the encryptor derives a 32-byte AES-256 key from it via SHA-256
	// (optionally salted), so providers may return arbitrary-length
	// secrets. The returned key must be stable across calls for the same
	// logical key id.
	GetKey(ctx context.Context) ([]byte, error)
}

// KeychainProvider is an in-memory SecretProvider for tests and local
// development. It is NOT a real OS keychain: secrets live in process
// memory and are wiped on process exit. It is safe for concurrent use.
type KeychainProvider struct {
	mu  sync.RWMutex
	key []byte
	id  string
}

// NewKeychainProvider returns a KeychainProvider holding a copy of key.
// The key id is a short hex fingerprint of the derived AES key, used to
// tag EncryptedProfile.KeyID so a decryptor can refuse mismatched keys
// early instead of relying solely on GCM auth failure.
func NewKeychainProvider(key []byte) *KeychainProvider {
	derived := deriveAESKey(key, nil)
	return &KeychainProvider{
		key: append([]byte(nil), key...),
		id:  keyID(derived),
	}
}

// GetKey returns a copy of the stored key.
func (p *KeychainProvider) GetKey(ctx context.Context) ([]byte, error) {
	if p == nil {
		return nil, errors.New("keychain provider: nil provider")
	}
	_ = ctx
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.key) == 0 {
		return nil, errors.New("keychain provider: empty key")
	}
	out := append([]byte(nil), p.key...)
	return out, nil
}

// KeyID returns the fingerprint of the derived AES key for this provider.
func (p *KeychainProvider) KeyID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.id
}

// EncryptedProfile is the sealed payload produced by AuthEncryptor.Encrypt
// and consumed by Decrypt (spec L4260).
type EncryptedProfile struct {
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
	Algorithm  string `json:"algorithm"`
	KeyID      string `json:"key_id"`
	// Plaintext is only populated when encryption is disabled; it lets
	// Decrypt return the original bytes without a provider. Never set in
	// production-enabled mode.
	Plaintext []byte `json:"plaintext,omitempty"`
}

// AuthEncryptStats tracks encryptor throughput for observability.
type AuthEncryptStats struct {
	Total     uint64 // total operations (encrypt + decrypt)
	Encrypted uint64 // successful encrypts
	Decrypted uint64 // successful decrypts
	Failed    uint64 // failed operations (encrypt or decrypt)
}

// AuthEncryptor performs AES-256-GCM encrypt/decrypt of auth profile
// material (spec L4260). It is safe for concurrent use.
type AuthEncryptor struct {
	cfg  AuthEncryptConfig
	mu   sync.RWMutex
	stat AuthEncryptStats
}

// NewAuthEncryptor builds an encryptor from cfg.
func NewAuthEncryptor(cfg AuthEncryptConfig) (*AuthEncryptor, error) {
	if cfg.Enabled && cfg.KeyProvider == nil {
		return nil, errors.New("auth encryptor: enabled but no key provider")
	}
	return &AuthEncryptor{cfg: cfg}, nil
}

// Encrypt seals plaintext under AES-256-GCM. When the encryptor is
// disabled, plaintext is returned verbatim in EncryptedProfile.Plaintext
// with Algorithm="" so Decrypt can round-trip without a key.
func (e *AuthEncryptor) Encrypt(plaintext []byte) (EncryptedProfile, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stat.Total++

	if !e.cfg.Enabled {
		// Pass-through mode: no crypto, no provider needed.
		return EncryptedProfile{
			Algorithm: "",
			Plaintext: append([]byte(nil), plaintext...),
		}, nil
	}

	if e.cfg.KeyProvider == nil {
		e.stat.Failed++
		return EncryptedProfile{}, errors.New("auth encryptor: no key provider")
	}

	key, err := e.cfg.KeyProvider.GetKey(context.Background())
	if err != nil {
		e.stat.Failed++
		return EncryptedProfile{}, fmt.Errorf("auth encryptor: get key: %w", err)
	}

	derived := deriveAESKey(key, e.cfg.Salt)
	block, err := aes.NewCipher(derived)
	if err != nil {
		e.stat.Failed++
		return EncryptedProfile{}, fmt.Errorf("auth encryptor: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		e.stat.Failed++
		return EncryptedProfile{}, fmt.Errorf("auth encryptor: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		e.stat.Failed++
		return EncryptedProfile{}, fmt.Errorf("auth encryptor: nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	e.stat.Encrypted++
	return EncryptedProfile{
		Nonce:      nonce,
		Ciphertext: ct,
		Algorithm:  AlgorithmAES256GCM,
		KeyID:      keyID(derived),
	}, nil
}

// Decrypt opens an EncryptedProfile. It supports both the encrypted
// (AES-256-GCM) and disabled (pass-through) shapes produced by Encrypt.
func (e *AuthEncryptor) Decrypt(p EncryptedProfile) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stat.Total++

	// Pass-through shape from a disabled encryptor.
	if p.Algorithm == "" {
		if p.Plaintext == nil && p.Ciphertext == nil {
			e.stat.Failed++
			return nil, errors.New("auth encryptor: empty disabled profile")
		}
		if p.Plaintext != nil {
			e.stat.Decrypted++
			return append([]byte(nil), p.Plaintext...), nil
		}
		// Disabled mode but ciphertext present (shouldn't happen via
		// Encrypt); treat as plaintext.
		e.stat.Decrypted++
		return append([]byte(nil), p.Ciphertext...), nil
	}

	if p.Algorithm != AlgorithmAES256GCM {
		e.stat.Failed++
		return nil, fmt.Errorf("auth encryptor: unsupported algorithm %q", p.Algorithm)
	}
	if len(p.Nonce) == 0 || len(p.Ciphertext) == 0 {
		e.stat.Failed++
		return nil, errors.New("auth encryptor: missing nonce or ciphertext")
	}
	if e.cfg.KeyProvider == nil {
		e.stat.Failed++
		return nil, errors.New("auth encryptor: no key provider")
	}

	key, err := e.cfg.KeyProvider.GetKey(context.Background())
	if err != nil {
		e.stat.Failed++
		return nil, fmt.Errorf("auth encryptor: get key: %w", err)
	}
	derived := deriveAESKey(key, e.cfg.Salt)

	// Early key-id mismatch detection: avoids a GCM auth failure that
	// leaks less actionable context. Still rely on GCM auth as the
	// security boundary.
	if id := keyID(derived); p.KeyID != "" && id != p.KeyID {
		e.stat.Failed++
		return nil, fmt.Errorf("auth encryptor: key id mismatch (have %s, want %s)", id, p.KeyID)
	}

	block, err := aes.NewCipher(derived)
	if err != nil {
		e.stat.Failed++
		return nil, fmt.Errorf("auth encryptor: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		e.stat.Failed++
		return nil, fmt.Errorf("auth encryptor: gcm: %w", err)
	}
	pt, err := gcm.Open(nil, p.Nonce, p.Ciphertext, nil)
	if err != nil {
		e.stat.Failed++
		return nil, fmt.Errorf("auth encryptor: decrypt: %w", err)
	}
	e.stat.Decrypted++
	return pt, nil
}

// Stats returns a snapshot of the encryptor stats.
func (e *AuthEncryptor) Stats() AuthEncryptStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.stat
}

// deriveAESKey derives a 32-byte AES-256 key from arbitrary-length key
// material and an optional salt using SHA-256. If salt is non-empty, the
// material is hashed twice (material||salt, then that digest||material)
// to fold the salt in without reducing entropy below 256 bits when the
// material is already >=32 bytes of high entropy. This is intentionally
// simple and dependency-free; production callers with low-entropy
// passwords should pre-KDF before injecting into the provider.
func deriveAESKey(material, salt []byte) []byte {
	if len(salt) == 0 {
		h := sha256.Sum256(material)
		return h[:]
	}
	h1 := sha256.New()
	h1.Write(material)
	h1.Write(salt)
	d1 := h1.Sum(nil)
	h2 := sha256.New()
	h2.Write(d1)
	h2.Write(material)
	return h2.Sum(nil)
}

// keyID returns a short hex fingerprint of a derived key for mismatch
// detection. It is NOT secret: it is a truncated SHA-256 of the key.
func keyID(derived []byte) string {
	h := sha256.Sum256(derived)
	return hex.EncodeToString(h[:])[:keyIDSize]
}
