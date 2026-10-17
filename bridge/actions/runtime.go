package actions

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	formactions "github.com/Christopher-Schulze/Artemis/actions"
	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/cdpops"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	artemistabs "github.com/Christopher-Schulze/Artemis/bridge/tabs"
	artemisdownload "github.com/Christopher-Schulze/Artemis/download"
	"github.com/Christopher-Schulze/Artemis/input"
	"github.com/Christopher-Schulze/Artemis/platform"
)

type Kind string

const (
	KindNavigate      Kind = "navigate"
	KindReload        Kind = "reload"
	KindBack          Kind = "back"
	KindForward       Kind = "forward"
	KindWait          Kind = "wait"
	KindFocus         Kind = "focus"
	KindClick         Kind = "click"
	KindHover         Kind = "hover"
	KindScroll        Kind = "scroll"
	KindKey           Kind = "key"
	KindType          Kind = "type"
	KindClear         Kind = "clear"
	KindFill          Kind = "fill"
	KindFillForm      Kind = "fill_form"
	KindSelect        Kind = "select"
	KindCheck         Kind = "check"
	KindUncheck       Kind = "uncheck"
	KindDrag          Kind = "drag"
	KindUpload        Kind = "upload"
	KindDownload      Kind = "download"
	KindScreenshot    Kind = "screenshot"
	KindPDF           Kind = "pdf"
	KindDialog        Kind = "dialog"
	KindTabOpen       Kind = "tab_open"
	KindTabClose      Kind = "tab_close"
	KindTabList       Kind = "tab_list"
	KindTabSwitch     Kind = "tab_switch"
	KindEvaluate      Kind = "evaluate"
	KindAssert        Kind = "assert"
	KindViewport      Kind = "viewport"
	KindFrameEvaluate Kind = "frame_evaluate"
)

type FailureClass string

const (
	FailureValidation    FailureClass = "validation"
	FailurePolicy        FailureClass = "policy_denied"
	FailureTarget        FailureClass = "target"
	FailureActionability FailureClass = "actionability"
	FailureProtocol      FailureClass = "protocol"
	FailurePostcondition FailureClass = "postcondition"
	FailureTimeout       FailureClass = "timeout"
	FailureCancelled     FailureClass = "cancelled"
	FailureEvidence      FailureClass = "evidence_unavailable"
)

type Postcondition struct {
	Type     string `json:"type"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
	Passed   bool   `json:"passed"`
}
type Evidence struct {
	ActionID        string                `json:"actionId,omitempty"`
	Action          Kind                  `json:"action"`
	Effect          EffectState           `json:"effect"`
	CaptureAttempts int                   `json:"captureAttempts"`
	ReplayAdmitted  bool                  `json:"replayAdmitted"`
	Ref             string                `json:"ref,omitempty"`
	PolicyDecision  bridge.PolicyDecision `json:"policyDecision,omitempty"`
	BackendDOMID    int64                 `json:"backendDomId,omitempty"`
	FrameID         string                `json:"frameId,omitempty"`
	BeforeHash      string                `json:"beforeHash,omitempty"`
	AfterHash       string                `json:"afterHash,omitempty"`
	StartedAt       time.Time             `json:"startedAt"`
	Duration        time.Duration         `json:"duration"`
	Attempts        int                   `json:"attempts"`
	Postcondition   Postcondition         `json:"postcondition"`
}
type Request struct {
	Kind             Kind                    `json:"kind"`
	Ref              string                  `json:"ref,omitempty"`
	TargetRef        string                  `json:"targetRef,omitempty"`
	URL              string                  `json:"url,omitempty"`
	Text             string                  `json:"text,omitempty"`
	Value            string                  `json:"value,omitempty"`
	Key              string                  `json:"key,omitempty"`
	Expression       string                  `json:"expression,omitempty"`
	Files            []string                `json:"files,omitempty"`
	DownloadDir      string                  `json:"downloadDir,omitempty"`
	Format           string                  `json:"format,omitempty"`
	Quality          int                     `json:"quality,omitempty"`
	ForModel         bool                    `json:"forModel,omitempty"`
	X                float64                 `json:"x,omitempty"`
	Y                float64                 `json:"y,omitempty"`
	DeltaX           float64                 `json:"deltaX,omitempty"`
	DeltaY           float64                 `json:"deltaY,omitempty"`
	Timeout          time.Duration           `json:"timeout,omitempty"`
	Accept           bool                    `json:"accept,omitempty"`
	PromptText       string                  `json:"promptText,omitempty"`
	DialogGeneration uint64                  `json:"dialogGeneration,omitempty"`
	Idempotent       bool                    `json:"idempotent,omitempty"`
	RetryMax         int                     `json:"retryMax,omitempty"`
	Width            int                     `json:"width,omitempty"`
	Height           int                     `json:"height,omitempty"`
	FrameID          string                  `json:"frameId,omitempty"`
	TargetID         string                  `json:"targetId,omitempty"`
	FormIntent       *formactions.FormIntent `json:"formIntent,omitempty"`
	// Button selects the click button (left, right, middle); empty is left.
	Button string `json:"button,omitempty"`
	// Human executes click, type and scroll with human-like real CDP input.
	Human *HumanProfile `json:"human,omitempty"`
	// Pointer is an explicit visual fallback: click or hover at a top-level
	// viewport point instead of a reference. Only the grounding owner sets
	// it after binding the point to a current visual observation.
	Pointer *PointerTarget `json:"pointer,omitempty"`
}

// PointerTarget is a top-level viewport point in CSS pixels.
type PointerTarget struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Outcome struct {
	Success            bool                  `json:"success"`
	Value              any                   `json:"value,omitempty"`
	Bytes              []byte                `json:"bytes,omitempty"`
	MIME               string                `json:"mime,omitempty"`
	Width              int                   `json:"width,omitempty"`
	Height             int                   `json:"height,omitempty"`
	DecodePath         string                `json:"decodePath,omitempty"`
	DecodeAcceleration *platform.ProbeResult `json:"decodeAcceleration,omitempty"`
	InputMemory        *platform.ProbeResult `json:"inputMemory,omitempty"`
	ImageMemory        *platform.ProbeResult `json:"imageMemory,omitempty"`
	ModelPrepared      bool                  `json:"modelPrepared,omitempty"`
	TargetID           string                `json:"targetId,omitempty"`
	OutcomeUnknown     bool                  `json:"outcomeUnknown,omitempty"`
	Download           *Download             `json:"download,omitempty"`
	Failure            FailureClass          `json:"failure,omitempty"`
	Error              string                `json:"error,omitempty"`
	Evidence           Evidence              `json:"evidence"`
}
type Download struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}
type Policy func(context.Context, Request) error

type Runtime struct {
	page           *bridge.Page
	observer       *bridgeobserve.Collector
	policy         Policy
	policyHook     *bridge.PolicyHook
	now            func() time.Time
	defaultTimeout time.Duration
	navigator      *cdpops.Navigator
	pointer        *cdpops.PointerDispatcher
	tabs           *artemistabs.TabRegistry
	userID         string
	downloads      *artemisdownload.DownloadManager
	downloadOnce   sync.Once
	downloadErr    error
	formIntents    *formactions.FormIntentRuntime
	jpegOnce       sync.Once
	jpegPipeline   *bridge.JPEGPipeline
	inputMemory    bridge.ScreenshotMemoryAdmission
	imageMetrics   func(bridge.ImageMetricEvent)
	imageCPU       func(context.Context) (bridge.ImageCPUConfig, error)
	pointerMu      sync.Mutex
	pointerAt      input.MousePoint
	// stopRecycler ends the tab recycling supervisor; Close runs it once.
	stopRecycler func()
	closeOnce    sync.Once
	closeErr     error
}

// RuntimeConfig injects lifecycle owners used by action execution.
type RuntimeConfig struct {
	Downloads         *artemisdownload.DownloadManager
	FormIntentMetrics func(formactions.FormIntentMetricEvent)
	// UserID scopes tab ownership and listings to the authenticated session.
	UserID string
	// DefaultTimeout bounds each action when Request.Timeout is unset.
	// Zero keeps the built-in 15s default.
	DefaultTimeout time.Duration
	// PolicyEngine authorizes actual native target realms before each effect.
	// Managed callers provide it; low-level callers own their authorization.
	PolicyEngine bridge.PolicyEngine
	InputMemory  bridge.ScreenshotMemoryAdmission
	ImageMetrics func(bridge.ImageMetricEvent)
	ImageCPU     func(context.Context) (bridge.ImageCPUConfig, error)
	// TabRecycle tunes idle and memory-pressure recycling of the tabs this
	// runtime opens (spec L4482, L4522); zero fields take the spec defaults.
	TabRecycle artemistabs.RecyclePolicy
	// DisableTabRecycle turns pool tab recycling off.
	DisableTabRecycle bool
}

func NewRuntime(page *bridge.Page, observer *bridgeobserve.Collector, policy Policy) (*Runtime, error) {
	return NewRuntimeWithConfig(page, observer, policy, RuntimeConfig{})
}

// NewRuntimeWithConfig creates a runtime with explicit lifecycle owners.
func NewRuntimeWithConfig(page *bridge.Page, observer *bridgeobserve.Collector, policy Policy, config RuntimeConfig) (*Runtime, error) {
	if page == nil {
		return nil, errors.New("actions runtime: page required")
	}
	if observer == nil {
		return nil, errors.New("actions runtime: observer required")
	}
	caller := pageCaller{page: page}
	source := &pageTargetSource{root: page, activeID: page.TargetID(), userID: config.UserID}
	formIntents, err := NewPageFormIntentRuntime(page, config.FormIntentMetrics)
	if err != nil {
		return nil, err
	}
	defaultTimeout := config.DefaultTimeout
	if defaultTimeout <= 0 {
		defaultTimeout = 15 * time.Second
	}
	var policyHook *bridge.PolicyHook
	if config.PolicyEngine != nil {
		policyHook = bridge.NewPolicyHook(config.PolicyEngine)
	}
	runtime := &Runtime{
		page: page, observer: observer, policy: policy, policyHook: policyHook, now: time.Now, defaultTimeout: defaultTimeout,
		navigator: cdpops.NewNavigator(caller), pointer: cdpops.NewPointerDispatcher(caller), userID: config.UserID,
		tabs: artemistabs.NewTabRegistry(source), downloads: config.Downloads, formIntents: formIntents,
		inputMemory:  config.InputMemory,
		imageMetrics: config.ImageMetrics,
		imageCPU:     config.ImageCPU,
	}
	if !config.DisableTabRecycle {
		recycleCtx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			runtime.tabs.RunRecycler(recycleCtx, config.TabRecycle)
		}()
		runtime.stopRecycler = func() {
			cancel()
			<-done
		}
	}
	if err := page.AddCleanup(runtime.Close); err != nil {
		if runtime.stopRecycler != nil {
			runtime.stopRecycler()
		}
		return nil, err
	}
	return runtime, nil
}

func (r *Runtime) Execute(ctx context.Context, request Request) (result Outcome) {
	return r.measureExecution(ctx, request, func(ctx context.Context) Outcome {
		return r.executeScheduled(ctx, request)
	})
}

func (r *Runtime) executeScheduled(ctx context.Context, request Request) (result Outcome) {
	if r == nil || r.page == nil || ctx == nil || request.Kind == KindDialog {
		return r.execute(ctx, request)
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = r.defaultTimeout
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lease, err := r.page.AcquireExecution(ctx)
	if err != nil {
		return failedNow(Evidence{Action: request.Kind, Effect: EffectNotAttempted}, classifyContext(ctx, FailureTarget), err.Error())
	}
	defer lease.Release()
	if err := lease.Run(r.page, func(ctx context.Context) { result = r.execute(ctx, request) }); err != nil {
		return failedNow(Evidence{Action: request.Kind, Effect: EffectNotAttempted}, classifyContext(ctx, FailureCancelled), err.Error())
	}
	return result
}

// ExecuteLeased preserves atomic session reference checks and action execution
// under the same canonical page admission; a foreign or released lease fails.
func (r *Runtime) ExecuteLeased(request Request, lease *bridge.PageLease) (result Outcome) {
	if r == nil {
		return Outcome{Failure: FailureValidation, Error: "action: initialized runtime required"}
	}
	if request.Kind == KindDialog {
		return failedNow(Evidence{Action: request.Kind, Effect: EffectNotAttempted}, FailureValidation, "dialog replies require the explicit control path")
	}
	if err := lease.Run(r.page, func(ctx context.Context) {
		result = r.measureExecution(ctx, request, func(ctx context.Context) Outcome { return r.execute(ctx, request) })
	}); err != nil {
		return failedNow(Evidence{Action: request.Kind, Effect: EffectNotAttempted}, FailureCancelled, err.Error())
	}
	return result
}

func (r *Runtime) measureExecution(ctx context.Context, request Request, execute func(context.Context) Outcome) (result Outcome) {
	if r != nil && request.Kind == KindScreenshot && r.imageMetrics != nil {
		measurementCtx := ctx
		if measurementCtx == nil {
			measurementCtx = context.Background()
		}
		var operation *bridge.ImageOperation
		measurementCtx, operation = bridge.BeginImageOperation(measurementCtx, bridge.ImageOperationCapture, bridge.ScreenshotFormat(screenshotParameters(request).Format), r.imageMetrics)
		if ctx != nil {
			ctx = measurementCtx
		}
		defer func() { operation.Finish(measurementCtx, screenshotMetricOutcome(result)) }()
	}
	return execute(ctx)
}

func (r *Runtime) execute(ctx context.Context, request Request) (result Outcome) {
	if r == nil || r.now == nil || r.page == nil || r.observer == nil {
		return Outcome{Failure: FailureValidation, Error: "action: initialized runtime required", Evidence: Evidence{Action: request.Kind, Effect: EffectNotAttempted}}
	}
	start := r.now()
	evidence := Evidence{Action: request.Kind, Ref: request.Ref, StartedAt: start, Effect: EffectNotAttempted}
	if ctx == nil {
		return failed(evidence, FailureValidation, "action: context required", start, r.now())
	}
	if err := ctx.Err(); err != nil {
		return failed(evidence, classifyContext(ctx, FailureCancelled), err.Error(), start, r.now())
	}
	identity, err := newActionID()
	if err != nil {
		return failed(evidence, FailureValidation, "action identity: "+err.Error(), start, r.now())
	}
	evidence.ActionID = identity
	evidence.ReplayAdmitted = permitsActionReplay(request.Kind)
	if request.Timeout <= 0 {
		request.Timeout = r.defaultTimeout
	}
	actionCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	if err := ValidateRequest(request); err != nil {
		return failed(evidence, FailureValidation, err.Error(), start, r.now())
	}
	if request.Kind == KindDialog {
		dialog, ok := r.page.PendingDialog()
		if !ok || request.DialogGeneration != 0 && request.DialogGeneration != dialog.Generation {
			return failed(evidence, FailureTarget, "observed pending dialog unavailable", start, r.now())
		}
		request.DialogGeneration = dialog.Generation
	}
	if r.policyHook != nil {
		if err := r.authorizeNativeAction(actionCtx, request); err != nil {
			evidence.PolicyDecision = bridge.PolicyDecisionDeny
			var policyError *bridge.PolicyError
			if errors.As(err, &policyError) && policyError.Decision == bridge.PolicyDecisionChallenge {
				evidence.PolicyDecision = bridge.PolicyDecisionChallenge
			}
			return failed(evidence, classifyContext(actionCtx, FailurePolicy), err.Error(), start, r.now())
		}
		evidence.PolicyDecision = bridge.PolicyDecisionAllow
	}
	if r.policy != nil {
		if err := r.policy(actionCtx, request); err != nil {
			return failed(evidence, classifyContext(actionCtx, FailurePolicy), err.Error(), start, r.now())
		}
	}
	if err := actionCtx.Err(); err != nil {
		return failed(evidence, classifyContext(actionCtx, FailureCancelled), err.Error(), start, r.now())
	}
	if request.Kind == KindDialog {
		evidence.BeforeHash = snapshotHash(r.observer.LastSnapshot())
		evidence.Effect = EffectPossible
		outcome := r.dialog(actionCtx, request, evidence)
		if outcome.Success {
			r.recordPageActivity(r.now())
			outcome.Evidence.Effect = EffectDispatched
			after, err := r.captureDialogEvidence(actionCtx)
			if err != nil {
				outcome.Success, outcome.Failure, outcome.Error = false, classifyContext(actionCtx, FailureEvidence), fmt.Sprintf("capture after dialog: %v; dialog was dispatched and must not be replayed", err)
			} else {
				outcome.Evidence.AfterHash = snapshotHash(after)
				if outcome.Evidence.AfterHash == "" {
					outcome.Success, outcome.Failure, outcome.Error = false, FailureEvidence, "after-dialog evidence hash unavailable; dialog must not be replayed"
				}
			}
		}
		if err := actionCtx.Err(); err != nil {
			outcome.Success, outcome.Failure, outcome.Error = false, classifyContext(actionCtx, FailureCancelled), err.Error()
		}
		outcome.Evidence.Attempts = 1
		outcome.Evidence.Duration = r.now().Sub(start)
		return outcome
	}
	attempts := 1
	if request.Idempotent && request.RetryMax > 0 {
		attempts = request.RetryMax + 1
	}
	var before bridgeobserve.Snapshot
	err = nil
	for attempt := 1; attempt <= attempts; attempt++ {
		evidence.CaptureAttempts = attempt
		before, err = r.observer.Capture(actionCtx, bridgeobserve.ModeFull, "")
		if err == nil {
			break
		}
		if attempt < attempts {
			select {
			case <-actionCtx.Done():
				return failed(evidence, classifyContext(actionCtx, FailureCancelled), actionCtx.Err().Error(), start, r.now())
			case <-time.After(time.Duration(attempt) * 25 * time.Millisecond):
			}
		}
	}
	if err != nil {
		return failed(evidence, classifyContext(actionCtx, FailureProtocol), fmt.Sprintf("capture before action: %v", err), start, r.now())
	}
	evidence.BeforeHash = snapshotHash(before)
	if evidence.BeforeHash == "" {
		return failed(evidence, FailureEvidence, "before-action evidence hash unavailable", start, r.now())
	}
	if err := actionCtx.Err(); err != nil {
		return failed(evidence, classifyContext(actionCtx, FailureCancelled), err.Error(), start, r.now())
	}
	var outcome Outcome
	if !evidence.ReplayAdmitted {
		attempts = 1
		evidence.Effect = EffectPossible
	} else {
		evidence.Effect = EffectReadOnly
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		evidence.Attempts = attempt
		outcome = r.executeOnce(actionCtx, request, evidence)
		if outcome.Success && request.Kind != KindTabOpen && request.Kind != KindTabClose && request.Kind != KindTabList && request.Kind != KindTabSwitch {
			r.recordPageActivity(r.now())
		}
		if outcome.Success || !transient(outcome.Failure) || attempt == attempts {
			break
		}
		select {
		case <-actionCtx.Done():
			return failed(evidence, classifyContext(actionCtx, outcome.Failure), actionCtx.Err().Error(), start, r.now())
		case <-time.After(time.Duration(attempt) * 25 * time.Millisecond):
		}
	}
	if !outcome.Success {
		outcome.Evidence.Duration = r.now().Sub(start)
		return outcome
	}
	if !evidence.ReplayAdmitted {
		outcome.Evidence.Effect = EffectDispatched
	}
	if !outcome.Evidence.Postcondition.Passed {
		outcome.Success, outcome.Failure, outcome.Error = false, FailurePostcondition, "action completed without a verified local postcondition; do not replay"
		outcome.Evidence.Duration = r.now().Sub(start)
		return outcome
	}
	if request.Kind == KindTabClose && r.page.State() == bridge.TargetStateClosed {
		outcome.Success, outcome.Failure, outcome.Error = false, FailureEvidence, "target closed; live after-action evidence is unavailable; close must not be replayed"
		outcome.Evidence.Duration = r.now().Sub(start)
		return outcome
	}
	after, err := r.observer.Capture(actionCtx, bridgeobserve.ModeFull, "")
	if err != nil {
		outcome.Success, outcome.Failure, outcome.Error = false, classifyContext(actionCtx, FailureEvidence), fmt.Sprintf("capture after action: %v; action already completed and must not be replayed", err)
	} else {
		outcome.Evidence.AfterHash = snapshotHash(after)
		if outcome.Evidence.AfterHash == "" {
			outcome.Success, outcome.Failure, outcome.Error = false, FailureEvidence, "after-action evidence hash unavailable; action must not be replayed"
		}
	}
	if err := actionCtx.Err(); err != nil {
		outcome.Success, outcome.Failure, outcome.Error = false, classifyContext(actionCtx, FailureCancelled), err.Error()
	}
	outcome.Evidence.Duration = r.now().Sub(start)
	return outcome
}

func (r *Runtime) recordPageActivity(at time.Time) {
	if r.page == nil || at.IsZero() {
		return
	}
	r.page.RecordActivity(at)
	if r.tabs != nil {
		r.tabs.RecordActivity(r.page.TargetID(), at)
	}
}

func (r *Runtime) executeOnce(ctx context.Context, q Request, e Evidence) Outcome {
	switch q.Kind {
	case KindNavigate:
		return r.navigate(ctx, q, e, false)
	case KindReload:
		return r.reload(ctx, q, e)
	case KindBack:
		return r.history(ctx, q, e, -1)
	case KindForward:
		return r.history(ctx, q, e, 1)
	case KindWait:
		return r.wait(ctx, q, e)
	case KindScroll:
		return r.scroll(ctx, q, e)
	case KindKey:
		return r.key(ctx, q, e)
	case KindScreenshot:
		return r.screenshot(ctx, q, e)
	case KindPDF:
		return r.pdf(ctx, q, e)
	case KindDialog:
		return r.dialog(ctx, q, e)
	case KindTabOpen:
		return r.tabOpen(ctx, q, e)
	case KindTabClose:
		targetID := q.TargetID
		if targetID == "" {
			targetID = r.page.TargetID()
		}
		if targetID == r.page.TargetID() {
			if err := r.invalidateFormIntentPage(); err != nil {
				return failedNow(e, FailureProtocol, "invalidate form intent: "+err.Error())
			}
		}
		closed, err := r.tabs.CloseTabContext(ctx, targetID)
		if err != nil {
			return failedNow(e, FailureProtocol, err.Error())
		}
		if !closed {
			return failedNow(e, FailureTarget, "tab target not found")
		}
		e.Postcondition = Postcondition{Type: "page_closed", Passed: true}
		return Outcome{Success: true, Evidence: e}
	case KindTabList:
		return r.tabList(ctx, e)
	case KindTabSwitch:
		return r.tabSwitch(ctx, q, e)
	case KindEvaluate:
		return r.evaluate(ctx, q, e)
	case KindAssert:
		return r.assert(ctx, q, e)
	case KindViewport:
		return r.viewport(ctx, q, e)
	case KindFrameEvaluate:
		return r.frameEvaluate(ctx, q, e)
	case KindFillForm:
		return r.fillFormIntent(ctx, q, e)
	}
	if q.Pointer != nil {
		return r.dispatchPointerTarget(ctx, q, e)
	}
	node, resolved := r.resolve(ctx, q.Ref, e)
	if !resolved.Success {
		return resolved
	}
	e = resolved.Evidence
	switch q.Kind {
	case KindFocus:
		return r.elementBool(ctx, node, e, "function(){this.focus();return document.activeElement===this}", nil, "focused")
	case KindClick:
		return r.click(ctx, node, q, e)
	case KindHover:
		return r.hover(ctx, node, e)
	case KindType:
		return r.typeText(ctx, node, q, e, false)
	case KindClear:
		return r.setValue(ctx, node, "", e, "cleared")
	case KindFill:
		return r.typeText(ctx, node, q, e, true)
	case KindSelect:
		return r.elementBool(ctx, node, e, "function(v){this.value=v;this.dispatchEvent(new Event('input',{bubbles:true}));this.dispatchEvent(new Event('change',{bubbles:true}));return this.value===v}", []any{q.Value}, "selected")
	case KindCheck:
		return r.setChecked(ctx, node, true, e)
	case KindUncheck:
		return r.setChecked(ctx, node, false, e)
	case KindDrag:
		return r.drag(ctx, node, q, e)
	case KindUpload:
		return r.upload(ctx, node, q, e)
	case KindDownload:
		return r.download(ctx, node, q, e)
	default:
		return failedNow(e, FailureValidation, "unsupported action kind")
	}
}

// ValidateRequest rejects malformed requests before setup or browser effects.
func ValidateRequest(q Request) error {
	if q.ForModel && q.Kind != KindScreenshot {
		return errors.New("action: forModel only applies to screenshots")
	}
	if q.Kind == KindDialog && (q.Ref != "" || q.TargetRef != "" || q.FrameID != "") {
		return errors.New("action: dialog replies use their observed renderer, not element/frame references")
	}
	valid := map[Kind]bool{KindNavigate: true, KindReload: true, KindBack: true, KindForward: true, KindWait: true, KindFocus: true, KindClick: true, KindHover: true, KindScroll: true, KindKey: true, KindType: true, KindClear: true, KindFill: true, KindFillForm: true, KindSelect: true, KindCheck: true, KindUncheck: true, KindDrag: true, KindUpload: true, KindDownload: true, KindScreenshot: true, KindPDF: true, KindDialog: true, KindTabOpen: true, KindTabClose: true, KindTabList: true, KindTabSwitch: true, KindEvaluate: true, KindAssert: true, KindViewport: true, KindFrameEvaluate: true}
	if !valid[q.Kind] {
		return fmt.Errorf("action: unsupported kind %q", q.Kind)
	}
	if (q.Kind == KindNavigate || q.Kind == KindTabOpen) && q.URL == "" {
		return errors.New("action: URL required")
	}
	if q.Pointer != nil {
		if q.Kind != KindClick && q.Kind != KindHover || q.Ref != "" || q.TargetRef != "" {
			return errors.New("action: pointer targets apply only to click or hover without references")
		}
		if math.IsNaN(q.Pointer.X) || math.IsNaN(q.Pointer.Y) || math.IsInf(q.Pointer.X, 0) || math.IsInf(q.Pointer.Y, 0) || q.Pointer.X < 0 || q.Pointer.Y < 0 {
			return errors.New("action: pointer target must be a finite viewport point")
		}
	}
	requiresRef := map[Kind]bool{KindFocus: true, KindClick: true, KindHover: true, KindType: true, KindClear: true, KindFill: true, KindSelect: true, KindCheck: true, KindUncheck: true, KindDrag: true, KindUpload: true, KindDownload: true}
	if requiresRef[q.Kind] && q.Ref == "" && q.Pointer == nil {
		return errors.New("action: stable ref required")
	}
	if (q.Kind == KindType || q.Kind == KindFill) && q.Text == "" && q.Value == "" {
		return errors.New("action: text required")
	}
	if q.Kind == KindFillForm {
		if q.FormIntent == nil {
			return errors.New("action: form intent required")
		}
		if err := q.FormIntent.Validate(); err != nil {
			return err
		}
	}
	if q.Kind == KindSelect && q.Value == "" {
		return errors.New("action: select value required")
	}
	if q.Kind == KindKey && q.Key == "" {
		return errors.New("action: key required")
	}
	if (q.Kind == KindEvaluate || q.Kind == KindAssert) && q.Expression == "" {
		return errors.New("action: expression required")
	}
	if q.Kind == KindViewport && (q.Width <= 0 || q.Height <= 0) {
		return errors.New("action: positive viewport width and height required")
	}
	if q.Kind == KindFrameEvaluate && (q.FrameID == "" || q.Expression == "") {
		return errors.New("action: frameId and expression required")
	}
	if q.Kind == KindTabSwitch && q.TargetID == "" {
		return errors.New("action: tab switch targetId required")
	}
	if q.Kind == KindUpload && len(q.Files) == 0 {
		return errors.New("action: upload files required")
	}
	if q.Kind == KindDownload && q.DownloadDir != "" {
		return errors.New("action: downloadDir is forbidden; downloads use the session-owned directory")
	}
	if q.Kind == KindScreenshot {
		if q.Format != "" && q.Format != "png" && q.Format != "jpeg" {
			return errors.New("action: screenshot format must be png or jpeg")
		}
		if q.Quality < 0 || q.Quality > 100 || q.Quality > 0 && q.Format == "png" {
			return errors.New("action: JPEG quality must be 0..100 and cannot apply to PNG")
		}
		if q.ForModel && q.Format == "png" {
			return errors.New("action: model screenshots require JPEG")
		}
		if q.Quality > 0 && q.Format == "" && !q.ForModel {
			return errors.New("action: JPEG quality requires an explicit JPEG format")
		}
	}
	if q.Kind == KindDrag && q.TargetRef == "" {
		return errors.New("action: drag target ref required")
	}
	if q.RetryMax < 0 || q.RetryMax > 3 {
		return errors.New("action: retryMax must be 0..3")
	}
	if q.RetryMax > 0 && !q.Idempotent {
		return errors.New("action: retry requires idempotent=true")
	}
	return nil
}

func (r *Runtime) captureDialogEvidence(ctx context.Context) (bridgeobserve.Snapshot, error) {
	lease, err := r.page.AcquireExecution(ctx)
	if err != nil {
		return bridgeobserve.Snapshot{}, err
	}
	defer lease.Release()
	return r.observer.Capture(lease.Context(), bridgeobserve.ModeEvidence, "")
}

func (r *Runtime) resolve(ctx context.Context, ref string, e Evidence) (bridgeobserve.Node, Outcome) {
	return r.resolvePointerTarget(ctx, ref, e, false)
}

func (r *Runtime) resolvePointerTarget(ctx context.Context, ref string, e Evidence, dropTarget bool) (bridgeobserve.Node, Outcome) {
	resolution, err := r.observer.Resolve(ctx, ref)
	if err != nil {
		return bridgeobserve.Node{}, failedNow(e, classifyContext(ctx, FailureProtocol), err.Error())
	}
	if resolution.Status != bridgeobserve.ResolutionExact && resolution.Status != bridgeobserve.ResolutionReResolved {
		return bridgeobserve.Node{}, failedNow(e, FailureTarget, "target "+string(resolution.Status)+": "+resolution.Reason)
	}
	n := *resolution.Node
	state := bridgeobserve.ClassifyActionability(&n)
	if state == bridgeobserve.ActionabilityHitUnknown || dropTarget && state == bridgeobserve.ActionabilityNonInteractive {
		if _, _, err := r.elementPoint(ctx, n); err != nil {
			return bridgeobserve.Node{}, failedNow(e, FailureActionability, err.Error())
		}
		if dropTarget && state == bridgeobserve.ActionabilityNonInteractive {
			e.BackendDOMID, e.FrameID = n.BackendNodeID, n.FrameID
			return n, Outcome{Success: true, Evidence: e}
		}
		resolution, err = r.observer.Resolve(ctx, ref)
		if err != nil || resolution.Node == nil {
			return bridgeobserve.Node{}, failedNow(e, FailureActionability, "target unavailable after scrolling")
		}
		n = *resolution.Node
	}
	e.BackendDOMID = n.BackendNodeID
	e.FrameID = n.FrameID
	if state := bridgeobserve.ClassifyActionability(&n); state != bridgeobserve.ActionabilityReady || !n.Interactable {
		return bridgeobserve.Node{}, failedNow(e, FailureActionability, "target is "+string(state))
	}
	return n, Outcome{Success: true, Evidence: e}
}

func (r *Runtime) navigate(ctx context.Context, q Request, e Evidence, _ bool) Outcome {
	result := r.navigator.Navigate(ctx, cdpops.NavigationRequest{URL: q.URL, WaitUntil: cdpops.WaitLoad, Timeout: q.Timeout})
	if !result.Success {
		return failedNow(e, classifyContext(ctx, FailureProtocol), result.Error)
	}
	if err := r.invalidateFormIntentPage(); err != nil {
		return failedNow(e, FailureProtocol, "invalidate form intent: "+err.Error())
	}
	var value string
	if err := r.evalInto(ctx, "location.href", &value); err != nil {
		return failedNow(e, FailurePostcondition, err.Error())
	}
	passed := sameNavigationURL(q.URL, value)
	e.Postcondition = Postcondition{Type: "url", Expected: q.URL, Actual: value, Passed: passed}
	if !passed {
		return failedNow(e, FailurePostcondition, "navigation URL postcondition failed")
	}
	return Outcome{Success: true, Value: value, Evidence: e}
}
func (r *Runtime) history(ctx context.Context, q Request, e Evidence, delta int) Outcome {
	var result cdpops.NavigationResult
	var policyErr error
	authorize := func(ctx context.Context, target string) error {
		policyErr = r.authorizeHistoryTarget(ctx, q.Kind, target)
		return policyErr
	}
	if delta < 0 {
		result = r.navigator.GoBack(ctx, authorize)
	} else {
		result = r.navigator.GoForward(ctx, authorize)
	}
	if policyErr != nil {
		e.Effect, e.Attempts, e.PolicyDecision = EffectNotAttempted, 0, bridge.PolicyDecisionDeny
		var decision *bridge.PolicyError
		if errors.As(policyErr, &decision) {
			e.PolicyDecision = decision.Decision
		}
		return failedNow(e, classifyContext(ctx, FailurePolicy), policyErr.Error())
	}
	if !result.Success {
		return failedNow(e, classifyContext(ctx, FailureProtocol), result.Error)
	}
	if err := r.invalidateFormIntentPage(); err != nil {
		return failedNow(e, FailureProtocol, "invalidate form intent: "+err.Error())
	}
	e.Postcondition = Postcondition{Type: "history_ready", Passed: true}
	return Outcome{Success: true, Evidence: e}
}

func (r *Runtime) reload(ctx context.Context, q Request, e Evidence) Outcome {
	result := r.navigator.Reload(ctx)
	if !result.Success {
		return failedNow(e, classifyContext(ctx, FailureProtocol), result.Error)
	}
	if err := r.invalidateFormIntentPage(); err != nil {
		return failedNow(e, FailureProtocol, "invalidate form intent: "+err.Error())
	}
	e.Postcondition = Postcondition{Type: "reload_ready", Passed: true}
	return Outcome{Success: true, Evidence: e}
}
func (r *Runtime) wait(ctx context.Context, q Request, e Evidence) Outcome {
	if err := r.navigator.WaitForLoad(ctx, cdpops.WaitLoad, q.Timeout); err != nil {
		return failedNow(e, classifyContext(ctx, FailureTimeout), err.Error())
	}
	e.Postcondition = Postcondition{Type: "document_ready", Actual: "complete", Passed: true}
	return Outcome{Success: true, Evidence: e}
}
func (r *Runtime) click(ctx context.Context, n bridgeobserve.Node, q Request, e Evidence) Outcome {
	button := cdpops.MouseButton(q.Button)
	if button == "" {
		button = cdpops.MouseButtonLeft
	}
	if !cdpops.IsValidMouseButton(button) {
		return failedNow(e, FailureValidation, fmt.Sprintf("click: invalid button %q", q.Button))
	}
	if err := q.Human.validate(); err != nil {
		return failedNow(e, FailureValidation, err.Error())
	}
	x, y, err := r.elementPoint(ctx, n)
	if err != nil {
		return failedNow(e, FailureActionability, err.Error())
	}
	if q.Human != nil {
		if err := r.humanClick(ctx, x, y, button, q.Human); err != nil {
			return failedNow(e, classifyContext(ctx, FailureProtocol), err.Error())
		}
		e.Postcondition = Postcondition{Type: "input_dispatch", Actual: "mouseReleased", Passed: true}
		return Outcome{Success: true, Evidence: e}
	}
	target := input.MousePoint{X: x, Y: y}
	if err := r.movePointer(ctx, target); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	// Human clicks hold the button briefly — an instant press/release pair
	// within the same millisecond is a synthetic signature.
	if err := r.pressAndRelease(ctx, target, target, button, jitterDuration(45*time.Millisecond, 110*time.Millisecond)); err != nil {
		return failedNow(e, classifyContext(ctx, FailureProtocol), err.Error())
	}
	e.Postcondition = Postcondition{Type: "input_dispatch", Actual: "mouseReleased", Passed: true}
	return Outcome{Success: true, Evidence: e}
}

// dispatchPointerTarget dispatches a click or hover at an explicit viewport point. The
// caller has bound the point to current pixels; there is no element
// postcondition, so success only proves the input was dispatched.
func (r *Runtime) dispatchPointerTarget(ctx context.Context, q Request, e Evidence) Outcome {
	target := input.MousePoint{X: q.Pointer.X, Y: q.Pointer.Y}
	if q.Kind == KindHover {
		if err := r.movePointer(ctx, target); err != nil {
			return failedNow(e, classifyContext(ctx, FailureProtocol), err.Error())
		}
		e.Postcondition = Postcondition{Type: "input_dispatch", Actual: "mouseMoved", Passed: true}
		return Outcome{Success: true, Evidence: e}
	}
	button := cdpops.MouseButton(q.Button)
	if button == "" {
		button = cdpops.MouseButtonLeft
	}
	if !cdpops.IsValidMouseButton(button) {
		return failedNow(e, FailureValidation, fmt.Sprintf("click: invalid button %q", q.Button))
	}
	if err := q.Human.validate(); err != nil {
		return failedNow(e, FailureValidation, err.Error())
	}
	if q.Human != nil {
		if err := r.humanClick(ctx, target.X, target.Y, button, q.Human); err != nil {
			return failedNow(e, classifyContext(ctx, FailureProtocol), err.Error())
		}
	} else {
		if err := r.movePointer(ctx, target); err != nil {
			return failedNow(e, classifyContext(ctx, FailureProtocol), err.Error())
		}
		if err := r.pressAndRelease(ctx, target, target, button, jitterDuration(45*time.Millisecond, 110*time.Millisecond)); err != nil {
			return failedNow(e, classifyContext(ctx, FailureProtocol), err.Error())
		}
	}
	e.Postcondition = Postcondition{Type: "input_dispatch", Actual: "mouseReleased", Passed: true}
	return Outcome{Success: true, Evidence: e}
}

func (r *Runtime) hover(ctx context.Context, n bridgeobserve.Node, e Evidence) Outcome {
	x, y, pointErr := r.elementPoint(ctx, n)
	if pointErr != nil {
		return failedNow(e, FailureActionability, pointErr.Error())
	}
	if err := r.pointer.MouseMoveContext(ctx, x, y); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	return r.elementBool(ctx, n, e, "function(){return this.matches(':hover')}", nil, "hovered")
}
func (r *Runtime) scroll(ctx context.Context, q Request, e Evidence) Outcome {
	var before scrollPosition
	if err := r.evalInto(ctx, "({x:scrollX,y:scrollY})", &before); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	if err := q.Human.validate(); err != nil {
		return failedNow(e, FailureValidation, err.Error())
	}
	if q.Human == nil {
		if err := r.pointer.Wheel(ctx, q.X, q.Y, q.DeltaX, q.DeltaY); err != nil {
			return failedNow(e, FailureProtocol, err.Error())
		}
	} else {
		steps, interval := humanScrollDeltas(q.DeltaX, q.DeltaY, q.Human)
		for i, step := range steps {
			if i > 0 {
				if err := sleepContext(ctx, interval); err != nil {
					return failedNow(e, classifyContext(ctx, FailureTimeout), err.Error())
				}
			}
			if err := r.pointer.Wheel(ctx, q.X, q.Y, step.X, step.Y); err != nil {
				return failedNow(e, FailureProtocol, err.Error())
			}
		}
	}
	var after scrollPosition
	passed := q.DeltaX == 0 && q.DeltaY == 0
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	expired := false
	for !passed && !expired {
		if err := r.evalInto(ctx, "({x:scrollX,y:scrollY})", &after); err != nil {
			return failedNow(e, FailurePostcondition, err.Error())
		}
		passed = before != after
		if passed {
			break
		}
		select {
		case <-ctx.Done():
			return failedNow(e, classifyContext(ctx, FailureTimeout), ctx.Err().Error())
		case <-deadline.C:
			expired = true
		case <-ticker.C:
		}
	}
	e.Postcondition = Postcondition{Type: "scroll_offset", Expected: "changed", Actual: after, Passed: passed}
	if !passed {
		return failedNow(e, FailurePostcondition, "scroll offset did not change")
	}
	return Outcome{Success: true, Value: after, Evidence: e}
}
func (r *Runtime) key(ctx context.Context, q Request, e Evidence) Outcome {
	for _, kind := range []string{"keyDown", "keyUp"} {
		params := dispatchKeyEventParams{Type: kind, Key: q.Key, Text: q.Text}
		if err := r.page.Call(ctx, "Input.dispatchKeyEvent", params, &emptyResult{}); err != nil {
			return failedNow(e, FailureProtocol, err.Error())
		}
	}
	e.Postcondition = Postcondition{Type: "input_dispatch", Actual: "keyUp", Passed: true}
	return Outcome{Success: true, Evidence: e}
}
func (r *Runtime) typeText(ctx context.Context, n bridgeobserve.Node, q Request, e Evidence, clear bool) Outcome {
	text := q.Text
	if text == "" {
		text = q.Value
	}
	expected := text
	if clear {
		if out := r.setValue(ctx, n, "", e, "cleared"); !out.Success {
			return out
		}
	} else {
		var current string
		if err := r.callElementInto(ctx, n, "function(){return this.value}", nil, &current); err != nil {
			return failedNow(e, FailureProtocol, err.Error())
		}
		expected = current + text
	}
	if err := q.Human.validate(); err != nil {
		return failedNow(e, FailureValidation, err.Error())
	}
	if out := r.elementBool(ctx, n, e, "function(){this.focus();return document.activeElement===this}", nil, "focused"); !out.Success {
		return out
	}
	recipient := func(ctx context.Context) error {
		var focused bool
		if err := r.callElementInto(ctx, n, "function(){return this.getRootNode().activeElement===this}", nil, &focused); err != nil {
			return err
		}
		if !focused {
			return ErrInputRecipientChanged
		}
		return nil
	}
	if err := r.typeTextHuman(ctx, text, q.Human, recipient); err != nil {
		if errors.Is(err, ErrInputRecipientChanged) {
			return failedNow(e, FailurePostcondition, err.Error())
		}
		return failedNow(e, classifyContext(ctx, FailureProtocol), err.Error())
	}
	return r.verifyValue(ctx, n, expected, e, "typed")
}

// typeTextHuman types one rune at a time through real key events
// (rawKeyDown → char → keyUp) with bounded inter-key jitter. Instant
// Input.insertText produces no keydown/keyup events at all — behavioral
// detectors flag that as synthetic input immediately.
// ErrInputRecipientChanged stops typing when focus moved away from the
// requested element, so keystrokes never reach another recipient.
var ErrInputRecipientChanged = errors.New("type: focus left the requested element")

func (r *Runtime) typeTextHuman(ctx context.Context, text string, profile *HumanProfile, recipient func(context.Context) error) error {
	if profile != nil {
		keys, delays := humanKeys(text, profile, profile.random())
		for i, key := range keys {
			if err := recipient(ctx); err != nil {
				return err
			}
			if err := r.dispatchKey(ctx, key, recipient); err != nil {
				return err
			}
			if err := sleepContext(ctx, delays[i]); err != nil {
				return err
			}
		}
		return nil
	}
	for _, ch := range text {
		if err := recipient(ctx); err != nil {
			return err
		}
		if err := r.dispatchKey(ctx, ch, recipient); err != nil {
			return err
		}
		if err := sleepContext(ctx, jitterDuration(8*time.Millisecond, 35*time.Millisecond)); err != nil {
			return err
		}
	}
	return nil
}

// jitterDuration returns a random duration in [min,max) — human input cadence
// is deliberately irregular, fixed intervals are a synthetic signature.
func jitterDuration(min, max time.Duration) time.Duration {
	span := int64(max - min)
	var jitter int64
	if span > 0 {
		var b [8]byte
		if _, err := rand.Read(b[:]); err == nil {
			jitter = int64(binary.LittleEndian.Uint64(b[:]) % uint64(span))
		}
	}
	return min + time.Duration(jitter)
}

func (r *Runtime) setValue(ctx context.Context, n bridgeobserve.Node, value string, e Evidence, post string) Outcome {
	return r.elementBool(ctx, n, e, "function(v){this.focus();this.value=v;this.dispatchEvent(new Event('input',{bubbles:true}));this.dispatchEvent(new Event('change',{bubbles:true}));return this.value===v}", []any{value}, post)
}
func (r *Runtime) verifyValue(ctx context.Context, n bridgeobserve.Node, want string, e Evidence, post string) Outcome {
	var value string
	if err := r.callElementInto(ctx, n, "function(){return this.value}", nil, &value); err != nil {
		return failedNow(e, FailurePostcondition, err.Error())
	}
	passed := value == want
	e.Postcondition = Postcondition{Type: post, Expected: want, Actual: value, Passed: passed}
	if !passed {
		return failedNow(e, FailurePostcondition, "value postcondition failed")
	}
	return Outcome{Success: true, Value: value, Evidence: e}
}
func (r *Runtime) setChecked(ctx context.Context, n bridgeobserve.Node, want bool, e Evidence) Outcome {
	return r.elementBool(ctx, n, e, "function(v){if(this.checked!==v){this.click()}return this.checked===v}", []any{want}, "checked")
}
func (r *Runtime) drag(ctx context.Context, n bridgeobserve.Node, q Request, e Evidence) Outcome {
	target, res := r.resolvePointerTarget(ctx, q.TargetRef, e, true)
	if !res.Success {
		return res
	}
	sx, sy, err := r.elementPoint(ctx, n)
	if err != nil {
		return failedNow(e, FailureActionability, err.Error())
	}
	tx, ty, err := r.elementPoint(ctx, target)
	if err != nil {
		return failedNow(e, FailureActionability, err.Error())
	}
	// Human drags trace a path, not a teleport — ease through a few
	// intermediate points between press and release.
	steps := 6
	move := func(x, y float64, held bool) error {
		params := dispatchMouseEventParams{Type: "mouseMoved", X: x, Y: y}
		if held {
			params.Button = "left"
			params.Buttons = 1
		}
		if err := r.page.Call(ctx, "Input.dispatchMouseEvent", params, &emptyResult{}); err != nil {
			return err
		}
		return sleepContext(ctx, jitterDuration(6*time.Millisecond, 22*time.Millisecond))
	}
	if err := move(sx, sy, false); err != nil {
		return failedNow(e, classifyContext(ctx, FailureTimeout), err.Error())
	}
	if err := r.page.Call(ctx, "Input.dispatchMouseEvent", dispatchMouseEventParams{Type: "mousePressed", X: sx, Y: sy, Button: "left", Buttons: 1}, &emptyResult{}); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	for i := 1; i <= steps; i++ {
		// ease-out curve: fast start, settles into the target like a hand.
		f := 1 - math.Pow(1-float64(i)/float64(steps), 2)
		if err := move(sx+(tx-sx)*f, sy+(ty-sy)*f, true); err != nil {
			return failedNow(e, classifyContext(ctx, FailureTimeout), err.Error())
		}
	}
	if err := r.page.Call(ctx, "Input.dispatchMouseEvent", dispatchMouseEventParams{Type: "mouseReleased", X: tx, Y: ty, Button: "left", Buttons: 1}, &emptyResult{}); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	e.Postcondition = Postcondition{Type: "drag_dispatch", Actual: q.TargetRef, Passed: true}
	return Outcome{Success: true, Evidence: e}
}
func (r *Runtime) upload(ctx context.Context, n bridgeobserve.Node, q Request, e Evidence) Outcome {
	for _, path := range q.Files {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return failedNow(e, FailureValidation, "upload file unavailable: "+path)
		}
	}
	params := setFileInputFilesParams{Files: q.Files, BackendNodeID: n.BackendNodeID}
	if err := r.page.Call(ctx, "DOM.setFileInputFiles", params, &emptyResult{}); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	var names []string
	if err := r.callElementInto(ctx, n, "function(){return Array.from(this.files).map(f=>f.name)}", nil, &names); err != nil {
		return failedNow(e, FailurePostcondition, err.Error())
	}
	passed := len(names) == len(q.Files)
	e.Postcondition = Postcondition{Type: "uploaded_files", Expected: len(q.Files), Actual: len(names), Passed: passed}
	if !passed {
		return failedNow(e, FailurePostcondition, "upload file count mismatch")
	}
	return Outcome{Success: true, Value: names, Evidence: e}
}
func (r *Runtime) download(ctx context.Context, n bridgeobserve.Node, q Request, e Evidence) (outcome Outcome) {
	manager, err := r.downloadManager()
	if err != nil {
		return failedNow(e, FailureValidation, err.Error())
	}
	stage, err := manager.NewBrowserStage()
	if err != nil {
		return failedNow(e, FailureValidation, err.Error())
	}
	defer func() { outcome = withCleanupFailure(outcome, "download stage cleanup", stage.Close()) }()
	release, err := r.page.AcquireDownloadLease(ctx)
	if err != nil {
		return failedNow(e, classifyContext(ctx, FailureTimeout), err.Error())
	}
	defer release()
	subscription, err := r.page.SubscribeBrowserEvents(64)
	if err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	defer subscription.Close()
	params := setDownloadBehaviorParams{Behavior: "allow", BrowserContextID: r.page.BrowserContextID(), DownloadPath: stage.Directory(), EventsEnabled: true}
	if err := r.page.CallBrowser(ctx, "Browser.setDownloadBehavior", params, &emptyResult{}); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	defer func() { outcome = withCleanupFailure(outcome, "download deny reset", r.disableDownloads()) }()
	if out := r.click(ctx, n, q, e); !out.Success {
		return out
	}
	var guid, filename string
	for {
		select {
		case <-ctx.Done():
			failed := failedNow(e, classifyContext(ctx, FailureTimeout), "download did not complete: "+ctx.Err().Error())
			return withCleanupFailure(failed, "download cancel", r.cancelDownload(guid))
		case event, ok := <-subscription.Events:
			if !ok {
				return failedNow(e, FailureProtocol, "download event stream closed")
			}
			switch event.Method {
			case "Browser.downloadWillBegin":
				var started downloadWillBeginEvent
				if guid != "" || json.Unmarshal(event.Params, &started) != nil || started.GUID == "" {
					continue
				}
				owned, err := r.page.OwnsFrame(ctx, started.FrameID)
				if err != nil {
					return failedNow(e, FailureProtocol, err.Error())
				}
				if owned {
					guid, filename = started.GUID, started.SuggestedFilename
				}
			case "Browser.downloadProgress":
				var progress downloadProgressEvent
				if guid == "" || json.Unmarshal(event.Params, &progress) != nil || progress.GUID != guid {
					// Progress before this page's accepted begin event, or for a
					// foreign GUID, never advances this download.
					continue
				}
				if err := manager.ValidatePending(int64(progress.ReceivedBytes)); err != nil {
					return withCleanupFailure(failedNow(e, FailurePolicy, err.Error()), "download cancel", r.cancelDownload(guid))
				}
				if progress.State == "canceled" {
					return failedNow(e, FailureProtocol, "download canceled by browser")
				}
				if progress.State != "completed" {
					continue
				}
				size, err := stage.StagedSize(filename)
				if err != nil {
					return failedNow(e, FailureProtocol, err.Error())
				}
				if size == 0 {
					// An empty file is never adopted: no artifact, failed outcome.
					e.Postcondition = Postcondition{Type: "download_complete", Expected: ">0 bytes", Actual: int64(0), Passed: false}
					return failedNow(e, FailurePostcondition, "download completed with 0 bytes")
				}
				download, err := stage.Adopt(filename, "")
				if err != nil {
					return failedNow(e, FailurePolicy, err.Error())
				}
				e.Postcondition = Postcondition{Type: "download_complete", Expected: ">0 bytes", Actual: download.Size, Passed: download.Size > 0}
				return Outcome{Success: true, Download: &Download{Path: download.Path, Filename: download.Filename, MIME: download.MIME, Size: download.Size, SHA256: download.SHA256}, Evidence: e}
			}
		case err, ok := <-subscription.Errors:
			if !ok || err == nil {
				return failedNow(e, FailureProtocol, "download event stream failed")
			}
			return failedNow(e, FailureProtocol, err.Error())
		}
	}
}

func (r *Runtime) downloadManager() (*artemisdownload.DownloadManager, error) {
	r.downloadOnce.Do(func() {
		if r.downloads != nil {
			return
		}
		policy, err := r.page.NetworkPolicy()
		if err != nil {
			r.downloadErr = err
			return
		}
		sessionID := r.page.BrowserContextID()
		if sessionID == "" {
			sessionID = "default-" + r.page.TargetID()
		}
		r.downloads, r.downloadErr = artemisdownload.NewDownloadManager(artemisdownload.DownloadConfig{SessionID: sessionID, Policy: policy})
	})
	return r.downloads, r.downloadErr
}

func (r *Runtime) cancelDownload(guid string) error {
	if guid == "" {
		return nil
	}
	params := cancelDownloadParams{GUID: guid, BrowserContextID: r.page.BrowserContextID()}
	cancelCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return r.page.CallBrowser(cancelCtx, "Browser.cancelDownload", params, &emptyResult{})
}

func (r *Runtime) disableDownloads() error {
	params := setDownloadBehaviorParams{Behavior: "deny", BrowserContextID: r.page.BrowserContextID(), EventsEnabled: false}
	disableCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return r.page.CallBrowser(disableCtx, "Browser.setDownloadBehavior", params, &emptyResult{})
}

// withCleanupFailure keeps the primary outcome cause and appends a failed
// cleanup effect; an unconfirmed cleanup can never report success.
func withCleanupFailure(outcome Outcome, label string, err error) Outcome {
	if err == nil {
		return outcome
	}
	outcome.Success = false
	if outcome.Failure == "" {
		outcome.Failure = FailureProtocol
	}
	if outcome.Error == "" {
		outcome.Error = fmt.Sprintf("%s: %v", label, err)
	} else {
		outcome.Error = fmt.Sprintf("%s; %s: %v", outcome.Error, label, err)
	}
	return outcome
}

func (r *Runtime) screenshot(ctx context.Context, q Request, e Evidence) Outcome {
	params := screenshotParameters(q)
	if q.Ref != "" {
		clip, resolved := r.elementScreenshotClip(ctx, q.Ref, e)
		if !resolved.Success {
			return resolved
		}
		params.Clip, e = clip, resolved.Evidence
	}
	var result dataResult
	bridge.ImageOperationFromContext(ctx).CaptureAttempt()
	if err := r.page.Call(ctx, "Page.captureScreenshot", params, &result); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	if len(result.Data) == 0 || len(result.Data) > bridge.JPEGEncodedLimit {
		return failedNow(e, FailurePostcondition, "screenshot exceeds encoded image budget")
	}
	return r.screenshotInputOutcome(ctx, q, params.Format, result.Data, e)
}
func (r *Runtime) pdf(ctx context.Context, _ Request, e Evidence) Outcome {
	var result dataResult
	params := printToPDFParams{PrintBackground: true, TransferMode: "ReturnAsBase64"}
	if err := r.page.Call(ctx, "Page.printToPDF", params, &result); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	raw, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil || !strings.HasPrefix(string(raw), "%PDF-") {
		return failedNow(e, FailurePostcondition, "invalid PDF bytes")
	}
	e.Postcondition = Postcondition{Type: "pdf_signature", Actual: "%PDF-", Passed: true}
	return Outcome{Success: true, Bytes: raw, MIME: "application/pdf", Evidence: e}
}
func (r *Runtime) dialog(ctx context.Context, q Request, e Evidence) Outcome {
	if err := r.page.HandlePendingDialog(ctx, q.DialogGeneration, q.Accept, q.PromptText); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	e.Postcondition = Postcondition{Type: "dialog_handled", Expected: q.Accept, Actual: q.Accept, Passed: true}
	return Outcome{Success: true, Evidence: e}
}
func (r *Runtime) tabOpen(ctx context.Context, q Request, e Evidence) Outcome {
	tab, err := r.tabs.CreateTabContext(ctx, r.userID, q.URL)
	if err != nil {
		outcome := failedNow(e, FailureProtocol, err.Error())
		var createErr *artemistabs.TargetCreateError
		if errors.As(err, &createErr) {
			outcome.TargetID = createErr.TargetID
			outcome.OutcomeUnknown = createErr.OutcomeUnknown
		}
		return outcome
	}
	e.Postcondition = Postcondition{Type: "tab_attached", Actual: tab.ID, Passed: tab.ID != ""}
	return Outcome{Success: true, TargetID: tab.ID, Evidence: e}
}
func (r *Runtime) tabList(ctx context.Context, e Evidence) Outcome {
	tabs, err := r.tabs.ListTabsContext(ctx, r.userID)
	if err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	ids := make([]string, len(tabs))
	for i, tab := range tabs {
		ids[i] = tab.ID
	}
	e.Postcondition = Postcondition{Type: "tab_list", Actual: len(ids), Passed: true}
	return Outcome{Success: true, Value: ids, Evidence: e}
}
func (r *Runtime) tabSwitch(ctx context.Context, q Request, e Evidence) Outcome {
	if err := r.tabs.ActivateTabContext(ctx, q.TargetID); err != nil {
		if errors.Is(err, artemistabs.ErrTargetNotFound) {
			return failedNow(e, FailureTarget, err.Error())
		}
		return failedNow(e, FailureProtocol, err.Error())
	}
	e.Postcondition = Postcondition{Type: "tab_activated", Actual: q.TargetID, Passed: true}
	return Outcome{Success: true, TargetID: q.TargetID, Evidence: e}
}
func (r *Runtime) evaluate(ctx context.Context, q Request, e Evidence) Outcome {
	value, err := r.eval(ctx, q.Expression)
	if err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	e.Postcondition = Postcondition{Type: "evaluation_completed", Passed: true}
	return Outcome{Success: true, Value: value, Evidence: e}
}
func (r *Runtime) assert(ctx context.Context, q Request, e Evidence) Outcome {
	var value bool
	if err := r.evalInto(ctx, q.Expression, &value); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	e.Postcondition = Postcondition{Type: "assertion", Expected: true, Actual: value, Passed: value}
	if !value {
		return failedNow(e, FailurePostcondition, "assertion returned false")
	}
	return Outcome{Success: true, Value: value, Evidence: e}
}

func (r *Runtime) viewport(ctx context.Context, q Request, e Evidence) Outcome {
	params := setDeviceMetricsOverrideParams{Width: q.Width, Height: q.Height, DeviceScaleFactor: 1, Mobile: false}
	if err := r.page.Call(ctx, "Emulation.setDeviceMetricsOverride", params, &emptyResult{}); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	var dimensions viewportDimensions
	if err := r.evalInto(ctx, "({width:innerWidth,height:innerHeight})", &dimensions); err != nil {
		return failedNow(e, FailurePostcondition, err.Error())
	}
	passed := dimensions.Width == q.Width && dimensions.Height == q.Height
	e.Postcondition = Postcondition{Type: "viewport", Expected: viewportDimensions{Width: q.Width, Height: q.Height}, Actual: dimensions, Passed: passed}
	if !passed {
		return failedNow(e, FailurePostcondition, "viewport postcondition failed")
	}
	return Outcome{Success: true, Value: dimensions, Evidence: e}
}

func (r *Runtime) frameEvaluate(ctx context.Context, q Request, e Evidence) Outcome {
	var world createIsolatedWorldResult
	worldParams := createIsolatedWorldParams{FrameID: q.FrameID, WorldName: "artemis-action", GrantUniversalAccess: false}
	if err := r.page.CallFrame(ctx, q.FrameID, "Page.createIsolatedWorld", worldParams, &world); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	if world.ExecutionContextID == 0 {
		return failedNow(e, FailureProtocol, "isolated world returned no execution context")
	}
	var result runtimeCallResult
	evaluateParams := runtimeEvaluateParams{Expression: q.Expression, ContextID: world.ExecutionContextID, ReturnByValue: true, AwaitPromise: true}
	if err := r.page.CallFrame(ctx, q.FrameID, "Runtime.evaluate", evaluateParams, &result); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	value, err := result.value()
	if err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	e.Postcondition = Postcondition{Type: "frame_evaluation", Actual: q.FrameID, Passed: true}
	return Outcome{Success: true, Value: value, Evidence: e}
}

func (r *Runtime) elementBool(ctx context.Context, n bridgeobserve.Node, e Evidence, function string, args []any, post string) Outcome {
	var value bool
	if err := r.callElementInto(ctx, n, function, args, &value); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	e.Postcondition = Postcondition{Type: post, Expected: true, Actual: value, Passed: value}
	if !value {
		return failedNow(e, FailurePostcondition, post+" postcondition failed")
	}
	return Outcome{Success: true, Value: value, Evidence: e}
}
func (r *Runtime) callElementInto(ctx context.Context, n bridgeobserve.Node, function string, args []any, destination any) error {
	result, err := r.callElementRemote(ctx, n, function, args)
	if err != nil {
		return err
	}
	return result.valueInto(destination)
}

func (r *Runtime) callElementRemote(ctx context.Context, n bridgeobserve.Node, function string, args []any) (runtimeCallResult, error) {
	var resolved resolveNodeResult
	if err := r.page.CallFrame(ctx, n.FrameID, "DOM.resolveNode", resolveNodeParams{BackendNodeID: n.BackendNodeID}, &resolved); err != nil {
		return runtimeCallResult{}, err
	}
	if resolved.Object.ObjectID == "" {
		return runtimeCallResult{}, errors.New("resolve element: empty object ID")
	}
	arguments, err := encodeCallArguments(args)
	if err != nil {
		return runtimeCallResult{}, err
	}
	params := callFunctionOnParams{ObjectID: resolved.Object.ObjectID, FunctionDeclaration: function, Arguments: arguments, ReturnByValue: true, AwaitPromise: true}
	var result runtimeCallResult
	if err := r.page.CallFrame(ctx, n.FrameID, "Runtime.callFunctionOn", params, &result); err != nil {
		return runtimeCallResult{}, err
	}
	return result, nil
}

func (r *Runtime) elementPoint(ctx context.Context, n bridgeobserve.Node) (float64, float64, error) {
	if err := r.page.CallFrame(ctx, n.FrameID, "DOM.scrollIntoViewIfNeeded", resolveNodeParams{BackendNodeID: n.BackendNodeID}, &emptyResult{}); err != nil {
		return 0, 0, err
	}
	return r.observer.PointerPoint(ctx, n)
}

func (r *Runtime) eval(ctx context.Context, expression string) (any, error) {
	result, err := r.evalRemote(ctx, expression)
	if err != nil {
		return nil, err
	}
	return result.value()
}

func (r *Runtime) evalInto(ctx context.Context, expression string, destination any) error {
	result, err := r.evalRemote(ctx, expression)
	if err != nil {
		return err
	}
	return result.valueInto(destination)
}

func (r *Runtime) evalRemote(ctx context.Context, expression string) (runtimeCallResult, error) {
	params := runtimeEvaluateParams{Expression: expression, ReturnByValue: true, AwaitPromise: true}
	var result runtimeCallResult
	if err := r.page.Call(ctx, "Runtime.evaluate", params, &result); err != nil {
		return runtimeCallResult{}, err
	}
	return result, nil
}
func snapshotHash(snapshot bridgeobserve.Snapshot) string {
	record, err := bridgeobserve.NewTraceRecord(snapshot)
	if err != nil {
		return ""
	}
	return record.SnapshotHash
}
func failed(e Evidence, class FailureClass, message string, start, end time.Time) Outcome {
	e.Duration = end.Sub(start)
	return Outcome{Failure: class, Error: message, Evidence: e}
}
func failedNow(e Evidence, class FailureClass, message string) Outcome {
	return Outcome{Failure: class, Error: message, Evidence: e}
}
func classifyContext(ctx context.Context, fallback FailureClass) FailureClass {
	if errors.Is(ctx.Err(), context.Canceled) {
		return FailureCancelled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return FailureTimeout
	}
	return fallback
}
func transient(class FailureClass) bool { return class == FailureTimeout || class == FailureProtocol }
