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
	scope := fs.String("scope", string(profile.SharePrivate), "share scope for create: private, workspace, operator")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: artemis profile [flags]

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *owner == "" {
		errf("profile: --owner required")
		return 2
	}

	manager := profile.NewProfileManager(*root, nil)

	var value any
	var err error
	switch *op {
	case "create":
		if *name == "" {
			errf("profile: --name required for create")
			return 2
		}
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
		if *name == "" {
			errf("profile: --name required for get")
			return 2
		}
		value, err = manager.Get(*name, *owner)
	case "delete":
		if *name == "" {
			errf("profile: --name required for delete")
			return 2
		}
		err = manager.Delete(*name, *owner)
		value = map[string]string{"status": "deleted", "name": *name}
	default:
		errf("profile: unsupported operation %q", *op)
		return 2
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
