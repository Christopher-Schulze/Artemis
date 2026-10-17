package main

import (
	"fmt"
	"os"

	"github.com/Christopher-Schulze/Artemis/profile"
)

func cmdProfile(args []string) int {
	fs := newFlagSet("profile")
	root := fs.String("root", "", "profiles root directory (default: user profile dir)")
	owner := fs.String("owner", "", "owner user reference")
	op := fs.String("op", "list", "list, create, get, delete")
	name := fs.String("name", "", "profile name for create/get/delete")
	scope := fs.String("scope", string(profile.SharePrivate), "share scope for create: private, workspace_shared, operator_shared")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis profile [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if fs.NArg() != 0 || *owner == "" {
		errf("profile: --owner required; positional arguments are not accepted")
		return 2
	}
	switch *op {
	case "list":
	case "create", "get", "delete":
		if *name == "" {
			errf("profile: --name required for %s", *op)
			return 2
		}
	default:
		errf("profile: unsupported operation %q", *op)
		return 2
	}
	if value := profile.ShareScope(*scope); value != profile.SharePrivate && value != profile.ShareWorkspaceShared && value != profile.ShareOperatorShared {
		errf("profile: invalid share scope")
		return 2
	}

	manager := profile.NewProfileManager(*root, nil)
	if err := manager.Err(); err != nil {
		errf("profile: %v", err)
		return 1
	}

	var value any
	var err error
	switch *op {
	case "create":
		p := &profile.BrowserProfile{
			Name:         *name,
			OwnerUserRef: *owner,
			ShareScope:   profile.ShareScope(*scope),
		}
		err = manager.Create(p)
		value = p
	case "list":
		value = manager.List(*owner)
	case "get":
		value, err = manager.Get(*name, *owner)
	case "delete":
		err = manager.Delete(*name, *owner)
		value = map[string]string{"status": "deleted", "name": *name}
	}

	if err != nil {
		errf("profile: %v", err)
		return 1
	}

	if err := printJSON(os.Stdout, value); err != nil {
		errf("profile: %v", err)
		return 1
	}
	return 0
}
