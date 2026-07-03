package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// SP-artemis-profile-SEC (credentials.go, security_privacy)
// Claim: NewCredentialStore denies non-32-byte keys, encryptPassword denies
// empty passwords, decryptPassword denies missing ciphertext/nonce,
// StoreCredential denies empty profile/domain/username, GetCredential denies
// missing records, DeleteCredential denies unknown IDs
// =============================================================================

func TestWFArtemisProfile_CredentialStoreDeniesInvalidInput(t *testing.T) {
	// Security: credential store must deny invalid keys, empty passwords,
	// missing fields, and unknown records to prevent credential bypass.

	tmpDir := t.TempDir()
	validKey := make([]byte, 32)
	for i := range validKey {
		validKey[i] = byte(i)
	}

	cases := []struct {
		name string
		fn   func() error
	}{
		{
			"new_store_short_key",
			func() error {
				_, err := NewCredentialStore(filepath.Join(tmpDir, "cred1.enc"), []byte("short"))
				return err
			},
		},
		{
			"new_store_long_key",
			func() error {
				_, err := NewCredentialStore(filepath.Join(tmpDir, "cred2.enc"), make([]byte, 64))
				return err
			},
		},
		{
			"new_store_empty_key",
			func() error {
				_, err := NewCredentialStore(filepath.Join(tmpDir, "cred3.enc"), []byte{})
				return err
			},
		},
		{
			"store_empty_profile",
			func() error {
				s, _ := NewCredentialStore(filepath.Join(tmpDir, "cred4.enc"), validKey)
				_, err := s.StoreCredential("", "example.com", "user", "pass", LoginSelectors{})
				return err
			},
		},
		{
			"store_empty_domain",
			func() error {
				s, _ := NewCredentialStore(filepath.Join(tmpDir, "cred5.enc"), validKey)
				_, err := s.StoreCredential("profile1", "", "user", "pass", LoginSelectors{})
				return err
			},
		},
		{
			"store_empty_username",
			func() error {
				s, _ := NewCredentialStore(filepath.Join(tmpDir, "cred6.enc"), validKey)
				_, err := s.StoreCredential("profile1", "example.com", "", "pass", LoginSelectors{})
				return err
			},
		},
		{
			"store_empty_password",
			func() error {
				s, _ := NewCredentialStore(filepath.Join(tmpDir, "cred7.enc"), validKey)
				_, err := s.StoreCredential("profile1", "example.com", "user", "", LoginSelectors{})
				return err
			},
		},
		{
			"store_whitespace_only_profile",
			func() error {
				s, _ := NewCredentialStore(filepath.Join(tmpDir, "cred8.enc"), validKey)
				_, err := s.StoreCredential("   ", "example.com", "user", "pass", LoginSelectors{})
				return err
			},
		},
		{
			"get_missing_credential",
			func() error {
				s, _ := NewCredentialStore(filepath.Join(tmpDir, "cred9.enc"), validKey)
				_, _, err := s.GetCredential("nonexistent", "nowhere.com")
				return err
			},
		},
		{
			"delete_unknown_id",
			func() error {
				s, _ := NewCredentialStore(filepath.Join(tmpDir, "cred10.enc"), validKey)
				return s.DeleteCredential("nonexistent-id")
			},
		},
		{
			"update_last_used_unknown_id",
			func() error {
				s, _ := NewCredentialStore(filepath.Join(tmpDir, "cred11.enc"), validKey)
				return s.UpdateLastUsed("nonexistent-id", true)
			},
		},
	}
	blocked := 0
	for _, c := range cases {
		err := c.fn()
		if err == nil {
			t.Fatalf("%s: expected error, got nil", c.name)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(cases))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all invalid inputs denied), got %.1f", denyRate)
	}

	// Baseline: valid store with valid key succeeds (positive control)
	storePath := filepath.Join(tmpDir, "valid.enc")
	store, err := NewCredentialStore(storePath, validKey)
	if err != nil {
		t.Fatalf("valid NewCredentialStore must succeed, got: %v", err)
	}

	// Baseline: valid StoreCredential succeeds
	id, err := store.StoreCredential("profile1", "example.com", "user1", "secret123", LoginSelectors{})
	if err != nil {
		t.Fatalf("valid StoreCredential must succeed, got: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty credential ID")
	}

	// Baseline: valid GetCredential returns decrypted password
	rec, password, err := store.GetCredential("profile1", "example.com")
	if err != nil {
		t.Fatalf("valid GetCredential must succeed, got: %v", err)
	}
	if password != "secret123" {
		t.Fatalf("expected decrypted password 'secret123', got %s", password)
	}
	if rec.Username != "user1" {
		t.Fatalf("expected username 'user1', got %s", rec.Username)
	}

	// Baseline: ListCredentials returns summaries without passwords
	summaries := store.ListCredentials("profile1")
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Username != "user1" {
		t.Fatalf("expected username 'user1', got %s", summaries[0].Username)
	}

	// Baseline: DeleteCredential succeeds for valid ID
	if err := store.DeleteCredential(id); err != nil {
		t.Fatalf("valid DeleteCredential must succeed, got: %v", err)
	}

	// Baseline: file permissions are 0600
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat store file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected file perm 0600, got %o", info.Mode().Perm())
	}
}
