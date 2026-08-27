package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	artemis "github.com/Christopher-Schulze/Artemis"
)

func TestCapabilitiesCommandUsesCanonicalRegistry(t *testing.T) {
	var output bytes.Buffer
	if err := printCapabilities(&output); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Version      string               `json:"version"`
		Capabilities []artemis.Capability `json:"capabilities"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if payload.Version != artemis.Version {
		t.Fatalf("version = %q, want %q", payload.Version, artemis.Version)
	}
	if len(payload.Capabilities) != len(artemis.Capabilities()) {
		t.Fatalf("capability count = %d, want %d", len(payload.Capabilities), len(artemis.Capabilities()))
	}
}

func TestHelpReportsUnavailableCapabilities(t *testing.T) {
	var output bytes.Buffer
	if err := printUsage(&output); err != nil {
		t.Fatalf("print usage: %v", err)
	}
	help := output.String()
	for _, required := range []string{artemis.Version, "agent.high_level", "chromium.cdp", string(artemis.SupportUnavailable)} {
		if !strings.Contains(help, required) {
			t.Fatalf("help missing %q", required)
		}
	}
}
