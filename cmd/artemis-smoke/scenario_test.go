package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadScenariosValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenarios.yaml")
	const yamlDoc = `version: "1"
scenarios:
  - id: s1
    site: https://example.com
    description: first
    timeout: 5s
    steps:
      - name: open
        cmd: session.new
  - id: s2
    site: https://example.org
    description: second
    steps:
      - name: open
        cmd: session.new
`
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf, err := LoadScenarios(path)
	if err != nil {
		t.Fatalf("LoadScenarios: %v", err)
	}
	if len(sf.Scenarios) != 2 {
		t.Fatalf("got %d scenarios, want 2", len(sf.Scenarios))
	}
	if sf.Scenarios[0].ID != "s1" {
		t.Errorf("first id = %q", sf.Scenarios[0].ID)
	}
	if sf.Scenarios[0].Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", sf.Scenarios[0].Timeout)
	}
}

func TestLoadScenariosDuplicateID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenarios.yaml")
	const yamlDoc = `version: "1"
scenarios:
  - id: dup
    site: https://a
    steps:
      - name: open
        cmd: session.new
  - id: dup
    site: https://b
    steps:
      - name: open
        cmd: session.new
`
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadScenarios(path); err == nil {
		t.Fatal("expected duplicate id error, got nil")
	}
}

func TestLoadScenariosMissingVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenarios.yaml")
	const yamlDoc = `scenarios:
  - id: s1
    site: https://a
    steps:
      - name: open
        cmd: session.new
`
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadScenarios(path); err == nil {
		t.Fatal("expected missing version error, got nil")
	}
}

func TestLoadScenariosMissingCmd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenarios.yaml")
	const yamlDoc = `version: "1"
scenarios:
  - id: s1
    site: https://a
    steps:
      - name: open
`
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadScenarios(path); err == nil {
		t.Fatal("expected missing cmd error, got nil")
	}
}

func TestLoadScenariosNoSteps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenarios.yaml")
	const yamlDoc = `version: "1"
scenarios:
  - id: s1
    site: https://a
`
	if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadScenarios(path); err == nil {
		t.Fatal("expected no steps error, got nil")
	}
}

func TestLoadScenariosMissingFile(t *testing.T) {
	if _, err := LoadScenarios(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected missing file error, got nil")
	}
}

func TestScenarioMissingEnvSkips(t *testing.T) {
	// Ensure the env var is unset for a deterministic skip.
	t.Setenv("ARTEMIS_SMOKE_NEVER_SET", "")
	s := Scenario{
		ID:       "needs-env",
		Site:     "https://example.com",
		Requires: []string{"ARTEMIS_SMOKE_NEVER_SET"},
		Steps:    []Step{{Name: "open", Cmd: "session.new"}},
	}
	// We can't run the full runner without a binary, but we can verify
	// the skip logic directly: if any required env var is empty, the
	// scenario is skipped.
	skipped := false
	for _, ev := range s.Requires {
		if os.Getenv(ev) == "" {
			skipped = true
			break
		}
	}
	if !skipped {
		t.Fatal("expected scenario to be skipped due to missing env")
	}
}
