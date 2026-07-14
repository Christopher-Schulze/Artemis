// Package main implements the artemis-release artifact generator.
//
// It produces checksums, a Go-module SBOM (CycloneDX format), and a license
// report from the artemis source tree without requiring external tooling.
// The output is deterministic and reproducible from a clean checkout.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const sbomFormat = "CycloneDX"

type sbomComponent struct {
	BOMRef  string `json:"bom-ref" xml:"bom-ref,attr"`
	Type    string `json:"type" xml:"type,attr"`
	Name    string `json:"name" xml:"name"`
	Version string `json:"version" xml:"version"`
	Purl    string `json:"purl,omitempty" xml:"purl,omitempty"`
}

type cycloneDXBOM struct {
	XMLName      xml.Name        `xml:"bom"`
	XMLNS        string          `xml:"xmlns,attr"`
	Version      int             `xml:"version,attr"`
	SerialNumber string          `xml:"serialNumber,attr"`
	Components   []sbomComponent `xml:"components>component"`
}

type checksumEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type releaseManifest struct {
	Version     string          `json:"version"`
	Commit      string          `json:"commit"`
	GeneratedAt string          `json:"generated_at"`
	GoVersion   string          `json:"go_version"`
	OS          string          `json:"os"`
	Arch        string          `json:"arch"`
	Checksums   []checksumEntry `json:"checksums"`
	SBOMFormat  string          `json:"sbom_format"`
	License     string          `json:"license"`
}

func main() {
	version := flag.String("version", "0.0.0-dev", "release version")
	outputDir := flag.String("output", "dist", "output directory for artifacts")
	flag.Parse()

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", *outputDir, err)
		os.Exit(1)
	}

	commit := gitCommit()
	goVersion := runtime.Version()

	checksums, err := produceChecksums(*outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checksums: %v\n", err)
		os.Exit(1)
	}

	if err := produceSBOM(*outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "sbom: %v\n", err)
		os.Exit(1)
	}

	if err := produceLicenseReport(*outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "license report: %v\n", err)
		os.Exit(1)
	}

	manifest := releaseManifest{
		Version:     *version,
		Commit:      commit,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		GoVersion:   goVersion,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Checksums:   checksums,
		SBOMFormat:  sbomFormat,
		License:     "MIT",
	}
	manifestPath := filepath.Join(*outputDir, "release-manifest.json")
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal manifest: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write manifest: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("==> Release artifacts written to %s\n", *outputDir)
	fmt.Printf("    checksums.txt (%d files)\n", len(checksums))
	fmt.Printf("    sbom.cdx.xml\n")
	fmt.Printf("    license-report.txt\n")
	fmt.Printf("    release-manifest.json\n")
}

func gitCommit() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func produceChecksums(outputDir string) ([]checksumEntry, error) {
	out, err := exec.Command("git", "ls-files").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	sort.Strings(files)

	var entries []checksumEntry
	var buf strings.Builder
	for _, f := range files {
		if f == "" {
			continue
		}
		h, err := hashFile(f)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", f, err)
		}
		entry := checksumEntry{Path: f, SHA256: h}
		entries = append(entries, entry)
		fmt.Fprintf(&buf, "%s  %s\n", h, f)
	}
	checksumPath := filepath.Join(outputDir, "checksums.txt")
	if err := os.WriteFile(checksumPath, []byte(buf.String()), 0o644); err != nil {
		return nil, fmt.Errorf("write checksums: %w", err)
	}
	return entries, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func produceSBOM(outputDir string) error {
	out, err := exec.Command("go", "list", "-m", "-json", "all").Output()
	if err != nil {
		return fmt.Errorf("go list -m -json all: %w", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	var components []sbomComponent
	for dec.More() {
		var mod struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
			Replace *struct {
				Path    string `json:"Path"`
				Version string `json:"Version"`
			} `json:"Replace,omitempty"`
		}
		if err := dec.Decode(&mod); err != nil {
			return fmt.Errorf("decode module: %w", err)
		}
		name := mod.Path
		ver := mod.Version
		if mod.Replace != nil {
			name = mod.Replace.Path
			ver = mod.Replace.Version
		}
		if ver == "" {
			continue
		}
		components = append(components, sbomComponent{
			BOMRef:  fmt.Sprintf("pkg:golang/%s@%s", name, ver),
			Type:    "library",
			Name:    name,
			Version: ver,
			Purl:    fmt.Sprintf("pkg:golang/%s@%s", name, ver),
		})
	}

	bom := cycloneDXBOM{
		XMLNS:        "http://cyclonedx.org/schema/bom/1.4",
		Version:      1,
		SerialNumber: fmt.Sprintf("urn:uuid:%d", time.Now().UnixNano()),
		Components:   components,
	}

	sbomPath := filepath.Join(outputDir, "sbom.cdx.xml")
	data, err := xml.MarshalIndent(bom, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sbom: %w", err)
	}
	header := []byte(xml.Header)
	if err := os.WriteFile(sbomPath, append(header, data...), 0o644); err != nil {
		return fmt.Errorf("write sbom: %w", err)
	}
	return nil
}

func produceLicenseReport(outputDir string) error {
	var buf strings.Builder
	buf.WriteString("Artemis License Report\n")
	buf.WriteString("======================\n\n")
	buf.WriteString("Project license: MIT (see LICENSE)\n\n")
	buf.WriteString("Vendored dependencies:\n\n")

	v8License := "BSD-style (see third_party/v8go/LICENSE)"
	if data, err := os.ReadFile("third_party/v8go/LICENSE"); err == nil {
		firstLine := strings.SplitN(string(data), "\n", 2)[0]
		v8License = firstLine
	}
	fmt.Fprintf(&buf, "  - rogchap.com/v8go (vendored fork): %s\n", v8License)
	fmt.Fprintf(&buf, "    Provenance: third_party/v8go/PROVENANCE.md\n")
	fmt.Fprintf(&buf, "    Patches: third_party/v8go/ARTEMIS_PATCHES.md\n\n")

	buf.WriteString("Go module dependencies (from go.mod):\n")
	out, err := exec.Command("go", "list", "-m", "all").Output()
	if err != nil {
		buf.WriteString("  (go list failed; see go.mod for direct dependencies)\n")
	} else {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			fmt.Fprintf(&buf, "  - %s\n", line)
		}
	}

	reportPath := filepath.Join(outputDir, "license-report.txt")
	if err := os.WriteFile(reportPath, []byte(buf.String()), 0o644); err != nil {
		return fmt.Errorf("write license report: %w", err)
	}
	return nil
}
