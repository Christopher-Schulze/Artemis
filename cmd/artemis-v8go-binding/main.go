// artemis-v8go-binding precompiles the vendored v8go C++ binding into the
// static archives third_party/v8go/lib/<os_arch>/libv8go_artemis.a and records
// the source digest in third_party/v8go/lib/BINDING.sha256.
//
// V8 from github.com/tommie/v8go is built against Chromium's libc++, so the
// binding must be compiled with Clang >= 21, the vendored libc++ headers and
// -nostdinc++. Go accepts neither the compiler choice nor -nostdinc++ in #cgo
// directives; precompiling on darwin keeps every darwin build on the default
// toolchain without special CC or CGO_CXXFLAGS. Run after changing any .cc or
// .h file in third_party/v8go, from the Artemis module root:
//
//	go run ./cmd/artemis-v8go-binding -llvm /opt/homebrew/opt/llvm/bin
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tommie/v8go/bindingdigest"
)

const (
	minimumClangMajor = 21
	archiveName       = "libv8go_artemis.a"
	manifestName      = "BINDING.sha256"
)

// target is one darwin architecture the archives are built for.
type target struct {
	name   string // lib/<name>
	goarch string
	triple string
}

var targets = map[string]target{
	"darwin_arm64": {name: "darwin_arm64", goarch: "arm64", triple: "arm64-apple-macos13.0"},
	"darwin_amd64": {name: "darwin_amd64", goarch: "amd64", triple: "x86_64-apple-macos13.0"},
}

// cxxFlags mirror the CXXFLAGS in third_party/v8go/cgo.go plus -nostdinc++.
var cxxFlags = []string{
	"-O2", "-fno-rtti", "-std=c++20", "-fPIC", "-stdlib=libc++", "-nostdinc++", "-Wall",
	"-D_LIBCPP_HARDENING_MODE=_LIBCPP_HARDENING_MODE_EXTENSIVE",
	"-DV8_COMPRESS_POINTERS", "-DV8_31BIT_SMIS_ON_64BIT_ARCH", "-DV8_ENABLE_SANDBOX", "-DV8_CPPGC_MICROTASK_QUEUE",
	"-DV8_DEPRECATION_WARNINGS", "-DV8_IMMINENT_DEPRECATION_WARNINGS",
}

func main() {
	llvm := flag.String("llvm", defaultLLVM(), "directory containing clang++ (>= 21) and llvm-ar")
	dir := flag.String("v8go", filepath.Join("third_party", "v8go"), "vendored v8go directory")
	archs := flag.String("arch", "darwin_arm64,darwin_amd64", "comma-separated lib targets")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := run(ctx, *llvm, *dir, strings.Split(*archs, ",")); err != nil {
		fmt.Fprintln(os.Stderr, "artemis-v8go-binding:", err)
		os.Exit(1)
	}
}

func defaultLLVM() string {
	if dir := os.Getenv("ARTEMIS_LLVM_BIN"); dir != "" {
		return dir
	}
	return "/opt/homebrew/opt/llvm/bin"
}

func run(ctx context.Context, llvm, dir string, names []string) error {
	clang := filepath.Join(llvm, "clang++")
	archiver := filepath.Join(llvm, "llvm-ar")
	if err := requireClang(ctx, clang); err != nil {
		return err
	}
	sources, err := filepath.Glob(filepath.Join(dir, "*.cc"))
	if err != nil || len(sources) == 0 {
		return fmt.Errorf("no C++ sources in %s: %v", dir, err)
	}
	digest, err := bindingdigest.SourceDigest(os.DirFS(dir))
	if err != nil {
		return err
	}
	for _, name := range names {
		spec, ok := targets[strings.TrimSpace(name)]
		if !ok {
			return fmt.Errorf("unknown target %q", name)
		}
		if err := buildTarget(ctx, clang, archiver, dir, sources, spec); err != nil {
			return fmt.Errorf("%s: %w", spec.name, err)
		}
	}
	manifest := fmt.Sprintf("%s  .cc and .h sources of %s\n", digest, filepath.Base(dir))
	return os.WriteFile(filepath.Join(dir, "lib", manifestName), []byte(manifest), 0o644)
}

var clangVersion = regexp.MustCompile(`clang version (\d+)\.`)

func requireClang(ctx context.Context, clang string) error {
	out, err := exec.CommandContext(ctx, clang, "--version").Output()
	if err != nil {
		return fmt.Errorf("run %s: %w", clang, err)
	}
	match := clangVersion.FindSubmatch(out)
	if match == nil {
		return fmt.Errorf("unrecognised clang version output: %s", bytes.TrimSpace(out))
	}
	major, err := strconv.Atoi(string(match[1]))
	if err != nil || major < minimumClangMajor || bytes.Contains(out, []byte("Apple clang")) {
		return fmt.Errorf("V8's libc++ headers need LLVM Clang >= %d, got: %s", minimumClangMajor, bytes.SplitN(out, []byte("\n"), 2)[0])
	}
	return nil
}

func buildTarget(ctx context.Context, clang, archiver, dir string, sources []string, spec target) error {
	work, err := os.MkdirTemp("", "artemis-v8go-binding-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := exportHeader(ctx, dir, work, spec); err != nil {
		return err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	var objects []string
	for _, source := range sources {
		object := filepath.Join(work, strings.TrimSuffix(filepath.Base(source), ".cc")+".o")
		args := append([]string{"-c", "-target", spec.triple, "-I" + work, "-I" + absDir,
			"-I" + filepath.Join(absDir, "deps", "include"),
			"-I" + filepath.Join(absDir, "deps", "include_libcxx"),
			"-I" + filepath.Join(absDir, "deps", "include_libcxxabi"),
			"-ffile-prefix-map=" + absDir + "=v8go",
			"-ffile-prefix-map=" + work + "=cgo"}, cxxFlags...)
		args = append(args, source, "-o", object)
		if out, err := exec.CommandContext(ctx, clang, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("compile %s: %w\n%s", filepath.Base(source), err, out)
		}
		objects = append(objects, object)
	}
	libDir := filepath.Join(dir, "lib", spec.name)
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		return err
	}
	archive := filepath.Join(libDir, archiveName)
	if err := os.Remove(archive); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	args := append([]string{"rcsD", archive}, objects...)
	if out, err := exec.CommandContext(ctx, archiver, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("archive: %w\n%s", err, out)
	}
	return nil
}

// exportHeader generates _cgo_export.h for the Go callbacks the C++ calls,
// exactly as cgo produces it when compiling the package for the target.
func exportHeader(ctx context.Context, dir, work string, spec target) error {
	env := append(os.Environ(), "GOOS=darwin", "GOARCH="+spec.goarch, "CGO_ENABLED=1")
	list := exec.CommandContext(ctx, "go", "list", "-f", `{{join .CgoFiles " "}}`, ".")
	list.Dir, list.Env = dir, env
	out, err := list.Output()
	if err != nil {
		return fmt.Errorf("list cgo files: %w", err)
	}
	files := strings.Fields(string(out))
	if len(files) == 0 {
		return errors.New("no cgo files")
	}
	args := append([]string{"tool", "cgo", "-objdir", work, "-importpath", "github.com/tommie/v8go", "--", "-I."}, files...)
	cgo := exec.CommandContext(ctx, "go", args...)
	cgo.Dir, cgo.Env = dir, env
	if out, err := cgo.CombinedOutput(); err != nil {
		return fmt.Errorf("cgo: %w\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(work, "_cgo_export.h")); err != nil {
		return fmt.Errorf("cgo produced no export header: %w", err)
	}
	return nil
}
