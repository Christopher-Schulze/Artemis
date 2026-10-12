package profile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStorageManager(t *testing.T) *StorageManager {
	t.Helper()
	dir := t.TempDir()
	return NewStorageManager(dir)
}

func TestStorageManager_AddAndListLocalStorage(t *testing.T) {
	s := newTestStorageManager(t)
	s.AddLocalStorage(&LocalStorageEntry{Domain: "a.de", Key: "k1", Value: "v1"})
	s.AddLocalStorage(&LocalStorageEntry{Domain: "a.de", Key: "k2", Value: "v2"})
	s.AddLocalStorage(&LocalStorageEntry{Domain: "b.de", Key: "k1", Value: "v3"})
	all := s.ListLocalStorage("")
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	aOnly := s.ListLocalStorage("a.de")
	if len(aOnly) != 2 {
		t.Fatalf("expected 2 for a.de, got %d", len(aOnly))
	}
	// Sorted by domain then key
	if aOnly[0].Key != "k1" {
		t.Fatalf("expected sorted k1 first, got %s", aOnly[0].Key)
	}
}

func TestStorageManager_AddLocalStorage_Replaces(t *testing.T) {
	s := newTestStorageManager(t)
	s.AddLocalStorage(&LocalStorageEntry{Domain: "a.de", Key: "k1", Value: "old"})
	s.AddLocalStorage(&LocalStorageEntry{Domain: "a.de", Key: "k1", Value: "new"})
	entries := s.ListLocalStorage("a.de")
	if len(entries) != 1 {
		t.Fatalf("expected 1 (replaced), got %d", len(entries))
	}
	if entries[0].Value != "new" {
		t.Fatalf("value not replaced: %s", entries[0].Value)
	}
}

func TestStorageManager_ClearLocalStorage(t *testing.T) {
	s := newTestStorageManager(t)
	s.AddLocalStorage(&LocalStorageEntry{Domain: "a.de", Key: "k1", Value: "v1"})
	s.AddLocalStorage(&LocalStorageEntry{Domain: "a.de", Key: "k2", Value: "v2"})
	s.AddLocalStorage(&LocalStorageEntry{Domain: "b.de", Key: "k1", Value: "v3"})
	removed := s.ClearLocalStorage("a.de")
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}
	if len(s.ListLocalStorage("")) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(s.ListLocalStorage("")))
	}
}

func TestStorageManager_ClearAllLocalStorage(t *testing.T) {
	s := newTestStorageManager(t)
	s.AddLocalStorage(&LocalStorageEntry{Domain: "a.de", Key: "k1", Value: "v1"})
	s.AddLocalStorage(&LocalStorageEntry{Domain: "b.de", Key: "k1", Value: "v2"})
	removed := s.ClearAllLocalStorage()
	if removed != 2 {
		t.Fatalf("expected 2, got %d", removed)
	}
	if len(s.ListLocalStorage("")) != 0 {
		t.Fatal("expected empty after ClearAll")
	}
}

func TestStorageManager_GetStorageSize(t *testing.T) {
	s := newTestStorageManager(t)
	// Write some files into the profile dir
	defaultDir := filepath.Join(s.profileDir, "Default")
	_ = os.MkdirAll(defaultDir, 0o700)
	_ = os.WriteFile(filepath.Join(defaultDir, "Cookies"), []byte("12345678"), 0o600)
	_ = os.WriteFile(filepath.Join(defaultDir, "History"), []byte("abcd"), 0o600)
	size, err := s.GetStorageSize()
	if err != nil {
		t.Fatalf("GetStorageSize: %v", err)
	}
	if size != 12 {
		t.Fatalf("expected 12 bytes, got %d", size)
	}
}

func TestStorageManager_GetStorageSize_MissingDir(t *testing.T) {
	s := NewStorageManager("")
	_, err := s.GetStorageSize()
	if err == nil {
		t.Fatal("expected error for missing profile dir")
	}
}

func TestStorageManager_DataLocations(t *testing.T) {
	s := newTestStorageManager(t)
	defaultDir := filepath.Join(s.profileDir, "Default")
	_ = os.MkdirAll(filepath.Join(defaultDir, "Local Storage"), 0o700)
	_ = os.WriteFile(filepath.Join(defaultDir, "Local Storage", "a.de.localstorage"), []byte("data"), 0o600)
	_ = os.MkdirAll(filepath.Join(defaultDir, "IndexedDB"), 0o700)
	_ = os.WriteFile(filepath.Join(defaultDir, "IndexedDB", "file.idb"), []byte("idb"), 0o600)
	locs, err := s.DataLocations()
	if err != nil {
		t.Fatalf("DataLocations: %v", err)
	}
	kinds := map[StorageKind]bool{}
	for _, l := range locs {
		kinds[l.Kind] = true
		if l.SizeBytes <= 0 {
			t.Fatalf("location %s has 0 size", l.Kind)
		}
	}
	if !kinds[StorageLocalStorage] {
		t.Fatal("Local Storage not enumerated")
	}
	if !kinds[StorageIndexedDB] {
		t.Fatal("IndexedDB not enumerated")
	}
	if kinds[StorageCookies] {
		t.Fatal("Cookies should not exist (not created)")
	}
}

func TestStorageManager_DataLocations_EmptyDir(t *testing.T) {
	s := newTestStorageManager(t)
	locs, err := s.DataLocations()
	if err != nil {
		t.Fatalf("DataLocations: %v", err)
	}
	if len(locs) != 0 {
		t.Fatalf("expected 0 locations for empty profile, got %d", len(locs))
	}
}

func TestStorageManager_PurgeAll(t *testing.T) {
	s := newTestStorageManager(t)
	defaultDir := filepath.Join(s.profileDir, "Default")
	_ = os.MkdirAll(defaultDir, 0o700)
	_ = os.WriteFile(filepath.Join(defaultDir, "Cookies"), []byte("12345678"), 0o600)
	s.AddLocalStorage(&LocalStorageEntry{Domain: "a.de", Key: "k1", Value: "v1"})
	size, err := s.PurgeAll()
	if err != nil {
		t.Fatalf("PurgeAll: %v", err)
	}
	if size != 8 {
		t.Fatalf("expected 8 bytes freed, got %d", size)
	}
	if _, err := os.Stat(s.profileDir); !os.IsNotExist(err) {
		t.Fatalf("profile dir still exists: %v", err)
	}
	if len(s.ListLocalStorage("")) != 0 {
		t.Fatal("localStorage not cleared by PurgeAll")
	}
}

func TestStorageManager_PurgeAll_MissingDir(t *testing.T) {
	s := NewStorageManager("")
	_, err := s.PurgeAll()
	if err == nil {
		t.Fatal("expected error for missing profile dir")
	}
}

func TestStorageKind_Constants(t *testing.T) {
	// Ensure all 5 storage kinds are distinct (spec L4577).
	kinds := []StorageKind{
		StorageCookies,
		StorageLocalStorage,
		StorageSessionStorage,
		StorageIndexedDB,
		StorageCache,
	}
	seen := map[StorageKind]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Fatalf("duplicate storage kind: %s", k)
		}
		seen[k] = true
	}
}

func TestStorageManager_NilSafe(t *testing.T) {
	var s *StorageManager
	if s.ListLocalStorage("") != nil {
		t.Fatal("nil ListLocalStorage should return nil")
	}
	if s.ClearLocalStorage("x") != 0 {
		t.Fatal("nil ClearLocalStorage should return 0")
	}
}

// Ensure now() helper works (compile + runtime check).
func TestNowHelper(t *testing.T) {
	t1 := now()
	time.Sleep(1 * time.Millisecond)
	t2 := now()
	if !t2.After(t1) {
		t.Fatal("now() not advancing")
	}
}
