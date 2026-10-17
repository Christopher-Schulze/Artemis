package fixture

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
)

// NewServerFromCorpus validates all bytes before opening a listener and binds
// only the corresponding built-in handlers to the admitted descriptors.
func NewServerFromCorpus(data []byte) (*Server, error) {
	scenarios, err := DecodeCorpus(data)
	if err != nil {
		return nil, err
	}
	server := NewServer()
	if err := server.RegisterAll(scenarios); err != nil {
		server.Close()
		return nil, err
	}
	server.mu.Lock()
	server.corpusData = append([]byte(nil), data...)
	server.corpusScenarios = scenarios
	server.mu.Unlock()
	return server, nil
}

// CorpusScenarios preserves the admitted execution order and detached assertions.
func (s *Server) CorpusScenarios() ([]Scenario, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || len(s.corpusData) == 0 || len(s.scenarios) != len(s.corpusScenarios) {
		return nil, fmt.Errorf("fixture corpus: live unchanged corpus owner required")
	}
	out := make([]Scenario, len(s.corpusScenarios))
	for index, scenario := range s.corpusScenarios {
		out[index] = cloneScenario(scenario)
	}
	return out, nil
}

// CorpusManifest binds the runner's exact requested input to what this live
// server admitted. Added routes or mutated assertions cannot acquire that proof.
func (s *Server) CorpusManifest(runner string, requested Scenario) (fixtureasset.Manifest, error) {
	prepared, _, err := prepareScenario(requested)
	if err != nil {
		return fixtureasset.Manifest{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || len(s.corpusData) == 0 || len(s.scenarios) != len(s.corpusScenarios) {
		return fixtureasset.Manifest{}, fmt.Errorf("fixture corpus: live unchanged corpus owner required")
	}
	registered, found := s.scenarios[prepared.Path]
	if !found {
		return fixtureasset.Manifest{}, fmt.Errorf("fixture corpus: requested route not admitted")
	}
	actual, err := json.Marshal(corpusCase{Scenario: registered, HasHandler: registered.Handler != nil})
	if err != nil {
		return fixtureasset.Manifest{}, err
	}
	wanted, err := json.Marshal(corpusCase{Scenario: prepared, HasHandler: prepared.Handler != nil})
	if err != nil || !bytes.Equal(actual, wanted) {
		return fixtureasset.Manifest{}, fmt.Errorf("fixture corpus: requested descriptor or assertions differ")
	}
	return corpusManifest(s.corpusData, runner, registered)
}
