package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfileManagerPersistsMetadataAndSwitchState(t *testing.T) {
	root := t.TempDir()
	manager := NewProfileManager(root, nil)
	if err := manager.Create(&BrowserProfile{Name: "one", OwnerUserRef: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Create(&BrowserProfile{Name: "two", OwnerUserRef: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SwitchProfile("two", "owner"); err != nil {
		t.Fatal(err)
	}
	restarted := NewProfileManager(root, nil)
	one, err := restarted.Get("one", "owner")
	if err != nil {
		t.Fatal(err)
	}
	two, err := restarted.Get("two", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if one.IsActive || !two.IsActive {
		t.Fatalf("switch state was not durable: one=%v two=%v", one.IsActive, two.IsActive)
	}
	if err := restarted.Delete("one", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProfileManager(root, nil).Get("one", "owner"); err == nil {
		t.Fatal("deleted metadata returned after restart")
	}
}

func TestProfileManagerCorruptMetadataFailsClosed(t *testing.T) {
	root := t.TempDir()
	manager := NewProfileManager(root, nil)
	if err := manager.Create(&BrowserProfile{Name: "one", OwnerUserRef: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt := NewProfileManager(root, nil)
	if _, err := corrupt.Get("one", "owner"); err == nil {
		t.Fatal("corrupt metadata did not fail closed")
	}
}
