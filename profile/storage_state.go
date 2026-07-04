package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// storage_state.go (spec L4262: Storage State Persistence).
//
// StorageState is the unified JSON container for persisting and restoring a
// browser profile's storage state across sessions. It bundles cookies,
// localStorage, sessionStorage, and indexedDB origins into a single
// exportable/importable document. This is the Go equivalent of Playwright's
// `storageState` and the Vercel Agent-Browser `auth.rs` storage-state
// persistence (spec 28.7: Storage State Persistence).
//
// JSON schema:
//
//	{
//	  "version": 1,
//	  "exported_at": "2026-01-01T00:00:00Z",
//	  "cookies": [
//	    {"domain":"example.com","name":"session","value":"abc","path":"/",
//	     "expires_at":"2026-12-31T00:00:00Z","secure":true,"http_only":true,
//	     "same_site":"Lax"}
//	  ],
//	  "origins": [
//	    {"origin":"https://example.com",
//	     "local_storage":[{"key":"theme","value":"dark"}],
//	     "session_storage":[{"key":"tab","value":"1"}]}
//	  ]
//	}

// StorageStateVersion is the supported schema version for StorageState.
const StorageStateVersion = 1

// StorageState is the unified storage-state container (spec L4262).
type StorageState struct {
	Version    int                  `json:"version"`
	ExportedAt time.Time            `json:"exported_at"`
	Cookies    []Cookie             `json:"cookies"`
	Origins    []StorageStateOrigin `json:"origins"`
}

// StorageStateOrigin bundles all per-origin storage for one origin
// (spec L4262).
type StorageStateOrigin struct {
	Origin         string           `json:"origin"`
	LocalStorage   []LocalStorageKV `json:"local_storage"`
	SessionStorage []LocalStorageKV `json:"session_storage"`
}

// LocalStorageKV is a single key/value pair for localStorage or sessionStorage.
// This is a simplified form of LocalStorageEntry that omits the Domain field
// (the origin already carries that) and is used only inside StorageState.
type LocalStorageKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ExportStorageState builds a StorageState from the cookie store, storage
// manager, and session-scoped sessionStorage entries (spec L4262). The
// returned document is ready to serialize via json.Marshal.
//
// Parameters:
//   - cookies: the CookieStore to export from (nil = no cookies).
//   - storage: the StorageManager to export localStorage from (nil = no LS).
//   - sessionEntries: per-origin sessionStorage entries (nil = no SS).
func ExportStorageState(cookies *CookieStore, storage *StorageManager, sessionEntries []StorageStateOrigin) (*StorageState, error) {
	state := &StorageState{
		Version:    StorageStateVersion,
		ExportedAt: now().UTC(),
	}

	if cookies != nil {
		state.Cookies = cookies.ListCookies("")
	}

	if storage != nil {
		lsEntries := storage.ListLocalStorage("")
		// Group localStorage entries by domain -> origin.
		lsByOrigin := make(map[string][]LocalStorageKV)
		for _, e := range lsEntries {
			origin := domainToOrigin(e.Domain)
			lsByOrigin[origin] = append(lsByOrigin[origin], LocalStorageKV{
				Key:   e.Key,
				Value: e.Value,
			})
		}
		// Merge into sessionEntries origins or create new origins.
		originMap := make(map[string]*StorageStateOrigin)
		for i := range sessionEntries {
			o := &sessionEntries[i]
			originMap[o.Origin] = o
		}
		for origin, kvs := range lsByOrigin {
			if existing, ok := originMap[origin]; ok {
				existing.LocalStorage = append(existing.LocalStorage, kvs...)
			} else {
				newOrigin := StorageStateOrigin{Origin: origin, LocalStorage: kvs}
				originMap[origin] = &newOrigin
			}
		}
		// Collect all origins.
		for _, o := range originMap {
			state.Origins = append(state.Origins, *o)
		}
	} else if len(sessionEntries) > 0 {
		state.Origins = append(state.Origins, sessionEntries...)
	}

	// Sort origins for deterministic output.
	sort.Slice(state.Origins, func(i, j int) bool {
		return state.Origins[i].Origin < state.Origins[j].Origin
	})
	// Sort cookies for deterministic output.
	sort.Slice(state.Cookies, func(i, j int) bool {
		if state.Cookies[i].Domain != state.Cookies[j].Domain {
			return state.Cookies[i].Domain < state.Cookies[j].Domain
		}
		return state.Cookies[i].Name < state.Cookies[j].Name
	})

	return state, nil
}

// ImportStorageState applies a StorageState document to the cookie store and
// storage manager (spec L4262). Cookies are imported via ImportCookies;
// localStorage entries are added via AddLocalStorage; sessionStorage entries
// are returned to the caller (the StorageManager does not track sessionStorage
// directly, so the caller must inject them via CDP).
//
// Returns the sessionStorage entries that the caller must inject separately.
func ImportStorageState(state *StorageState, cookies *CookieStore, storage *StorageManager) ([]LocalStorageKV, error) {
	if state == nil {
		return nil, errors.New("storage state: nil state")
	}
	if state.Version != StorageStateVersion {
		return nil, fmt.Errorf("storage state: unsupported version %d (want %d)", state.Version, StorageStateVersion)
	}

	var allSessionStorage []LocalStorageKV

	if cookies != nil && len(state.Cookies) > 0 {
		data, err := json.Marshal(state.Cookies)
		if err != nil {
			return nil, fmt.Errorf("storage state: marshal cookies: %w", err)
		}
		if err := cookies.ImportCookies(data); err != nil {
			return nil, fmt.Errorf("storage state: import cookies: %w", err)
		}
	}

	if storage != nil {
		for _, origin := range state.Origins {
			domain := originToHost(origin.Origin)
			for _, kv := range origin.LocalStorage {
				storage.AddLocalStorage(&LocalStorageEntry{
					Domain: domain,
					Key:    kv.Key,
					Value:  kv.Value,
				})
			}
			allSessionStorage = append(allSessionStorage, origin.SessionStorage...)
		}
	} else {
		for _, origin := range state.Origins {
			allSessionStorage = append(allSessionStorage, origin.SessionStorage...)
		}
	}

	return allSessionStorage, nil
}

// SerializeStorageState marshals a StorageState to JSON bytes (spec L4262).
func SerializeStorageState(state *StorageState) ([]byte, error) {
	if state == nil {
		return nil, errors.New("storage state: nil state")
	}
	return json.MarshalIndent(state, "", "  ")
}

// DeserializeStorageState unmarshals a StorageState from JSON bytes
// (spec L4262). Validates the schema version.
func DeserializeStorageState(data []byte) (*StorageState, error) {
	var state StorageState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("storage state: parse: %w", err)
	}
	if state.Version != StorageStateVersion {
		return nil, fmt.Errorf("storage state: unsupported version %d (want %d)", state.Version, StorageStateVersion)
	}
	return &state, nil
}

// SaveStorageStateFile writes a StorageState to a JSON file (spec L4262).
// The file is written with 0600 permissions; parent dirs are created with
// 0700 permissions.
func SaveStorageStateFile(state *StorageState, path string) error {
	data, err := SerializeStorageState(state)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("storage state: mkdir: %w", err)
		}
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadStorageStateFile reads a StorageState from a JSON file (spec L4262).
func LoadStorageStateFile(path string) (*StorageState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storage state: read %s: %w", path, err)
	}
	return DeserializeStorageState(data)
}

// domainToOrigin converts a cookie/localStorage domain to a storage origin.
// Cookies use "example.com" form; origins need "https://example.com".
// This is a best-effort conversion: it assumes HTTPS for non-localhost,
// HTTP for localhost. Callers that need precise origin control should
// construct StorageStateOrigin directly.
func domainToOrigin(domain string) string {
	if domain == "" {
		return ""
	}
	if domain == "localhost" || domain == "127.0.0.1" || domain == "::1" {
		return "http://" + domain
	}
	return "https://" + domain
}

// originToHost is the inverse of domainToOrigin: strips the scheme.
func originToHost(origin string) string {
	for _, scheme := range []string{"https://", "http://"} {
		if len(origin) > len(scheme) && origin[:len(scheme)] == scheme {
			return origin[len(scheme):]
		}
	}
	return origin
}
