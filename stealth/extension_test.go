package stealth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStealthExtensionGenerate(t *testing.T) {
	dir := t.TempDir()
	ext := NewStealthExtension(Defaults())
	if err := ext.GenerateStealthExtension(dir); err != nil {
		t.Fatal(err)
	}

	// Verify manifest.json exists
	manifestPath := filepath.Join(dir, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest.json missing: %v", err)
	}

	// Verify content.js exists and contains stealth patches
	contentPath := filepath.Join(dir, "content.js")
	content, err := os.ReadFile(contentPath)
	if err != nil {
		t.Fatalf("content.js missing: %v", err)
	}
	if !strings.Contains(string(content), "webdriver") {
		t.Fatal("content.js must contain stealth patches")
	}
}

func TestStealthExtensionValidateManifest(t *testing.T) {
	dir := t.TempDir()
	ext := NewStealthExtension(Defaults())
	if err := ext.GenerateStealthExtension(dir); err != nil {
		t.Fatal(err)
	}
	if err := ValidateManifest(dir); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
}

func TestStealthExtensionLoadFlag(t *testing.T) {
	ext := NewStealthExtension(Defaults())
	flag := ext.ExtensionLoadFlag("/tmp/test-ext")
	if flag != "--load-extension=/tmp/test-ext" {
		t.Fatalf("expected --load-extension=/tmp/test-ext, got %s", flag)
	}
}

func TestStealthExtensionLoadFlagDefaultDir(t *testing.T) {
	ext := NewStealthExtension(Defaults())
	flag := ext.ExtensionLoadFlag("")
	if !strings.HasPrefix(flag, "--load-extension=") {
		t.Fatalf("expected --load-extension= prefix, got %s", flag)
	}
	if !strings.HasSuffix(flag, "stealth-ext") {
		t.Fatalf("expected stealth-ext suffix, got %s", flag)
	}
}

func TestEvasionModeString(t *testing.T) {
	if EvasionExtension.String() != "extension" {
		t.Fatalf("expected extension, got %s", EvasionExtension.String())
	}
	if EvasionRuntime.String() != "runtime" {
		t.Fatalf("expected runtime, got %s", EvasionRuntime.String())
	}
}

func TestDefaultEvasionMode(t *testing.T) {
	if DefaultEvasionMode() != EvasionExtension {
		t.Fatal("default evasion mode must be EvasionExtension (Option B)")
	}
}

func TestStealthExtensionGenerateEmptyDir(t *testing.T) {
	ext := NewStealthExtension(Defaults())
	err := ext.GenerateStealthExtension("")
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestValidateManifestMissingFile(t *testing.T) {
	err := ValidateManifest(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
}
