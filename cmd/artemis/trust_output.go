package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Christopher-Schulze/Artemis/security"
)

// newCLIIDPI returns the checker that labels standalone page-derived output
// (spec L4202): default-on, taint+fence, no trusted origins.
func newCLIIDPI() *security.IDPIChecker {
	return security.NewIDPIChecker(security.DefaultIDPIConfig())
}

// labelJSONObject adds the top-level trust member to page-derived JSON.
func labelJSONObject(raw []byte, pageURL, source string) ([]byte, error) {
	trust, err := newCLIIDPI().Label(pageURL, source, raw)
	if err != nil {
		return nil, err
	}
	return security.WithTrustMember(raw, trust)
}

// printLabeledJSON writes v like printJSON with a top-level trust member.
func printLabeledJSON(w io.Writer, v any, pageURL, source string) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	trust, err := newCLIIDPI().Label(pageURL, source, raw)
	if err != nil {
		return err
	}
	return writeLabeledJSON(w, raw, trust)
}

// writeLabeledJSON indents raw with trust as top-level member; a value that
// is not a JSON object is carried as {"trust":...,"data":value}.
func writeLabeledJSON(w io.Writer, raw []byte, trust security.ContentTrust) error {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		wrapped, err := json.Marshal(struct {
			Data json.RawMessage `json:"data"`
		}{Data: raw})
		if err != nil {
			return err
		}
		raw = wrapped
	}
	labeled, err := security.WithTrustMember(raw, trust)
	if err != nil {
		return err
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, labeled, "", "  "); err != nil {
		return err
	}
	if indented.Len() >= maxCLIOutputBytes {
		return fmt.Errorf("JSON output exceeds %d bytes including newline", maxCLIOutputBytes)
	}
	return writeCLIOutput(w, append(indented.Bytes(), '\n'))
}

// printFenced writes page-derived text inside the spoof-proof IDPI fence.
func printFenced(w io.Writer, text, pageURL, source string) error {
	fenced, _, err := newCLIIDPI().Fence(pageURL, source, text)
	if err != nil {
		return err
	}
	return printText(w, fenced)
}
