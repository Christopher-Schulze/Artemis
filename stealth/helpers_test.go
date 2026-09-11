package stealth

import "testing"

func TestParseChromeMajor(t *testing.T) {
	for in, want := range map[string]int{"152.0.7977.83": 152, "120": 120, "1.0": 1} {
		got, err := ParseChromeMajor(in)
		if err != nil || got != want {
			t.Errorf("ParseChromeMajor(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "0.0", "abc", "-1"} {
		if _, err := ParseChromeMajor(bad); err == nil {
			t.Errorf("ParseChromeMajor(%q) must fail", bad)
		}
	}
}

func TestClientHintsPlatform(t *testing.T) {
	for in, want := range map[string]string{
		"MacIntel": "macos", "macOS": "macos", "Win32": "windows",
		"Windows": "windows", "Linux x86_64": "linux", "Linux": "linux",
	} {
		if got := ClientHintsPlatform(in); got != want {
			t.Errorf("ClientHintsPlatform(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLaunchFlagCounts(t *testing.T) {
	if LaunchFlagCount() <= 0 {
		t.Fatal("stealth tiers must have launch flags")
	}
	if LaunchFlagCountFor(StealthDefault) != 0 {
		t.Fatal("default tier must not inject stealth flags")
	}
	if LaunchFlagCountFor(StealthStealth) != LaunchFlagCount() || LaunchFlagCountFor(StealthParanoid) != LaunchFlagCount() {
		t.Fatal("stealth/paranoid must share the flag set")
	}
}

func TestResolveGeoPresetDeterministic(t *testing.T) {
	de := ResolveGeoPreset("de")
	if de.Languages != "de-DE,de,en-US,en" || de.Timezone != "Europe/Berlin" {
		t.Fatalf("de preset: %+v", de)
	}
	us := ResolveGeoPreset("us")
	if us.Timezone != "America/New_York" {
		t.Fatalf("us preset: %+v", us)
	}
	if de.Seed != "geo:de" {
		t.Fatalf("seed must be derived: %q", de.Seed)
	}
	// Unknown codes keep defaults but still carry the geo seed.
	unk := ResolveGeoPreset("xx")
	if unk.Seed != "geo:xx" {
		t.Fatalf("seed: %q", unk.Seed)
	}
}
