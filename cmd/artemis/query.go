package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/security"
	"github.com/Christopher-Schulze/Artemis/serve"
)

func cmdQuery(args []string) (exitCode int) {
	fs := newFlagSet("query")
	control := addControlFlags(fs)
	mode := fs.String("mode", "interactive", "projection: full, interactive, subtree or evidence")
	root := fs.String("subtree", "", "page-bound subtree reference")
	target := fs.String("target", "", "page-bound reference to diagnose")
	frame := fs.String("frame", "", "observed frame ID")
	roles := fs.String("roles", "", "comma-separated accessibility roles")
	text := fs.String("text", "", "literal case-insensitive name/value filter")
	nodes := fs.Int("max-nodes", 200, "maximum selected nodes")
	bytes := fs.Int("max-bytes", 32768, "maximum query-result JSON bytes")
	base := fs.String("since", "", "previous query revision for delta or unchanged output")
	timeout := fs.Duration("timeout", 20*time.Second, "query deadline")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	epochSupplied := false
	fs.Visit(func(value *flag.Flag) {
		if value.Name == "snapshot-epoch" {
			epochSupplied = true
		}
	})
	if fs.NArg() != 0 || *control.server == "" || *control.pageID == "" || epochSupplied || *timeout <= 0 || *nodes < 1 || *bytes < 512 || *bytes > maxCLIOutputBytes {
		errf("query requires an existing --server, --page, positive budgets/deadline and no URL or snapshot-epoch")
		return 2
	}
	options := bridgeobserve.QueryOptions{Mode: bridgeobserve.Mode(*mode), SubtreeRef: *root, TargetRef: *target, FrameID: *frame, Text: *text, MaxNodes: *nodes, MaxBytes: *bytes}
	switch options.Mode {
	case bridgeobserve.ModeFull, bridgeobserve.ModeInteractive, bridgeobserve.ModeSubtree, bridgeobserve.ModeEvidence:
	default:
		errf("query: unsupported mode")
		return 2
	}
	if (options.Mode == bridgeobserve.ModeSubtree) != (strings.TrimSpace(*root) != "") {
		errf("query: subtree mode requires --subtree; other modes forbid it")
		return 2
	}
	if len(*text) > 256 || len(*frame) > 128 || len(*root) > 128 || len(*target) > 128 || !utf8.ValidString(*text) || !utf8.ValidString(*frame) || !utf8.ValidString(*root) || !utf8.ValidString(*target) || strings.Count(*roles, ",") >= 32 {
		errf("query: selector exceeds text/identity/role limits")
		return 2
	}
	if *roles != "" {
		for _, role := range strings.Split(*roles, ",") {
			role = strings.TrimSpace(role)
			if role == "" || len(role) > 64 || !utf8.ValidString(role) {
				errf("query: invalid role")
				return 2
			}
			options.Roles = append(options.Roles, role)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client, err := control.connect(ctx)
	if err != nil {
		errf("query: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "query control close", client.Close)()
	var result bridgeobserve.QueryResult
	params := serve.ChromiumQueryParams{SessionID: *control.sessionID, PageID: *control.pageID, Request: bridgeobserve.QueryRequest{Options: options, BaseRevision: *base}}
	trust, err := client.CallLabeled(ctx, serve.CmdChromiumQuery, params, &result)
	if err != nil {
		errf("query: %v", err)
		return 1
	}
	if err := result.Validate(); err != nil {
		errf("query response: %v", err)
		return 1
	}
	if trust == nil {
		errf("query response: missing trust label")
		return 1
	}
	trust.Source = "query"
	raw, err := json.Marshal(result)
	if err == nil {
		raw, err = security.WithTrustMember(raw, *trust)
	}
	if err != nil || len(raw) > *bytes {
		errf("query response exceeds byte budget or cannot be encoded: %v", err)
		return 1
	}
	if len(raw) < *bytes {
		raw = append(raw, '\n')
	}
	if err := writeCLIOutput(os.Stdout, raw); err != nil {
		errf("query output: %v", err)
		return 1
	}
	return 0
}
