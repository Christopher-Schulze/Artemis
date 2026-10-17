package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Christopher-Schulze/Artemis/security"
	"github.com/Christopher-Schulze/Artemis/serve"
)

type controlFlags struct {
	server, token, clientID, sessionID, pageID *string
	epoch                                      *uint64
}

func addControlFlags(fs *flag.FlagSet) controlFlags {
	return controlFlags{
		server:    fs.String("server", os.Getenv("ARTEMIS_SERVER"), "existing steering endpoint (ws://127.0.0.1:9333/)"),
		token:     fs.String("token", os.Getenv("ARTEMIS_SERVE_TOKEN"), "steering bearer token"),
		clientID:  fs.String("client-id", os.Getenv("ARTEMIS_CLIENT_ID"), "server-issued reconnect identity"),
		sessionID: fs.String("session", "", "explicit session ID at the steering owner"),
		pageID:    fs.String("page", "", "explicit page ID at the steering owner"),
		epoch:     fs.Uint64("snapshot-epoch", 0, "observed document epoch required for ref actions"),
	}
}

func (f controlFlags) connect(ctx context.Context) (*serve.Client, error) {
	if *f.clientID == "" {
		return nil, fmt.Errorf("persistent operation requires --client-id; use artemis control --cmd session.new first")
	}
	if *f.sessionID == "" {
		return nil, fmt.Errorf("persistent operation requires --session")
	}
	return serve.Dial(ctx, *f.server, *f.token, *f.clientID)
}

// rejectLocalControlFlags prevents silently ignored process/policy configuration.
func rejectLocalControlFlags(fs *flag.FlagSet, allowed map[string]bool) error {
	var err error
	fs.Visit(func(value *flag.Flag) {
		if !allowed[value.Name] && err == nil {
			err = fmt.Errorf("--%s configures one-shot operation; configure it on artemis serve", value.Name)
		}
	})
	return err
}

func controlAllowed(extra ...string) map[string]bool {
	allowed := map[string]bool{"server": true, "token": true, "client-id": true, "session": true, "page": true, "snapshot-epoch": true, "timeout": true}
	for _, name := range extra {
		allowed[name] = true
	}
	return allowed
}

func cmdControl(args []string) (exitCode int) {
	fs := newFlagSet("control")
	control := addControlFlags(fs)
	command := fs.String("cmd", "version", "steering command, e.g. session.new or chromium.open")
	params := fs.String("params", "{}", "typed command parameters as JSON")
	timeout := fs.Duration("timeout", 30*time.Second, "command timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var targetFlag bool
	fs.Visit(func(value *flag.Flag) {
		if value.Name == "session" || value.Name == "page" || value.Name == "snapshot-epoch" {
			targetFlag = true
		}
	})
	if targetFlag {
		errf("control session/page/epoch belong in --params")
		return 2
	}
	if fs.NArg() != 0 || *timeout <= 0 || len(*params) > maxCLIRequestBytes || !json.Valid([]byte(*params)) {
		errf("control requires JSON within %d bytes, positive timeout and no positional arguments", maxCLIRequestBytes)
		return 2
	}
	if err := serve.Command(*command).Validate(); err != nil {
		errf("control: %v", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client, err := serve.Dial(ctx, *control.server, *control.token, *control.clientID)
	if err != nil {
		errf("control: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "control connection close", client.Close)()
	var result json.RawMessage
	trust, err := client.CallLabeled(ctx, serve.Command(*command), json.RawMessage(*params), &result)
	if err != nil {
		errf("control: %v", err)
		return 1
	}
	if err := printJSON(os.Stdout, struct {
		ClientID string                 `json:"clientId"`
		Result   json.RawMessage        `json:"result"`
		Trust    *security.ContentTrust `json:"trust,omitempty"`
	}{ClientID: client.ClientID(), Result: result, Trust: trust}); err != nil {
		errf("control output: %v", err)
		return 1
	}
	return 0
}
