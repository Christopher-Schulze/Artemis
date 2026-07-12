package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Christopher-Schulze/Artemis/profile"
)

func cmdSession(args []string) int {
	flags := flag.NewFlagSet("session", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", "", "profile runtime root")
	owner := flags.String("owner", "", "owner user reference")
	op := flags.String("op", "list", "open, list, close")
	profileID := flags.String("profile", "", "profile ID")
	class := flags.String("class", string(profile.ProfileEphemeral), "ephemeral, persistent, attached")
	sessionID := flags.String("id", "", "session ID for close")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	manager, err := profile.NewRuntimeManager(*root)
	if err != nil {
		errf("session: %v", err)
		return 1
	}
	var value any
	switch *op {
	case "open":
		value, err = manager.Open(context.Background(), profile.OpenSessionRequest{ProfileID: profile.ProfileID(*profileID), OwnerUserRef: *owner, Class: profile.ProfileClass(*class)})
	case "list":
		if *owner == "" {
			err = fmt.Errorf("owner required")
		} else {
			value = manager.List(*owner)
		}
	case "close":
		err = manager.Close(context.Background(), profile.SessionID(*sessionID), *owner)
		value = map[string]string{"status": "closed", "id": *sessionID}
	default:
		err = fmt.Errorf("unsupported operation %q", *op)
	}
	if err != nil {
		errf("session: %v", err)
		return 1
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		errf("session: %v", err)
		return 1
	}
	return 0
}
