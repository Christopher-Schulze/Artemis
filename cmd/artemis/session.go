package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/profile"
	"github.com/Christopher-Schulze/Artemis/serve"
)

func cmdSession(args []string) (exitCode int) {
	flags := newFlagSet("session")
	control := addControlFlags(flags)
	op := flags.String("op", "list", "open, list, close")
	profileID := flags.String("profile", "", "profile ID")
	class := flags.String("class", string(profile.ProfileEphemeral), "ephemeral, persistent, attached")
	sessionID := flags.String("id", "", "session ID for close")
	timeout := flags.Duration("timeout", 30*time.Second, "command timeout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*control.server) == "" || *timeout <= 0 {
		errf("session: --server and positive --timeout required; positional arguments are not accepted")
		return 2
	}
	allowed := map[string]bool{"server": true, "token": true, "client-id": true, "timeout": true, "op": true}
	switch *op {
	case "open":
		allowed["profile"], allowed["class"] = true, true
		if value := profile.ProfileClass(*class); value != profile.ProfileEphemeral && value != profile.ProfilePersistent && value != profile.ProfileAttached {
			errf("session: invalid profile class")
			return 2
		}
		if *class != string(profile.ProfileEphemeral) && *profileID == "" {
			errf("session: --profile required for persistent or attached class")
			return 2
		}
	case "list":
	case "close":
		allowed["id"] = true
		if *sessionID == "" {
			errf("session: --id required for close")
			return 2
		}
	default:
		errf("session: unsupported operation %q", *op)
		return 2
	}
	var ignored string
	flags.Visit(func(value *flag.Flag) {
		if !allowed[value.Name] && ignored == "" {
			ignored = value.Name
		}
	})
	if ignored != "" {
		errf("session: --%s is not accepted for --op %s", ignored, *op)
		return 2
	}
	if *op != "open" && *control.clientID == "" {
		errf("session: --client-id from open required for list/close")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client, err := serve.Dial(ctx, *control.server, *control.token, *control.clientID)
	if err != nil {
		errf("session: %v", err)
		return 1
	}
	defer cleanupOnReturn(&exitCode, "session connection close", client.Close)()
	var result json.RawMessage
	switch *op {
	case "open":
		err = client.Call(ctx, serve.CmdSessionNew, serve.SessionNewParams{ProfileID: *profileID, Class: *class}, &result)
	case "list":
		err = client.Call(ctx, serve.CmdSessionList, nil, &result)
	case "close":
		err = client.Call(ctx, serve.CmdSessionClose, serve.SessionCloseParams{SessionID: *sessionID}, &result)
	}
	if err != nil {
		errf("session: %v", err)
		return 1
	}
	if err := printJSON(os.Stdout, struct {
		ClientID string          `json:"clientId"`
		Result   json.RawMessage `json:"result"`
	}{ClientID: client.ClientID(), Result: result}); err != nil {
		errf("session: %v", err)
		return 1
	}
	return 0
}
