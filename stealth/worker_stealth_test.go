package stealth

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsWorkerTarget(t *testing.T) {
	tests := []struct {
		targetType string
		want       bool
	}{
		{"service_worker", true},
		{"shared_worker", true},
		{"worker", true},
		{"dedicated_worker", true},
		{"page", false},
		{"iframe", false},
		{"", false},
		{"browser", false},
	}
	for _, tt := range tests {
		t.Run(tt.targetType, func(t *testing.T) {
			if got := IsWorkerTarget(tt.targetType); got != tt.want {
				t.Errorf("IsWorkerTarget(%q) = %v, want %v", tt.targetType, got, tt.want)
			}
		})
	}
}

func TestWorkerTargetInfo_MarshalJSON(t *testing.T) {
	info := WorkerTargetInfo{
		TargetID:   "target-123",
		TargetType: "service_worker",
		Title:      "SW",
		URL:        "https://example.com/sw.js",
	}
	data, err := info.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var decoded WorkerTargetInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.TargetID != "target-123" {
		t.Fatalf("expected target_id=target-123, got %s", decoded.TargetID)
	}
	if decoded.TargetType != "service_worker" {
		t.Fatalf("expected target_type=service_worker, got %s", decoded.TargetType)
	}
	if decoded.URL != "https://example.com/sw.js" {
		t.Fatalf("expected url, got %s", decoded.URL)
	}
}

func TestWorkerTargetInfo_MarshalJSON_SafeEscaping(t *testing.T) {
	// Verify HTML-unsafe characters are safely escaped (spec L4058:
	// safe JSON marshaling).
	info := WorkerTargetInfo{
		TargetID:   `<script>alert(1)</script>`,
		TargetType: "worker",
	}
	data, err := info.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	str := string(data)
	// json.Marshal escapes < and > by default.
	if !strings.Contains(str, `\u003c`) {
		t.Errorf("expected < to be escaped to \\u003c in JSON output: %s", str)
	}
	if !strings.Contains(str, `\u003e`) {
		t.Errorf("expected > to be escaped to \\u003e in JSON output: %s", str)
	}
}

func TestWorkerStealthTracker_ShouldApply(t *testing.T) {
	tracker := NewWorkerStealthTracker()
	if !tracker.ShouldApply("target-1") {
		t.Error("expected first call to return true")
	}
	if tracker.ShouldApply("target-1") {
		t.Error("expected second call to return false (already applied)")
	}
	if !tracker.ShouldApply("target-2") {
		t.Error("expected first call for different target to return true")
	}
}

func TestWorkerStealthTracker_ShouldApply_EmptyID(t *testing.T) {
	tracker := NewWorkerStealthTracker()
	if tracker.ShouldApply("") {
		t.Error("expected false for empty target ID")
	}
}

func TestWorkerStealthTracker_IsApplied(t *testing.T) {
	tracker := NewWorkerStealthTracker()
	tracker.ShouldApply("target-1")
	if !tracker.IsApplied("target-1") {
		t.Error("expected target-1 to be applied")
	}
	if tracker.IsApplied("target-2") {
		t.Error("expected target-2 to not be applied")
	}
}

func TestWorkerStealthTracker_Reset(t *testing.T) {
	tracker := NewWorkerStealthTracker()
	tracker.ShouldApply("target-1")
	tracker.ShouldApply("target-2")
	if tracker.Count() != 2 {
		t.Fatalf("expected count=2, got %d", tracker.Count())
	}
	tracker.Reset()
	if tracker.Count() != 0 {
		t.Fatalf("expected count=0 after reset, got %d", tracker.Count())
	}
}

func TestWorkerStealthTracker_Count(t *testing.T) {
	tracker := NewWorkerStealthTracker()
	if tracker.Count() != 0 {
		t.Fatalf("expected 0, got %d", tracker.Count())
	}
	tracker.ShouldApply("a")
	tracker.ShouldApply("b")
	if tracker.Count() != 2 {
		t.Fatalf("expected 2, got %d", tracker.Count())
	}
}

func TestWorkerStealthScript_Defaults(t *testing.T) {
	script := WorkerStealthScript(Defaults())
	if script == "" {
		t.Fatal("expected non-empty script")
	}
	if !strings.Contains(script, "self.navigator") {
		t.Error("expected script to reference self.navigator (worker context)")
	}
	if !strings.Contains(script, "webdriver") {
		t.Error("expected script to patch navigator.webdriver")
	}
	if !strings.Contains(script, "userAgent") {
		t.Error("expected script to patch navigator.userAgent")
	}
	if !strings.Contains(script, "platform") {
		t.Error("expected script to patch navigator.platform")
	}
	if !strings.Contains(script, "language") {
		t.Error("expected script to patch navigator.language")
	}
	if !strings.Contains(script, "languages") {
		t.Error("expected script to patch navigator.languages")
	}
}

func TestWorkerStealthScript_CustomProfile(t *testing.T) {
	p := Profile{
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) Chrome/125.0.0.0",
		Platform:  "Linux x86_64",
		Languages: "en-US,en",
		Vendor:    "Google Inc.",
	}
	script := WorkerStealthScript(p)
	if !strings.Contains(script, "Mozilla/5.0 (X11; Linux x86_64) Chrome/125.0.0.0") {
		t.Error("expected script to contain custom UA")
	}
	if !strings.Contains(script, "Linux x86_64") {
		t.Error("expected script to contain custom platform")
	}
}

func TestWorkerStealthScript_EmptyProfileUsesDefaults(t *testing.T) {
	script := WorkerStealthScript(Profile{})
	defaults := Defaults()
	if !strings.Contains(script, defaults.UserAgent) {
		t.Error("expected empty profile to use default UA")
	}
	if !strings.Contains(script, defaults.Platform) {
		t.Error("expected empty profile to use default platform")
	}
}

func TestWorkerStealthScript_ContainsDefineProperty(t *testing.T) {
	script := WorkerStealthScript(Defaults())
	if !strings.Contains(script, "Object.defineProperty") {
		t.Error("expected script to use Object.defineProperty")
	}
	if !strings.Contains(script, "configurable: true") {
		t.Error("expected script to use configurable: true")
	}
}

func TestWorkerStealthScript_PatchesWebdriverFalse(t *testing.T) {
	script := WorkerStealthScript(Defaults())
	if !strings.Contains(script, "define('webdriver', () => false)") {
		t.Error("expected script to set navigator.webdriver = false")
	}
}

func TestWorkerStealthScript_LanguagesArray(t *testing.T) {
	p := Profile{
		UserAgent: "test-ua",
		Platform:  "test-platform",
		Languages: "de-DE,de,en-US,en",
		Vendor:    "test-vendor",
	}
	script := WorkerStealthScript(p)
	if !strings.Contains(script, `de-DE`) || !strings.Contains(script, `en-US`) {
		t.Errorf("expected script to contain languages, got: %s", script)
	}
}

func TestWorkerStealthScript_JSONParseSafe(t *testing.T) {
	// The languages JSON must be a valid JSON string that JSON.parse can
	// handle (spec L4058: safe JSON marshaling).
	script := WorkerStealthScript(Defaults())
	// Extract the JSON.parse argument by finding the pattern.
	idx := strings.Index(script, "JSON.parse(")
	if idx < 0 {
		t.Fatal("expected JSON.parse in script")
	}
	// The argument is a quoted string literal - verify it's properly quoted.
	rest := script[idx+len("JSON.parse("):]
	if !strings.HasPrefix(rest, `"`) {
		t.Fatal("expected JSON.parse argument to be a quoted string")
	}
}

func TestWorkerStealthHandler_HandleAttachedToTarget_Worker(t *testing.T) {
	handler := NewWorkerStealthHandler(Defaults())
	info, script, shouldApply := handler.HandleAttachedToTarget("target-1", "service_worker")
	if !shouldApply {
		t.Fatal("expected shouldApply=true for worker target")
	}
	if info == nil {
		t.Fatal("expected non-nil info")
	}
	if info.TargetID != "target-1" {
		t.Fatalf("expected target_id=target-1, got %s", info.TargetID)
	}
	if info.TargetType != "service_worker" {
		t.Fatalf("expected target_type=service_worker, got %s", info.TargetType)
	}
	if script == "" {
		t.Fatal("expected non-empty script")
	}
}

func TestWorkerStealthHandler_HandleAttachedToTarget_NonWorker(t *testing.T) {
	handler := NewWorkerStealthHandler(Defaults())
	info, script, shouldApply := handler.HandleAttachedToTarget("target-1", "page")
	if shouldApply {
		t.Fatal("expected shouldApply=false for non-worker target")
	}
	if info != nil {
		t.Fatal("expected nil info for non-worker target")
	}
	if script != "" {
		t.Fatal("expected empty script for non-worker target")
	}
}

func TestWorkerStealthHandler_HandleAttachedToTarget_AlreadyApplied(t *testing.T) {
	handler := NewWorkerStealthHandler(Defaults())
	// First call applies.
	_, _, shouldApply1 := handler.HandleAttachedToTarget("target-1", "service_worker")
	if !shouldApply1 {
		t.Fatal("expected first call to apply")
	}
	// Second call should not re-apply.
	info, script, shouldApply2 := handler.HandleAttachedToTarget("target-1", "service_worker")
	if shouldApply2 {
		t.Fatal("expected second call to not apply")
	}
	if info != nil {
		t.Fatal("expected nil info on duplicate")
	}
	if script != "" {
		t.Fatal("expected empty script on duplicate")
	}
}

func TestWorkerStealthHandler_HandleAttachedToTarget_DifferentWorkers(t *testing.T) {
	handler := NewWorkerStealthHandler(Defaults())
	_, _, ok1 := handler.HandleAttachedToTarget("target-1", "service_worker")
	_, _, ok2 := handler.HandleAttachedToTarget("target-2", "shared_worker")
	_, _, ok3 := handler.HandleAttachedToTarget("target-3", "worker")
	if !ok1 || !ok2 || !ok3 {
		t.Fatal("expected all three different workers to apply")
	}
	if handler.Tracker().Count() != 3 {
		t.Fatalf("expected 3 tracked targets, got %d", handler.Tracker().Count())
	}
}

func TestWorkerStealthHandler_HandleAttachedToTarget_AllWorkerTypes(t *testing.T) {
	workerTypes := []string{
		"service_worker",
		"shared_worker",
		"worker",
		"dedicated_worker",
	}
	for _, wt := range workerTypes {
		handler := NewWorkerStealthHandler(Defaults())
		_, _, shouldApply := handler.HandleAttachedToTarget("target-1", wt)
		if !shouldApply {
			t.Errorf("expected shouldApply=true for worker type %s", wt)
		}
	}
}

func TestWorkerStealthHandler_Tracker(t *testing.T) {
	handler := NewWorkerStealthHandler(Defaults())
	tracker := handler.Tracker()
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}
	if tracker.Count() != 0 {
		t.Fatalf("expected 0 tracked, got %d", tracker.Count())
	}
}

func TestWorkerStealthHandler_Profile(t *testing.T) {
	p := Defaults()
	handler := NewWorkerStealthHandler(p)
	got := handler.Profile()
	if got.UserAgent != p.UserAgent {
		t.Fatalf("expected UA=%s, got %s", p.UserAgent, got.UserAgent)
	}
}

func TestSafeJSONStringArray(t *testing.T) {
	result := SafeJSONStringArray([]string{"en-US", "en"}, `["fallback"]`)
	if result != `["en-US","en"]` {
		t.Fatalf("expected [\"en-US\",\"en\"], got %s", result)
	}
}

func TestSafeJSONStringArray_Empty(t *testing.T) {
	result := SafeJSONStringArray([]string{}, `["fallback"]`)
	if result != `[]` {
		t.Fatalf("expected [], got %s", result)
	}
}

func TestSafeJSONStringArray_Fallback(t *testing.T) {
	// Passing nil-safe values that would cause marshal to fail is hard
	// with []string, but we can test the fallback path by checking
	// the fallback is returned when the round-trip fails.
	// Since json.Marshal always succeeds for []string, we test the
	// fallback path indirectly by verifying the function returns valid JSON.
	result := SafeJSONStringArray([]string{"de-DE", "de"}, `["en-US","en"]`)
	var check []string
	if err := json.Unmarshal([]byte(result), &check); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}
	if len(check) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(check))
	}
}

func TestParseLanguages(t *testing.T) {
	result := parseLanguages("de-DE,de,en-US,en")
	if len(result) != 4 {
		t.Fatalf("expected 4 languages, got %d", len(result))
	}
	if result[0] != "de-DE" {
		t.Fatalf("expected first=de-DE, got %s", result[0])
	}
	if result[3] != "en" {
		t.Fatalf("expected last=en, got %s", result[3])
	}
}

func TestParseLanguages_Empty(t *testing.T) {
	result := parseLanguages("")
	if len(result) != 2 {
		t.Fatalf("expected 2 default languages, got %d", len(result))
	}
	if result[0] != "en-US" {
		t.Fatalf("expected en-US, got %s", result[0])
	}
}

func TestParseLanguages_WithSpaces(t *testing.T) {
	result := parseLanguages(" de-DE , en-US ")
	if len(result) != 2 {
		t.Fatalf("expected 2 languages, got %d", len(result))
	}
	if result[0] != "de-DE" {
		t.Fatalf("expected de-DE, got %s", result[0])
	}
}

func TestFirstLanguage(t *testing.T) {
	if firstLanguage("de-DE,de,en-US,en") != "de-DE" {
		t.Error("expected de-DE")
	}
	if firstLanguage("") != "en-US" {
		t.Error("expected en-US for empty input")
	}
}

func TestWorkerStealthScript_ParityWithMainPage(t *testing.T) {
	// The worker stealth script must apply the same UA as the main page
	// stealth script (spec L4058: same UA/emulation as main page).
	p := Defaults()
	mainScript := Script(p)
	workerScript := WorkerStealthScript(p)

	// Both scripts must reference the same UA.
	if !strings.Contains(mainScript, p.UserAgent) {
		t.Fatal("main script missing UA")
	}
	if !strings.Contains(workerScript, p.UserAgent) {
		t.Fatal("worker script missing UA")
	}

	// Both scripts must set webdriver to false/undefined.
	if !strings.Contains(mainScript, "webdriver") {
		t.Fatal("main script missing webdriver patch")
	}
	if !strings.Contains(workerScript, "webdriver") {
		t.Fatal("worker script missing webdriver patch")
	}
}

func TestWorkerStealthScript_NoWindowReference(t *testing.T) {
	// Worker contexts do not have `window` - the script must use
	// `self.navigator` instead (spec L4058: prevents detection via
	// worker navigator properties).
	script := WorkerStealthScript(Defaults())
	if strings.Contains(script, "window.navigator") {
		t.Error("script must not reference window.navigator (workers have no window)")
	}
	if !strings.Contains(script, "self.navigator") {
		t.Error("script must reference self.navigator for worker context")
	}
}
