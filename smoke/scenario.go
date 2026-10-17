// Package smoke implements the artemis-smoke harness: it builds
// cmd/artemis, starts the steering server, drives a list of scenarios
// from a YAML spec against real sites via the same WebSocket API that
// external agents use, and writes a per-scenario pass/fail scorecard
// with captured evidence (DOM snapshot, timings, error trail).
package smoke

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
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
	Version          string     `yaml:"version"`
	Scenarios        []Scenario `yaml:"scenarios"`
	sourceSHA256     string
	normalizedSHA256 string
	source           []byte
	fixture          fixtureasset.Manifest
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
	data, err := readSmokeInput(path, maxScenarioFileBytes)
	if err != nil {
		return nil, err
	}
	return decodeScenarios(data)
}

func readSmokeInput(path string, limit int64) ([]byte, error) {
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("read %s: input file exceeds %d bytes", path, limit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.Join(fmt.Errorf("invalid opened scenario file"), statErr, file.Close())
	}
	data, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, fmt.Errorf("read/close scenario file: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("scenario file grew beyond byte limit")
	}
	return data, nil
}

func decodeScenarios(data []byte) (*ScenarioFile, error) {
	if int64(len(data)) > maxScenarioFileBytes {
		return nil, fmt.Errorf("scenario byte limit exceeded")
	}
	var sf ScenarioFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&sf); err != nil {
		return nil, fmt.Errorf("parse scenario: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parse scenario: multiple YAML documents are not allowed")
		}
		return nil, fmt.Errorf("parse scenario: trailing document: %w", err)
	}
	if err := sf.Validate(); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	sf.sourceSHA256 = hex.EncodeToString(digest[:])
	normalized, err := sf.matrixDigest()
	if err != nil {
		return nil, err
	}
	sf.normalizedSHA256 = normalized
	sf.source = append([]byte(nil), data...)
	sf.fixture = sf.defaultFixtureManifest()
	if _, err := sf.fixtureAsset(); err != nil {
		return nil, err
	}
	return &sf, nil
}

func (sf *ScenarioFile) matrixDigest() (string, error) {
	raw, err := json.Marshal(sf)
	if err != nil {
		return "", err
	}
	if int64(len(raw)) > maxScenarioFileBytes {
		return "", fmt.Errorf("normalized scenario matrix exceeds byte limit")
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// Validate checks the scenario file for structural correctness.
func (sf *ScenarioFile) Validate() error {
	if sf == nil || sf.Version == "" || len(sf.Version) > 128 {
		return fmt.Errorf("scenario file: missing version")
	}
	if len(sf.Scenarios) == 0 || len(sf.Scenarios) > 128 {
		return fmt.Errorf("scenario file: no scenarios")
	}
	seen := make(map[string]bool, len(sf.Scenarios))
	steps := 0
	for i := range sf.Scenarios {
		s := &sf.Scenarios[i]
		if s.ID == "" || len(s.ID) > 128 {
			return fmt.Errorf("scenario[%d]: missing id", i)
		}
		if sanitize(s.ID) != s.ID {
			return fmt.Errorf("scenario[%d]: id %q must contain only letters, digits, hyphen or underscore", i, s.ID)
		}
		if seen[s.ID] {
			return fmt.Errorf("scenario %q: duplicate id", s.ID)
		}
		seen[s.ID] = true
		if s.Site == "" || len(s.Site) > 4096 || len(s.Description) > 4096 || len(s.Requires) > 32 {
			return fmt.Errorf("scenario %q: missing site", s.ID)
		}
		if s.NetworkFail != "" && s.NetworkFail != "tolerant" && s.NetworkFail != "strict" {
			return fmt.Errorf("scenario %q: network_fail must be tolerant or strict", s.ID)
		}
		for _, key := range s.Requires {
			if key == "" || len(key) > 256 || strings.ContainsAny(key, "=\x00") {
				return fmt.Errorf("scenario %q: requires contains an invalid environment key", s.ID)
			}
		}
		if s.Timeout < 0 {
			return fmt.Errorf("scenario %q: timeout must not be negative", s.ID)
		}
		steps += len(s.Steps)
		if len(s.Steps) == 0 || len(s.Steps) > 256 || steps > 4096 {
			return fmt.Errorf("scenario %q: no steps", s.ID)
		}
		for j := range s.Steps {
			st := &s.Steps[j]
			if st.Cmd == "" {
				return fmt.Errorf("scenario %q step[%d]: missing cmd", s.ID, j)
			}
			if st.Name == "" || len(st.Name) > 128 {
				return fmt.Errorf("scenario %q step[%d]: missing name", s.ID, j)
			}
			if !supportedStepCommands[st.Cmd] {
				return fmt.Errorf("scenario %q step[%d]: unsupported cmd %q", s.ID, j, st.Cmd)
			}
		}
	}
	return nil
}
