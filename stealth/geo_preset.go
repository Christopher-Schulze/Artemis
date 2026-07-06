package stealth

import "strings"

// ResolveGeoPreset maps a geo code to a deterministic stealth profile overlay.
func ResolveGeoPreset(code string) Profile {
	p := Defaults()
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "de", "de-de":
		p.Languages = "de-DE,de,en-US,en"
		p.Timezone = "Europe/Berlin"
	case "us", "en-us":
		p.Languages = "en-US,en"
		p.Timezone = "America/New_York"
	case "uk", "gb":
		p.Languages = "en-GB,en"
		p.Timezone = "Europe/London"
	}
	p.Seed = "geo:" + code
	return p
}
