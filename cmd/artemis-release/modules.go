package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type goModuleWire struct {
	Path     string        `json:"Path"`
	Version  string        `json:"Version"`
	Main     bool          `json:"Main"`
	Indirect bool          `json:"Indirect"`
	Dir      string        `json:"Dir"`
	Sum      string        `json:"Sum"`
	GoModSum string        `json:"GoModSum"`
	Replace  *goModuleWire `json:"Replace"`
	Error    *struct {
		Err string `json:"Err"`
	} `json:"Error"`
}

type goModuleDownloadWire struct {
	Path     string `json:"Path"`
	Version  string `json:"Version"`
	Dir      string `json:"Dir"`
	Sum      string `json:"Sum"`
	GoModSum string `json:"GoModSum"`
	Error    string `json:"Error"`
}

func loadModuleInventory(ctx context.Context, sourceRoot, version string, runner commandRunner) (moduleInventory, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if _, err := runner.Output(ctx, sourceRoot, "go", "mod", "tidy", "-diff"); err != nil {
		return moduleInventory{}, fmt.Errorf("verify immutable module metadata: %w", err)
	}
	listed, err := runner.Output(ctx, sourceRoot, "go", "list", "-m", "-json", "-mod=readonly", "all")
	if err != nil {
		return moduleInventory{}, fmt.Errorf("list module graph: %w", err)
	}
	modules, err := decodeModules(listed, version)
	if err != nil {
		return moduleInventory{}, err
	}
	if err = hydrateModuleSources(ctx, sourceRoot, modules, runner); err != nil {
		return moduleInventory{}, err
	}
	graph, err := runner.Output(ctx, sourceRoot, "go", "mod", "graph")
	if err != nil {
		return moduleInventory{}, fmt.Errorf("read module dependency graph: %w", err)
	}
	edges, err := parseModuleGraph(graph, modules)
	if err != nil {
		return moduleInventory{}, err
	}
	if err := attachModuleDigests(ctx, sourceRoot, modules, runner); err != nil {
		return moduleInventory{}, err
	}
	return newModuleInventory(modules, edges)
}

func decodeModules(data []byte, releaseVersion string) ([]goModule, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var modules []goModule
	for {
		var wire goModuleWire
		err := decoder.Decode(&wire)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode module inventory: %w", err)
		}
		module, err := normalizeModule(wire, releaseVersion)
		if err != nil {
			return nil, err
		}
		modules = append(modules, module)
	}
	if len(modules) == 0 {
		return nil, errors.New("module inventory is empty")
	}
	sort.Slice(modules, func(i, j int) bool { return moduleGraphKey(modules[i]) < moduleGraphKey(modules[j]) })
	return modules, nil
}

func normalizeModule(wire goModuleWire, releaseVersion string) (goModule, error) {
	if wire.Path == "" {
		return goModule{}, errors.New("module path required")
	}
	if wire.Error != nil && wire.Error.Err != "" {
		return goModule{}, fmt.Errorf("module %s: %s", wire.Path, wire.Error.Err)
	}
	module := goModule{Path: wire.Path, Version: wire.Version, Main: wire.Main, Indirect: wire.Indirect, Dir: wire.Dir, Sum: wire.Sum, GoModSum: wire.GoModSum}
	if module.Main {
		module.Version = releaseVersion
	} else if module.Version == "" {
		return goModule{}, fmt.Errorf("module %s has no resolved version", module.Path)
	}
	if wire.Replace != nil {
		replacement, err := normalizeReplacement(*wire.Replace)
		if err != nil {
			return goModule{}, fmt.Errorf("module %s replacement: %w", module.Path, err)
		}
		module.Replace = &replacement
	}
	if module.Main && module.Dir == "" {
		return goModule{}, fmt.Errorf("module %s has no resolved source directory", module.Path)
	}
	return module, nil
}

func normalizeReplacement(wire goModuleWire) (goModule, error) {
	if wire.Path == "" {
		return goModule{}, errors.New("path required")
	}
	return goModule{Path: wire.Path, Version: wire.Version, Dir: wire.Dir, Sum: wire.Sum, GoModSum: wire.GoModSum}, nil
}

func hydrateModuleSources(ctx context.Context, sourceRoot string, modules []goModule, runner commandRunner) error {
	queries := moduleDownloadQueries(modules)
	if len(queries) == 0 {
		return nil
	}
	args := append([]string{"mod", "download", "-json"}, queries...)
	output, err := runner.Output(ctx, sourceRoot, "go", args...)
	if err != nil {
		return fmt.Errorf("download selected module sources: %w", err)
	}
	downloads, err := decodeModuleDownloads(output)
	if err != nil {
		return err
	}
	for index := range modules {
		if err := hydrateModule(&modules[index], downloads); err != nil {
			return err
		}
	}
	return nil
}

func moduleDownloadQueries(modules []goModule) []string {
	queries := make([]string, 0, len(modules))
	for _, module := range modules {
		switch {
		case module.Main:
			continue
		case module.Replace != nil && module.Replace.Version != "":
			queries = append(queries, module.Replace.Path+"@"+module.Replace.Version)
		case module.Replace == nil:
			queries = append(queries, module.Path+"@"+module.Version)
		}
	}
	return sortedUnique(queries)
}

func decodeModuleDownloads(data []byte) (map[string]goModuleDownloadWire, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	downloads := make(map[string]goModuleDownloadWire)
	for {
		var download goModuleDownloadWire
		err := decoder.Decode(&download)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode selected module download: %w", err)
		}
		if download.Error != "" || download.Path == "" || download.Version == "" || download.Dir == "" || download.Sum == "" || download.GoModSum == "" {
			return nil, fmt.Errorf("module download evidence incomplete for %s@%s: %s", download.Path, download.Version, download.Error)
		}
		key := download.Path + "@" + download.Version
		if _, duplicate := downloads[key]; duplicate {
			return nil, fmt.Errorf("duplicate module download evidence for %s", key)
		}
		downloads[key] = download
	}
	return downloads, nil
}

func hydrateModule(module *goModule, downloads map[string]goModuleDownloadWire) error {
	if module.Main {
		return nil
	}
	target := module
	if module.Replace != nil {
		if module.Replace.Version == "" {
			if module.Replace.Dir == "" {
				return fmt.Errorf("local replacement %s has no source directory", module.Replace.Path)
			}
			return nil
		}
		target = module.Replace
	}
	key := target.Path + "@" + target.Version
	download, ok := downloads[key]
	if !ok {
		return fmt.Errorf("selected module download evidence missing for %s", key)
	}
	target.Dir = download.Dir
	target.Sum = download.Sum
	target.GoModSum = download.GoModSum
	if module.Replace == nil {
		module.Dir = download.Dir
		module.Sum = download.Sum
		module.GoModSum = download.GoModSum
	}
	return nil
}

func parseModuleGraph(data []byte, modules []goModule) (map[string][]string, error) {
	known, selectedByPath, err := selectedModuleKeys(modules)
	if err != nil {
		return nil, err
	}
	edges := make(map[string][]string, len(modules))
	for key := range known {
		edges[key] = nil
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, errors.New("module dependency graph is empty")
	}
	for lineNumber, line := range strings.Split(trimmed, "\n") {
		if err := addModuleGraphEdge(edges, known, selectedByPath, lineNumber+1, line); err != nil {
			return nil, err
		}
	}
	for key := range edges {
		edges[key] = sortedUnique(edges[key])
	}
	return edges, nil
}

func selectedModuleKeys(modules []goModule) (map[string]struct{}, map[string]string, error) {
	known := make(map[string]struct{}, len(modules))
	selectedByPath := make(map[string]string, len(modules))
	for _, module := range modules {
		key := moduleGraphKey(module)
		known[key] = struct{}{}
		if _, duplicate := selectedByPath[module.Path]; duplicate {
			return nil, nil, fmt.Errorf("module graph has multiple selected versions for %s", module.Path)
		}
		selectedByPath[module.Path] = key
	}
	return known, selectedByPath, nil
}

func addModuleGraphEdge(edges map[string][]string, known map[string]struct{}, selectedByPath map[string]string, lineNumber int, line string) error {
	fields := strings.Fields(line)
	if len(fields) != 2 {
		return fmt.Errorf("module graph line %d must contain one edge", lineNumber)
	}
	if _, ok := known[fields[0]]; !ok {
		return nil
	}
	dependencyPath, err := moduleTokenPath(fields[1])
	if err != nil {
		return fmt.Errorf("module graph line %d dependency: %w", lineNumber, err)
	}
	if dependencyPath == "go" || dependencyPath == "toolchain" {
		return nil
	}
	selected, ok := selectedByPath[dependencyPath]
	if !ok {
		return fmt.Errorf("module graph line %d references unselected dependency %s", lineNumber, fields[1])
	}
	edges[fields[0]] = append(edges[fields[0]], selected)
	return nil
}

func moduleTokenPath(token string) (string, error) {
	separator := strings.LastIndexByte(token, '@')
	if separator <= 0 || separator == len(token)-1 {
		return "", fmt.Errorf("invalid module token %q", token)
	}
	return token[:separator], nil
}

func attachModuleDigests(ctx context.Context, sourceRoot string, modules []goModule, runner commandRunner) error {
	for index := range modules {
		module := &modules[index]
		if module.Main {
			digest, err := gitTreeDigest(ctx, sourceRoot, ".", runner)
			if err != nil {
				return fmt.Errorf("main module tree: %w", err)
			}
			module.Sum = digest
		}
		if module.Replace != nil {
			digest, err := replacementDigest(ctx, sourceRoot, *module.Replace, runner)
			if err != nil {
				return fmt.Errorf("module %s replacement digest: %w", module.Path, err)
			}
			module.Replace.Sum = digest
		}
	}
	return nil
}

func replacementDigest(ctx context.Context, sourceRoot string, replacement goModule, runner commandRunner) (string, error) {
	if replacement.Version != "" {
		return goChecksumHex(firstNonEmpty(replacement.Sum, replacement.GoModSum))
	}
	relative, err := filepath.Rel(sourceRoot, replacement.Dir)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("local replacement must be inside the clean source root")
	}
	return gitTreeDigest(ctx, sourceRoot, relative, runner)
}

func gitTreeDigest(ctx context.Context, sourceRoot, relative string, runner commandRunner) (string, error) {
	gitRoot, err := gitRootForSource(ctx, sourceRoot, runner)
	if err != nil {
		return "", err
	}
	rootRelative, err := filepath.Rel(gitRoot, sourceRoot)
	if err != nil || strings.HasPrefix(rootRelative, ".."+string(filepath.Separator)) {
		return "", errors.New("module root is outside the Git worktree")
	}
	objectPath := filepath.Clean(filepath.Join(rootRelative, relative))
	if objectPath == "." {
		objectPath = ""
	}
	object := "HEAD^{tree}"
	if objectPath != "" {
		object = "HEAD:" + filepath.ToSlash(objectPath)
	}
	out, err := runner.Output(ctx, sourceRoot, "git", "rev-parse", object)
	if err != nil {
		return "", err
	}
	digest := strings.TrimSpace(string(out))
	if len(digest) != 40 && len(digest) != 64 {
		return "", fmt.Errorf("unexpected Git object digest %q", digest)
	}
	return "git:" + digest, nil
}

func newModuleInventory(modules []goModule, edges map[string][]string) (moduleInventory, error) {
	mainPath := ""
	var digestParts []string
	for _, module := range modules {
		if module.Main {
			if mainPath != "" {
				return moduleInventory{}, errors.New("multiple main modules in inventory")
			}
			mainPath = module.Path
		}
		digestParts = append(digestParts, moduleDigestLine(module))
		for _, dependency := range edges[moduleGraphKey(module)] {
			digestParts = append(digestParts, "edge="+moduleGraphKey(module)+"->"+dependency)
		}
	}
	if mainPath == "" {
		return moduleInventory{}, errors.New("main module missing from inventory")
	}
	return moduleInventory{MainPath: mainPath, Modules: modules, Edges: edges, GraphDigest: "sha256:" + stableDigest(digestParts...)}, nil
}

func moduleDigestLine(module goModule) string {
	line := fmt.Sprintf("module=%s|version=%s|main=%t|indirect=%t|sum=%s|gomod=%s", module.Path, module.Version, module.Main, module.Indirect, module.Sum, module.GoModSum)
	if module.Replace != nil {
		line += fmt.Sprintf("|replace=%s@%s|replace_digest=%s", module.Replace.Path, module.Replace.Version, module.Replace.Sum)
	}
	return line
}

func moduleGraphKey(module goModule) string {
	if module.Main {
		return module.Path
	}
	return module.Path + "@" + module.Version
}

func moduleBOMRef(module goModule) string {
	return modulePURL(module.Path, module.Version)
}

func modulePURL(path, version string) string {
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	return "pkg:golang/" + strings.Join(segments, "/") + "@" + url.PathEscape(version) + "?type=module"
}

func goChecksumHex(checksum string) (string, error) {
	if !strings.HasPrefix(checksum, "h1:") {
		return "", errors.New("go checksum is missing h1 prefix")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(checksum, "h1:"))
	if err != nil || len(decoded) != 32 {
		return "", errors.New("go checksum is not a SHA-256 value")
	}
	return "sha256:" + hex.EncodeToString(decoded), nil
}

func sortedUnique(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func collectLicenses(stageRoot string, inventory moduleInventory, generatedAt string, writeFile func(string, []byte, os.FileMode) error) (licenseReport, error) {
	report := licenseReport{SchemaVersion: licenseReportSchema, GeneratedAt: generatedAt}
	for _, module := range inventory.Modules {
		component, err := collectComponentLicenses(stageRoot, module, writeFile)
		if err != nil {
			return licenseReport{}, err
		}
		report.Components = append(report.Components, component)
	}
	return report, nil
}

func collectComponentLicenses(stageRoot string, module goModule, writeFile func(string, []byte, os.FileMode) error) (componentLicenseEvidence, error) {
	sourceDir := module.Dir
	if module.Replace != nil {
		sourceDir = module.Replace.Dir
	}
	files, err := findLicenseFiles(sourceDir)
	if err != nil {
		return componentLicenseEvidence{}, fmt.Errorf("module %s licenses: %w", module.Path, err)
	}
	component := componentLicenseEvidence{BOMRef: moduleBOMRef(module), ModulePath: module.Path, Version: module.Version, Main: module.Main, Indirect: module.Indirect}
	if module.Replace != nil {
		component.Replacement = &moduleReplacement{Path: module.Replace.Path, Version: module.Replace.Version, Digest: module.Replace.Sum}
	}
	for _, source := range files {
		evidence, err := copyLicense(stageRoot, module, source, writeFile)
		if err != nil {
			return componentLicenseEvidence{}, err
		}
		component.Licenses = append(component.Licenses, evidence)
	}
	return component, nil
}

func findLicenseFiles(sourceDir string) ([]string, error) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		name := strings.ToUpper(entry.Name())
		if !strings.HasPrefix(name, "LICENSE") && !strings.HasPrefix(name, "COPYING") && !strings.HasPrefix(name, "NOTICE") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("license evidence %s is not a regular file", entry.Name())
		}
		files = append(files, filepath.Join(sourceDir, entry.Name()))
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, errors.New("no top-level LICENSE, COPYING or NOTICE evidence")
	}
	return files, nil
}

func copyLicense(stageRoot string, module goModule, source string, writeFile func(string, []byte, os.FileMode) error) (licenseEvidence, error) {
	data, err := os.ReadFile(filepath.Clean(source))
	if err != nil {
		return licenseEvidence{}, fmt.Errorf("read %s license: %w", module.Path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return licenseEvidence{}, fmt.Errorf("module %s license %s is empty", module.Path, filepath.Base(source))
	}
	directory := stableDigest(moduleBOMRef(module))[:12] + "-" + sanitizePathToken(module.Path)
	relative := filepath.ToSlash(filepath.Join("licenses", directory, filepath.Base(source)))
	if err := writeFile(filepath.Join(stageRoot, filepath.FromSlash(relative)), data, 0o644); err != nil {
		return licenseEvidence{}, fmt.Errorf("write %s license evidence: %w", module.Path, err)
	}
	spdxID, name := classifyLicense(data)
	return licenseEvidence{Path: relative, SHA256: stableDigestBytes(data), SPDXID: spdxID, Name: name}, nil
}

func classifyLicense(data []byte) (string, string) {
	text := strings.ToLower(string(data))
	hasMIT := strings.Contains(text, "permission is hereby granted, free of charge")
	hasApache := strings.Contains(text, "apache license") && strings.Contains(text, "version 2.0")
	switch {
	case hasMIT && hasApache:
		return "", "MIT OR Apache-2.0"
	case hasMIT:
		return "MIT", "MIT License"
	case hasApache:
		return "Apache-2.0", "Apache License 2.0"
	case strings.Contains(text, "neither the name") && strings.Contains(text, "redistribution and use"):
		return "BSD-3-Clause", "BSD 3-Clause License"
	case strings.Contains(text, "redistribution and use") && strings.Contains(text, "disclaimer"):
		return "BSD-2-Clause", "BSD 2-Clause License"
	case strings.Contains(text, "mozilla public license") && strings.Contains(text, "version 2.0"):
		return "MPL-2.0", "Mozilla Public License 2.0"
	default:
		return "", firstLicenseLine(data)
	}
}

func firstLicenseLine(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.Trim(line, "-=") != "" {
			if len(line) > 160 {
				return line[:160]
			}
			return line
		}
	}
	return "Unclassified license text"
}

func sanitizePathToken(value string) string {
	var builder strings.Builder
	previousDash := false
	for _, char := range strings.ToLower(value) {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			builder.WriteRune(char)
			previousDash = false
		} else if builder.Len() > 0 && !previousDash {
			builder.WriteByte('-')
			previousDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
