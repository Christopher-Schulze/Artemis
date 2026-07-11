package artemis

import "testing"

func TestCapabilityRegistryValid(t *testing.T) {
	if err := ValidateCapabilityRegistry(); err != nil {
		t.Fatal(err)
	}
}

func TestCapabilityRegistryReturnsDefensiveCopy(t *testing.T) {
	capabilities := Capabilities()
	capabilities[0].ID = "mutated"
	if len(capabilities[2].OmnimusTools) == 0 {
		t.Fatal("test requires a capability with an Omnimus tool")
	}
	capabilities[2].OmnimusTools[0] = "mutated"

	fresh := Capabilities()
	if fresh[0].ID == "mutated" || fresh[2].OmnimusTools[0] == "mutated" {
		t.Fatal("Capabilities leaked mutable registry state")
	}
}

func TestUnavailableCapabilities(t *testing.T) {
	for _, capability := range Capabilities() {
		if capability.State == SupportUnavailable && len(capability.OmnimusTools) != 0 {
			t.Fatalf("unavailable capability %q exposes tools %v", capability.ID, capability.OmnimusTools)
		}
	}
	for _, name := range []string{"browser_navigate", "browser_extract", "snapshot", "click", "type", "fill_form", "select", "upload", "login", "screenshot", "evidence", "browser_diff", "browser_switch_profile", "browser_session_status", "browser_console", "browser_network", "browser_scroll", "browser_press_key"} {
		if OmnimusToolSupported(name) {
			t.Fatalf("tool %q must remain unavailable until it has a persistent runtime owner and behavior proof", name)
		}
	}
}

func TestSupportedOmnimusToolsAreExplicit(t *testing.T) {
	for _, name := range []string{"scrape", "scrape_static", "scrape_batch"} {
		if !OmnimusToolSupported(name) {
			t.Fatalf("supported tool %q missing from capability registry", name)
		}
	}
}
