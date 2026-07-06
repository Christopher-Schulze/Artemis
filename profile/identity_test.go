package profile

import (
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestGenerateUUID5Deterministic(t *testing.T) {
	ns := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	a := GenerateUUID5(ns, "alice")
	b := GenerateUUID5(ns, "alice")
	if a != b {
		t.Fatalf("UUID5 not deterministic: %s vs %s", a, b)
	}
}

func TestGenerateUUID5DifferentNames(t *testing.T) {
	ns := DefaultNamespace
	a := GenerateUUID5(ns, "alice")
	b := GenerateUUID5(ns, "bob")
	if a == b {
		t.Fatal("different names must yield different UUIDs")
	}
}

func TestGenerateUUID5DifferentNamespaces(t *testing.T) {
	ns1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	ns2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	a := GenerateUUID5(ns1, "alice")
	b := GenerateUUID5(ns2, "alice")
	if a == b {
		t.Fatal("different namespaces must yield different UUIDs")
	}
}

func TestGenerateUUID5VersionAndVariant(t *testing.T) {
	id := GenerateUUID5(DefaultNamespace, "test")
	if id.Version() != 5 {
		t.Fatalf("version = %d, want 5", id.Version())
	}
	if id.Variant() != uuid.RFC4122 {
		t.Fatalf("variant = %d, want RFC4122", id.Variant())
	}
}

func TestGenerateUUID5MatchesGoogleUUID(t *testing.T) {
	ns := DefaultNamespace
	ours := GenerateUUID5(ns, "cross-check")
	theirs := uuid.NewSHA1(ns, []byte("cross-check"))
	if ours != theirs {
		t.Fatalf("UUID5 mismatch with google/uuid: %s vs %s", ours, theirs)
	}
}

func TestGenerateUUID5FormatString(t *testing.T) {
	id := GenerateUUID5(DefaultNamespace, "format")
	s := id.String()
	if _, err := uuid.Parse(s); err != nil {
		t.Fatalf("generated UUID string not parseable: %v", err)
	}
}

func TestGetOrCreateSameNameSameID(t *testing.T) {
	m := NewIdentityManager(DefaultNamespace, NewInMemoryPersistence())
	a := m.GetOrCreateIdentity("work-profile")
	b := m.GetOrCreateIdentity("work-profile")
	if a.UserID != b.UserID {
		t.Fatalf("same name must yield same UserID: %s vs %s", a.UserID, b.UserID)
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		t.Fatal("CreatedAt must be stable on cached return")
	}
}

func TestGetOrCreateDifferentNameDifferentID(t *testing.T) {
	m := NewIdentityManager(DefaultNamespace, NewInMemoryPersistence())
	a := m.GetOrCreateIdentity("work")
	b := m.GetOrCreateIdentity("personal")
	if a.UserID == b.UserID {
		t.Fatal("different names must yield different UserIDs")
	}
}

func TestGetOrCreateEmptyNameReturnsZero(t *testing.T) {
	m := NewIdentityManager(DefaultNamespace, NewInMemoryPersistence())
	id := m.GetOrCreateIdentity("")
	if id.UserID != uuid.Nil {
		t.Fatal("empty name must yield zero identity")
	}
}

func TestGetOrCreatePersistsAndReloads(t *testing.T) {
	p := NewInMemoryPersistence()
	m1 := NewIdentityManager(DefaultNamespace, p)
	a := m1.GetOrCreateIdentity("persisted-profile")

	// New manager, same persistence: must reload the same identity.
	m2 := NewIdentityManager(DefaultNamespace, p)
	b := m2.GetOrCreateIdentity("persisted-profile")
	if a.UserID != b.UserID {
		t.Fatalf("persisted identity not reloaded: %s vs %s", a.UserID, b.UserID)
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		t.Fatal("CreatedAt must be preserved across reload")
	}
}

func TestGetOrCreateNilPersistenceStillDeterministic(t *testing.T) {
	m := NewIdentityManager(DefaultNamespace, nil)
	a := m.GetOrCreateIdentity("ephemeral")
	b := m.GetOrCreateIdentity("ephemeral")
	if a.UserID != b.UserID {
		t.Fatal("nil persistence must still be deterministic within manager")
	}
}

func TestLookupExisting(t *testing.T) {
	m := NewIdentityManager(DefaultNamespace, NewInMemoryPersistence())
	created := m.GetOrCreateIdentity("lookup-me")
	got, err := m.Lookup("lookup-me")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.UserID != created.UserID {
		t.Fatalf("Lookup mismatch: %s vs %s", got.UserID, created.UserID)
	}
}

func TestLookupMissingFails(t *testing.T) {
	m := NewIdentityManager(DefaultNamespace, NewInMemoryPersistence())
	if _, err := m.Lookup("nope"); err == nil {
		t.Fatal("Lookup of missing identity must error")
	}
}

func TestLookupEmptyNameFails(t *testing.T) {
	m := NewIdentityManager(DefaultNamespace, NewInMemoryPersistence())
	if _, err := m.Lookup(""); err == nil {
		t.Fatal("Lookup empty name must error")
	}
}

func TestInMemoryPersistenceSaveLoad(t *testing.T) {
	p := NewInMemoryPersistence()
	id := ProfileIdentity{
		UserID:      GenerateUUID5(DefaultNamespace, "x"),
		Namespace:   DefaultNamespace,
		ProfileName: "x",
	}
	if err := p.Save(id); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := p.Load("x")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.UserID != id.UserID {
		t.Fatalf("loaded UserID mismatch: %s vs %s", got.UserID, id.UserID)
	}
}

func TestInMemoryPersistenceSaveEmptyNameFails(t *testing.T) {
	p := NewInMemoryPersistence()
	if err := p.Save(ProfileIdentity{}); err == nil {
		t.Fatal("Save with empty name must fail")
	}
}

func TestInMemoryPersistenceLoadMissingFails(t *testing.T) {
	p := NewInMemoryPersistence()
	if _, err := p.Load("missing"); err == nil {
		t.Fatal("Load missing must fail")
	}
}

func TestInMemoryPersistenceOverwrite(t *testing.T) {
	p := NewInMemoryPersistence()
	id1 := ProfileIdentity{UserID: GenerateUUID5(DefaultNamespace, "n"), Namespace: DefaultNamespace, ProfileName: "n"}
	id2 := ProfileIdentity{UserID: GenerateUUID5(uuid.MustParse("00000000-0000-0000-0000-000000000001"), "n"), Namespace: DefaultNamespace, ProfileName: "n"}
	if err := p.Save(id1); err != nil {
		t.Fatalf("Save1: %v", err)
	}
	if err := p.Save(id2); err != nil {
		t.Fatalf("Save2: %v", err)
	}
	got, err := p.Load("n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.UserID != id2.UserID {
		t.Fatal("overwrite did not take effect")
	}
}

func TestIdentityManagerNilNamespaceUsesDefault(t *testing.T) {
	m := NewIdentityManager(uuid.Nil, nil)
	if m.Namespace() != DefaultNamespace {
		t.Fatal("nil namespace must fall back to DefaultNamespace")
	}
}

func TestIdentityManagerConcurrentDeterministic(t *testing.T) {
	m := NewIdentityManager(DefaultNamespace, NewInMemoryPersistence())
	var wg sync.WaitGroup
	results := make([]ProfileIdentity, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = m.GetOrCreateIdentity("concurrent-profile")
		}(i)
	}
	wg.Wait()
	for i := 1; i < 16; i++ {
		if results[i].UserID != results[0].UserID {
			t.Fatalf("goroutine %d UserID differs: %s vs %s", i, results[i].UserID, results[0].UserID)
		}
	}
}

func TestIdentityManagerConcurrentDistinctNames(t *testing.T) {
	m := NewIdentityManager(DefaultNamespace, NewInMemoryPersistence())
	var wg sync.WaitGroup
	ids := make(map[string]ProfileIdentity)
	var mu sync.Mutex
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "p-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
			id := m.GetOrCreateIdentity(name)
			mu.Lock()
			ids[name] = id
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	// Each distinct name has a distinct, deterministic UserID.
	seen := make(map[uuid.UUID]string)
	for name, id := range ids {
		if other, dup := seen[id.UserID]; dup {
			t.Fatalf("UserID collision between %q and %q", other, name)
		}
		seen[id.UserID] = name
		// Re-fetch must be stable.
		again := m.GetOrCreateIdentity(name)
		if again.UserID != id.UserID {
			t.Fatalf("non-deterministic for %q", name)
		}
	}
}
