// Package main implements the artemis-release artifact generator.
//
// It produces checksums, a Go-module SBOM (CycloneDX format), and a license
// report from the Artemis source tree without requiring an external SBOM tool.
// The output is deterministic and reproducible from a clean checkout.
package main

import (
	"os"
)

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}
