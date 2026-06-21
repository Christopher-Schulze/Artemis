package stealth

import (
	"strings"
	"testing"
)

func TestBundledScript(t *testing.T) {
	script := BundledScript()
	if script == "" {
		t.Fatal("expected non-empty bundled script")
	}
}

func TestBundledScriptSize(t *testing.T) {
	size := BundledScriptSize()
	if size == 0 {
		t.Fatal("expected non-zero size")
	}
	if size < 1000 {
		t.Fatalf("expected at least 1KB, got %d bytes", size)
	}
}

func TestBundledScriptHash(t *testing.T) {
	hash := BundledScriptHash()
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if len(hash) != 64 { // SHA-256 hex = 64 chars
		t.Fatalf("expected 64 char hash, got %d", len(hash))
	}
}

func TestIsBundled(t *testing.T) {
	if !IsBundled() {
		t.Fatal("expected script to be bundled via go:embed")
	}
}

func TestBundledScriptContainsPatches(t *testing.T) {
	script := BundledScript()
	// Verify key patches are present
	checks := []string{
		"navigator.webdriver",
		"window.chrome",
		"permissions.query",
		"plugins",
		"Canvas",
		"WebGL",
		"AudioContext",
		"hardwareConcurrency",
		"deviceMemory",
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Errorf("bundled script missing patch for %q", check)
		}
	}
}

func TestBundledScriptIsIIFE(t *testing.T) {
	script := BundledScript()
	if !strings.Contains(script, "(function()") {
		t.Fatal("expected IIFE wrapper in script")
	}
}

func TestParanoidScript(t *testing.T) {
	script := ParanoidScript()
	if script == "" {
		t.Fatal("expected non-empty paranoid script")
	}
	if !strings.Contains(script, "Paranoid") {
		t.Fatal("expected paranoid marker in script")
	}
}

func TestParanoidScriptContainsReferrerPatch(t *testing.T) {
	script := ParanoidScript()
	if !strings.Contains(script, "referrer") {
		t.Fatal("expected referrer patch in paranoid script")
	}
}

func TestParanoidScriptContainsTypingRhythmPatch(t *testing.T) {
	script := ParanoidScript()
	if !strings.Contains(script, "typoRate") {
		t.Fatal("expected typing rhythm patch in paranoid script")
	}
}

func TestParanoidScriptLargerThanBase(t *testing.T) {
	base := BundledScriptSize()
	paranoid := len(ParanoidScript())
	if paranoid <= base {
		t.Fatalf("expected paranoid > base, got paranoid=%d base=%d", paranoid, base)
	}
}

func TestPatchCountConstants(t *testing.T) {
	if BasePatchCount != 27 {
		t.Fatalf("expected 27 base patches, got %d", BasePatchCount)
	}
	if ParanoidPatchCount != 2 {
		t.Fatalf("expected 2 paranoid patches, got %d", ParanoidPatchCount)
	}
}

func TestBundledScriptDeterministic(t *testing.T) {
	hash1 := BundledScriptHash()
	hash2 := BundledScriptHash()
	if hash1 != hash2 {
		t.Fatal("expected deterministic hash")
	}
}
