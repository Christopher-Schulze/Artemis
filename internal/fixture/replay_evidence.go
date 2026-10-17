package fixture

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ReplayEvidenceFields carries new inert observations under a verified capture
// profile. The profile's configuration is never exported with these fields.
type ReplayEvidenceFields struct {
	URL        string
	Title      string
	Text       string
	Diagnostic string
}

// RedactEvidenceFields reuses this record's already compiled redaction owner.
// It returns no partial fields on invalid input or replacement expansion.
func (r *Replay) RedactEvidenceFields(fields ReplayEvidenceFields) (ReplayEvidenceFields, error) {
	if r == nil || r.redactionProfile == nil || r.redactionProfile.enabled != r.Redacted || r.redactionProfile.hash != r.RedactionProfileHash {
		return ReplayEvidenceFields{}, fmt.Errorf("replay evidence: verified redaction profile required")
	}
	if err := fields.validate(); err != nil {
		return ReplayEvidenceFields{}, err
	}
	for _, field := range []replayField{{&fields.URL, replayURLField}, {&fields.Title, replayTextField}, {&fields.Text, replayTextField}, {&fields.Diagnostic, replayTextField}} {
		value, _, err := r.redactionProfile.field(field)
		if err != nil {
			return ReplayEvidenceFields{}, err
		}
		*field.value = value
	}
	if err := fields.validate(); err != nil {
		return ReplayEvidenceFields{}, err
	}
	return fields, nil
}

func (fields ReplayEvidenceFields) validate() error {
	if len(fields.Diagnostic) > 64<<10 || strings.ContainsRune(fields.Diagnostic, 0) {
		return fmt.Errorf("replay evidence: observation or diagnostic exceeds its bounds")
	}
	remaining := maxReplayTextBytes + (64 << 10)
	for _, value := range []string{fields.URL, fields.Title, fields.Text} {
		if len(value) > remaining || !utf8.ValidString(value) {
			return fmt.Errorf("replay evidence: bounded UTF-8 observations required")
		}
		remaining -= len(value)
	}
	if !utf8.ValidString(fields.Diagnostic) {
		return fmt.Errorf("replay evidence: UTF-8 diagnostic required")
	}
	return nil
}
