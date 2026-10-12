package bridge

import (
	"strings"
	"testing"
)

func TestRegisterFeature(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterFeature(FeatureWebSocket, true, "1.0")
	if !r.IsAvailable(FeatureWebSocket) {
		t.Fatal("websocket should be available")
	}
}

func TestIsAvailableUnregistered(t *testing.T) {
	r := NewFeatureRegistry()
	if r.IsAvailable(FeatureTracing) {
		t.Fatal("unregistered should be unavailable")
	}
}

func TestIsAvailableFalse(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterFeature(FeatureHAR, false, "")
	if r.IsAvailable(FeatureHAR) {
		t.Fatal("har should be unavailable")
	}
}

func TestGetFeature(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterFeature(FeatureDevTools, true, "120")
	st, ok := r.GetFeature(FeatureDevTools)
	if !ok {
		t.Fatal("should be registered")
	}
	if st.Version != "120" || !st.Available {
		t.Fatalf("status=%+v", st)
	}
}

func TestGetFeatureMissing(t *testing.T) {
	r := NewFeatureRegistry()
	if _, ok := r.GetFeature(FeatureStreaming); ok {
		t.Fatal("missing feature should return ok=false")
	}
}

func TestGetAllFeatures(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterFeature(FeatureWebSocket, true, "1")
	r.RegisterFeature(FeatureTracing, true, "2")
	r.RegisterFeature(FeatureHAR, false, "")
	all := r.GetAllFeatures()
	if len(all) != 3 {
		t.Fatalf("count=%d", len(all))
	}
}

func TestCheckWarningAllAvailable(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterFeature(FeatureWebSocket, true, "1")
	r.RegisterFeature(FeatureTracing, true, "2")
	if w := r.CheckWarning([]BrowserFeature{FeatureWebSocket, FeatureTracing}); w != "" {
		t.Fatalf("warning=%q", w)
	}
}

func TestCheckWarningMissing(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterFeature(FeatureWebSocket, true, "1")
	w := r.CheckWarning([]BrowserFeature{FeatureWebSocket, FeatureTracing})
	if w == "" {
		t.Fatal("expected warning")
	}
	if !strings.Contains(w, "tracing") {
		t.Fatalf("warning=%q", w)
	}
}

func TestCheckWarningUnavailable(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterFeature(FeatureHAR, false, "")
	w := r.CheckWarning([]BrowserFeature{FeatureHAR})
	if w == "" {
		t.Fatal("expected warning for unavailable feature")
	}
	if !strings.Contains(w, "har") {
		t.Fatalf("warning=%q", w)
	}
}

func TestCheckWarningEmptyRequired(t *testing.T) {
	r := NewFeatureRegistry()
	if w := r.CheckWarning(nil); w != "" {
		t.Fatalf("warning=%q", w)
	}
}

func TestRegisterAll(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterAll([]FeatureStatus{
		{Feature: FeatureWebSocket, Available: true, Version: "1"},
		{Feature: FeatureTracing, Available: false},
	})
	if r.Count() != 2 {
		t.Fatalf("count=%d", r.Count())
	}
	if !r.IsAvailable(FeatureWebSocket) {
		t.Fatal("websocket should be available")
	}
	if r.IsAvailable(FeatureTracing) {
		t.Fatal("tracing should be unavailable")
	}
}

func TestUnregister(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterFeature(FeatureAnnotations, true, "1")
	r.Unregister(FeatureAnnotations)
	if r.IsAvailable(FeatureAnnotations) {
		t.Fatal("should be unregistered")
	}
}

func TestCount(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterFeature(FeatureWebSocket, true, "")
	r.RegisterFeature(FeatureTracing, true, "")
	if r.Count() != 2 {
		t.Fatalf("count=%d", r.Count())
	}
}

func TestAvailableCount(t *testing.T) {
	r := NewFeatureRegistry()
	r.RegisterFeature(FeatureWebSocket, true, "")
	r.RegisterFeature(FeatureTracing, false, "")
	r.RegisterFeature(FeatureHAR, true, "")
	if r.AvailableCount() != 2 {
		t.Fatalf("available=%d", r.AvailableCount())
	}
}

func TestAllFeatureConstants(t *testing.T) {
	features := []BrowserFeature{
		FeatureWebSocket, FeatureTracing, FeatureHAR,
		FeatureAnnotations, FeatureStreaming, FeatureDevTools,
	}
	seen := map[BrowserFeature]bool{}
	for _, f := range features {
		if seen[f] {
			t.Fatalf("duplicate feature %s", f)
		}
		seen[f] = true
	}
	if len(features) != 6 {
		t.Fatalf("count=%d", len(features))
	}
}

func TestCheckWarningMultipleMissing(t *testing.T) {
	r := NewFeatureRegistry()
	w := r.CheckWarning([]BrowserFeature{FeatureWebSocket, FeatureTracing, FeatureHAR})
	if !strings.Contains(w, "websocket") || !strings.Contains(w, "tracing") || !strings.Contains(w, "har") {
		t.Fatalf("warning should list all missing: %q", w)
	}
}
