package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/Christopher-Schulze/Artemis/security"
)

// adBlockFlags are the ad/tracker blocking settings (spec L4206-L4210).
// Blocking is on by default; --adblock=false disables it.
type adBlockFlags struct {
	enabled   *bool
	whitelist *string
	updates   *bool
}

func addAdBlockFlag(fs *flag.FlagSet) *bool {
	return fs.Bool("adblock", true, "block ads and trackers (embedded base list); --adblock=false disables")
}

func addServeAdBlockFlags(fs *flag.FlagSet) adBlockFlags {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return adBlockFlags{
		enabled:   addAdBlockFlag(fs),
		whitelist: fs.String("adblock-whitelist", filepath.Join(home, ".artemis", "browser", "adblock_whitelist.json"), "persistent adblock site whitelist (empty keeps it in memory)"),
		updates:   fs.Bool("adblock-updates", false, "download verified EasyList/EasyPrivacy updates every 24h into ~/.artemis/browser/blocklists"),
	}
}

// blocker builds the shared blocker, or nil when blocking is disabled.
func (f adBlockFlags) blocker() (*security.AdBlocker, error) {
	if !*f.enabled {
		return nil, nil
	}
	whitelist, err := security.NewAdBlockerDetection(*f.whitelist)
	if err != nil {
		return nil, err
	}
	return security.NewAdBlockerWithConfig(whitelist, security.DefaultEasyListUpdateConfig().RolloutConfig())
}

// startUpdates runs the EasyList updater until ctx ends; wait joins it.
func (f adBlockFlags) startUpdates(ctx context.Context, blocker *security.AdBlocker) (wait func(), err error) {
	if blocker == nil || !*f.updates {
		return func() {}, nil
	}
	updater, err := security.NewEasyListUpdater(security.DefaultEasyListUpdateConfig(), blocker)
	if err != nil {
		return nil, fmt.Errorf("adblock updates: %w", err)
	}
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		if err := updater.Run(ctx, slog.Default()); err != nil {
			slog.Warn("adblock updater stopped", slog.String("error", err.Error()))
		}
	}()
	return group.Wait, nil
}
