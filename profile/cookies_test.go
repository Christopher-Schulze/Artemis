package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestCookieStore(t *testing.T) *CookieStore {
	t.Helper()
	return NewCookieStore()
}

func sampleCookie(domain, name string, expires time.Time) *Cookie {
	return &Cookie{
		Domain:    domain,
		Name:      name,
		Value:     "val-" + name,
		Path:      "/",
		ExpiresAt: expires,
		Secure:    true,
		HTTPOnly:  true,
	}
}

func TestCookieStore_AddAndList(t *testing.T) {
	s := newTestCookieStore(t)
	s.Add(sampleCookie("a.de", "c1", time.Time{}))
	s.Add(sampleCookie("a.de", "c2", time.Time{}))
	s.Add(sampleCookie("b.de", "c1", time.Time{}))
	all := s.ListCookies("")
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	aOnly := s.ListCookies("a.de")
	if len(aOnly) != 2 {
		t.Fatalf("expected 2 for a.de, got %d", len(aOnly))
	}
}

func TestCookieStore_AddReplaces(t *testing.T) {
	s := newTestCookieStore(t)
	s.Add(sampleCookie("a.de", "c1", time.Time{}))
	s.Add(&Cookie{Domain: "a.de", Name: "c1", Value: "new", Path: "/"})
	cookies := s.ListCookies("a.de")
	if len(cookies) != 1 {
		t.Fatalf("expected 1 (replaced), got %d", len(cookies))
	}
	if cookies[0].Value != "new" {
		t.Fatalf("value not replaced: %s", cookies[0].Value)
	}
}

func TestCookieStore_ExportImport_Roundtrip(t *testing.T) {
	s := newTestCookieStore(t)
	s.Add(sampleCookie("a.de", "c1", time.Time{}))
	s.Add(sampleCookie("a.de", "c2", time.Time{}))
	data, err := s.ExportCookies("")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	s2 := newTestCookieStore(t)
	if err := s2.ImportCookies(data); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got := s2.ListCookies(""); len(got) != 2 {
		t.Fatalf("expected 2 after import, got %d", len(got))
	}
}

func TestCookieStore_Export_DomainFilter(t *testing.T) {
	s := newTestCookieStore(t)
	s.Add(sampleCookie("a.de", "c1", time.Time{}))
	s.Add(sampleCookie("b.de", "c1", time.Time{}))
	data, err := s.ExportCookies("a.de")
	if err != nil {
		t.Fatal(err)
	}
	var cookies []Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1, got %d", len(cookies))
	}
	if cookies[0].Domain != "a.de" {
		t.Fatalf("domain=%s", cookies[0].Domain)
	}
}

func TestCookieStore_Import_ReplacesDomain(t *testing.T) {
	s := newTestCookieStore(t)
	s.Add(sampleCookie("a.de", "old", time.Time{}))
	s.Add(sampleCookie("b.de", "keep", time.Time{}))
	importData, _ := json.Marshal([]Cookie{
		{Domain: "a.de", Name: "new1", Path: "/"},
		{Domain: "a.de", Name: "new2", Path: "/"},
	})
	if err := s.ImportCookies(importData); err != nil {
		t.Fatal(err)
	}
	aCookies := s.ListCookies("a.de")
	if len(aCookies) != 2 {
		t.Fatalf("expected 2 new a.de cookies, got %d", len(aCookies))
	}
	bCookies := s.ListCookies("b.de")
	if len(bCookies) != 1 {
		t.Fatalf("b.de cookies should be untouched, got %d", len(bCookies))
	}
}

func TestCookieStore_Import_InvalidJSON(t *testing.T) {
	s := newTestCookieStore(t)
	if err := s.ImportCookies([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCookieStore_ClearExpired(t *testing.T) {
	s := newTestCookieStore(t)
	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)
	s.Add(sampleCookie("a.de", "expired", past))
	s.Add(sampleCookie("a.de", "valid", future))
	s.Add(sampleCookie("a.de", "session", time.Time{})) // no expiry
	removed := s.ClearExpired()
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}
	if len(s.ListCookies("")) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(s.ListCookies("")))
	}
}

func TestCookieStore_ClearDomain(t *testing.T) {
	s := newTestCookieStore(t)
	s.Add(sampleCookie("a.de", "c1", time.Time{}))
	s.Add(sampleCookie("a.de", "c2", time.Time{}))
	s.Add(sampleCookie("b.de", "c1", time.Time{}))
	removed := s.ClearDomain("a.de")
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}
	if len(s.ListCookies("")) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(s.ListCookies("")))
	}
}

func TestCookieStore_ClearAll(t *testing.T) {
	s := newTestCookieStore(t)
	s.Add(sampleCookie("a.de", "c1", time.Time{}))
	s.Add(sampleCookie("b.de", "c1", time.Time{}))
	removed := s.ClearAll()
	if removed != 2 {
		t.Fatalf("expected 2, got %d", removed)
	}
	if len(s.ListCookies("")) != 0 {
		t.Fatal("expected empty after ClearAll")
	}
}

func TestCookieStore_ExportImportFile(t *testing.T) {
	s := newTestCookieStore(t)
	s.Add(sampleCookie("a.de", "c1", time.Time{}))
	path := filepath.Join(t.TempDir(), "sub", "cookies.json")
	if err := s.ExportToFile(path, ""); err != nil {
		t.Fatalf("ExportToFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("file too open: %v", info.Mode().Perm())
	}
	s2 := newTestCookieStore(t)
	if err := s2.ImportFromFile(path); err != nil {
		t.Fatalf("ImportFromFile: %v", err)
	}
	if len(s2.ListCookies("")) != 1 {
		t.Fatalf("expected 1 after file import, got %d", len(s2.ListCookies("")))
	}
}

func TestCookieStore_ImportFromFile_NotExist(t *testing.T) {
	s := newTestCookieStore(t)
	if err := s.ImportFromFile("/nonexistent/path/cookies.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}
