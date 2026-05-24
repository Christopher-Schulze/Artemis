package stealth

import "strings"

// LaunchFlags carries Chromium launch arguments with harmful-flag blocking.
type LaunchFlags struct {
	Args []string
}

var blockedLaunchFlags = []string{
	"--disable-web-security",
	"--disable-features=IsolateOrigins",
	"--no-sandbox",
}

// DefaultLaunchFlags returns safe defaults for Artemis launches.
func DefaultLaunchFlags() LaunchFlags {
	return LaunchFlags{Args: []string{"--disable-blink-features=AutomationControlled"}}
}

func (f LaunchFlags) Allows(flag string) bool {
	flag = strings.TrimSpace(flag)
	for _, b := range blockedLaunchFlags {
		if strings.EqualFold(flag, b) {
			return false
		}
	}
	return true
}
