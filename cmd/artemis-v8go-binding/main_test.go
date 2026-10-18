package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLLVM writes an executable clang++ stand-in that prints the given version
// banner, so the toolchain admission can be exercised without a real LLVM.
func fakeLLVM(t *testing.T, banner string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"" + banner + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "clang++"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDefaultLLVMPrefersTheEnvironmentOverride(t *testing.T) {
	t.Setenv("ARTEMIS_LLVM_BIN", "/custom/llvm/bin")
	if got := defaultLLVM(); got != "/custom/llvm/bin" {
		t.Fatalf("ARTEMIS_LLVM_BIN must win, got %q", got)
	}
	t.Setenv("ARTEMIS_LLVM_BIN", "")
	if got := defaultLLVM(); got != "/opt/homebrew/opt/llvm/bin" {
		t.Fatalf("default LLVM location changed unexpectedly: %q", got)
	}
}

func TestRequireClangAdmitsOnlyRecentLLVMClang(t *testing.T) {
	ctx := context.Background()
	if err := requireClang(ctx, filepath.Join(t.TempDir(), "clang++")); err == nil || !strings.Contains(err.Error(), "run ") {
		t.Fatalf("a missing compiler must fail with the run error, got %v", err)
	}
	if err := requireClang(ctx, filepath.Join(fakeLLVM(t, "Apple clang version 17.0.0"), "clang++")); err == nil || !strings.Contains(err.Error(), "LLVM Clang") {
		t.Fatalf("Apple clang must be rejected, got %v", err)
	}
	if err := requireClang(ctx, filepath.Join(fakeLLVM(t, "clang version 15.0.7"), "clang++")); err == nil || !strings.Contains(err.Error(), "LLVM Clang") {
		t.Fatalf("an old LLVM clang must be rejected, got %v", err)
	}
	if err := requireClang(ctx, filepath.Join(fakeLLVM(t, "garbage banner"), "clang++")); err == nil || !strings.Contains(err.Error(), "unrecognised clang version") {
		t.Fatalf("an unparsable banner must be rejected, got %v", err)
	}
	if err := requireClang(ctx, filepath.Join(fakeLLVM(t, "clang version 21.1.0"), "clang++")); err != nil {
		t.Fatalf("a recent LLVM clang must be admitted, got %v", err)
	}
}

func TestRunRejectsUnknownTargetsAndMissingSources(t *testing.T) {
	ctx := context.Background()
	llvm := fakeLLVM(t, "clang version 21.1.0")
	empty := t.TempDir()
	if err := run(ctx, llvm, empty, []string{"darwin_arm64"}); err == nil || !strings.Contains(err.Error(), "no C++ sources") {
		t.Fatalf("a v8go directory without sources must be rejected, got %v", err)
	}
	withSources := t.TempDir()
	if err := os.WriteFile(filepath.Join(withSources, "binding.cc"), []byte("// binding\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, llvm, withSources, []string{"nope"}); err == nil || !strings.Contains(err.Error(), `unknown target "nope"`) {
		t.Fatalf("an unknown lib target must be rejected before any build, got %v", err)
	}
}
