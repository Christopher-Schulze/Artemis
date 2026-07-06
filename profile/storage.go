package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// StorageKind enumerates Chromium storage locations (spec L4577).
type StorageKind string

const (
	StorageCookies        StorageKind = "cookies"
	StorageLocalStorage   StorageKind = "local_storage"
	StorageSessionStorage StorageKind = "session_storage"
	StorageIndexedDB      StorageKind = "indexeddb"
	StorageCache          StorageKind = "cache"
)

// LocalStorageEntry is one localStorage key/value (spec L4577).
type LocalStorageEntry struct {
	Domain string `json:"domain"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

// DataLocation is one enumerated storage location for
// RightsProcessor.FindAllData (spec L4577).
type DataLocation struct {
	Kind      StorageKind `json:"kind"`
	Path      string      `json:"path"`
	Domain    string      `json:"domain,omitempty"`
	SizeBytes int64       `json:"size_bytes"`
}

// StorageManager handles localStorage listing, storage size and data
// location enumeration (spec L4577).
type StorageManager struct {
	mu         sync.Mutex
	ls         map[string]*LocalStorageEntry // keyed by domain|key
	profileDir string                        // browser profiles dir: {owner}/{name}/
}

// NewStorageManager creates a manager. profileDir is the per-profile
// Chromium user-data-dir; used for DataLocations enumeration.
func NewStorageManager(profileDir string) *StorageManager {
	return &StorageManager{
		ls:         make(map[string]*LocalStorageEntry),
		profileDir: profileDir,
	}
}

// AddLocalStorage inserts or replaces one localStorage entry.
func (s *StorageManager) AddLocalStorage(e *LocalStorageEntry) {
	if s == nil || e == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ls[lsKey(e)] = e
}

// ListLocalStorage returns all localStorage entries, optionally filtered
// by domain (spec L4577).
func (s *StorageManager) ListLocalStorage(domain string) []LocalStorageEntry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LocalStorageEntry, 0, len(s.ls))
	for _, e := range s.ls {
		if domain != "" && e.Domain != domain {
			continue
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// ClearLocalStorage removes all localStorage entries for a domain
// (spec L4577: Art.17 calls ClearDomain when no retention block exists).
func (s *StorageManager) ClearLocalStorage(domain string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for k, e := range s.ls {
		if e.Domain == domain {
			delete(s.ls, k)
			removed++
		}
	}
	return removed
}

// ClearAllLocalStorage removes all localStorage entries (spec L4577).
func (s *StorageManager) ClearAllLocalStorage() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.ls)
	s.ls = make(map[string]*LocalStorageEntry)
	return n
}

// GetStorageSize returns the total bytes used by all storage locations on
// disk for the profile (spec L4577). Walks the profile dir recursively.
func (s *StorageManager) GetStorageSize() (int64, error) {
	if s == nil {
		return 0, errors.New("storage: nil manager")
	}
	if s.profileDir == "" {
		return 0, errors.New("storage: missing profile dir")
	}
	var total int64
	err := filepath.Walk(s.profileDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("storage: walk: %w", err)
	}
	return total, nil
}

// DataLocations enumerates all storage locations for the profile
// (spec L4577: RightsProcessor.FindAllData). Returns one DataLocation per
// known Chromium storage subdir that exists on disk.
func (s *StorageManager) DataLocations() ([]DataLocation, error) {
	if s == nil {
		return nil, errors.New("storage: nil manager")
	}
	if s.profileDir == "" {
		return nil, errors.New("storage: missing profile dir")
	}
	defaultDir := filepath.Join(s.profileDir, "Default")
	locations := []struct {
		kind StorageKind
		path string
	}{
		{StorageCookies, filepath.Join(defaultDir, "Cookies")},
		{StorageLocalStorage, filepath.Join(defaultDir, "Local Storage")},
		{StorageSessionStorage, filepath.Join(defaultDir, "Session Storage")},
		{StorageIndexedDB, filepath.Join(defaultDir, "IndexedDB")},
		{StorageCache, filepath.Join(defaultDir, "Cache")},
	}
	out := make([]DataLocation, 0, len(locations))
	for _, loc := range locations {
		size, err := dirSize(loc.path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if os.IsNotExist(err) {
			continue
		}
		out = append(out, DataLocation{
			Kind:      loc.kind,
			Path:      loc.path,
			SizeBytes: size,
		})
	}
	return out, nil
}

// PurgeAll removes the entire profile data dir from disk
// (spec L4577: Art.17 profile purge). Caller must have passed the access
// gate. Returns total bytes freed.
func (s *StorageManager) PurgeAll() (int64, error) {
	if s == nil {
		return 0, errors.New("storage: nil manager")
	}
	if s.profileDir == "" {
		return 0, errors.New("storage: missing profile dir")
	}
	size, _ := s.GetStorageSize()
	s.mu.Lock()
	s.ls = make(map[string]*LocalStorageEntry)
	s.mu.Unlock()
	if err := os.RemoveAll(s.profileDir); err != nil {
		return 0, fmt.Errorf("storage: purge: %w", err)
	}
	return size, nil
}

func dirSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	err = filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	return total, err
}

func lsKey(e *LocalStorageEntry) string {
	return strings.ToLower(e.Domain) + "|" + e.Key
}

// now is overridable for tests via package-internal clock; default time.Now.
var now = func() time.Time { return time.Now().UTC() }
