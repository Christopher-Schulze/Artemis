package profile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// failingProvider is a SecretProvider that always errors, used to exercise
// the error path of the encryptor without a real keychain.
type failingProvider struct{}

func (failingProvider) GetKey(ctx context.Context) ([]byte, error) {
	_ = ctx
	return nil, errors.New("provider unavailable")
}

func newTestEncryptor(t *testing.T, enabled bool, key []byte) *AuthEncryptor {
	t.Helper()
	cfg := AuthEncryptConfig{Enabled: enabled}
	if key != nil {
		cfg.KeyProvider = NewKeychainProvider(key)
	}
	e, err := NewAuthEncryptor(cfg)
	if err != nil {
		t.Fatalf("NewAuthEncryptor: %v", err)
	}
	return e
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("test-master-key-32-bytes-long!!"))
	plain := []byte(`{"token":"secret","user":"alice"}`)
	enc, err := e.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc.Algorithm != AlgorithmAES256GCM {
		t.Fatalf("algorithm = %q, want %q", enc.Algorithm, AlgorithmAES256GCM)
	}
	if len(enc.Nonce) == 0 || len(enc.Ciphertext) == 0 {
		t.Fatal("nonce/ciphertext empty")
	}
	if len(enc.Plaintext) != 0 {
		t.Fatal("enabled mode must not leak plaintext")
	}
	dec, err := e.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(dec, plain) {
		t.Fatalf("roundtrip mismatch: got %q want %q", dec, plain)
	}
}

func TestEncryptDisabledPassthrough(t *testing.T) {
	e := newTestEncryptor(t, false, nil)
	plain := []byte("no-crypto")
	enc, err := e.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc.Algorithm != "" {
		t.Fatalf("disabled algorithm = %q, want empty", enc.Algorithm)
	}
	if !bytes.Equal(enc.Plaintext, plain) {
		t.Fatalf("passthrough plaintext mismatch: %q vs %q", enc.Plaintext, plain)
	}
	dec, err := e.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(dec, plain) {
		t.Fatalf("disabled roundtrip mismatch: %q vs %q", dec, plain)
	}
}

func TestEncryptEnabledNoProviderFails(t *testing.T) {
	_, err := NewAuthEncryptor(AuthEncryptConfig{Enabled: true})
	if err == nil {
		t.Fatal("expected error when enabled with no provider")
	}
}

func TestEncryptWrongKeyFails(t *testing.T) {
	enc := newTestEncryptor(t, true, []byte("key-one-32-bytes-padding-pad!!"))
	dec := newTestEncryptor(t, true, []byte("key-two-32-bytes-padding-pad!!"))
	sealed, err := enc.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := dec.Decrypt(sealed); err == nil {
		t.Fatal("decrypt with wrong key must fail")
	}
}

func TestEncryptKeyIDMismatchFails(t *testing.T) {
	enc := newTestEncryptor(t, true, []byte("key-one-32-bytes-padding-pad!!"))
	dec := newTestEncryptor(t, true, []byte("key-two-32-bytes-padding-pad!!"))
	sealed, err := enc.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	_, err = dec.Decrypt(sealed)
	if err == nil {
		t.Fatal("expected key id mismatch error")
	}
}

func TestEncryptEmptyPlaintext(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("k"))
	enc, err := e.Encrypt(nil)
	if err != nil {
		t.Fatalf("Encrypt(nil): %v", err)
	}
	dec, err := e.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if len(dec) != 0 {
		t.Fatalf("empty roundtrip = %q, want empty", dec)
	}
}

func TestEncryptLargePlaintext(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("k"))
	plain := make([]byte, 1<<20) // 1 MiB
	for i := range plain {
		plain[i] = byte(i % 251)
	}
	enc, err := e.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	dec, err := e.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(dec, plain) {
		t.Fatal("large plaintext roundtrip mismatch")
	}
}

func TestEncryptFailingProvider(t *testing.T) {
	cfg := AuthEncryptConfig{Enabled: true, KeyProvider: failingProvider{}}
	e, err := NewAuthEncryptor(cfg)
	if err != nil {
		t.Fatalf("NewAuthEncryptor: %v", err)
	}
	if _, err := e.Encrypt([]byte("x")); err == nil {
		t.Fatal("expected encrypt error from failing provider")
	}
	// Decrypt also fails on the provider path.
	if _, err := e.Decrypt(EncryptedProfile{
		Nonce:      []byte("n"),
		Ciphertext: []byte("c"),
		Algorithm:  AlgorithmAES256GCM,
		KeyID:      "abcd",
	}); err == nil {
		t.Fatal("expected decrypt error from failing provider")
	}
}

func TestDecryptUnsupportedAlgorithm(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("k"))
	_, err := e.Decrypt(EncryptedProfile{
		Algorithm:  "ROT13",
		Nonce:      []byte("n"),
		Ciphertext: []byte("c"),
	})
	if err == nil {
		t.Fatal("expected unsupported algorithm error")
	}
}

func TestDecryptMissingNonceOrCiphertext(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("k"))
	cases := []EncryptedProfile{
		{Algorithm: AlgorithmAES256GCM, Ciphertext: []byte("c")},
		{Algorithm: AlgorithmAES256GCM, Nonce: []byte("n")},
	}
	for i, c := range cases {
		if _, err := e.Decrypt(c); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestDecryptDisabledEmptyProfileFails(t *testing.T) {
	e := newTestEncryptor(t, false, nil)
	if _, err := e.Decrypt(EncryptedProfile{}); err == nil {
		t.Fatal("expected error for empty disabled profile")
	}
}

func TestEncryptStats(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("k"))
	for i := 0; i < 3; i++ {
		if _, err := e.Encrypt([]byte("x")); err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
	}
	enc, _ := e.Encrypt([]byte("y"))
	if _, err := e.Decrypt(enc); err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	// One decrypt failure to bump Failed.
	if _, err := e.Decrypt(EncryptedProfile{Algorithm: "bad"}); err == nil {
		t.Fatal("expected failure")
	}
	s := e.Stats()
	if s.Encrypted != 4 {
		t.Fatalf("Encrypted = %d, want 4", s.Encrypted)
	}
	if s.Decrypted != 1 {
		t.Fatalf("Decrypted = %d, want 1", s.Decrypted)
	}
	if s.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", s.Failed)
	}
	if s.Total != 6 {
		t.Fatalf("Total = %d, want 6", s.Total)
	}
}

func TestEncryptNonceUniqueness(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("k"))
	seen := make(map[string]bool)
	for i := 0; i < 64; i++ {
		enc, err := e.Encrypt([]byte("same"))
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		key := string(enc.Nonce)
		if seen[key] {
			t.Fatalf("nonce reused at iteration %d", i)
		}
		seen[key] = true
	}
}

func TestEncryptCiphertextDiffersForSamePlaintext(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("k"))
	a, _ := e.Encrypt([]byte("same"))
	b, _ := e.Encrypt([]byte("same"))
	if bytes.Equal(a.Ciphertext, b.Ciphertext) {
		t.Fatal("ciphertexts must differ due to random nonce")
	}
	if bytes.Equal(a.Nonce, b.Nonce) {
		t.Fatal("nonces must differ")
	}
}

func TestEncryptSaltAffectsKeyID(t *testing.T) {
	key := []byte("shared-key")
	cfgA := AuthEncryptConfig{Enabled: true, KeyProvider: NewKeychainProvider(key), Salt: []byte("saltA")}
	cfgB := AuthEncryptConfig{Enabled: true, KeyProvider: NewKeychainProvider(key), Salt: []byte("saltB")}
	eA, _ := NewAuthEncryptor(cfgA)
	eB, _ := NewAuthEncryptor(cfgB)
	encA, err := eA.Encrypt([]byte("x"))
	if err != nil {
		t.Fatalf("eA.Encrypt: %v", err)
	}
	if _, err := eB.Decrypt(encA); err == nil {
		t.Fatal("decrypt with different salt must fail")
	}
	if encA.KeyID == "" {
		t.Fatal("key id empty")
	}
}

func TestEncryptConcurrent(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("k"))
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				plain := []byte(fmt.Sprintf("g%d-i%d", seed, i))
				enc, err := e.Encrypt(plain)
				if err != nil {
					t.Errorf("Encrypt: %v", err)
					return
				}
				dec, err := e.Decrypt(enc)
				if err != nil {
					t.Errorf("Decrypt: %v", err)
					return
				}
				if !bytes.Equal(dec, plain) {
					t.Errorf("roundtrip mismatch")
				}
			}
		}(g)
	}
	wg.Wait()
	s := e.Stats()
	if s.Total != 800 {
		t.Fatalf("Total = %d, want 800", s.Total)
	}
}

func TestKeychainProviderNilSafe(t *testing.T) {
	var p *KeychainProvider
	if _, err := p.GetKey(context.Background()); err == nil {
		t.Fatal("nil provider must error")
	}
}

func TestKeychainProviderEmptyKeyFails(t *testing.T) {
	p := &KeychainProvider{}
	if _, err := p.GetKey(context.Background()); err == nil {
		t.Fatal("empty key must error")
	}
}

func TestKeychainProviderKeyIDStable(t *testing.T) {
	p1 := NewKeychainProvider([]byte("abc"))
	p2 := NewKeychainProvider([]byte("abc"))
	if p1.KeyID() != p2.KeyID() {
		t.Fatal("same key must yield same key id")
	}
	p3 := NewKeychainProvider([]byte("xyz"))
	if p1.KeyID() == p3.KeyID() {
		t.Fatal("different keys must yield different key ids")
	}
}

func TestEncryptTamperedCiphertextFails(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("k"))
	enc, err := e.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	enc.Ciphertext[0] ^= 0xff
	if _, err := e.Decrypt(enc); err == nil {
		t.Fatal("tampered ciphertext must fail GCM auth")
	}
}

func TestEncryptTamperedNonceFails(t *testing.T) {
	e := newTestEncryptor(t, true, []byte("k"))
	enc, err := e.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	enc.Nonce[0] ^= 0xff
	if _, err := e.Decrypt(enc); err == nil {
		t.Fatal("tampered nonce must fail GCM auth")
	}
}

func TestDefaultAuthEncryptConfig(t *testing.T) {
	c := DefaultAuthEncryptConfig()
	if c.Enabled {
		t.Fatal("default config must be disabled")
	}
	if c.KeyProvider != nil {
		t.Fatal("default config must have no provider")
	}
}
