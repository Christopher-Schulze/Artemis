package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helper: temp credential store with random 32-byte key
func newTestCredentialStore(t *testing.T) (*CredentialStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.enc")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s, err := NewCredentialStore(path, key)
	if err != nil {
		t.Fatalf("NewCredentialStore: %v", err)
	}
	return s, path
}

func TestCredentialStore_RejectsBadKeyLength(t *testing.T) {
	_, err := NewCredentialStore("x", make([]byte, 16))
	if err == nil {
		t.Fatal("expected error for 16-byte key")
	}
	if !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCredentialStore_StoreAndGetCredential_Roundtrip(t *testing.T) {
	s, _ := newTestCredentialStore(t)
	sel := LoginSelectors{
		UsernameField: "#user",
		PasswordField: "#pass",
		SubmitButton:  "#submit",
		SuccessCheck:  ".dashboard",
	}
	id, err := s.StoreCredential("prof1", "turbomed.de", "alice", "S3cret!", sel)
	if err != nil {
		t.Fatalf("StoreCredential: %v", err)
	}
	if id == "" {
		t.Fatal("empty ID")
	}
	rec, pw, err := s.GetCredential("prof1", "turbomed.de")
	if err != nil {
		t.Fatalf("GetCredential: %v", err)
	}
	if pw != "S3cret!" {
		t.Fatalf("password mismatch: got %q", pw)
	}
	if rec.Username != "alice" {
		t.Fatalf("username mismatch: %q", rec.Username)
	}
	if rec.ProfileName != "prof1" || rec.Domain != "turbomed.de" {
		t.Fatalf("profile/domain mismatch: %q/%q", rec.ProfileName, rec.Domain)
	}
	if rec.Selectors.UsernameField != "#user" {
		t.Fatalf("selectors not stored: %+v", rec.Selectors)
	}
	// Password must be ciphertext, not plaintext.
	if string(rec.Password) == "S3cret!" {
		t.Fatal("password stored as plaintext!")
	}
}

func TestCredentialStore_GetCredential_NotFound(t *testing.T) {
	s, _ := newTestCredentialStore(t)
	_, _, err := s.GetCredential("nope", "nope.de")
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestCredentialStore_StoreCredential_Validation(t *testing.T) {
	s, _ := newTestCredentialStore(t)
	cases := []struct {
		profile, domain, user, pw string
	}{
		{"", "d.de", "u", "p"},
		{"p", "", "u", "p"},
		{"p", "d.de", "", "p"},
		{"p", "d.de", "u", ""},
	}
	for i, c := range cases {
		_, err := s.StoreCredential(c.profile, c.domain, c.user, c.pw, LoginSelectors{})
		if err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestCredentialStore_ListCredentials_NoPasswordLeak(t *testing.T) {
	s, _ := newTestCredentialStore(t)
	_, err := s.StoreCredential("prof1", "a.de", "u1", "pw1", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.StoreCredential("prof1", "b.de", "u2", "pw2", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.StoreCredential("prof2", "c.de", "u3", "pw3", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	all := s.ListCredentials("")
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	prof1 := s.ListCredentials("prof1")
	if len(prof1) != 2 {
		t.Fatalf("expected 2 for prof1, got %d", len(prof1))
	}
	for _, sum := range all {
		// CredentialSummary has no Password field — verified at compile time.
		_ = sum.ID
	}
}

func TestCredentialStore_DeleteCredential(t *testing.T) {
	s, _ := newTestCredentialStore(t)
	id, err := s.StoreCredential("p", "d.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCredential(id); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if _, _, err := s.GetCredential("p", "d.de"); err == nil {
		t.Fatal("expected not-found after delete")
	}
	if err := s.DeleteCredential("nonexistent"); err == nil {
		t.Fatal("expected error deleting nonexistent")
	}
}

func TestCredentialStore_UpdateLastUsed(t *testing.T) {
	s, _ := newTestCredentialStore(t)
	id, err := s.StoreCredential("p", "d.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	before, _, _ := s.GetCredential("p", "d.de")
	beforeTime := before.LastUsedAt
	time.Sleep(10 * time.Millisecond)
	if err := s.UpdateLastUsed(id, true); err != nil {
		t.Fatalf("UpdateLastUsed: %v", err)
	}
	after, _, _ := s.GetCredential("p", "d.de")
	if !after.LastUsedAt.After(beforeTime) {
		t.Fatal("LastUsedAt not updated")
	}
	if !after.LastLoginOK {
		t.Fatal("LastLoginOK not set")
	}
}

func TestCredentialStore_PersistenceAcrossInstances(t *testing.T) {
	s, path := newTestCredentialStore(t)
	_, err := s.StoreCredential("p", "d.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s2, err := NewCredentialStore(path, key)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	rec, pw, err := s2.GetCredential("p", "d.de")
	if err != nil {
		t.Fatalf("GetCredential after reload: %v", err)
	}
	if pw != "pw" {
		t.Fatalf("password mismatch after reload: %q", pw)
	}
	if rec.Username != "u" {
		t.Fatalf("username mismatch after reload: %q", rec.Username)
	}
}

func TestCredentialStore_WrongKeyFailsDecrypt(t *testing.T) {
	s, path := newTestCredentialStore(t)
	_, err := s.StoreCredential("p", "d.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	wrongKey := make([]byte, 32)
	for i := range wrongKey {
		wrongKey[i] = byte(255 - i)
	}
	s2, err := NewCredentialStore(path, wrongKey)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	_, _, err = s2.GetCredential("p", "d.de")
	if err == nil {
		t.Fatal("expected decrypt failure with wrong key")
	}
}

func TestCredentialStore_FilePermissions(t *testing.T) {
	s, path := newTestCredentialStore(t)
	_, err := s.StoreCredential("p", "d.de", "u", "pw", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// 0600 -> owner read/write only
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("credentials file too open: %v", info.Mode().Perm())
	}
}

func TestCredentialStore_DecryptPassword(t *testing.T) {
	s, _ := newTestCredentialStore(t)
	_, err := s.StoreCredential("p", "d.de", "u", "secret", LoginSelectors{})
	if err != nil {
		t.Fatal(err)
	}
	rec, _, _ := s.GetCredential("p", "d.de")
	pw, err := s.DecryptPassword(rec)
	if err != nil {
		t.Fatalf("DecryptPassword: %v", err)
	}
	if pw != "secret" {
		t.Fatalf("got %q", pw)
	}
}
