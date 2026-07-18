package main

import (
	"fmt"
	"os"

	"github.com/Christopher-Schulze/Artemis/diagnostics"
)

func cmdDiagnostics(args []string) int {
	defaultConfig, err := cliDiagnosticsConfig(true)
	if err != nil {
		errf("diagnostics config: %v", err)
		return 1
	}
	fs := newFlagSet("diagnostics")
	path := fs.String("file", defaultConfig.Path, "redacted diagnostics JSONL path")
	limit := fs.Int("limit", 100, "maximum newest records to emit")
	recordType := fs.String("type", "all", "record type: all, policy, resource")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: artemis diagnostics [flags]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || *limit < 1 || *limit > diagnostics.DefaultMaxRecords {
		fs.Usage()
		return 2
	}
	wanted := diagnostics.RecordType("")
	switch *recordType {
	case "all":
	case "policy":
		wanted = diagnostics.RecordPolicyDecision
	case "resource":
		wanted = diagnostics.RecordResourceUsage
	default:
		errf("diagnostics type %q invalid", *recordType)
		return 2
	}
	defaultConfig.Path = *path
	store, err := diagnostics.NewStore(defaultConfig)
	if err != nil {
		errf("diagnostics open: %v", err)
		return 1
	}
	records, err := store.Snapshot()
	if err != nil {
		errf("diagnostics read: %v", err)
		return 1
	}
	filtered := make([]diagnostics.Record, 0, len(records))
	for _, record := range records {
		if wanted == "" || record.Type == wanted {
			filtered = append(filtered, record)
		}
	}
	if len(filtered) > *limit {
		filtered = filtered[len(filtered)-*limit:]
	}
	if err := printJSON(os.Stdout, filtered); err != nil {
		errf("diagnostics output: %v", err)
		return 1
	}
	return 0
}
