// Package main implements the artemis-smoke harness: it builds
// cmd/artemis, starts the steering server, drives a list of scenarios
// from a YAML spec against real sites via the same WebSocket API that
// external agents use, and writes a per-scenario pass/fail scorecard
// with captured evidence (DOM snapshot, timings, error trail).
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const maxScenarioFileBytes int64 = 1 << 20

var supportedStepCommands = map[string]bool{
	"session.new":        true,
	"session.close":      true,
	"page.open":          true,
	"page.close":         true,
	"page.eval":          true,
	"page.dump":          true,
	"page.click_by_text": true,
	"page.type":          true,
	"page.wait_idle":     true,
	"page.assert":        true,
}

// ScenarioFile is the top-level YAML document.
type ScenarioFile struct {
	Version   string     `yaml:"version"`
	Scenarios []Scenario `yaml:"scenarios"`
}

// Scenario is a single named sequence of steps against one site.
type Scenario struct {
	ID          string        `yaml:"id"`
	Site        string        `yaml:"site"`
	Description string        `yaml:"description"`
	NetworkFail string        `yaml:"network_fail"` // "tolerant" (default) or "strict"
	Timeout     time.Duration `yaml:"timeout"`      // per-step timeout; 0 = runner default
	Requires    []string      `yaml:"requires"`     // env vars that must be present (e.g. login creds)
	Steps       []Step        `yaml:"steps"`
}

// Step is one action/assertion in a scenario.
type Step struct {
	Name   string         `yaml:"name"`
	Cmd    string         `yaml:"cmd"` // session.new|page.open|page.type|page.click_by_text|page.dump|page.assert|page.wait_idle|page.close|session.close
	Params map[string]any `yaml:"params"`
	// AssertPass is only consulted for cmd=page.assert; if true the
	// runner requires the response value's "pass" field to be true.
	AssertPass *bool `yaml:"assert_pass"`
}

// LoadScenarios reads and validates a scenario YAML file.
func LoadScenarios(path string) (*ScenarioFile, error) {
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > maxScenarioFileBytes {
		return nil, fmt.Errorf("read %s: scenario file exceeds %d bytes", path, maxScenarioFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var sf ScenarioFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&sf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parse %s: multiple YAML documents are not allowed", path)
		}
		return nil, fmt.Errorf("parse %s: trailing document: %w", path, err)
	}
	if err := sf.Validate(); err != nil {
		return nil, err
	}
	return &sf, nil
}

// Validate checks the scenario file for structural correctness.
func (sf *ScenarioFile) Validate() error {
	if sf.Version == "" {
		return fmt.Errorf("scenario file: missing version")
	}
	if len(sf.Scenarios) == 0 {
		return fmt.Errorf("scenario file: no scenarios")
	}
	seen := make(map[string]bool, len(sf.Scenarios))
	for i := range sf.Scenarios {
		s := &sf.Scenarios[i]
		if s.ID == "" {
			return fmt.Errorf("scenario[%d]: missing id", i)
		}
		if sanitize(s.ID) != s.ID {
			return fmt.Errorf("scenario[%d]: id %q must contain only letters, digits, hyphen or underscore", i, s.ID)
		}
		if seen[s.ID] {
			return fmt.Errorf("scenario %q: duplicate id", s.ID)
		}
		seen[s.ID] = true
		if s.Site == "" {
			return fmt.Errorf("scenario %q: missing site", s.ID)
		}
		if s.NetworkFail != "" && s.NetworkFail != "tolerant" && s.NetworkFail != "strict" {
			return fmt.Errorf("scenario %q: network_fail must be tolerant or strict", s.ID)
		}
		if s.Timeout < 0 {
			return fmt.Errorf("scenario %q: timeout must not be negative", s.ID)
		}
		if len(s.Steps) == 0 {
			return fmt.Errorf("scenario %q: no steps", s.ID)
		}
		for j := range s.Steps {
			st := &s.Steps[j]
			if st.Cmd == "" {
				return fmt.Errorf("scenario %q step[%d]: missing cmd", s.ID, j)
			}
			if st.Name == "" {
				return fmt.Errorf("scenario %q step[%d]: missing name", s.ID, j)
			}
			if !supportedStepCommands[st.Cmd] {
				return fmt.Errorf("scenario %q step[%d]: unsupported cmd %q", s.ID, j, st.Cmd)
			}
		}
	}
	return nil
}
