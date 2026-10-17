package observe

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
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
	cfg.CaptureScreenshot = func(context.Context, string) (*CapturedScreenshot, error) {
		return workflowTestScreenshot(t), nil
	}
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
		Screenshot: workflowTestScreenshot(t),
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
	if err := r.Record(ActionCapture{
		ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
		Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e5", Confidence: 0.9}},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
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
	if err := r.Record(ActionCapture{
		ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: `{"ref":"e5"}`},
		Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e5", Confidence: 0.9}},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := r.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}
	// Verify file permissions.
	info, err := os.Stat(filepath.Clean(path))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 0600", info.Mode().Perm())
	}
	// Verify content is valid JSON.
	data, err := os.ReadFile(filepath.Clean(path))
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
	if err := r.Record(ActionCapture{
		ToolCall:  CapturedToolCall{Tool: "browser_navigate", ArgsJSON: `{"url":"https://example.com"}`},
		Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e1", Confidence: 0.9}},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
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
	if err := r.Record(ActionCapture{
		ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
		Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e5", Confidence: 0.9}},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	rec := r.Stop()
	if !rec.EndTime.Equal(fixedTime) {
		t.Errorf("EndTime = %v, want %v", rec.EndTime, fixedTime)
	}
}

func TestWorkflowCaptureConcurrentRecord(t *testing.T) {
	r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "op1", "test-flow")
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			results <- r.Record(ActionCapture{
				ToolCall:  CapturedToolCall{Tool: "browser_click", ArgsJSON: "{}"},
				Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e1", Confidence: 0.9}},
			})
		}()
	}
	for i := 0; i < 20; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent Record: %v", err)
		}
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

func workflowTestScreenshot(t *testing.T) *CapturedScreenshot {
	t.Helper()
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return &CapturedScreenshot{Timestamp: time.Now(), Reason: "actual_test_pixels", DataB64: base64.StdEncoding.EncodeToString(pixels.Bytes())}
}

func TestWorkflowCaptureCannotInventPixelsOrAppendAfterStop(t *testing.T) {
	r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "owner", "flow")
	action := ActionCapture{ToolCall: CapturedToolCall{Tool: "click", ArgsJSON: "{}"}, Selectors: []CapturedSelector{{Kind: SelectorCSS, Value: "button", Confidence: .3}}}
	if err := r.Record(action); err != nil {
		t.Fatal(err)
	}
	first := r.Stop()
	if first.Actions[0].Screenshot != nil || !first.Actions[0].ScreenshotNeeded {
		t.Fatalf("missing real pixels became fabricated metadata: %+v", first.Actions[0])
	}
	_, _, screenshots, _, _, _ := r.Stats()
	if screenshots != 0 {
		t.Fatalf("nonexistent screenshots counted: %d", screenshots)
	}
	r.SetNow(func() time.Time { return first.EndTime.Add(time.Hour) })
	if second := r.Stop(); !second.EndTime.Equal(first.EndTime) {
		t.Fatal("repeated Stop rewrote the actual endpoint")
	}
	if err := r.Record(action); err == nil || len(r.Recording().Actions) != 1 {
		t.Fatalf("stopped recording admitted another step: %v", err)
	}
	for _, screenshot := range []*CapturedScreenshot{{Reason: "metadata_only"}, {DataB64: base64.StdEncoding.EncodeToString([]byte("not pixels"))}} {
		fresh := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "owner", "flow")
		action.Screenshot = screenshot
		if err := fresh.Record(action); err == nil || len(fresh.Recording().Actions) != 0 {
			t.Fatalf("invalid screenshot admitted: %+v err=%v", screenshot, err)
		}
	}
}

func TestWorkflowCaptureSnapshotsAreImmutableAndSavePreservesExistingFiles(t *testing.T) {
	cfg := DefaultWorkflowCaptureConfig()
	cfg.ScreenshotDir = filepath.Join(t.TempDir(), "pixels")
	r := NewWorkflowCaptureRecorder(cfg, "owner", "flow")
	action := ActionCapture{ToolCall: CapturedToolCall{Tool: "click", ArgsJSON: "{}"}, AXBefore: &CapturedAXSnapshot{NodeCount: 1, Nodes: []AXNode{{ID: "e1", Name: "Save", ChildIDs: []string{"e2"}}}}, Selectors: []CapturedSelector{{Kind: SelectorAX, Value: "e1", Confidence: .9}}, Screenshot: workflowTestScreenshot(t)}
	if err := r.Record(action); err != nil {
		t.Fatal(err)
	}
	action.AXBefore.Nodes[0].Name, action.AXBefore.Nodes[0].ChildIDs[0] = "external", "foreign"
	action.Selectors[0].Value = "foreign"
	returned := r.Recording()
	returned.Actions[0].AXBefore.Nodes[0].Name = "returned mutation"
	actual := r.Recording().Actions[0]
	if actual.AXBefore.Nodes[0].Name != "Save" || actual.AXBefore.Nodes[0].ChildIDs[0] != "e2" || actual.Selectors[0].Value != "e1" || actual.Screenshot.Path == "" || actual.Screenshot.DataB64 != "" {
		t.Fatalf("recording leaked mutable aliases or failed pixel persistence: %+v", actual)
	}
	if _, err := validateCapturedPixels(actual.Screenshot); err != nil {
		t.Fatalf("persisted pixels are not a real image: %v", err)
	}
	path := filepath.Join(t.TempDir(), "recording.json")
	if err := r.SaveToFile(path); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SaveToFile(path); err == nil {
		t.Fatal("existing recording was overwritten")
	}
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatalf("existing recording bytes changed: %v", err)
	}
}

func TestWorkflowCaptureCancellationAndPayloadBudgets(t *testing.T) {
	cfg := DefaultWorkflowCaptureConfig()
	cfg.MaxBytes = 4096
	r := NewWorkflowCaptureRecorder(cfg, "owner", "flow")
	action := ActionCapture{ToolCall: CapturedToolCall{Tool: "click", ArgsJSON: "{}"}}
	if err := r.Record(action); err == nil || !strings.Contains(err.Error(), "byte budget exceeded") || len(r.Recording().Actions) != 0 {
		t.Fatalf("reserved metadata exceeded declared payload budget: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fresh := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "owner", "flow")
	if err := fresh.RecordContext(ctx, action); err != context.Canceled || len(fresh.Recording().Actions) != 0 {
		t.Fatalf("cancelled capture appended evidence: %v", err)
	}
}

func TestWorkflowCaptureStopCancelsPixelsAndWaiterCanExpire(t *testing.T) {
	started := make(chan struct{})
	cfg := DefaultWorkflowCaptureConfig()
	cfg.CaptureScreenshot = func(ctx context.Context, _ string) (*CapturedScreenshot, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	r := NewWorkflowCaptureRecorder(cfg, "owner", "flow")
	defer r.Stop()
	action := ActionCapture{ToolCall: CapturedToolCall{Tool: "click", ArgsJSON: "{}"}, Selectors: []CapturedSelector{{Kind: SelectorCSS, Value: "button", Confidence: .3}}}
	joined := make(chan error, 1)
	go func() { joined <- r.RecordContext(t.Context(), action) }()
	<-started
	waitCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := r.RecordContext(waitCtx, action); err != context.DeadlineExceeded {
		t.Fatalf("queued record did not respect its own deadline: %v", err)
	}
	if snapshot := r.Stop(); len(snapshot.Actions) != 0 {
		t.Fatal("unfinished capture was published at Stop")
	}
	if err := <-joined; err == nil {
		t.Fatal("Stop did not cancel the owning pixel capture")
	}
}

func TestWorkflowCaptureRejectsImageHeaderWithoutPixels(t *testing.T) {
	screenshot := workflowTestScreenshot(t)
	raw, err := base64.StdEncoding.DecodeString(screenshot.DataB64)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 33 {
		t.Fatal("fixture does not contain a complete PNG image header")
	}
	header := raw[:33]
	if _, err := png.DecodeConfig(bytes.NewReader(header)); err != nil {
		t.Fatalf("negative fixture must have valid dimensions before missing pixels: %v", err)
	}
	screenshot.DataB64 = base64.StdEncoding.EncodeToString(header)
	if _, err := validateCapturedPixels(screenshot); err == nil {
		t.Fatal("valid geometry alone manufactured actual pixel evidence")
	}
}

func TestWorkflowCaptureRejectsInvalidToolAndSelectorEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		action ActionCapture
	}{
		{"invalid JSON", ActionCapture{ToolCall: CapturedToolCall{Tool: "click", ArgsJSON: "broken"}}},
		{"unknown selector", ActionCapture{ToolCall: CapturedToolCall{Tool: "click"}, Selectors: []CapturedSelector{{Kind: "unknown", Value: "e1", Confidence: .9}}}},
		{"missing selector", ActionCapture{ToolCall: CapturedToolCall{Tool: "click"}, Selectors: []CapturedSelector{{Kind: SelectorAX, Confidence: .9}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := NewWorkflowCaptureRecorder(DefaultWorkflowCaptureConfig(), "owner", "flow")
			if err := r.Record(test.action); err == nil || len(r.Recording().Actions) != 0 {
				t.Fatalf("invalid evidence appended: %+v err=%v", test.action, err)
			}
		})
	}
}
