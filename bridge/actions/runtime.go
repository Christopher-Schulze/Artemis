package actions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
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
)

type Postcondition struct {
	Type     string `json:"type"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
	Passed   bool   `json:"passed"`
}
type Evidence struct {
	Action        Kind          `json:"action"`
	Ref           string        `json:"ref,omitempty"`
	BackendDOMID  int64         `json:"backendDomId,omitempty"`
	FrameID       string        `json:"frameId,omitempty"`
	BeforeHash    string        `json:"beforeHash,omitempty"`
	AfterHash     string        `json:"afterHash,omitempty"`
	StartedAt     time.Time     `json:"startedAt"`
	Duration      time.Duration `json:"duration"`
	Attempts      int           `json:"attempts"`
	Postcondition Postcondition `json:"postcondition"`
}
type Request struct {
	Kind        Kind                    `json:"kind"`
	Ref         string                  `json:"ref,omitempty"`
	TargetRef   string                  `json:"targetRef,omitempty"`
	URL         string                  `json:"url,omitempty"`
	Text        string                  `json:"text,omitempty"`
	Value       string                  `json:"value,omitempty"`
	Key         string                  `json:"key,omitempty"`
	Expression  string                  `json:"expression,omitempty"`
	Files       []string                `json:"files,omitempty"`
	DownloadDir string                  `json:"downloadDir,omitempty"`
	Format      string                  `json:"format,omitempty"`
	Quality     int                     `json:"quality,omitempty"`
	X           float64                 `json:"x,omitempty"`
	Y           float64                 `json:"y,omitempty"`
	DeltaX      float64                 `json:"deltaX,omitempty"`
	DeltaY      float64                 `json:"deltaY,omitempty"`
	Timeout     time.Duration           `json:"timeout,omitempty"`
	Accept      bool                    `json:"accept,omitempty"`
	PromptText  string                  `json:"promptText,omitempty"`
	Idempotent  bool                    `json:"idempotent,omitempty"`
	RetryMax    int                     `json:"retryMax,omitempty"`
	Width       int                     `json:"width,omitempty"`
	Height      int                     `json:"height,omitempty"`
	FrameID     string                  `json:"frameId,omitempty"`
	TargetID    string                  `json:"targetId,omitempty"`
	FormIntent  *formactions.FormIntent `json:"formIntent,omitempty"`
}
type Outcome struct {
	Success  bool         `json:"success"`
	Value    any          `json:"value,omitempty"`
	Bytes    []byte       `json:"bytes,omitempty"`
	MIME     string       `json:"mime,omitempty"`
	Width    int          `json:"width,omitempty"`
	Height   int          `json:"height,omitempty"`
	TargetID string       `json:"targetId,omitempty"`
	Download *Download    `json:"download,omitempty"`
	Failure  FailureClass `json:"failure,omitempty"`
	Error    string       `json:"error,omitempty"`
	Evidence Evidence     `json:"evidence"`
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
	page         *bridge.Page
	observer     *bridgeobserve.Collector
	policy       Policy
	now          func() time.Time
	navigator    *cdpops.Navigator
	pointer      *cdpops.PointerDispatcher
	tabs         *artemistabs.TabRegistry
	downloads    *artemisdownload.DownloadManager
	downloadOnce sync.Once
	downloadErr  error
	formIntents  *formactions.FormIntentRuntime
}

// RuntimeConfig injects lifecycle owners used by action execution.
type RuntimeConfig struct {
	Downloads         *artemisdownload.DownloadManager
	FormIntentMetrics func(formactions.FormIntentMetricEvent)
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
	source := &pageTargetSource{root: page, activeID: page.TargetID()}
	formIntents, err := NewPageFormIntentRuntime(page, config.FormIntentMetrics)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		page: page, observer: observer, policy: policy, now: time.Now,
		navigator: cdpops.NewNavigator(caller), pointer: cdpops.NewPointerDispatcher(caller),
		tabs: artemistabs.NewTabRegistry(source), downloads: config.Downloads, formIntents: formIntents,
	}, nil
}

func (r *Runtime) Execute(ctx context.Context, request Request) Outcome {
	start := r.now()
	evidence := Evidence{Action: request.Kind, Ref: request.Ref, StartedAt: start}
	if ctx == nil {
		return failed(evidence, FailureValidation, "action: context required", start, r.now())
	}
	if request.Timeout <= 0 {
		request.Timeout = 15 * time.Second
	}
	actionCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	if err := validateRequest(request); err != nil {
		return failed(evidence, FailureValidation, err.Error(), start, r.now())
	}
	if r.policy != nil {
		if err := r.policy(actionCtx, request); err != nil {
			return failed(evidence, FailurePolicy, err.Error(), start, r.now())
		}
	}
	if request.Kind == KindDialog {
		evidence.BeforeHash = snapshotHash(r.observer.LastSnapshot())
		outcome := r.dialog(actionCtx, request, evidence)
		if outcome.Success {
			after, err := r.observer.Capture(actionCtx, bridgeobserve.ModeEvidence, "")
			if err != nil {
				return failed(evidence, classifyContext(actionCtx, FailureProtocol), fmt.Sprintf("capture after dialog: %v", err), start, r.now())
			}
			outcome.Evidence.AfterHash = snapshotHash(after)
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
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		evidence.Attempts = attempt
		before, err = r.observer.Capture(actionCtx, bridgeobserve.ModeFull, "")
		if err == nil {
			break
		}
		if attempt < attempts {
			select {
			case <-actionCtx.Done():
				break
			case <-time.After(time.Duration(attempt) * 25 * time.Millisecond):
			}
		}
	}
	if err != nil {
		return failed(evidence, classifyContext(actionCtx, FailureProtocol), fmt.Sprintf("capture before action: %v", err), start, r.now())
	}
	evidence.BeforeHash = snapshotHash(before)
	var outcome Outcome
	for attempt := 1; attempt <= attempts; attempt++ {
		evidence.Attempts = attempt
		outcome = r.executeOnce(actionCtx, request, evidence)
		if outcome.Success || !transient(outcome.Failure) || attempt == attempts {
			break
		}
		select {
		case <-actionCtx.Done():
			outcome = failedNow(evidence, classifyContext(actionCtx, outcome.Failure), actionCtx.Err().Error())
			attempt = attempts
		case <-time.After(time.Duration(attempt) * 25 * time.Millisecond):
		}
	}
	if !outcome.Success {
		outcome.Evidence.Duration = r.now().Sub(start)
		return outcome
	}
	after, err := r.observer.Capture(actionCtx, bridgeobserve.ModeFull, "")
	if err == nil {
		outcome.Evidence.AfterHash = snapshotHash(after)
	}
	outcome.Evidence.Duration = r.now().Sub(start)
	return outcome
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
	node, resolved := r.resolve(ctx, q.Ref, e)
	if !resolved.Success {
		return resolved
	}
	e = resolved.Evidence
	switch q.Kind {
	case KindFocus:
		return r.elementBool(ctx, node, e, "function(){this.focus();return document.activeElement===this}", nil, "focused")
	case KindClick:
		return r.click(ctx, node, e)
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

func validateRequest(q Request) error {
	valid := map[Kind]bool{KindNavigate: true, KindReload: true, KindBack: true, KindForward: true, KindWait: true, KindFocus: true, KindClick: true, KindHover: true, KindScroll: true, KindKey: true, KindType: true, KindClear: true, KindFill: true, KindFillForm: true, KindSelect: true, KindCheck: true, KindUncheck: true, KindDrag: true, KindUpload: true, KindDownload: true, KindScreenshot: true, KindPDF: true, KindDialog: true, KindTabOpen: true, KindTabClose: true, KindTabList: true, KindTabSwitch: true, KindEvaluate: true, KindAssert: true, KindViewport: true, KindFrameEvaluate: true}
	if !valid[q.Kind] {
		return fmt.Errorf("action: unsupported kind %q", q.Kind)
	}
	if (q.Kind == KindNavigate || q.Kind == KindTabOpen) && q.URL == "" {
		return errors.New("action: URL required")
	}
	requiresRef := map[Kind]bool{KindFocus: true, KindClick: true, KindHover: true, KindType: true, KindClear: true, KindFill: true, KindSelect: true, KindCheck: true, KindUncheck: true, KindDrag: true, KindUpload: true, KindDownload: true}
	if requiresRef[q.Kind] && q.Ref == "" {
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

func (r *Runtime) resolve(ctx context.Context, ref string, e Evidence) (bridgeobserve.Node, Outcome) {
	resolution, err := r.observer.Resolve(ctx, ref)
	if err != nil {
		return bridgeobserve.Node{}, failedNow(e, classifyContext(ctx, FailureProtocol), err.Error())
	}
	if resolution.Status != bridgeobserve.ResolutionExact && resolution.Status != bridgeobserve.ResolutionReResolved {
		return bridgeobserve.Node{}, failedNow(e, FailureTarget, "target "+string(resolution.Status)+": "+resolution.Reason)
	}
	n := *resolution.Node
	e.BackendDOMID = n.BackendNodeID
	e.FrameID = n.FrameID
	if !n.Visible {
		return bridgeobserve.Node{}, failedNow(e, FailureActionability, "target is hidden")
	}
	if n.Disabled {
		return bridgeobserve.Node{}, failedNow(e, FailureActionability, "target is disabled")
	}
	if n.Hit == bridgeobserve.HitCovered {
		return bridgeobserve.Node{}, failedNow(e, FailureActionability, "target is covered")
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
	value, err := r.eval(ctx, "location.href")
	if err != nil {
		return failedNow(e, FailurePostcondition, err.Error())
	}
	actual := fmt.Sprint(value)
	passed := strings.HasPrefix(actual, q.URL)
	e.Postcondition = Postcondition{Type: "url", Expected: q.URL, Actual: actual, Passed: passed}
	if !passed {
		return failedNow(e, FailurePostcondition, "navigation URL postcondition failed")
	}
	return Outcome{Success: true, Value: actual, Evidence: e}
}
func (r *Runtime) history(ctx context.Context, q Request, e Evidence, delta int) Outcome {
	var result cdpops.NavigationResult
	if delta < 0 {
		result = r.navigator.GoBack(ctx)
	} else {
		result = r.navigator.GoForward(ctx)
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
func (r *Runtime) click(ctx context.Context, n bridgeobserve.Node, e Evidence) Outcome {
	x, y, err := r.elementPoint(ctx, n)
	if err != nil {
		return failedNow(e, FailureActionability, err.Error())
	}
	if err := r.pointer.MouseMoveContext(ctx, x, y); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	if err := r.pointer.ClickContext(ctx, x, y, cdpops.MouseButtonLeft); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
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
	before, err := r.eval(ctx, "({x:scrollX,y:scrollY})")
	if err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	if err := r.pointer.Wheel(ctx, q.X, q.Y, q.DeltaX, q.DeltaY); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	var after any
	passed := q.DeltaX == 0 && q.DeltaY == 0
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	expired := false
	for !passed && !expired {
		after, err = r.eval(ctx, "({x:scrollX,y:scrollY})")
		if err != nil {
			return failedNow(e, FailurePostcondition, err.Error())
		}
		passed = fmt.Sprint(before) != fmt.Sprint(after)
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
		if err := r.page.Call(ctx, "Input.dispatchKeyEvent", map[string]any{"type": kind, "key": q.Key, "text": q.Text}, &struct{}{}); err != nil {
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
		current, err := r.callElement(ctx, n, "function(){return this.value}", nil)
		if err != nil {
			return failedNow(e, FailureProtocol, err.Error())
		}
		expected = fmt.Sprint(current) + text
	}
	if out := r.elementBool(ctx, n, e, "function(){this.focus();return document.activeElement===this}", nil, "focused"); !out.Success {
		return out
	}
	if err := r.page.Call(ctx, "Input.insertText", map[string]any{"text": text}, &struct{}{}); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	return r.verifyValue(ctx, n, expected, e, "typed")
}
func (r *Runtime) setValue(ctx context.Context, n bridgeobserve.Node, value string, e Evidence, post string) Outcome {
	return r.elementBool(ctx, n, e, "function(v){this.focus();this.value=v;this.dispatchEvent(new Event('input',{bubbles:true}));this.dispatchEvent(new Event('change',{bubbles:true}));return this.value===v}", []any{value}, post)
}
func (r *Runtime) verifyValue(ctx context.Context, n bridgeobserve.Node, want string, e Evidence, post string) Outcome {
	value, err := r.callElement(ctx, n, "function(){return this.value}", nil)
	if err != nil {
		return failedNow(e, FailurePostcondition, err.Error())
	}
	passed := fmt.Sprint(value) == want
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
	target, res := r.resolve(ctx, q.TargetRef, e)
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
	events := []struct {
		kind   string
		x, y   float64
		button string
	}{{"mouseMoved", sx, sy, ""}, {"mousePressed", sx, sy, "left"}, {"mouseMoved", tx, ty, "left"}, {"mouseReleased", tx, ty, "left"}}
	for _, event := range events {
		params := map[string]any{"type": event.kind, "x": event.x, "y": event.y}
		if event.button != "" {
			params["button"] = event.button
			params["buttons"] = 1
		}
		if err := r.page.Call(ctx, "Input.dispatchMouseEvent", params, &struct{}{}); err != nil {
			return failedNow(e, FailureProtocol, err.Error())
		}
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
	if err := r.page.Call(ctx, "DOM.setFileInputFiles", map[string]any{"files": q.Files, "backendNodeId": n.BackendNodeID}, &struct{}{}); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	value, err := r.callElement(ctx, n, "function(){return Array.from(this.files).map(f=>f.name)}", nil)
	if err != nil {
		return failedNow(e, FailurePostcondition, err.Error())
	}
	names, ok := value.([]any)
	passed := ok && len(names) == len(q.Files)
	e.Postcondition = Postcondition{Type: "uploaded_files", Expected: len(q.Files), Actual: len(names), Passed: passed}
	if !passed {
		return failedNow(e, FailurePostcondition, "upload file count mismatch")
	}
	return Outcome{Success: true, Value: value, Evidence: e}
}
func (r *Runtime) download(ctx context.Context, n bridgeobserve.Node, q Request, e Evidence) Outcome {
	manager, err := r.downloadManager()
	if err != nil {
		return failedNow(e, FailureValidation, err.Error())
	}
	stage, err := manager.NewBrowserStage()
	if err != nil {
		return failedNow(e, FailureValidation, err.Error())
	}
	defer stage.Close()
	subscription, err := r.page.SubscribeBrowserEvents(64)
	if err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	defer subscription.Close()
	params := map[string]any{"behavior": "allow", "downloadPath": stage.Directory(), "eventsEnabled": true}
	if id := r.page.BrowserContextID(); id != "" {
		params["browserContextId"] = id
	}
	if err := r.page.CallBrowser(ctx, "Browser.setDownloadBehavior", params, &struct{}{}); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	defer r.disableDownloads()
	if out := r.click(ctx, n, e); !out.Success {
		return out
	}
	var guid, filename string
	for {
		select {
		case <-ctx.Done():
			r.cancelDownload(guid)
			return failedNow(e, classifyContext(ctx, FailureTimeout), "download did not complete: "+ctx.Err().Error())
		case event, ok := <-subscription.Events:
			if !ok {
				return failedNow(e, FailureProtocol, "download event stream closed")
			}
			switch event.Method {
			case "Browser.downloadWillBegin":
				var started struct {
					GUID              string `json:"guid"`
					SuggestedFilename string `json:"suggestedFilename"`
				}
				if json.Unmarshal(event.Params, &started) == nil && started.GUID != "" {
					guid, filename = started.GUID, started.SuggestedFilename
				}
			case "Browser.downloadProgress":
				var progress struct {
					GUID          string  `json:"guid"`
					State         string  `json:"state"`
					ReceivedBytes float64 `json:"receivedBytes"`
				}
				if json.Unmarshal(event.Params, &progress) != nil || progress.GUID == "" || guid != "" && progress.GUID != guid {
					continue
				}
				if guid == "" {
					guid = progress.GUID
				}
				if err := manager.ValidatePending(int64(progress.ReceivedBytes)); err != nil {
					r.cancelDownload(guid)
					return failedNow(e, FailurePolicy, err.Error())
				}
				if progress.State == "canceled" {
					return failedNow(e, FailureProtocol, "download canceled by browser")
				}
				if progress.State != "completed" {
					continue
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

func (r *Runtime) cancelDownload(guid string) {
	if guid == "" {
		return
	}
	params := map[string]any{"guid": guid}
	if id := r.page.BrowserContextID(); id != "" {
		params["browserContextId"] = id
	}
	cancelCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = r.page.CallBrowser(cancelCtx, "Browser.cancelDownload", params, &struct{}{})
}

func (r *Runtime) disableDownloads() {
	params := map[string]any{"behavior": "deny", "eventsEnabled": false}
	if id := r.page.BrowserContextID(); id != "" {
		params["browserContextId"] = id
	}
	disableCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = r.page.CallBrowser(disableCtx, "Browser.setDownloadBehavior", params, &struct{}{})
}

func (r *Runtime) screenshot(ctx context.Context, q Request, e Evidence) Outcome {
	format := q.Format
	if format == "" {
		format = "png"
	}
	params := map[string]any{"format": format, "fromSurface": true, "captureBeyondViewport": true}
	if format == "jpeg" && q.Quality > 0 {
		params["quality"] = q.Quality
	}
	var result struct {
		Data string `json:"data"`
	}
	if err := r.page.Call(ctx, "Page.captureScreenshot", params, &result); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	raw, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	cfg, kind, err := image.DecodeConfig(strings.NewReader(string(raw)))
	if err != nil {
		return failedNow(e, FailurePostcondition, "invalid image: "+err.Error())
	}
	passed := kind == format || format == "jpeg" && kind == "jpeg"
	e.Postcondition = Postcondition{Type: "image_signature", Expected: format, Actual: kind, Passed: passed}
	if !passed {
		return failedNow(e, FailurePostcondition, "image format mismatch")
	}
	return Outcome{Success: true, Bytes: raw, MIME: "image/" + kind, Width: cfg.Width, Height: cfg.Height, Evidence: e}
}
func (r *Runtime) pdf(ctx context.Context, _ Request, e Evidence) Outcome {
	var result struct {
		Data string `json:"data"`
	}
	if err := r.page.Call(ctx, "Page.printToPDF", map[string]any{"printBackground": true, "transferMode": "ReturnAsBase64"}, &result); err != nil {
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
	if err := r.page.Call(ctx, "Page.handleJavaScriptDialog", map[string]any{"accept": q.Accept, "promptText": q.PromptText}, &struct{}{}); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	e.Postcondition = Postcondition{Type: "dialog_handled", Expected: q.Accept, Actual: q.Accept, Passed: true}
	return Outcome{Success: true, Evidence: e}
}
func (r *Runtime) tabOpen(ctx context.Context, q Request, e Evidence) Outcome {
	tab, err := r.tabs.CreateTabContext(ctx, "", q.URL)
	if err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	e.Postcondition = Postcondition{Type: "tab_attached", Actual: tab.ID, Passed: tab.ID != ""}
	return Outcome{Success: true, TargetID: tab.ID, Evidence: e}
}
func (r *Runtime) tabList(ctx context.Context, e Evidence) Outcome {
	tabs, err := r.tabs.ListTabsContext(ctx, "")
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
	value, err := r.eval(ctx, q.Expression)
	if err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	passed, valueBool := value.(bool)
	e.Postcondition = Postcondition{Type: "assertion", Expected: true, Actual: value, Passed: valueBool && passed}
	if !valueBool || !passed {
		return failedNow(e, FailurePostcondition, "assertion returned false")
	}
	return Outcome{Success: true, Value: value, Evidence: e}
}

func (r *Runtime) viewport(ctx context.Context, q Request, e Evidence) Outcome {
	if err := r.page.Call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": q.Width, "height": q.Height, "deviceScaleFactor": 1, "mobile": false}, &struct{}{}); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	value, err := r.eval(ctx, "({width:innerWidth,height:innerHeight})")
	if err != nil {
		return failedNow(e, FailurePostcondition, err.Error())
	}
	dimensions, ok := value.(map[string]any)
	passed := ok && dimensions["width"] == float64(q.Width) && dimensions["height"] == float64(q.Height)
	e.Postcondition = Postcondition{Type: "viewport", Expected: map[string]int{"width": q.Width, "height": q.Height}, Actual: value, Passed: passed}
	if !passed {
		return failedNow(e, FailurePostcondition, "viewport postcondition failed")
	}
	return Outcome{Success: true, Value: value, Evidence: e}
}

func (r *Runtime) frameEvaluate(ctx context.Context, q Request, e Evidence) Outcome {
	var world struct {
		ExecutionContextID int64 `json:"executionContextId"`
	}
	if err := r.page.CallFrame(ctx, q.FrameID, "Page.createIsolatedWorld", map[string]any{"frameId": q.FrameID, "worldName": "artemis-action", "grantUniveralAccess": false}, &world); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	var result struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails,omitempty"`
	}
	if err := r.page.CallFrame(ctx, q.FrameID, "Runtime.evaluate", map[string]any{"expression": q.Expression, "contextId": world.ExecutionContextID, "returnByValue": true, "awaitPromise": true}, &result); err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	if result.Exception != nil {
		return failedNow(e, FailureProtocol, result.Exception.Text)
	}
	e.Postcondition = Postcondition{Type: "frame_evaluation", Actual: q.FrameID, Passed: true}
	return Outcome{Success: true, Value: result.Result.Value, Evidence: e}
}

func (r *Runtime) elementBool(ctx context.Context, n bridgeobserve.Node, e Evidence, function string, args []any, post string) Outcome {
	value, err := r.callElement(ctx, n, function, args)
	if err != nil {
		return failedNow(e, FailureProtocol, err.Error())
	}
	passed, ok := value.(bool)
	e.Postcondition = Postcondition{Type: post, Expected: true, Actual: value, Passed: ok && passed}
	if !ok || !passed {
		return failedNow(e, FailurePostcondition, post+" postcondition failed")
	}
	return Outcome{Success: true, Value: value, Evidence: e}
}
func (r *Runtime) callElement(ctx context.Context, n bridgeobserve.Node, function string, args []any) (any, error) {
	var resolved struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := r.page.CallFrame(ctx, n.FrameID, "DOM.resolveNode", map[string]any{"backendNodeId": n.BackendNodeID}, &resolved); err != nil {
		return nil, err
	}
	if resolved.Object.ObjectID == "" {
		return nil, errors.New("resolve element: empty object ID")
	}
	arguments := make([]map[string]any, len(args))
	for i, arg := range args {
		arguments[i] = map[string]any{"value": arg}
	}
	var result struct {
		Result struct {
			Value       any    `json:"value"`
			Description string `json:"description"`
		} `json:"result"`
		Exception *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails,omitempty"`
	}
	err := r.page.CallFrame(ctx, n.FrameID, "Runtime.callFunctionOn", map[string]any{"objectId": resolved.Object.ObjectID, "functionDeclaration": function, "arguments": arguments, "returnByValue": true, "awaitPromise": true}, &result)
	if err != nil {
		return nil, err
	}
	if result.Exception != nil {
		return nil, errors.New(result.Exception.Text)
	}
	return result.Result.Value, nil
}

func (r *Runtime) elementPoint(ctx context.Context, n bridgeobserve.Node) (float64, float64, error) {
	value, err := r.callElement(ctx, n, "function(){this.scrollIntoView({block:'center',inline:'center'});const r=this.getBoundingClientRect();return {x:r.left+r.width/2,y:r.top+r.height/2,w:r.width,h:r.height}}", nil)
	if err != nil {
		return 0, 0, err
	}
	rect, ok := value.(map[string]any)
	if !ok {
		return 0, 0, errors.New("target geometry unavailable")
	}
	x, xok := rect["x"].(float64)
	y, yok := rect["y"].(float64)
	w, wok := rect["w"].(float64)
	h, hok := rect["h"].(float64)
	if !xok || !yok || !wok || !hok || w <= 0 || h <= 0 {
		return 0, 0, errors.New("target has no actionable viewport geometry")
	}
	offsetX, offsetY, err := r.frameOffset(ctx, n.FrameID)
	if err != nil {
		return 0, 0, err
	}
	return x + offsetX, y + offsetY, nil
}

func (r *Runtime) frameOffset(ctx context.Context, frameID string) (float64, float64, error) {
	var tree struct {
		FrameTree frameBranch `json:"frameTree"`
	}
	if err := r.page.Call(ctx, "Page.getFrameTree", map[string]any{}, &tree); err != nil {
		return 0, 0, err
	}
	parents := map[string]string{}
	flattenFrameParents(tree.FrameTree, parents)
	root := tree.FrameTree.Frame.ID
	x, y := 0.0, 0.0
	seen := map[string]bool{}
	for frameID != "" && frameID != root {
		if seen[frameID] {
			return 0, 0, errors.New("frame hierarchy cycle")
		}
		seen[frameID] = true
		var owner struct {
			BackendDOMID int64 `json:"backendNodeId"`
		}
		if err := r.page.Call(ctx, "DOM.getFrameOwner", map[string]any{"frameId": frameID}, &owner); err != nil {
			return 0, 0, err
		}
		if owner.BackendDOMID == 0 {
			return 0, 0, errors.New("frame owner unavailable")
		}
		value, err := r.callElement(ctx, bridgeobserve.Node{BackendNodeID: owner.BackendDOMID}, "function(){const r=this.getBoundingClientRect();return {x:r.left,y:r.top}}", nil)
		if err != nil {
			return 0, 0, err
		}
		rect, ok := value.(map[string]any)
		if !ok {
			return 0, 0, errors.New("frame owner geometry unavailable")
		}
		dx, xok := rect["x"].(float64)
		dy, yok := rect["y"].(float64)
		if !xok || !yok {
			return 0, 0, errors.New("frame owner coordinates unavailable")
		}
		x += dx
		y += dy
		frameID = parents[frameID]
	}
	return x, y, nil
}

type frameBranch struct {
	Frame struct {
		ID       string `json:"id"`
		ParentID string `json:"parentId,omitempty"`
	} `json:"frame"`
	ChildFrames []frameBranch `json:"childFrames,omitempty"`
}

func flattenFrameParents(branch frameBranch, parents map[string]string) {
	parents[branch.Frame.ID] = branch.Frame.ParentID
	for _, child := range branch.ChildFrames {
		flattenFrameParents(child, parents)
	}
}
func (r *Runtime) eval(ctx context.Context, expression string) (any, error) {
	var result struct {
		Result struct {
			Value       any    `json:"value"`
			Description string `json:"description"`
		} `json:"result"`
		Exception *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails,omitempty"`
	}
	if err := r.page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}, &result); err != nil {
		return nil, err
	}
	if result.Exception != nil {
		return nil, errors.New(result.Exception.Text)
	}
	return result.Result.Value, nil
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
