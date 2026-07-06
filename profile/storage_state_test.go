package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExportStorageStateEmpty(t *testing.T) {
	state, err := ExportStorageState(nil, nil, nil)
	if err != nil {
		t.Fatalf("ExportStorageState: %v", err)
	}
	if state.Version != StorageStateVersion {
		t.Errorf("Version = %d, want %d", state.Version, StorageStateVersion)
	}
	if len(state.Cookies) != 0 {
		t.Errorf("Cookies len = %d, want 0", len(state.Cookies))
	}
	if len(state.Origins) != 0 {
		t.Errorf("Origins len = %d, want 0", len(state.Origins))
	}
}

func TestExportStorageStateWithCookies(t *testing.T) {
	cs := NewCookieStore()
	cs.Add(&Cookie{Domain: "example.com", Name: "session", Value: "abc", Path: "/"})
	cs.Add(&Cookie{Domain: "example.com", Name: "csrf", Value: "xyz", Path: "/"})
	state, err := ExportStorageState(cs, nil, nil)
	if err != nil {
		t.Fatalf("ExportStorageState: %v", err)
	}
	if len(state.Cookies) != 2 {
		t.Fatalf("Cookies len = %d, want 2", len(state.Cookies))
	}
	// Cookies should be sorted by domain then name.
	if state.Cookies[0].Name != "csrf" {
		t.Errorf("Cookies[0].Name = %s, want csrf", state.Cookies[0].Name)
	}
	if state.Cookies[1].Name != "session" {
		t.Errorf("Cookies[1].Name = %s, want session", state.Cookies[1].Name)
	}
}

func TestExportStorageStateWithLocalStorage(t *testing.T) {
	sm := NewStorageManager("/tmp/test")
	sm.AddLocalStorage(&LocalStorageEntry{Domain: "example.com", Key: "theme", Value: "dark"})
	sm.AddLocalStorage(&LocalStorageEntry{Domain: "example.com", Key: "lang", Value: "en"})
	state, err := ExportStorageState(nil, sm, nil)
	if err != nil {
		t.Fatalf("ExportStorageState: %v", err)
	}
	if len(state.Origins) != 1 {
		t.Fatalf("Origins len = %d, want 1", len(state.Origins))
	}
	if state.Origins[0].Origin != "https://example.com" {
		t.Errorf("Origin = %s, want https://example.com", state.Origins[0].Origin)
	}
	if len(state.Origins[0].LocalStorage) != 2 {
		t.Errorf("LocalStorage len = %d, want 2", len(state.Origins[0].LocalStorage))
	}
}

func TestExportStorageStateWithSessionStorage(t *testing.T) {
	sessionEntries := []StorageStateOrigin{
		{
			Origin:         "https://example.com",
			SessionStorage: []LocalStorageKV{{Key: "tab", Value: "1"}},
		},
	}
	state, err := ExportStorageState(nil, nil, sessionEntries)
	if err != nil {
		t.Fatalf("ExportStorageState: %v", err)
	}
	if len(state.Origins) != 1 {
		t.Fatalf("Origins len = %d, want 1", len(state.Origins))
	}
	if len(state.Origins[0].SessionStorage) != 1 {
		t.Errorf("SessionStorage len = %d, want 1", len(state.Origins[0].SessionStorage))
	}
	if state.Origins[0].SessionStorage[0].Key != "tab" {
		t.Errorf("SessionStorage[0].Key = %s, want tab", state.Origins[0].SessionStorage[0].Key)
	}
}

func TestExportStorageStateMergesOrigins(t *testing.T) {
	sm := NewStorageManager("/tmp/test")
	sm.AddLocalStorage(&LocalStorageEntry{Domain: "example.com", Key: "theme", Value: "dark"})
	sessionEntries := []StorageStateOrigin{
		{
			Origin:         "https://example.com",
			SessionStorage: []LocalStorageKV{{Key: "tab", Value: "1"}},
		},
	}
	state, err := ExportStorageState(nil, sm, sessionEntries)
	if err != nil {
		t.Fatalf("ExportStorageState: %v", err)
	}
	if len(state.Origins) != 1 {
		t.Fatalf("Origins len = %d, want 1 (merged)", len(state.Origins))
	}
	if len(state.Origins[0].LocalStorage) != 1 {
		t.Errorf("LocalStorage len = %d, want 1", len(state.Origins[0].LocalStorage))
	}
	if len(state.Origins[0].SessionStorage) != 1 {
		t.Errorf("SessionStorage len = %d, want 1", len(state.Origins[0].SessionStorage))
	}
}

func TestImportStorageStateCookies(t *testing.T) {
	state := &StorageState{
		Version: StorageStateVersion,
		Cookies: []Cookie{
			{Domain: "example.com", Name: "session", Value: "abc", Path: "/"},
		},
	}
	cs := NewCookieStore()
	_, err := ImportStorageState(state, cs, nil)
	if err != nil {
		t.Fatalf("ImportStorageState: %v", err)
	}
	cookies := cs.ListCookies("")
	if len(cookies) != 1 {
		t.Fatalf("cookies len = %d, want 1", len(cookies))
	}
	if cookies[0].Value != "abc" {
		t.Errorf("cookies[0].Value = %s, want abc", cookies[0].Value)
	}
}

func TestImportStorageStateLocalStorage(t *testing.T) {
	state := &StorageState{
		Version: StorageStateVersion,
		Origins: []StorageStateOrigin{
			{
				Origin:       "https://example.com",
				LocalStorage: []LocalStorageKV{{Key: "theme", Value: "dark"}},
			},
		},
	}
	sm := NewStorageManager("/tmp/test")
	_, err := ImportStorageState(state, nil, sm)
	if err != nil {
		t.Fatalf("ImportStorageState: %v", err)
	}
	entries := sm.ListLocalStorage("")
	if len(entries) != 1 {
		t.Fatalf("localStorage entries = %d, want 1", len(entries))
	}
	if entries[0].Value != "dark" {
		t.Errorf("entries[0].Value = %s, want dark", entries[0].Value)
	}
}

func TestImportStorageStateReturnsSessionStorage(t *testing.T) {
	state := &StorageState{
		Version: StorageStateVersion,
		Origins: []StorageStateOrigin{
			{
				Origin:         "https://example.com",
				SessionStorage: []LocalStorageKV{{Key: "tab", Value: "1"}},
			},
		},
	}
	ss, err := ImportStorageState(state, nil, nil)
	if err != nil {
		t.Fatalf("ImportStorageState: %v", err)
	}
	if len(ss) != 1 {
		t.Fatalf("sessionStorage returned = %d, want 1", len(ss))
	}
	if ss[0].Key != "tab" || ss[0].Value != "1" {
		t.Errorf("ss[0] = %+v, want {tab 1}", ss[0])
	}
}

func TestImportStorageStateUnsupportedVersion(t *testing.T) {
	state := &StorageState{Version: 99}
	_, err := ImportStorageState(state, NewCookieStore(), nil)
	if err == nil {
		t.Error("expected error for unsupported version")
	}
}

func TestImportStorageStateNil(t *testing.T) {
	_, err := ImportStorageState(nil, nil, nil)
	if err == nil {
		t.Error("expected error for nil state")
	}
}

func TestSerializeDeserializeStorageState(t *testing.T) {
	original := &StorageState{
		Version:    StorageStateVersion,
		ExportedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Cookies:    []Cookie{{Domain: "example.com", Name: "s", Value: "v", Path: "/"}},
		Origins: []StorageStateOrigin{
			{Origin: "https://example.com", LocalStorage: []LocalStorageKV{{Key: "k", Value: "v"}}},
		},
	}
	data, err := SerializeStorageState(original)
	if err != nil {
		t.Fatalf("SerializeStorageState: %v", err)
	}
	roundtrip, err := DeserializeStorageState(data)
	if err != nil {
		t.Fatalf("DeserializeStorageState: %v", err)
	}
	if roundtrip.Version != original.Version {
		t.Errorf("Version = %d, want %d", roundtrip.Version, original.Version)
	}
	if len(roundtrip.Cookies) != 1 {
		t.Errorf("Cookies len = %d, want 1", len(roundtrip.Cookies))
	}
	if len(roundtrip.Origins) != 1 {
		t.Errorf("Origins len = %d, want 1", len(roundtrip.Origins))
	}
	if roundtrip.Cookies[0].Value != "v" {
		t.Errorf("Cookies[0].Value = %s, want v", roundtrip.Cookies[0].Value)
	}
}

func TestDeserializeStorageStateUnsupportedVersion(t *testing.T) {
	data := []byte(`{"version":99,"exported_at":"2026-01-01T00:00:00Z","cookies":[],"origins":[]}`)
	_, err := DeserializeStorageState(data)
	if err == nil {
		t.Error("expected error for unsupported version")
	}
}

func TestDeserializeStorageStateInvalidJSON(t *testing.T) {
	_, err := DeserializeStorageState([]byte(`{invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSaveLoadStorageStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "state.json")
	original := &StorageState{
		Version:    StorageStateVersion,
		ExportedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Cookies:    []Cookie{{Domain: "example.com", Name: "s", Value: "v", Path: "/"}},
	}
	if err := SaveStorageStateFile(original, path); err != nil {
		t.Fatalf("SaveStorageStateFile: %v", err)
	}
	// Verify file permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 0600", info.Mode().Perm())
	}
	loaded, err := LoadStorageStateFile(path)
	if err != nil {
		t.Fatalf("LoadStorageStateFile: %v", err)
	}
	if loaded.Version != original.Version {
		t.Errorf("Version = %d, want %d", loaded.Version, original.Version)
	}
	if len(loaded.Cookies) != 1 {
		t.Errorf("Cookies len = %d, want 1", len(loaded.Cookies))
	}
}

func TestLoadStorageStateFileMissing(t *testing.T) {
	_, err := LoadStorageStateFile("/nonexistent/path/state.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestDomainToOrigin(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"example.com", "https://example.com"},
		{"localhost", "http://localhost"},
		{"127.0.0.1", "http://127.0.0.1"},
		{"::1", "http://::1"},
		{"", ""},
	}
	for _, tc := range tests {
		got := domainToOrigin(tc.domain)
		if got != tc.want {
			t.Errorf("domainToOrigin(%q) = %q, want %q", tc.domain, got, tc.want)
		}
	}
}

func TestOriginToHost(t *testing.T) {
	tests := []struct {
		origin string
		want   string
	}{
		{"https://example.com", "example.com"},
		{"http://localhost", "localhost"},
		{"http://127.0.0.1", "127.0.0.1"},
		{"example.com", "example.com"},
	}
	for _, tc := range tests {
		got := originToHost(tc.origin)
		if got != tc.want {
			t.Errorf("originToHost(%q) = %q, want %q", tc.origin, got, tc.want)
		}
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	cs := NewCookieStore()
	cs.Add(&Cookie{Domain: "example.com", Name: "session", Value: "abc", Path: "/", Secure: true})
	sm := NewStorageManager("/tmp/test")
	sm.AddLocalStorage(&LocalStorageEntry{Domain: "example.com", Key: "theme", Value: "dark"})

	// Export.
	state, err := ExportStorageState(cs, sm, []StorageStateOrigin{
		{Origin: "https://example.com", SessionStorage: []LocalStorageKV{{Key: "tab", Value: "1"}}},
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Serialize + deserialize.
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	restored, err := DeserializeStorageState(data)
	if err != nil {
		t.Fatalf("Deserialize: %v", err)
	}

	// Import into fresh stores.
	cs2 := NewCookieStore()
	sm2 := NewStorageManager("/tmp/test2")
	ss, err := ImportStorageState(restored, cs2, sm2)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	// Verify cookies.
	cookies := cs2.ListCookies("")
	if len(cookies) != 1 {
		t.Fatalf("cookies len = %d, want 1", len(cookies))
	}
	if cookies[0].Value != "abc" {
		t.Errorf("cookies[0].Value = %s, want abc", cookies[0].Value)
	}

	// Verify localStorage.
	ls := sm2.ListLocalStorage("")
	if len(ls) != 1 {
		t.Fatalf("localStorage len = %d, want 1", len(ls))
	}
	if ls[0].Value != "dark" {
		t.Errorf("ls[0].Value = %s, want dark", ls[0].Value)
	}

	// Verify sessionStorage was returned.
	if len(ss) != 1 {
		t.Fatalf("sessionStorage len = %d, want 1", len(ss))
	}
	if ss[0].Key != "tab" || ss[0].Value != "1" {
		t.Errorf("ss[0] = %+v, want {tab 1}", ss[0])
	}
}
