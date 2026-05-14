package main

import (
	"strings"
	"testing"
)

func TestSnapshotPathTargetsRuntimeBlob(t *testing.T) {
	if snapshotPath != "js/snapshot.bin" {
		t.Fatalf("snapshotPath = %q, want js/snapshot.bin", snapshotPath)
	}
}

func TestStubsBootstrapDefinesRequiredGlobals(t *testing.T) {
	for _, token := range []string{
		"globalThis.window = globalThis",
		"globalThis.document = Object.create(null)",
		"globalThis.navigator = Object.create(null)",
		"globalThis.performance = Object.create(null)",
		"const NATIVE_NAMES = [",
		"__wrap",
		"__fetch",
		"__document",
		"__crypto_random",
	} {
		if !strings.Contains(stubsBootstrap, token) {
			t.Fatalf("stubsBootstrap missing %q", token)
		}
	}
}
