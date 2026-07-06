package observe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkflowCaptureRecordBasic(t *testing.T) {
	r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "op1", "test-flow")
	action := ActionCapture{
		ToolCall: CapturedToolCall{
			Tool:     "browser_click",
			ArgsJSON: `{"ref":"e5"}`,
		},
		Selectors: []CapturedSelector{
			{Kind: SelectorAX, Value: "e5", Confidence: 0.9},
		},
	}
	if err := r.Record(action); err != nil {
		t.Fatalf("Record: %v", err)
	}
	rec := r.Recording()
	if len(rec.Actions) != 1 {
		t.Fatalf("Actions len = %d, want 1", len(rec.Actions))
	}
	if rec.Actions[0].Index != 0 {
		t.Errorf("Index = %d, want 0", rec.Actions[0].Index)
	}
}

func TestWorkflowCaptureRecordMultiple(t *testing.T) {
	r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "op1", "test-flow")
	for i := 0; i < 5; i++ {
		if err := r.Record(ActionCapture{
			ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
			Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e1", Confidence: 0.9}},
		}); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}
	rec := r.Stop()
	if len(rec.Actions) != 5 {
		t.Fatalf("Actions len = %d, want 5", len(rec.Actions))
	}
	for i, a := range rec.Actions {
		if a.Index != i {
			t.Errorf("Actions[%d].Index = %d, want %d", i, a.Index, i)
		}
	}
	if rec.TotalDurationMs < 0 {
		t.Errorf("TotalDurationMs = %d, want >= 0", rec.TotalDurationMs)
	}
}

func TestWorkflowCaptureDisabled(t *testing.T) {
	cfg := DefaultWorkflowCaptureConfig()
	cfg.Enabled = false
	r := NewWorkflowCaptureRecorder(cfg, "op1", "test-flow")
	err := r.Record(ActionCapture{
		ToolCall: CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
	})
	if err == nil {
		t.Error("expected error when disabled")
	}
	total, _, _, _, errors, _ := r.Stats()
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if errors != 1 {
		t.Errorf("errors = %d, want 1", errors)
	}
}

func TestWorkflowCaptureMaxActions(t *testing.T) {
	cfg := DefaultWorkflowCaptureConfig()
	cfg.MaxActions = 3
	r := NewWorkflowCaptureRecorder(cfg, "op1", "test-flow")
	for i := 0; i < 3; i++ {
		if err := r.Record(ActionCapture{
			ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
			Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e1", Confidence: 0.9}},
		}); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}
	err := r.Record(ActionCapture{
		ToolCall: CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
	})
	if err == nil {
		t.Error("expected error when max actions reached")
	}
}

func TestWorkflowCaptureScreenshotFallback(t *testing.T) {
	cfg := DefaultWorkflowCaptureConfig()
	r := NewWorkflowCaptureRecorder(cfg, "op1", "test-flow")
	// Low-confidence selector should trigger screenshot fallback.
	err := r.Record(ActionCapture{
		ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
		Selectors: []CapturedSelector{{Kind: SelectorCSS, Value: "div.btn", Confidence: 0.3}},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	rec := r.Recording()
	if rec.Actions[0].Screenshot == nil {
		t.Error("expected screenshot fallback for low confidence")
	}
	_, _, screenshots, _, _, _ := r.Stats()
	if screenshots != 1 {
		t.Errorf("screenshots = %d, want 1", screenshots)
	}
}

func TestWorkflowCaptureNoScreenshotForHighConfidence(t *testing.T) {
	cfg := DefaultWorkflowCaptureConfig()
	r := NewWorkflowCaptureRecorder(cfg, "op1", "test-flow")
	err := r.Record(ActionCapture{
		ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
		Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e5", Confidence: 0.95}},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	rec := r.Recording()
	if rec.Actions[0].Screenshot != nil {
		t.Error("expected no screenshot for high confidence")
	}
}

func TestWorkflowCaptureExplicitScreenshot(t *testing.T) {
	cfg := DefaultWorkflowCaptureConfig()
	r := NewWorkflowCaptureRecorder(cfg, "op1", "test-flow")
	err := r.Record(ActionCapture{
		ToolCall:   CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
		Selectors:  []CapturedSelector{{Kind: SelectorAX, Value: "e5", Confidence: 0.95}},
		Screenshot: &CapturedScreenshot{Reason: "manual"},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	_, _, screenshots, _, _, _ := r.Stats()
	if screenshots != 1 {
		t.Errorf("screenshots = %d, want 1", screenshots)
	}
}

func TestWorkflowCaptureDestructive(t *testing.T) {
	r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "op1", "test-flow")
	if err := r.Record(ActionCapture{
		ToolCall:      CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
		Selectors:     []CapturedSelector{{Kind: SelectorAX, Value: "e5", Confidence: 0.9}},
		IsDestructive: true,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	_, _, _, destructive, _, _ := r.Stats()
	if destructive != 1 {
		t.Errorf("destructive = %d, want 1", destructive)
	}
}

func TestWorkflowCaptureStopFinalizes(t *testing.T) {
	r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "op1", "test-flow")
	r.Record(ActionCapture{
		ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
		Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e5", Confidence: 0.9}},
	})
	rec := r.Stop()
	if rec.EndTime.IsZero() {
		t.Error("EndTime should not be zero after Stop")
	}
	if rec.TotalDurationMs < 0 {
		t.Errorf("TotalDurationMs = %d, want >= 0", rec.TotalDurationMs)
	}
}

func TestWorkflowCaptureSaveToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "recording.json")
	r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "op1", "test-flow")
	r.Record(ActionCapture{
		ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: `{"ref":"e5"}`},
		Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e5", Confidence: 0.9}},
	})
	if err := r.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}
	// Verify file permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 0600", info.Mode().Perm())
	}
	// Verify content is valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var rec WorkflowRecording
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rec.Actions) != 1 {
		t.Errorf("Actions len = %d, want 1", len(rec.Actions))
	}
	_, _, _, _, _, saved := r.Stats()
	if saved != 1 {
		t.Errorf("saved = %d, want 1", saved)
	}
}

func TestLoadRecordingFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recording.json")
	r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "op1", "test-flow")
	r.Record(ActionCapture{
		ToolCall:  CapturedToolCall{Tool: "browser_navigate", ArgsJSON: `{"url":"https://example.com"}`},
		Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e1", Confidence: 0.9}},
	})
	if err := r.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}
	loaded, err := LoadRecordingFromFile(path)
	if err != nil {
		t.Fatalf("LoadRecordingFromFile: %v", err)
	}
	if loaded.Name != "test-flow" {
		t.Errorf("Name = %s, want test-flow", loaded.Name)
	}
	if len(loaded.Actions) != 1 {
		t.Errorf("Actions len = %d, want 1", len(loaded.Actions))
	}
	if loaded.Actions[0].ToolCall.Tool != "browser_navigate" {
		t.Errorf("Tool = %s, want browser_navigate", loaded.Actions[0].ToolCall.Tool)
	}
}

func TestLoadRecordingFromFileMissing(t *testing.T) {
	_, err := LoadRecordingFromFile("/nonexistent/path.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSelectorConfidence(t *testing.T) {
	rec := &WorkflowRecording{
		Actions: []ActionCapture{
			{Selectors: []CapturedSelector{{Confidence: 0.9}, {Confidence: 0.8}}},
			{Selectors: []CapturedSelector{{Confidence: 0.7}}},
		},
	}
	c := SelectorConfidence(rec)
	want := (0.9 + 0.8 + 0.7) / 3.0
	// Use tolerance for floating-point comparison.
	diff := c - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-9 {
		t.Errorf("SelectorConfidence = %f, want %f", c, want)
	}
}

func TestSelectorConfidenceEmpty(t *testing.T) {
	rec := &WorkflowRecording{Actions: []ActionCapture{{}}}
	if c := SelectorConfidence(rec); c != 0 {
		t.Errorf("SelectorConfidence empty = %f, want 0", c)
	}
}

func TestSelectorConfidenceNil(t *testing.T) {
	if c := SelectorConfidence(nil); c != 0 {
		t.Errorf("SelectorConfidence nil = %f, want 0", c)
	}
}

func TestHasDestructiveActions(t *testing.T) {
	rec := &WorkflowRecording{
		Actions: []ActionCapture{
			{IsDestructive: false},
			{IsDestructive: true},
		},
	}
	if !HasDestructiveActions(rec) {
		t.Error("HasDestructiveActions should be true")
	}
	rec2 := &WorkflowRecording{
		Actions: []ActionCapture{{IsDestructive: false}},
	}
	if HasDestructiveActions(rec2) {
		t.Error("HasDestructiveActions should be false")
	}
}

func TestDestructiveStepIndices(t *testing.T) {
	rec := &WorkflowRecording{
		Actions: []ActionCapture{
			{Index: 0, IsDestructive: false},
			{Index: 1, IsDestructive: true},
			{Index: 2, IsDestructive: false},
			{Index: 3, IsDestructive: true},
		},
	}
	indices := DestructiveStepIndices(rec)
	if len(indices) != 2 {
		t.Fatalf("indices len = %d, want 2", len(indices))
	}
	if indices[0] != 1 || indices[1] != 3 {
		t.Errorf("indices = %v, want [1 3]", indices)
	}
}

func TestContainsCredentialsOrPII(t *testing.T) {
	rec := &WorkflowRecording{
		Actions: []ActionCapture{
			{ToolCall: CapturedToolCall{ArgsJSON: `{"name":"John"}`}},
			{ToolCall: CapturedToolCall{ArgsJSON: `{"password":"secret123"}`}},
		},
	}
	if !ContainsCredentialsOrPII(rec) {
		t.Error("ContainsCredentialsOrPII should be true for password")
	}
	rec2 := &WorkflowRecording{
		Actions: []ActionCapture{
			{ToolCall: CapturedToolCall{ArgsJSON: `{"name":"John"}`}},
		},
	}
	if ContainsCredentialsOrPII(rec2) {
		t.Error("ContainsCredentialsOrPII should be false for clean args")
	}
}

func TestContainsCredentialsOrPIIPatterns(t *testing.T) {
	patterns := []string{"password", "secret", "api_key", "apikey", "token", "ssn", "credit_card", "card_number"}
	for _, p := range patterns {
		rec := &WorkflowRecording{
			Actions: []ActionCapture{
				{ToolCall: CapturedToolCall{ArgsJSON: `{"` + p + `":"value"}`}},
			},
		}
		if !ContainsCredentialsOrPII(rec) {
			t.Errorf("ContainsCredentialsOrPII should be true for pattern %q", p)
		}
	}
}

func TestRenderSkillContent(t *testing.T) {
	rec := &WorkflowRecording{
		Name: "test-flow",
		Actions: []ActionCapture{
			{Index: 0, ToolCall: CapturedToolCall{Tool: "browser_navigate"}},
			{Index: 1, ToolCall: CapturedToolCall{Tool: "browser_click"}},
		},
	}
	content := RenderSkillContent(rec)
	if content == "" {
		t.Error("RenderSkillContent should not be empty")
	}
	if !strings.Contains(content, "test-flow") {
		t.Error("content should contain workflow name")
	}
	if !strings.Contains(content, "browser_navigate") {
		t.Error("content should contain tool name")
	}
}

func TestRenderSkillContentNil(t *testing.T) {
	if RenderSkillContent(nil) != "" {
		t.Error("RenderSkillContent(nil) should be empty")
	}
}

func TestSortSelectors(t *testing.T) {
	selectors := []CapturedSelector{
		{Kind: SelectorCSS, Value: "a", Confidence: 0.5},
		{Kind: SelectorAX, Value: "b", Confidence: 0.9},
		{Kind: SelectorXPath, Value: "c", Confidence: 0.7},
	}
	SortSelectors(selectors)
	if selectors[0].Confidence != 0.9 {
		t.Errorf("first confidence = %f, want 0.9", selectors[0].Confidence)
	}
	if selectors[1].Confidence != 0.7 {
		t.Errorf("second confidence = %f, want 0.7", selectors[1].Confidence)
	}
	if selectors[2].Confidence != 0.5 {
		t.Errorf("third confidence = %f, want 0.5", selectors[2].Confidence)
	}
}

func TestWorkflowCaptureSetNow(t *testing.T) {
	r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "op1", "test-flow")
	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	r.SetNow(func() time.Time { return fixedTime })
	r.Record(ActionCapture{
		ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
		Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e5", Confidence: 0.9}},
	})
	rec := r.Stop()
	if !rec.EndTime.Equal(fixedTime) {
		t.Errorf("EndTime = %v, want %v", rec.EndTime, fixedTime)
	}
}

func TestWorkflowCaptureConcurrentRecord(t *testing.T) {
	r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "op1", "test-flow")
	done := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		go func() {
			r.Record(ActionCapture{
				ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
				Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e1", Confidence: 0.9}},
			})
			done <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
	rec := r.Stop()
	if len(rec.Actions) != 20 {
		t.Errorf("Actions len = %d, want 20", len(rec.Actions))
	}
	// Verify indices are unique and sequential 0..19.
	seen := make(map[int]bool)
	for _, a := range rec.Actions {
		if seen[a.Index] {
			t.Errorf("duplicate Index %d", a.Index)
		}
		seen[a.Index] = true
	}
	for i := 0; i < 20; i++ {
		if !seen[i] {
			t.Errorf("missing Index %d", i)
		}
	}
}
