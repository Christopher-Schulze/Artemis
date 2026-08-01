package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	artemisobserve "github.com/Christopher-Schulze/Artemis/observe"
)

func TestParseObserveFormat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    observeFormat
		wantErr bool
	}{
		{"default json", "json", formatEvidence, false},
		{"har", "har", formatHAR, false},
		{"ndjson", "ndjson", formatNDJSON, false},
		{"uppercase", "HAR", formatHAR, false},
		{"padded", "  ndjson  ", formatNDJSON, false},
		{"unknown", "xml", "", true},
		{"empty", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseObserveFormat(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseObserveFormat(%q) = %q, want an error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseObserveFormat(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("parseObserveFormat(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// observeFixture returns evidence carrying two network events.
func observeFixture() artemisobserve.ObservationEvidence {
	start := time.Date(2026, 3, 4, 10, 30, 0, 0, time.UTC)
	return artemisobserve.ObservationEvidence{
		Schema:     "artemis.observation.v1",
		CapturedAt: start,
		Network: []artemisobserve.NetworkEvent{
			{RequestID: "r1", URL: "https://fixture.test/a", Method: "GET", Status: 200, Bytes: 12, Start: start, DurationMS: 8},
			{RequestID: "r2", URL: "https://fixture.test/b", Method: "POST", Status: 201, Bytes: 34, Start: start.Add(time.Second), DurationMS: 16},
		},
	}
}

// TestWriteObserveOutputHAR proves the CLI reaches the spec-mandated HAR
// formatter and carries every captured event into the archive.
func TestWriteObserveOutputHAR(t *testing.T) {
	evidence := observeFixture()
	var out bytes.Buffer

	if err := writeObserveOutput(&out, formatHAR, evidence); err != nil {
		t.Fatalf("writeObserveOutput HAR: %v", err)
	}
	// The CLI must emit a loadable HAR file: the log nested under "log".
	var document struct {
		Log struct {
			Version string `json:"version"`
			Entries []struct {
				Request struct {
					URL string `json:"url"`
				} `json:"request"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("unmarshal HAR: %v", err)
	}
	log := document.Log
	if log.Version != "1.2" {
		t.Fatalf("HAR version = %q, want 1.2", log.Version)
	}
	if len(log.Entries) != len(evidence.Network) {
		t.Fatalf("HAR entries = %d, want %d", len(log.Entries), len(evidence.Network))
	}
	for i, entry := range log.Entries {
		if entry.Request.URL != evidence.Network[i].URL {
			t.Fatalf("entry %d URL = %q, want %q", i, entry.Request.URL, evidence.Network[i].URL)
		}
	}
}

// TestWriteObserveOutputNDJSON proves the CLI emits one decodable event per
// line rather than the full evidence document.
func TestWriteObserveOutputNDJSON(t *testing.T) {
	evidence := observeFixture()
	var out bytes.Buffer

	if err := writeObserveOutput(&out, formatNDJSON, evidence); err != nil {
		t.Fatalf("writeObserveOutput NDJSON: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != len(evidence.Network) {
		t.Fatalf("NDJSON lines = %d, want %d", len(lines), len(evidence.Network))
	}
	for i, line := range lines {
		var event artemisobserve.NetworkEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %d is not valid JSON: %v", i, err)
		}
		if event.RequestID != evidence.Network[i].RequestID {
			t.Fatalf("line %d requestId = %q, want %q", i, event.RequestID, evidence.Network[i].RequestID)
		}
	}
}

// TestWriteObserveOutputEvidenceDefault proves the default path still emits
// the full observation document, not just its network slice.
func TestWriteObserveOutputEvidenceDefault(t *testing.T) {
	evidence := observeFixture()
	var out bytes.Buffer

	if err := writeObserveOutput(&out, formatEvidence, evidence); err != nil {
		t.Fatalf("writeObserveOutput evidence: %v", err)
	}
	var decoded artemisobserve.ObservationEvidence
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}
	if decoded.Schema != evidence.Schema {
		t.Fatalf("schema = %q, want %q", decoded.Schema, evidence.Schema)
	}
	if len(decoded.Network) != len(evidence.Network) {
		t.Fatalf("network = %d, want %d", len(decoded.Network), len(evidence.Network))
	}
}

// TestCmdObserveRejectsUnknownFormat proves a bad --format value fails with a
// usage exit before any browser is launched.
func TestCmdObserveRejectsUnknownFormat(t *testing.T) {
	if code := cmdObserve([]string{"-format", "xml", "https://fixture.test"}); code != 2 {
		t.Fatalf("cmdObserve exit = %d, want 2", code)
	}
}
