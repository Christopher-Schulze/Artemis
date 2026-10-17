package fixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	replaySchemaVersion = 1
	maxReplayBytes      = 16 << 20
	maxReplayTextBytes  = 8 << 20
)

func (r *Replay) seal() error {
	if err := r.validateFields(); err != nil {
		return err
	}
	if err := r.validateRedaction(); err != nil {
		return err
	}
	hash, err := r.digest()
	if err != nil {
		return err
	}
	r.ContentHash = hash
	return nil
}

func (r *Replay) digest() (string, error) {
	copy := *r
	copy.ContentHash = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	if len(data)+64 > maxReplayBytes {
		return "", fmt.Errorf("replay: encoded byte limit exceeded")
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func (r *Replay) validatedBytes() ([]byte, error) {
	if err := r.validateFields(); err != nil {
		return nil, err
	}
	if err := r.validateRedaction(); err != nil {
		return nil, err
	}
	hash, err := r.digest()
	if err != nil {
		return nil, err
	}
	if r.ContentHash != hash {
		return nil, fmt.Errorf("replay: content hash mismatch")
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if len(data) > maxReplayBytes {
		return nil, fmt.Errorf("replay: encoded byte limit exceeded")
	}
	return data, nil
}

func (r *Replay) validateFields() error {
	if r == nil || r.SchemaVersion != replaySchemaVersion || len(r.ID) < 26 || len(r.ID) > 128 || strings.Trim(r.ID, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") != "" {
		return fmt.Errorf("replay: unsupported schema or invalid capture identity")
	}
	if r.ScenarioID == "" || r.ScenarioID != r.Scenario.ID || len(r.Runner) == 0 || len(r.Runner) > 64 || sanitize(r.Runner) != r.Runner {
		return fmt.Errorf("replay: inconsistent scenario or runner identity")
	}
	if r.CapturedAt.IsZero() || r.DurationMs < 0 || r.Result.StatusCode < 100 || r.Result.StatusCode > 599 {
		return fmt.Errorf("replay: missing capture time, invalid duration or unknown HTTP status")
	}
	if r.Scenario.Handler != nil {
		return fmt.Errorf("replay: executable handler cannot be serialized as evidence")
	}
	source := r.Scenario
	// The serialized source retains ordinary fixture bounds. The private
	// projection flag is never a way to publish an unloadable capture.
	source.inertReplay = false
	if _, _, err := prepareScenario(source); err != nil {
		return fmt.Errorf("replay scenario: %w", err)
	}
	if r.Metadata.OS == "" || r.Metadata.Arch == "" || r.Metadata.GoVersion == "" || (r.Redacted && r.Metadata.Hostname != "") {
		return fmt.Errorf("replay: incomplete metadata or retained private hostname")
	}
	if len(r.Result.Links) > 1024 || len(r.RedactionLog) > 128 {
		return fmt.Errorf("replay: link or redaction-log count exceeds limit")
	}
	if err := r.validateCorpusReference(); err != nil {
		return err
	}
	return r.validateTextBounds()
}

func (r *Replay) validateTextBounds() error {
	metadata := []string{r.Metadata.OS, r.Metadata.Arch, r.Metadata.GoVersion, r.Metadata.Hostname, r.Metadata.Browser, r.Metadata.BrowserVersion, r.Metadata.UserAgent, r.Metadata.Viewport}
	for _, field := range metadata {
		if len(field) > 4096 {
			return fmt.Errorf("replay: metadata field byte limit exceeded")
		}
	}
	total := 0
	for _, field := range replayFields(r) {
		if !utf8.ValidString(*field.value) || len(*field.value) > maxReplayTextBytes {
			return fmt.Errorf("replay: non-UTF-8 or oversized text field")
		}
		total += len(*field.value)
		if total > maxReplayTextBytes {
			return fmt.Errorf("replay: aggregate text byte limit exceeded")
		}
	}
	for _, name := range r.RedactionLog {
		if len(name) > 64 || !utf8.ValidString(name) {
			return fmt.Errorf("replay: invalid redaction-log text")
		}
		total += len(name)
	}
	if total > maxReplayTextBytes {
		return fmt.Errorf("replay: aggregate text and log byte limit exceeded")
	}
	return nil
}

func decodeReplay(data []byte, replay *Replay) error {
	if len(data) > maxReplayBytes || !utf8.Valid(data) {
		return fmt.Errorf("replay: oversized or non-UTF-8 encoded record")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(replay); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("replay: trailing encoded data")
	}
	canonical, err := json.Marshal(replay)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, canonical) {
		return fmt.Errorf("replay: record is not canonical JSON or contains duplicate fields")
	}
	return nil
}
