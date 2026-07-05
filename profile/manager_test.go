package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestManager(t *testing.T) (*ProfileManager, *BrowserProfileAccessGate, string) {
	t.Helper()
	base := t.TempDir()
	gate := &BrowserProfileAccessGate{IsOperator: func(u string) bool {
		return strings.HasPrefix(u, "op-")
	}}
	m := NewProfileManager(base, gate)
	return m, gate, base
}

func TestBrowserProfileAccessGate_Private(t *testing.T) {
	gate := &BrowserProfileAccessGate{}
	p := &BrowserProfile{OwnerUserRef: "alice", ShareScope: SharePrivate}
	if d := gate.Check(p, "alice"); !d.Allowed {
		t.Fatalf("owner denied: %s", d.Reason)
	}
	if d := gate.Check(p, "bob"); d.Allowed {
		t.Fatal("non-owner allowed for private")
	}
}

func TestBrowserProfileAccessGate_OperatorShared(t *testing.T) {
	gate := &BrowserProfileAccessGate{IsOperator: func(u string) bool {
		return u == "op-1"
	}}
	p := &BrowserProfile{OwnerUserRef: "alice", ShareScope: ShareOperatorShared}
	if d := gate.Check(p, "alice"); !d.Allowed {
		t.Fatalf("owner denied: %s", d.Reason)
	}
	if d := gate.Check(p, "op-1"); !d.Allowed {
		t.Fatalf("operator denied: %s", d.Reason)
	}
	if d := gate.Check(p, "random"); d.Allowed {
		t.Fatal("random user allowed for operator_shared")
	}
}

func TestBrowserProfileAccessGate_WorkspaceShared(t *testing.T) {
	gate := &BrowserProfileAccessGate{}
	p := &BrowserProfile{
		OwnerUserRef:       "alice",
		ShareScope:         ShareWorkspaceShared,
		SharedWithUserRefs: []string{"bob", "carol"},
	}
	if d := gate.Check(p, "alice"); !d.Allowed {
		t.Fatalf("owner denied: %s", d.Reason)
	}
	if d := gate.Check(p, "bob"); !d.Allowed {
		t.Fatalf("listed user denied: %s", d.Reason)
	}
	if d := gate.Check(p, "dave"); d.Allowed {
		t.Fatal("unlisted user allowed for workspace_shared")
	}
}

func TestBrowserProfileAccessGate_NilAndEmpty(t *testing.T) {
	gate := &BrowserProfileAccessGate{}
	if d := gate.Check(nil, "x"); d.Allowed {
		t.Fatal("nil profile allowed")
	}
	if d := gate.Check(&BrowserProfile{OwnerUserRef: "x", ShareScope: SharePrivate}, ""); d.Allowed {
		t.Fatal("empty caller allowed")
	}
}

func TestProfileManager_CreateAndGetDataDirCreated(t *testing.T) {
	m, _, base := newTestManager(t)
	p := &BrowserProfile{
		Name:         "prof1",
		OwnerUserRef: "alice",
		ShareScope:   SharePrivate,
	}
	if err := m.Create(p); err != nil {
		t.Fatalf("Create: %v", err)
	}
	expected := filepath.Join(base, "alice", "prof1")
	if p.DataDir != expected {
		t.Fatalf("DataDir=%s want %s", p.DataDir, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("data dir not created: %v", err)
	}
	if p.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not set")
	}
}

func TestProfileManager_Create_Duplicate(t *testing.T) {
	m, _, _ := newTestManager(t)
	p := &BrowserProfile{Name: "prof1", OwnerUserRef: "alice"}
	if err := m.Create(p); err != nil {
		t.Fatal(err)
	}
	if err := m.Create(&BrowserProfile{Name: "prof1", OwnerUserRef: "bob"}); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestProfileManager_Create_Validation(t *testing.T) {
	m, _, _ := newTestManager(t)
	cases := []struct {
		p *BrowserProfile
	}{
		{nil},
		{&BrowserProfile{Name: "", OwnerUserRef: "x"}},
		{&BrowserProfile{Name: "p", OwnerUserRef: ""}},
	}
	for i, c := range cases {
		if err := m.Create(c.p); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestProfileManager_Get_AccessGate(t *testing.T) {
	m, _, _ := newTestManager(t)
	p := &BrowserProfile{Name: "prof1", OwnerUserRef: "alice", ShareScope: SharePrivate}
	_ = m.Create(p)
	if _, err := m.Get("prof1", "alice"); err != nil {
		t.Fatalf("owner Get: %v", err)
	}
	if _, err := m.Get("prof1", "bob"); err == nil {
		t.Fatal("non-owner Get allowed")
	}
	if _, err := m.Get("nonexistent", "alice"); err == nil {
		t.Fatal("nonexistent Get allowed")
	}
}

func TestProfileManager_List_FilteredByAccess(t *testing.T) {
	m, _, _ := newTestManager(t)
	_ = m.Create(&BrowserProfile{Name: "a", OwnerUserRef: "alice", ShareScope: SharePrivate})
	_ = m.Create(&BrowserProfile{Name: "b", OwnerUserRef: "bob", ShareScope: ShareWorkspaceShared, SharedWithUserRefs: []string{"alice"}})
	_ = m.Create(&BrowserProfile{Name: "c", OwnerUserRef: "carol", ShareScope: SharePrivate})
	list := m.List("alice")
	names := map[string]bool{}
	for _, p := range list {
		names[p.Name] = true
	}
	if !names["a"] {
		t.Fatal("alice should see own profile a")
	}
	if !names["b"] {
		t.Fatal("alice should see shared profile b")
	}
	if names["c"] {
		t.Fatal("alice should NOT see carol's private profile c")
	}
}

func TestProfileManager_Delete(t *testing.T) {
	m, _, _ := newTestManager(t)
	_ = m.Create(&BrowserProfile{Name: "prof1", OwnerUserRef: "alice"})
	if err := m.Delete("prof1", "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Get("prof1", "alice"); err == nil {
		t.Fatal("profile still exists after delete")
	}
	if err := m.Delete("prof1", "alice"); err == nil {
		t.Fatal("delete nonexistent should error")
	}
	if err := m.Delete("prof1", "bob"); err == nil {
		t.Fatal("non-owner delete allowed")
	}
}

func TestProfileManager_SwitchProfile_DeactivatesOthers(t *testing.T) {
	m, _, _ := newTestManager(t)
	_ = m.Create(&BrowserProfile{Name: "a", OwnerUserRef: "alice"})
	_ = m.Create(&BrowserProfile{Name: "b", OwnerUserRef: "alice"})
	// Activate a
	a, err := m.SwitchProfile("a", "alice")
	if err != nil {
		t.Fatalf("SwitchProfile a: %v", err)
	}
	if !a.IsActive {
		t.Fatal("a not active after switch")
	}
	// Activate b -> a must deactivate
	b, err := m.SwitchProfile("b", "alice")
	if err != nil {
		t.Fatalf("SwitchProfile b: %v", err)
	}
	if !b.IsActive {
		t.Fatal("b not active after switch")
	}
	a2, _ := m.Get("a", "alice")
	if a2.IsActive {
		t.Fatal("a still active after switching to b")
	}
}

func TestProfileManager_SwitchProfile_AccessGate(t *testing.T) {
	m, _, _ := newTestManager(t)
	_ = m.Create(&BrowserProfile{Name: "prof1", OwnerUserRef: "alice", ShareScope: SharePrivate})
	if _, err := m.SwitchProfile("prof1", "bob"); err == nil {
		t.Fatal("non-owner switch allowed")
	}
}

func TestProfileManager_PurgeDataDir(t *testing.T) {
	m, _, _ := newTestManager(t)
	p := &BrowserProfile{Name: "prof1", OwnerUserRef: "alice"}
	_ = m.Create(p)
	// Write a file into the data dir
	f := filepath.Join(p.DataDir, "Cookies")
	_ = os.WriteFile(f, []byte("test"), 0o600)
	if err := m.PurgeDataDir(p); err != nil {
		t.Fatalf("PurgeDataDir: %v", err)
	}
	if _, err := os.Stat(p.DataDir); !os.IsNotExist(err) {
		t.Fatalf("data dir still exists after purge: %v", err)
	}
}

func TestProfileManager_DefaultProfileBaseDir(t *testing.T) {
	dir := DefaultProfileBaseDir()
	if dir == "" {
		// UserHomeDir may fail in some sandboxes; verify empty handling.
		return
	}
	if !strings.HasSuffix(dir, filepath.Join(".omnimus", "browser", "profiles")) {
		t.Fatalf("unexpected base dir: %s", dir)
	}
}
