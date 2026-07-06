package bridge

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDefaultJSToggleConfigRestrictive(t *testing.T) {
	cfg := DefaultJSToggleConfig("research")
	if cfg.EvaluateEnabled {
		t.Fatal("default should be restrictive (disabled)")
	}
	if cfg.ProfileName != "research" {
		t.Fatalf("profile=%q", cfg.ProfileName)
	}
}

func TestJSToggleCheckDisabledByDefault(t *testing.T) {
	toggle := NewJSToggle()
	err := toggle.Check("myprofile", "evaluate")
	if err == nil {
		t.Fatal("expected error when profile not registered")
	}
	var jsErr JSEvalDisabledError
	if !errors.As(err, &jsErr) {
		t.Fatal("should be JSEvalDisabledError")
	}
	if jsErr.ProfileName != "myprofile" {
		t.Fatalf("profile=%q", jsErr.ProfileName)
	}
}

func TestJSToggleCheckEnabled(t *testing.T) {
	toggle := NewJSToggle()
	toggle.SetProfile(JSToggleConfig{
		EvaluateEnabled: true,
		ProfileName:     "admin",
		Reason:          "admin profile requires JS eval",
	})
	if err := toggle.Check("admin", "evaluate"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJSToggleCheckDisabled(t *testing.T) {
	toggle := NewJSToggle()
	toggle.SetProfile(JSToggleConfig{
		EvaluateEnabled: false,
		ProfileName:     "banking",
		Reason:          "banking profile blocks JS eval",
	})
	err := toggle.Check("banking", "evaluate")
	if err == nil {
		t.Fatal("expected error when disabled")
	}
	if !strings.Contains(err.Error(), "banking") {
		t.Fatalf("error=%q", err.Error())
	}
}

func TestJSToggleIsEnabledUnregistered(t *testing.T) {
	toggle := NewJSToggle()
	if toggle.IsEnabled("unknown") {
		t.Fatal("unregistered profile should be disabled")
	}
}

func TestJSToggleIsEnabledRegistered(t *testing.T) {
	toggle := NewJSToggle()
	toggle.SetProfile(JSToggleConfig{EvaluateEnabled: true, ProfileName: "p1"})
	if !toggle.IsEnabled("p1") {
		t.Fatal("p1 should be enabled")
	}
}

func TestJSToggleGetProfileUnregistered(t *testing.T) {
	toggle := NewJSToggle()
	cfg := toggle.GetProfile("unknown")
	if cfg.EvaluateEnabled {
		t.Fatal("unregistered should return restrictive default")
	}
}

func TestJSToggleGetProfileRegistered(t *testing.T) {
	toggle := NewJSToggle()
	toggle.SetProfile(JSToggleConfig{EvaluateEnabled: true, ProfileName: "p1", Reason: "test"})
	cfg := toggle.GetProfile("p1")
	if !cfg.EvaluateEnabled || cfg.Reason != "test" {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestJSToggleEnableEvaluation(t *testing.T) {
	toggle := NewJSToggle()
	toggle.EnableEvaluation("dev", "development profile")
	if !toggle.IsEnabled("dev") {
		t.Fatal("dev should be enabled")
	}
	cfg := toggle.GetProfile("dev")
	if cfg.Reason != "development profile" {
		t.Fatalf("reason=%q", cfg.Reason)
	}
}

func TestJSToggleEnableThenDisable(t *testing.T) {
	toggle := NewJSToggle()
	toggle.EnableEvaluation("p1", "enabled")
	toggle.DisableEvaluation("p1", "disabled for security")
	if toggle.IsEnabled("p1") {
		t.Fatal("p1 should be disabled after DisableEvaluation")
	}
	cfg := toggle.GetProfile("p1")
	if cfg.Reason != "disabled for security" {
		t.Fatalf("reason=%q", cfg.Reason)
	}
}

func TestJSToggleDisableEvaluationNewProfile(t *testing.T) {
	toggle := NewJSToggle()
	toggle.DisableEvaluation("newprof", "never enabled")
	if toggle.IsEnabled("newprof") {
		t.Fatal("should be disabled")
	}
}

func TestJSToggleCheckContextCancelled(t *testing.T) {
	toggle := NewJSToggle()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := toggle.CheckContext(ctx, "p1", "evaluate")
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("error=%q", err.Error())
	}
}

func TestJSToggleCheckContextOK(t *testing.T) {
	toggle := NewJSToggle()
	toggle.EnableEvaluation("p1", "test")
	err := toggle.CheckContext(context.Background(), "p1", "evaluate")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJSToggleRemoveProfile(t *testing.T) {
	toggle := NewJSToggle()
	toggle.EnableEvaluation("p1", "test")
	if !toggle.RemoveProfile("p1") {
		t.Fatal("remove should return true")
	}
	if toggle.IsEnabled("p1") {
		t.Fatal("p1 should be gone")
	}
	if toggle.RemoveProfile("p1") {
		t.Fatal("second remove should return false")
	}
}

func TestJSToggleListProfiles(t *testing.T) {
	toggle := NewJSToggle()
	toggle.EnableEvaluation("a", "")
	toggle.EnableEvaluation("b", "")
	toggle.EnableEvaluation("c", "")
	profiles := toggle.ListProfiles()
	if len(profiles) != 3 {
		t.Fatalf("count=%d", len(profiles))
	}
}

func TestJSToggleProfileCount(t *testing.T) {
	toggle := NewJSToggle()
	toggle.EnableEvaluation("a", "")
	toggle.EnableEvaluation("b", "")
	if toggle.ProfileCount() != 2 {
		t.Fatalf("count=%d", toggle.ProfileCount())
	}
}

func TestJSToggleEnabledCount(t *testing.T) {
	toggle := NewJSToggle()
	toggle.EnableEvaluation("a", "")
	toggle.EnableEvaluation("b", "")
	toggle.DisableEvaluation("c", "")
	if toggle.EnabledCount() != 2 {
		t.Fatalf("enabled=%d", toggle.EnabledCount())
	}
}

func TestJSToggleSetProfileReplaces(t *testing.T) {
	toggle := NewJSToggle()
	toggle.SetProfile(JSToggleConfig{EvaluateEnabled: true, ProfileName: "p1", Reason: "first"})
	toggle.SetProfile(JSToggleConfig{EvaluateEnabled: false, ProfileName: "p1", Reason: "second"})
	cfg := toggle.GetProfile("p1")
	if cfg.EvaluateEnabled || cfg.Reason != "second" {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestJSEvalDisabledErrorMessage(t *testing.T) {
	err := JSEvalDisabledError{ProfileName: "bank", Action: "evaluate", Reason: "security policy"}
	msg := err.Error()
	if !strings.Contains(msg, "bank") || !strings.Contains(msg, "evaluate") || !strings.Contains(msg, "security") {
		t.Fatalf("msg=%q", msg)
	}
}

func TestJSToggleCheckWaitAction(t *testing.T) {
	toggle := NewJSToggle()
	toggle.DisableEvaluation("p1", "no JS in wait")
	err := toggle.Check("p1", "wait")
	if err == nil {
		t.Fatal("wait with fn should be blocked when disabled")
	}
}

func TestJSToggleConcurrentAccess(t *testing.T) {
	toggle := NewJSToggle()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			toggle.EnableEvaluation("p1", "concurrent")
		}
	}()
	for i := 0; i < 100; i++ {
		_ = toggle.IsEnabled("p1")
	}
	<-done
}
