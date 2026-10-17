package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// workflow_capture.go (spec L4561: Workflow Capture Recorder).
//
// WorkflowCaptureRecorder stores owner-supplied AX snapshots, selectors,
// action traces and actual optional pixels for guided workflow capture
// (spec ss3.5a.1 L1024). Its artifact is a workflow-draft input; validation
// of supplied data does not establish browser provenance or execute actions.
//
// The recorder is intentionally decoupled from the CDP layer: it accepts
// action evidence supplied by its owner. RecordContext can obtain actual
// fallback pixels through that owner's cancellable browser callback.
//
// Reference: research/agents/openclaw-main/extensions/browser/src/browser/pw-tools-core.activity.ts

// WorkflowCaptureConfig controls the recorder (spec L4561).
type WorkflowCaptureConfig struct {
	// Enabled gates recording. When false, Record returns an error.
	Enabled bool
	// ScreenshotFallback requests real pixels below the confidence threshold.
	ScreenshotFallback bool
	// SelectorConfidenceThreshold is the confidence below which a screenshot
	// fallback is taken (default 0.7).
	SelectorConfidenceThreshold float64
	// ScreenshotDir is where fallback screenshots are written. Empty means
	// screenshots are kept in-memory only (path stays empty).
	ScreenshotDir string
	// MaxActions caps the recording length; zero selects 128.
	MaxActions int
	// MaxBytes bounds charged recording content, including fallback pixels.
	// Zero selects 8 MiB; metadata reserves 4096 bytes within this bound.
	MaxBytes int
	// CaptureScreenshot is supplied by the owning browser. Nil reports missing
	// fallback evidence instead of manufacturing screenshot metadata.
	CaptureScreenshot func(context.Context, string) (*CapturedScreenshot, error)
}

// DefaultWorkflowCaptureConfig returns the canonical recorder config.
func DefaultWorkflowCaptureConfig() WorkflowCaptureConfig {
	return WorkflowCaptureConfig{
		Enabled:                     true,
		ScreenshotFallback:          true,
		SelectorConfidenceThreshold: 0.7,
	}
}

// SelectorKind identifies the selector strategy (spec L4561).
type SelectorKind string

const (
	SelectorCSS   SelectorKind = "css"
	SelectorAX    SelectorKind = "ax"
	SelectorXPath SelectorKind = "xpath"
	SelectorUCS   SelectorKind = "ucs"
)

// CapturedSelector is a stable selector for an action target (spec L4561).
type CapturedSelector struct {
	Kind       SelectorKind `json:"kind"`
	Value      string       `json:"value"`
	Confidence float64      `json:"confidence"`
}

// CapturedAXSnapshot is a compact AX snapshot taken before/after an action
// (spec L4561). It reuses AXNode from ax_diff.go.
type CapturedAXSnapshot struct {
	Timestamp time.Time `json:"timestamp"`
	NodeCount int       `json:"node_count"`
	// Nodes holds the source's observed bounded projection; NodeCount retains
	// its observed total rather than claiming that omissions were captured.
	Nodes []AXNode `json:"nodes,omitempty"`
}

// CapturedScreenshot is a fallback screenshot reference (spec L4561).
type CapturedScreenshot struct {
	Path      string    `json:"path,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason,omitempty"`
	// DataB64 is the base64-encoded screenshot when ScreenshotDir is empty.
	DataB64 string `json:"data_b64,omitempty"`
}

// CapturedToolCall is the tool-call trace for an action (spec L4561).
type CapturedToolCall struct {
	Tool       string `json:"tool"`
	ArgsJSON   string `json:"args_json"`
	ResultJSON string `json:"result_json,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// ActionCapture is a single recorded browser action (spec L4561).
type ActionCapture struct {
	Index            int                 `json:"index"`
	ToolCall         CapturedToolCall    `json:"tool_call"`
	AXBefore         *CapturedAXSnapshot `json:"ax_before,omitempty"`
	AXAfter          *CapturedAXSnapshot `json:"ax_after,omitempty"`
	Selectors        []CapturedSelector  `json:"selectors"`
	Screenshot       *CapturedScreenshot `json:"screenshot,omitempty"`
	ScreenshotNeeded bool                `json:"screenshot_needed,omitempty"`
	Timestamp        time.Time           `json:"timestamp"`
	IsDestructive    bool                `json:"is_destructive"`
}

// WorkflowRecording is a complete recorded workflow session (spec L4561).
type WorkflowRecording struct {
	ID              string          `json:"id"`
	OperatorID      string          `json:"operator_id"`
	Name            string          `json:"name"`
	Actions         []ActionCapture `json:"actions"`
	StartTime       time.Time       `json:"start_time"`
	EndTime         time.Time       `json:"end_time"`
	TotalDurationMs int64           `json:"total_duration_ms"`
}

// WorkflowCaptureStats tracks recorder counters (spec L4561).
type WorkflowCaptureStats struct {
	Total       atomic.Int64
	Recorded    atomic.Int64
	Screenshots atomic.Int64
	Destructive atomic.Int64
	Errors      atomic.Int64
	SavedToFile atomic.Int64
}

// WorkflowCaptureRecorder records browser actions into a WorkflowRecording
// (spec L4561). It is safe for concurrent use.
type WorkflowCaptureRecorder struct {
	mu            sync.Mutex
	cfg           WorkflowCaptureConfig
	recording     *WorkflowRecording
	stats         WorkflowCaptureStats
	now           func() time.Time
	stopped       bool
	bytes         int
	captureLease  chan struct{}
	captureCancel context.CancelFunc
}

// NewWorkflowCaptureRecorder creates a recorder for the given operator and
// workflow name. The recording starts immediately.
func NewWorkflowCaptureRecorder(cfg WorkflowCaptureConfig, operatorID, name string) *WorkflowCaptureRecorder {
	if cfg.SelectorConfidenceThreshold <= 0 {
		cfg.SelectorConfidenceThreshold = 0.7
	}
	if cfg.MaxActions == 0 {
		cfg.MaxActions = 128
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 8 * 1024 * 1024
	}
	return &WorkflowCaptureRecorder{
		cfg:          cfg,
		bytes:        4096,
		captureLease: make(chan struct{}, 1),
		recording: &WorkflowRecording{
			ID:         fmt.Sprintf("wf-%d", time.Now().UnixNano()),
			OperatorID: operatorID,
			Name:       name,
			StartTime:  time.Now(),
		},
		now: time.Now,
	}
}

// SetNow replaces the clock used for timestamps. Intended for tests.
func (r *WorkflowCaptureRecorder) SetNow(fn func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if fn == nil {
		fn = time.Now
	}
	r.now = fn
}

// Record adds a single action to the recording (spec L4561). Returns an
// error if the recorder is disabled or the action cap is reached. The
// action's Index is assigned automatically.
func (r *WorkflowCaptureRecorder) Record(action ActionCapture) error {
	return r.RecordContext(context.Background(), action)
}

// RecordContext admits an immutable action and optional actual fallback pixels.
func (r *WorkflowCaptureRecorder) RecordContext(ctx context.Context, action ActionCapture) error {
	if r == nil || r.captureLease == nil || ctx == nil {
		return fmt.Errorf("workflow capture: recorder and context required")
	}
	r.stats.Total.Add(1)
	select {
	case r.captureLease <- struct{}{}:
		defer func() { <-r.captureLease }()
	case <-ctx.Done():
		r.stats.Errors.Add(1)
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		r.mu.Lock()
		r.captureCancel = nil
		r.mu.Unlock()
	}()
	r.mu.Lock()
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		r.stats.Errors.Add(1)
		return err
	}
	if r.stopped || r.cfg.MaxActions < 1 || r.cfg.MaxBytes < 4096 || math.IsNaN(r.cfg.SelectorConfidenceThreshold) || math.IsInf(r.cfg.SelectorConfidenceThreshold, 0) || r.cfg.SelectorConfidenceThreshold > 1 || len(r.recording.Name) > 256 || len(r.recording.OperatorID) > 128 {
		r.mu.Unlock()
		r.stats.Errors.Add(1)
		return fmt.Errorf("workflow capture: stopped or invalid recording bounds")
	}

	if !r.cfg.Enabled {
		r.mu.Unlock()
		r.stats.Errors.Add(1)
		return fmt.Errorf("workflow capture: disabled")
	}

	if r.cfg.MaxActions > 0 && len(r.recording.Actions) >= r.cfg.MaxActions {
		r.mu.Unlock()
		r.stats.Errors.Add(1)
		return fmt.Errorf("workflow capture: max actions (%d) reached", r.cfg.MaxActions)
	}

	action.Index = len(r.recording.Actions)
	if action.Timestamp.IsZero() {
		action.Timestamp = r.now()
	}

	if err := validateCaptureShape(action); err != nil {
		r.mu.Unlock()
		r.stats.Errors.Add(1)
		return err
	}
	r.captureCancel = cancel
	r.mu.Unlock()
	action = cloneActionCapture(action)
	// Missing pixels are explicit; only the browser owner can supply them.
	if r.cfg.ScreenshotFallback && action.Screenshot == nil {
		if minConfidence(action.Selectors) < r.cfg.SelectorConfidenceThreshold {
			action.ScreenshotNeeded = true
			if r.cfg.CaptureScreenshot != nil {
				var err error
				action.Screenshot, err = r.cfg.CaptureScreenshot(ctx, "low_selector_confidence")
				if err != nil {
					r.stats.Errors.Add(1)
					return fmt.Errorf("workflow capture: fallback pixels: %w", err)
				}
				if action.Screenshot == nil {
					r.stats.Errors.Add(1)
					return fmt.Errorf("workflow capture: browser owner returned no fallback pixels")
				}
				copied := *action.Screenshot
				action.Screenshot = &copied
			}
		}
	}
	if err := ctx.Err(); err != nil {
		r.stats.Errors.Add(1)
		return err
	}
	charge, err := validateCapturePayload(action)
	if r.cfg.ScreenshotDir != "" && action.Screenshot != nil && action.Screenshot.DataB64 != "" {
		// Reserve escaped path metadata before any new image file is created.
		charge += 6 * (len(r.cfg.ScreenshotDir) + 128)
	}
	if err != nil || charge > r.cfg.MaxBytes-r.bytes {
		r.stats.Errors.Add(1)
		if err != nil {
			return err
		}
		return fmt.Errorf("workflow capture: recording byte budget exceeded")
	}
	if err := ctx.Err(); err != nil {
		r.stats.Errors.Add(1)
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		r.stats.Errors.Add(1)
		return fmt.Errorf("workflow capture: stopped before publication")
	}
	if action.Screenshot != nil {
		if r.cfg.ScreenshotDir != "" && action.Screenshot.DataB64 != "" {
			if err := persistCapturedPixels(action.Screenshot, r.cfg.ScreenshotDir, r.recording.ID, action.Index); err != nil {
				r.stats.Errors.Add(1)
				return err
			}
		}
		action.ScreenshotNeeded = false
		r.stats.Screenshots.Add(1)
	}

	if action.IsDestructive {
		r.stats.Destructive.Add(1)
	}

	r.recording.Actions = append(r.recording.Actions, cloneActionCapture(action))
	r.bytes += charge
	r.stats.Recorded.Add(1)
	return nil
}

// Stop finalizes the recording and returns it (spec L4561).
func (r *WorkflowCaptureRecorder) Stop() *WorkflowRecording {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.stopped {
		r.stopped = true
		if r.captureCancel != nil {
			r.captureCancel()
		}
		r.recording.EndTime = r.now()
		r.recording.TotalDurationMs = max(0, r.recording.EndTime.Sub(r.recording.StartTime).Milliseconds())
	}
	return cloneWorkflowRecording(r.recording)
}

// Recording returns the current in-progress recording without finalizing.
func (r *WorkflowCaptureRecorder) Recording() *WorkflowRecording {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneWorkflowRecording(r.recording)
}

// Stats returns a snapshot of the recorder counters.
func (r *WorkflowCaptureRecorder) Stats() (total, recorded, screenshots, destructive, errors, saved int64) {
	return r.stats.Total.Load(),
		r.stats.Recorded.Load(),
		r.stats.Screenshots.Load(),
		r.stats.Destructive.Load(),
		r.stats.Errors.Load(),
		r.stats.SavedToFile.Load()
}

// SaveToFile serializes the recording to a JSON file (spec L4561). The
// recording must be stopped first (or Stop is called implicitly). Parent
// directories are created with 0700 permissions; the file is written 0600.
func (r *WorkflowCaptureRecorder) SaveToFile(path string) error {
	if r == nil || len(r.recording.Name) > 256 || len(r.recording.OperatorID) > 128 || r.cfg.MaxBytes < 4096 {
		return fmt.Errorf("workflow capture: valid bounded recorder required for saving")
	}
	path = filepath.Clean(path)
	rec := r.Stop()
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("workflow capture: marshal: %w", err)
	}
	if len(data) > r.cfg.MaxBytes {
		return fmt.Errorf("workflow capture: serialized recording exceeds byte budget")
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("workflow capture: mkdir: %w", err)
		}
	}
	if err := writeNewCaptureFile(path, data); err != nil {
		return fmt.Errorf("workflow capture: write: %w", err)
	}
	r.stats.SavedToFile.Add(1)
	return nil
}

// LoadRecordingFromFile reads a previously saved recording (spec L4561).
func LoadRecordingFromFile(path string) (*WorkflowRecording, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workflow capture: read %s: %w", path, err)
	}
	var rec WorkflowRecording
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("workflow capture: parse: %w", err)
	}
	return &rec, nil
}

// SelectorConfidence calculates the average confidence across all selectors
// in all actions of a recording (spec L4561).
func SelectorConfidence(rec *WorkflowRecording) float64 {
	if rec == nil || len(rec.Actions) == 0 {
		return 0
	}
	total := 0.0
	count := 0
	for _, a := range rec.Actions {
		for _, s := range a.Selectors {
			total += s.Confidence
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

// HasDestructiveActions reports whether the recording contains any
// destructive actions (spec L4561).
func HasDestructiveActions(rec *WorkflowRecording) bool {
	if rec == nil {
		return false
	}
	for _, a := range rec.Actions {
		if a.IsDestructive {
			return true
		}
	}
	return false
}

// DestructiveStepIndices returns the 0-based indices of destructive actions.
func DestructiveStepIndices(rec *WorkflowRecording) []int {
	if rec == nil {
		return nil
	}
	var out []int
	for _, a := range rec.Actions {
		if a.IsDestructive {
			out = append(out, a.Index)
		}
	}
	return out
}

// ContainsCredentialsOrPII scans the recording's tool-call args for known
// credential/PII literal patterns (spec L4561). Returns true if any action's
// args contain a suspicious pattern.
func ContainsCredentialsOrPII(rec *WorkflowRecording) bool {
	if rec == nil {
		return false
	}
	piiPatterns := []string{"password", "secret", "api_key", "apikey", "token", "ssn", "credit_card", "card_number"}
	for _, a := range rec.Actions {
		lower := strings.ToLower(a.ToolCall.ArgsJSON)
		for _, p := range piiPatterns {
			if strings.Contains(lower, p) {
				return true
			}
		}
	}
	return false
}

// RenderSkillContent produces a draft SKILL.md content string from a
// recording (spec L4561). This is a compact preview; the full skill draft
// is produced by the the host workflow-capture pipeline.
func RenderSkillContent(rec *WorkflowRecording) string {
	if rec == nil {
		return ""
	}
	var b []byte
	b = fmt.Appendf(b, "# %s\n\n", rec.Name)
	b = append(b, "Category: workflow\n\n"...)
	b = append(b, "## Steps\n\n"...)
	for _, a := range rec.Actions {
		b = fmt.Appendf(b, "%d. %s (tool: %s)\n", a.Index+1, a.ToolCall.Tool, a.ToolCall.Tool)
	}
	return string(b)
}

// SortSelectors sorts selectors by confidence descending (highest first).
func SortSelectors(selectors []CapturedSelector) {
	sort.Slice(selectors, func(i, j int) bool {
		return selectors[i].Confidence > selectors[j].Confidence
	})
}

// minConfidence returns the lowest confidence among the selectors, or 1.0
// when the slice is empty (treat empty as "no selectors needed" = high
// confidence so no screenshot fallback fires).
func minConfidence(selectors []CapturedSelector) float64 {
	if len(selectors) == 0 {
		return 1.0
	}
	min := selectors[0].Confidence
	for _, s := range selectors[1:] {
		if s.Confidence < min {
			min = s.Confidence
		}
	}
	return min
}
