package artemis

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// workflow_journal.go persists a workflow run as an append-only JSONL step
// journal (spec L4559-L4561, durable resume). Every step writes an intent
// before its effect and a receipt after it; each line is fsynced, so after a
// crash the journal tells which steps verified, which failed without effect,
// and which were in flight. An unterminated tail is a torn append and is
// dropped; any other malformed line or a definition change refuses resume.

// Journal step states.
const (
	journalRun     = "run"
	journalIntent  = "intent"
	journalReceipt = "receipt"

	// WorkflowStepUncertain marks a destructive step whose effect cannot be
	// verified after interruption; resume stops there instead of repeating it.
	WorkflowStepUncertain = "uncertain"
)

type journalRecord struct {
	Kind       string    `json:"kind"`
	Definition string    `json:"definition,omitempty"`
	Name       string    `json:"name,omitempty"`
	Step       int       `json:"step"`
	Status     string    `json:"status,omitempty"`
	URL        string    `json:"url,omitempty"`
	Time       time.Time `json:"time"`
}

type workflowJournal struct {
	path string
}

func definitionHash(def WorkflowDefinition) (string, error) {
	encoded, err := json.Marshal(def)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// openWorkflowJournal loads an existing journal for def or starts one.
func openWorkflowJournal(path string, def WorkflowDefinition) (*workflowJournal, []journalRecord, error) {
	hash, err := definitionHash(def)
	if err != nil {
		return nil, nil, fmt.Errorf("workflow journal: %w", err)
	}
	journal := &workflowJournal{path: filepath.Clean(path)}
	records, err := journal.load()
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		if err := os.MkdirAll(filepath.Dir(journal.path), 0o700); err != nil {
			return nil, nil, fmt.Errorf("workflow journal: %w", err)
		}
		if err := journal.append(journalRecord{Kind: journalRun, Definition: hash, Name: def.Name}); err != nil {
			return nil, nil, err
		}
		return journal, nil, nil
	}
	if records[0].Kind != journalRun || records[0].Definition != hash {
		return nil, nil, errors.New("workflow journal: belongs to a different workflow definition")
	}
	return journal, records[1:], nil
}

func (j *workflowJournal) load() ([]journalRecord, error) {
	data, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("workflow journal: %w", err)
	}
	lines := bytes.Split(data, []byte{'\n'})
	torn := len(lines[len(lines)-1]) > 0
	lines = lines[:len(lines)-1]
	records := make([]journalRecord, 0, len(lines))
	for index, line := range lines {
		var record journalRecord
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("workflow journal line %d is corrupt: %w", index+1, err)
		}
		switch record.Kind {
		case journalRun, journalIntent, journalReceipt:
		default:
			return nil, fmt.Errorf("workflow journal line %d: unknown record %q", index+1, record.Kind)
		}
		records = append(records, record)
	}
	if torn {
		if err := j.rewrite(records); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (j *workflowJournal) rewrite(records []journalRecord) error {
	var buffer bytes.Buffer
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("workflow journal: %w", err)
		}
		buffer.Write(append(line, '\n'))
	}
	temporary := j.path + ".tmp"
	if err := os.WriteFile(temporary, buffer.Bytes(), 0o600); err != nil {
		return fmt.Errorf("workflow journal: %w", err)
	}
	if err := os.Rename(temporary, j.path); err != nil {
		return fmt.Errorf("workflow journal: %w", err)
	}
	return syncWorkflowDir(filepath.Dir(j.path))
}

// append writes one record durably (file and, on creation, directory sync).
func (j *workflowJournal) append(record journalRecord) error {
	record.Time = time.Now().UTC()
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("workflow journal: %w", err)
	}
	_, statErr := os.Stat(j.path)
	file, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("workflow journal: %w", err)
	}
	_, writeErr := file.Write(append(line, '\n'))
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("workflow journal: %w", err)
	}
	if errors.Is(statErr, os.ErrNotExist) {
		return syncWorkflowDir(filepath.Dir(j.path))
	}
	return nil
}

func syncWorkflowDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("workflow journal: %w", err)
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("workflow journal: %w", err)
	}
	return nil
}

// journalStep is the journaled state of one step: verified, failed,
// uncertain, pending (intent without receipt) or empty (never attempted).
type journalStep struct {
	status string
	url    string
}

func journalSteps(records []journalRecord, steps int) ([]journalStep, error) {
	states := make([]journalStep, steps)
	for _, record := range records {
		if record.Step < 0 || record.Step >= steps {
			return nil, fmt.Errorf("workflow journal: step %d outside the definition", record.Step)
		}
		switch record.Kind {
		case journalIntent:
			states[record.Step] = journalStep{status: "pending"}
		case journalReceipt:
			states[record.Step] = journalStep{status: record.Status, url: record.URL}
		}
	}
	return states, nil
}
