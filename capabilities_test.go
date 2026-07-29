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
	for _, name := range []string{"browser_navigate", "browser_extract", "snapshot", "click", "type", "fill_form", "select", "upload", "screenshot", "evidence", "browser_diff", "browser_switch_profile", "browser_session_status", "browser_console", "browser_network", "browser_scroll", "browser_press_key"} {
		if OmnimusToolSupported(name) {
			t.Fatalf("tool %q must remain unavailable until it has a persistent runtime owner and behavior proof", name)
		}
	}
}

func TestSupportedOmnimusToolsAreExplicit(t *testing.T) {
	for _, name := range []string{"scrape", "scrape_static", "scrape_batch", "login"} {
		if !OmnimusToolSupported(name) {
			t.Fatalf("supported tool %q missing from capability registry", name)
		}
	}
}

func TestDefaultCompatibilityMatrix(t *testing.T) {
	m := DefaultCompatibilityMatrix()
	if m.Version != Version {
		t.Fatalf("Version = %q, want %q", m.Version, Version)
	}
	if len(m.Modes) != 3 {
		t.Fatalf("expected 3 execution modes, got %d", len(m.Modes))
	}
	if len(m.Rows) != len(Capabilities()) {
		t.Fatalf("expected %d rows, got %d", len(Capabilities()), len(m.Rows))
	}
	seen := make(map[string]bool)
	for _, row := range m.Rows {
		if seen[row.ID] {
			t.Fatalf("duplicate capability %q in matrix", row.ID)
		}
		seen[row.ID] = true
		if row.State == "" {
			t.Fatalf("capability %q missing State", row.ID)
		}
	}
}
