package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionLoaderEmptyAllowlistNoArgs(t *testing.T) {
	l := NewExtensionLoader(nil)
	args, err := l.ChromiumArgs()
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 0 {
		t.Fatalf("empty allowlist must produce no args, got %v", args)
	}
}

func TestExtensionLoaderIsAllowed(t *testing.T) {
	l := NewExtensionLoader([]string{"adblock", "stealth"})
	if !l.IsAllowed("adblock") {
		t.Fatal("adblock must be allowed")
	}
	if l.IsAllowed("unknown") {
		t.Fatal("unknown must not be allowed")
	}
	if l.IsAllowed("") {
		t.Fatal("empty must not be allowed")
	}
}

func TestExtensionLoaderNilReceiverSafe(t *testing.T) {
	var l *ExtensionLoader
	if _, err := l.ChromiumArgs(); err == nil {
		t.Fatal("nil receiver ChromiumArgs must error")
	}
	if l.IsAllowed("x") {
		t.Fatal("nil receiver IsAllowed must return false")
	}
	if l.ResolvedDir() != "" {
		t.Fatal("nil receiver ResolvedDir must return empty")
	}
}

func TestExtensionLoaderResolvedExtensionsOnDisk(t *testing.T) {
	dir := t.TempDir()
	adblock := filepath.Join(dir, "adblock")
	stealth := filepath.Join(dir, "stealth")
	if err := os.MkdirAll(adblock, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stealth, 0o755); err != nil {
		t.Fatal(err)
	}
	l := &ExtensionLoader{
		BaseDir:   dir,
		Allowlist: []string{"adblock", "stealth"},
	}
	paths, errs := l.ResolvedExtensions()
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(paths))
	}
}

func TestExtensionLoaderMissingOnDiskRecordsError(t *testing.T) {
	dir := t.TempDir()
	l := &ExtensionLoader{
		BaseDir:   dir,
		Allowlist: []string{"missing"},
	}
	paths, errs := l.ResolvedExtensions()
	if len(paths) != 0 {
		t.Fatal("missing extension must not produce a path")
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
}

func TestExtensionLoaderFileNotDirRecordsError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := &ExtensionLoader{
		BaseDir:   dir,
		Allowlist: []string{"notadir"},
	}
	_, errs := l.ResolvedExtensions()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for file-not-dir, got %d", len(errs))
	}
}

func TestExtensionLoaderChromiumArgsFormat(t *testing.T) {
	dir := t.TempDir()
	ext1 := filepath.Join(dir, "ext1")
	ext2 := filepath.Join(dir, "ext2")
	if err := os.MkdirAll(ext1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ext2, 0o755); err != nil {
		t.Fatal(err)
	}
	l := &ExtensionLoader{
		BaseDir:   dir,
		Allowlist: []string{"ext1", "ext2"},
	}
	args, err := l.ChromiumArgs()
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	if args[0] != "--load-extension="+ext1+","+ext2 {
		t.Fatalf("unexpected load-extension arg: %s", args[0])
	}
	if args[1] != "--disable-extensions-except="+ext1+","+ext2 {
		t.Fatalf("unexpected disable-extensions-except arg: %s", args[1])
	}
}

func TestExtensionLoaderAbsoluteAllowlistEntry(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "abs-ext")
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatal(err)
	}
	l := &ExtensionLoader{
		BaseDir:   "/some/other/dir",
		Allowlist: []string{abs},
	}
	paths, errs := l.ResolvedExtensions()
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if len(paths) != 1 || paths[0] != abs {
		t.Fatalf("expected absolute path %s, got %v", abs, paths)
	}
}

func TestExtensionLoaderWithProfileDirOverridesBase(t *testing.T) {
	base := t.TempDir()
	profile := t.TempDir()
	ext := filepath.Join(profile, "ext")
	if err := os.MkdirAll(ext, 0o755); err != nil {
		t.Fatal(err)
	}
	l := &ExtensionLoader{
		BaseDir:   base,
		Allowlist: []string{"ext"},
	}
	l.WithProfileDir(profile)
	if l.ResolvedDir() != profile {
		t.Fatalf("expected profile dir, got %s", l.ResolvedDir())
	}
	paths, _ := l.ResolvedExtensions()
	if len(paths) != 1 {
		t.Fatalf("expected 1 path from profile dir, got %d", len(paths))
	}
}

func TestExtensionLoaderEmptyNameSkipped(t *testing.T) {
	dir := t.TempDir()
	l := &ExtensionLoader{
		BaseDir:   dir,
		Allowlist: []string{"", "  "},
	}
	paths, errs := l.ResolvedExtensions()
	if len(paths) != 0 {
		t.Fatalf("empty allowlist entries must be skipped, got %v", paths)
	}
	if len(errs) != 0 {
		t.Fatalf("empty entries must not produce errors, got %v", errs)
	}
}

func TestExtensionLoaderChromiumArgsMissingOnDiskErrors(t *testing.T) {
	dir := t.TempDir()
	l := &ExtensionLoader{
		BaseDir:   dir,
		Allowlist: []string{"missing"},
	}
	_, err := l.ChromiumArgs()
	if err == nil {
		t.Fatal("missing-on-disk must produce error from ChromiumArgs")
	}
}

func TestDefaultExtensionBaseDir(t *testing.T) {
	d := DefaultExtensionBaseDir()
	// In test environments without a home dir, the function returns "".
	// When set, it must be absolute and end with the canonical path.
	if d == "" {
		return
	}
	if !filepath.IsAbs(d) {
		t.Fatalf("expected absolute path, got %s", d)
	}
	if !strings.HasSuffix(d, filepath.Join(".omnimus", "browser", "extensions")) {
		t.Fatalf("expected path ending in .omnimus/browser/extensions, got %s", d)
	}
}
