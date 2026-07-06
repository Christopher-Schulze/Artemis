package observe

import (
	"encoding/json"
	"fmt"
)

// format.go (spec L4024: observe/format.go - output formatters HAR,
// NDJSON).
//
// This file is the spec-mandated facade for output formatters.
// The HAR implementation lives in har_export.go; this file adds
// NDJSON formatting and re-exports HAR types.
//
// Page observation: output formatters (HAR, NDJSON).

// OutputFormat enumerates the output format types
// (spec L4024: format.go - output formatters HAR, NDJSON).
type OutputFormat string

const (
	// OutputFormatHAR is the HTTP Archive format.
	OutputFormatHAR OutputFormat = "har"
	// OutputFormatNDJSON is the Newline-Delimited JSON format.
	OutputFormatNDJSON OutputFormat = "ndjson"
)

// HARLog is the spec-mandated re-export of HARLog from har_export.go
// (spec L4024: format.go - output formatters HAR).
type HARLogAlias = HARLog

// FormatHAR formats network events as a HAR log
// (spec L4024: format.go - output formatters HAR).
func FormatHAR(entries []HAREntry) ([]byte, error) {
	log := HARLog{
		Version: HARVersion,
		Creator: HARCreator{
			Name:    HARCreatorName,
			Version: HARCreatorVersion,
		},
		Entries: entries,
	}
	return json.MarshalIndent(log, "", "  ")
}

// FormatNDJSON formats events as newline-delimited JSON
// (spec L4024: format.go - output formatters NDJSON).
func FormatNDJSON(events []NetworkEvent) ([]byte, error) {
	result := make([]byte, 0, len(events)*256)
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			return nil, fmt.Errorf("ndjson: marshal error: %w", err)
		}
		result = append(result, line...)
		result = append(result, '\n')
	}
	return result, nil
}

// FormatConsoleNDJSON formats console entries as newline-delimited JSON
// (spec L4024: format.go - output formatters NDJSON).
func FormatConsoleNDJSON(entries []ConsoleEntry) ([]byte, error) {
	result := make([]byte, 0, len(entries)*128)
	for _, entry := range entries {
		line, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("ndjson: marshal error: %w", err)
		}
		result = append(result, line...)
		result = append(result, '\n')
	}
	return result, nil
}

// FormatOutput formats data in the specified output format
// (spec L4024: format.go - output formatters HAR, NDJSON).
func FormatOutput(format OutputFormat, events []NetworkEvent) ([]byte, error) {
	switch format {
	case OutputFormatHAR:
		harEntries := make([]HAREntry, 0, len(events))
		return FormatHAR(harEntries)
	case OutputFormatNDJSON:
		return FormatNDJSON(events)
	default:
		return nil, fmt.Errorf("format: unsupported output format %q", format)
	}
}

// IsSupportedFormat reports whether the format is supported
// (spec L4024: format.go - output formatters HAR, NDJSON).
func IsSupportedFormat(format OutputFormat) bool {
	return format == OutputFormatHAR || format == OutputFormatNDJSON
}
