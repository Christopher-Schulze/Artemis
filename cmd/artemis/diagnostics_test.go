package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Christopher-Schulze/Artemis/diagnostics"
)

func TestDiagnosticsCommandFiltersAndBoundsOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artemis.jsonl")
	store, err := diagnostics.NewStore(diagnostics.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if policyErr := store.AppendPolicy(diagnostics.PolicyDecision{Operation: "navigation", Transport: "https", Host: "example.test", Port: 443, Result: "allow", ReasonCode: "policy_match"}); policyErr != nil {
		t.Fatal(policyErr)
	}
	if resourceErr := store.AppendResource(diagnostics.ResourceUsage{Scope: "renderless", Requests: 1}); resourceErr != nil {
		t.Fatal(resourceErr)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer
	code := cmdDiagnostics([]string{"--file", path, "--type", "policy", "--limit", "1"})
	if err := writer.Close(); err != nil {
		t.Errorf("diagnostics output writer close: %v", err)
	}
	os.Stdout = oldStdout
	defer closeTestResource(t, "diagnostics output reader close", reader.Close)
	if code != 0 {
		t.Fatalf("diagnostics exit code=%d", code)
	}
	var records []diagnostics.Record
	if err := json.NewDecoder(reader).Decode(&records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Type != diagnostics.RecordPolicyDecision || records[0].Policy == nil {
		t.Fatalf("records=%+v", records)
	}
}

func TestDiagnosticsCommandRejectsInvalidFiltersAndBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artemis.jsonl")
	store, err := diagnostics.NewStore(diagnostics.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendResource(diagnostics.ResourceUsage{Scope: "renderless"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--file", path, "--type", "content"},
		{"--file", path, "--limit", "0"},
		{"--file", path, "--limit", "4097"},
	} {
		if code := cmdDiagnostics(args); code != 2 {
			t.Fatalf("args=%v exit code=%d, want 2", args, code)
		}
	}
}
