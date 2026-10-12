package bridge

import (
	"testing"
)

func TestDefaultPrivacyRoutingConfig(t *testing.T) {
	config := DefaultPrivacyRoutingConfig()
	if !config.Enabled {
		t.Error("expected enabled=true")
	}
	if len(config.CustomerDataPatterns) == 0 {
		t.Error("expected non-empty customer data patterns")
	}
	if len(config.CustomerDomains) == 0 {
		t.Error("expected non-empty customer domains")
	}
}

func TestPrivacyRoutingHook_CheckURL_Normal(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	result := hook.CheckURL("https://example.com/page")
	if result.Decision != PrivacyDecisionNormal {
		t.Errorf("expected normal, got %s", result.Decision)
	}
	if result.HasCustomerData {
		t.Error("expected has_customer_data=false")
	}
}

func TestPrivacyRoutingHook_CheckURL_CustomerDataPattern(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	result := hook.CheckURL("https://example.com/customer/profile")
	if result.Decision != PrivacyDecisionLocalOnly {
		t.Errorf("expected local_only, got %s", result.Decision)
	}
	if !result.HasCustomerData {
		t.Error("expected has_customer_data=true")
	}
}

func TestPrivacyRoutingHook_CheckURL_CustomerDomain(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	result := hook.CheckURL("https://portal.internal/dashboard")
	if result.Decision != PrivacyDecisionLocalOnly {
		t.Errorf("expected local_only, got %s", result.Decision)
	}
}

func TestPrivacyRoutingHook_CheckURL_Disabled(t *testing.T) {
	config := DefaultPrivacyRoutingConfig()
	config.Enabled = false
	hook := NewPrivacyRoutingHook(config)
	result := hook.CheckURL("https://example.com/customer/profile")
	if result.Decision != PrivacyDecisionNormal {
		t.Errorf("expected normal when disabled, got %s", result.Decision)
	}
}

func TestPrivacyRoutingHook_CheckURL_CaseInsensitive(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	result := hook.CheckURL("https://example.com/CUSTOMER/profile")
	if result.Decision != PrivacyDecisionLocalOnly {
		t.Errorf("expected local_only for uppercase, got %s", result.Decision)
	}
}

func TestPrivacyRoutingHook_ShouldUseLocalOnly_True(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	if !hook.ShouldUseLocalOnly("https://example.com/customer/data") {
		t.Error("expected true for customer URL")
	}
}

func TestPrivacyRoutingHook_ShouldUseLocalOnly_False(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	if hook.ShouldUseLocalOnly("https://example.com/public") {
		t.Error("expected false for normal URL")
	}
}

func TestPrivacyRoutingHook_Stats(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	hook.CheckURL("https://example.com/normal")
	hook.CheckURL("https://example.com/customer/data")
	hook.CheckURL("https://example.com/other")
	stats := hook.Stats()
	if stats.Total != 3 {
		t.Errorf("expected total=3, got %d", stats.Total)
	}
	if stats.NormalRouted != 2 {
		t.Errorf("expected normal_routed=2, got %d", stats.NormalRouted)
	}
	if stats.LocalOnlyRouted != 1 {
		t.Errorf("expected local_only_routed=1, got %d", stats.LocalOnlyRouted)
	}
}

func TestPrivacyRoutingHook_ResetStats(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	hook.CheckURL("https://example.com/normal")
	hook.ResetStats()
	if hook.Stats().Total != 0 {
		t.Error("expected total=0 after reset")
	}
}

func TestPrivacyRoutingHook_Config(t *testing.T) {
	config := DefaultPrivacyRoutingConfig()
	hook := NewPrivacyRoutingHook(config)
	if !hook.Config().Enabled {
		t.Error("config mismatch")
	}
}

func TestPrivacyRoutingHook_SetConfig(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	newConfig := PrivacyRoutingConfig{
		Enabled:              true,
		CustomerDataPatterns: []string{"secret"},
	}
	hook.SetConfig(newConfig)
	if hook.Config().CustomerDataPatterns[0] != "secret" {
		t.Error("config update failed")
	}
}

func TestPrivacyRoutingHook_AllPatternsDetected(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	patterns := []string{"customer", "account", "invoice", "billing", "payment", "ssn", "tax", "salary", "contract", "personal"}
	for _, p := range patterns {
		result := hook.CheckURL("https://example.com/" + p + "/page")
		if result.Decision != PrivacyDecisionLocalOnly {
			t.Errorf("expected local_only for pattern %s, got %s", p, result.Decision)
		}
	}
}

func TestPrivacyRoutingHook_EmptyURL(t *testing.T) {
	hook := NewPrivacyRoutingHook(DefaultPrivacyRoutingConfig())
	result := hook.CheckURL("")
	if result.Decision != PrivacyDecisionNormal {
		t.Errorf("expected normal for empty URL, got %s", result.Decision)
	}
}
