// Package main implements the artemis-smoke harness: it builds
// cmd/artemis, starts the steering server, drives a list of scenarios
// from a YAML spec against real sites via the same WebSocket API that
// external agents use, and writes a per-scenario pass/fail scorecard
// with captured evidence (DOM snapshot, timings, error trail).
package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var sf ScenarioFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
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
		if seen[s.ID] {
			return fmt.Errorf("scenario %q: duplicate id", s.ID)
		}
		seen[s.ID] = true
		if s.Site == "" {
			return fmt.Errorf("scenario %q: missing site", s.ID)
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
		}
	}
	return nil
}
