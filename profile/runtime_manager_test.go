package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRuntimeManagerPersistentOwnershipRestartAndRecovery(t *testing.T) {
	root := t.TempDir()
	manager, err := NewRuntimeManager(root)
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.Open(context.Background(), OpenSessionRequest{ProfileID: "medical", OwnerUserRef: "user-a", Class: ProfilePersistent, Lifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if session.ID == "" || session.ContextID == "" || session.ID == SessionID(session.ContextID) {
		t.Fatalf("invalid opaque identities: %+v", session)
	}
	pageID, err := manager.RegisterPage(session.ID, "user-a", "target-1")
	if err != nil {
		t.Fatal(err)
	}
	if target, err := manager.ResolvePage(session.ID, pageID, "user-a"); err != nil || target != "target-1" {
		t.Fatalf("resolve: target=%q err=%v", target, err)
	}
	if _, err := manager.Get(session.ID, "user-b"); !isRuntimeClass(err, FailureDenied) {
		t.Fatalf("cross-owner get must deny: %v", err)
	}

	restarted, err := NewRuntimeManager(root)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restarted.Get(session.ID, "user-a")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != SessionCrashed || !recovered.Recovered || len(recovered.Pages) != 0 {
		t.Fatalf("restart recovery mismatch: %+v", recovered)
	}
}

func TestRuntimeManagerEphemeralCleanupLimitsAndExpiry(t *testing.T) {
	manager, err := NewRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	session, err := manager.Open(context.Background(), OpenSessionRequest{ProfileID: "scratch", OwnerUserRef: "user-a", Class: ProfileEphemeral, Lifetime: time.Minute, Limits: ResourceLimits{MaxPages: 1, MaxMemoryBytes: 10, MaxDiskBytes: 20, MaxDownloadBytes: 30, MaxLifetime: time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session.DataDir, "secret"), []byte("ephemeral"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RegisterPage(session.ID, "user-a", "target-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RegisterPage(session.ID, "user-a", "target-2"); !isRuntimeClass(err, FailureLimit) {
		t.Fatalf("page budget must fail: %v", err)
	}
	if err := manager.Account(session.ID, "user-a", ResourceUsage{MemoryBytes: 11}); !isRuntimeClass(err, FailureLimit) {
		t.Fatalf("memory budget must fail: %v", err)
	}
	now = now.Add(2 * time.Minute)
	expired, err := manager.Expire(context.Background())
	if err != nil || len(expired) != 1 || expired[0] != session.ID {
		t.Fatalf("expire: ids=%v err=%v", expired, err)
	}
	if _, err := os.Stat(session.DataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ephemeral data survived: %v", err)
	}
}

func TestRuntimeManagerPersistentSingleWriterAndClose(t *testing.T) {
	manager, err := NewRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := OpenSessionRequest{ProfileID: "main", OwnerUserRef: "owner", Class: ProfilePersistent}
	first, err := manager.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), request); !isRuntimeClass(err, FailureConflict) {
		t.Fatalf("second writer must fail: %v", err)
	}
	if err := manager.Close(context.Background(), first.ID, "intruder"); !isRuntimeClass(err, FailureDenied) {
		t.Fatalf("cross-owner close must deny: %v", err)
	}
	if err := manager.Close(context.Background(), first.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), request); err != nil {
		t.Fatalf("lock not released: %v", err)
	}
}

func TestRuntimeManagerConcurrentPageBudget(t *testing.T) {
	manager, err := NewRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.Open(context.Background(), OpenSessionRequest{ProfileID: "load", OwnerUserRef: "owner", Class: ProfileEphemeral, Limits: ResourceLimits{MaxPages: 8}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := manager.RegisterPage(session.ID, "owner", "target"); err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			} else if !isRuntimeClass(err, FailureLimit) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if succeeded != 8 {
		t.Fatalf("budget race: got %d pages", succeeded)
	}
}

func TestRuntimeManagerRejectsCorruptionAndEscapingPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sessions.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRuntimeManager(root); !isRuntimeClass(err, FailureCorrupt) {
		t.Fatalf("corruption must fail closed: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "sessions.json")); err != nil {
		t.Fatal(err)
	}
	manager, err := NewRuntimeManager(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Open(context.Background(), OpenSessionRequest{ProfileID: "escape", OwnerUserRef: "owner", Class: ProfilePersistent, DataDir: filepath.Join(root, "..", "outside")})
	if !isRuntimeClass(err, FailureDenied) {
		t.Fatalf("escaping path must deny: %v", err)
	}
}

func TestRuntimeManagerExportImportExcludesCredentialMaterial(t *testing.T) {
	manager, err := NewRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.Open(context.Background(), OpenSessionRequest{ProfileID: "exported", OwnerUserRef: "owner", Class: ProfilePersistent})
	if err != nil {
		t.Fatal(err)
	}
	cookies := NewCookieStore()
	cookies.Add(&Cookie{Domain: "example.com", Name: "session", Value: "cookie-value"})
	storage := NewStorageManager(session.DataDir)
	storage.AddLocalStorage(&LocalStorageEntry{Domain: "example.com", Key: "theme", Value: "dark"})
	archive, err := manager.ExportProfile("exported", "owner", cookies, storage)
	if err != nil {
		t.Fatal(err)
	}
	if stringContains(string(archive), "password-secret") {
		t.Fatal("credential material leaked into archive")
	}
	importCookies := NewCookieStore()
	importStorage := NewStorageManager(t.TempDir())
	profileID, err := manager.ImportProfile(archive, "owner", importCookies, importStorage)
	if err != nil || profileID != "exported" {
		t.Fatalf("import: id=%q err=%v", profileID, err)
	}
	if len(importCookies.ListCookies("example.com")) != 1 || len(importStorage.ListLocalStorage("example.com")) != 1 {
		t.Fatal("storage state was not restored")
	}
	if _, err := manager.ImportProfile(archive, "intruder", importCookies, importStorage); !isRuntimeClass(err, FailureDenied) {
		t.Fatalf("cross-owner import must deny: %v", err)
	}
}

func stringContains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func isRuntimeClass(err error, class RuntimeFailure) bool {
	var runtimeErr *RuntimeError
	return errors.As(err, &runtimeErr) && runtimeErr.Class == class
}
