package artemis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/profile"
	"github.com/Christopher-Schulze/Artemis/security"
)

// workflow.go executes reusable verified browser workflows (spec L4551-L4563
// Workflow Capture Recorder replay, L4375-L4394) on a session-owned Chromium
// page. A versioned definition is validated and authorized before any effect;
// shadow mode resolves targets without dispatching; execution resolves every
// target uniquely against the current snapshot, dispatches through the
// canonical action runtime and verifies each declared outcome. Sensitive
// inputs are credential-store references that never enter results.

// WorkflowVersion is the only accepted definition schema version.
const WorkflowVersion = 1

const maxWorkflowSteps = 64

// WorkflowDefinition is a reusable, parameterized step list bound to origins.
type WorkflowDefinition struct {
	Version int             `json:"version"`
	Name    string          `json:"name"`
	Origins []string        `json:"origins"`
	Params  []WorkflowParam `json:"params,omitempty"`
	Steps   []WorkflowStep  `json:"steps"`
}

// WorkflowParam declares an input slot; sensitive slots take credential
// references only.
type WorkflowParam struct {
	Name      string `json:"name"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

// WorkflowStep is one action. Destructive steps (click, key, select, check,
// uncheck) need a Compensation note before they may run.
type WorkflowStep struct {
	Action       actions.Kind     `json:"action"`
	Target       *WorkflowTarget  `json:"target,omitempty"`
	URL          string           `json:"url,omitempty"`
	Value        string           `json:"value,omitempty"`
	Param        string           `json:"param,omitempty"`
	Key          string           `json:"key,omitempty"`
	Outcome      *WorkflowOutcome `json:"outcome,omitempty"`
	Compensation string           `json:"compensation,omitempty"`
}

// WorkflowTarget matches exactly one snapshot node by accessible role and
// name, optionally narrowed by element id.
type WorkflowTarget struct {
	Role string `json:"role"`
	Name string `json:"name"`
	ID   string `json:"id,omitempty"`
}

// WorkflowOutcome is a required postcondition checked after the step.
type WorkflowOutcome struct {
	Type  string          `json:"type"` // url_contains | text_present | element_present
	Value string          `json:"value,omitempty"`
	Node  *WorkflowTarget `json:"node,omitempty"`
}

// WorkflowInput fills a param: Value for ordinary slots, Credential for
// sensitive ones.
type WorkflowInput struct {
	Value      string         `json:"value,omitempty"`
	Credential *CredentialRef `json:"credential,omitempty"`
}

// CredentialRef points at a stored credential; the domain must match the
// page the value is typed into.
type CredentialRef struct {
	Profile string `json:"profile"`
	Domain  string `json:"domain"`
	Field   string `json:"field"` // username | password
}

// WorkflowRunOptions control one run. Journal, when set, is the durable
// step journal path; an existing journal resumes the run.
type WorkflowRunOptions struct {
	Shadow      bool                     `json:"shadow,omitempty"`
	Credentials *profile.CredentialStore `json:"-"`
	Journal     string                   `json:"-"`
}

// Workflow step states.
const (
	WorkflowStepPlanned      = "planned"
	WorkflowStepDeferred     = "deferred"
	WorkflowStepVerified     = "verified"
	WorkflowStepFailed       = "failed"
	WorkflowStepNotAttempted = "not_attempted"
)

// WorkflowStepResult is one step's receipt; it never contains input values.
type WorkflowStepResult struct {
	Index   int                   `json:"index"`
	Action  actions.Kind          `json:"action"`
	Ref     string                `json:"ref,omitempty"`
	Status  string                `json:"status"`
	Effect  actions.EffectState   `json:"effect,omitempty"`
	Failure string                `json:"failure,omitempty"`
	Error   string                `json:"error,omitempty"`
	Trust   security.ContentTrust `json:"trust"`
}

// WorkflowRunResult is the complete run receipt.
type WorkflowRunResult struct {
	Name    string               `json:"name"`
	Shadow  bool                 `json:"shadow"`
	Success bool                 `json:"success"`
	Steps   []WorkflowStepResult `json:"steps"`
}

var workflowActions = map[actions.Kind]bool{
	actions.KindNavigate: true, actions.KindClick: true, actions.KindFill: true, actions.KindType: true,
	actions.KindSelect: true, actions.KindCheck: true, actions.KindUncheck: true, actions.KindKey: true,
	actions.KindScroll: true, actions.KindWait: true, actions.KindClear: true, actions.KindHover: true, actions.KindFocus: true,
}

var destructiveActions = map[actions.Kind]bool{
	actions.KindClick: true, actions.KindKey: true, actions.KindSelect: true, actions.KindCheck: true, actions.KindUncheck: true,
}

// ValidateWorkflow checks schema, slots, origins and activation rules
// (missing compensation, literal secrets) without touching a page.
func ValidateWorkflow(def WorkflowDefinition, inputs map[string]WorkflowInput) error {
	if def.Version != WorkflowVersion {
		return fmt.Errorf("workflow: version %d unsupported", def.Version)
	}
	if strings.TrimSpace(def.Name) == "" || len(def.Name) > 256 {
		return errors.New("workflow: name required (max 256 bytes)")
	}
	if len(def.Steps) == 0 || len(def.Steps) > maxWorkflowSteps {
		return fmt.Errorf("workflow: 1..%d steps required", maxWorkflowSteps)
	}
	if len(def.Origins) == 0 {
		return errors.New("workflow: at least one origin required")
	}
	for _, origin := range def.Origins {
		if workflowOrigin(origin) != origin {
			return fmt.Errorf("workflow: origin %q must be scheme://host[:port]", origin)
		}
	}
	params := map[string]WorkflowParam{}
	for _, param := range def.Params {
		if param.Name == "" || params[param.Name].Name != "" {
			return fmt.Errorf("workflow: param %q empty or duplicate", param.Name)
		}
		params[param.Name] = param
	}
	for index, step := range def.Steps {
		if err := validateWorkflowStep(def, params, step); err != nil {
			return fmt.Errorf("workflow step %d: %w", index, err)
		}
	}
	for name, param := range params {
		input, ok := inputs[name]
		switch {
		case !ok:
			return fmt.Errorf("workflow: input %q missing", name)
		case param.Sensitive && (input.Credential == nil || input.Value != ""):
			return fmt.Errorf("workflow: sensitive input %q must be a credential reference", name)
		case !param.Sensitive && input.Credential != nil:
			return fmt.Errorf("workflow: input %q is not declared sensitive", name)
		case input.Credential != nil && (input.Credential.Field != "username" && input.Credential.Field != "password" || input.Credential.Profile == "" || input.Credential.Domain == ""):
			return fmt.Errorf("workflow: credential reference for %q is incomplete", name)
		}
	}
	for name := range inputs {
		if _, declared := params[name]; !declared {
			return fmt.Errorf("workflow: input %q is not a declared param", name)
		}
	}
	return nil
}

func validateWorkflowStep(def WorkflowDefinition, params map[string]WorkflowParam, step WorkflowStep) error {
	if !workflowActions[step.Action] {
		return fmt.Errorf("action %q not allowed in workflows", step.Action)
	}
	switch step.Action {
	case actions.KindNavigate:
		if !containsString(def.Origins, workflowOrigin(step.URL)) {
			return fmt.Errorf("navigation target %q is outside the workflow origins", step.URL)
		}
	case actions.KindWait, actions.KindScroll, actions.KindKey:
	default:
		if step.Target == nil || step.Target.Role == "" || step.Target.Name == "" {
			return errors.New("target role and name required")
		}
	}
	if step.Param != "" {
		param, ok := params[step.Param]
		if !ok {
			return fmt.Errorf("param %q not declared", step.Param)
		}
		if step.Value != "" {
			return errors.New("value and param are exclusive")
		}
		if param.Sensitive && step.Action != actions.KindFill && step.Action != actions.KindType {
			return errors.New("sensitive params may only be typed or filled")
		}
	}
	if (step.Action == actions.KindFill || step.Action == actions.KindType) && step.Value != "" && step.Target != nil && strings.EqualFold(step.Target.Role, "password") {
		return errors.New("literal value for a password field: use a sensitive param")
	}
	if destructiveActions[step.Action] && strings.TrimSpace(step.Compensation) == "" {
		return errors.New("destructive step needs a compensation note")
	}
	if step.Outcome != nil {
		switch step.Outcome.Type {
		case "url_contains", "text_present":
			if step.Outcome.Value == "" {
				return errors.New("outcome value required")
			}
		case "element_present":
			if step.Outcome.Node == nil || step.Outcome.Node.Role == "" || step.Outcome.Node.Name == "" {
				return errors.New("outcome node role and name required")
			}
		default:
			return fmt.Errorf("outcome type %q unsupported", step.Outcome.Type)
		}
	}
	return nil
}

func workflowOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// RunWorkflow validates, authorizes and runs def on a session-owned Chromium
// page. Shadow runs resolve targets on the current page without dispatching.
// Execution holds the page's exclusive writer lease, stops at the first
// failure and keeps the receipts of dispatched steps. With
// WorkflowRunOptions.Journal every step is journaled durably and a run whose
// journal already exists resumes after verifying the interrupted step.
func (s *Session) RunWorkflow(ctx context.Context, pageID string, def WorkflowDefinition, inputs map[string]WorkflowInput, options WorkflowRunOptions) (WorkflowRunResult, *TaskError) {
	result := WorkflowRunResult{Name: def.Name, Shadow: options.Shadow}
	if err := ValidateWorkflow(def, inputs); err != nil {
		return result, newTaskError(TaskErrorInvalidInput, "workflow", err)
	}
	if !options.Shadow && options.Credentials == nil && hasSensitiveParam(def) {
		return result, newTaskError(TaskErrorCapabilityUnavailable, "workflow", errors.New("credential store required for sensitive inputs"))
	}
	for _, origin := range def.Origins {
		for _, step := range def.Steps {
			if err := s.owner.authorizeBrowserRequest(ctx, bridge.PolicyRequest{Action: string(step.Action), URL: origin + "/", Method: "GET"}); err != nil {
				return result, classifyTaskError("workflow_authority", fmt.Errorf("step %s on %s is not authorized: %w", step.Action, origin, err))
			}
		}
	}
	for index := range def.Steps {
		result.Steps = append(result.Steps, WorkflowStepResult{Index: index, Action: def.Steps[index].Action, Status: WorkflowStepNotAttempted})
	}
	if options.Shadow {
		return s.runWorkflowSteps(ctx, pageID, def, inputs, options, result, nil, 0)
	}
	lease, taskErr := s.AcquireWriterLease(pageID, "workflow:"+def.Name, maxWriterLeaseTTL)
	if taskErr != nil {
		return result, taskErr
	}
	result, taskErr = s.runWorkflowLeased(WithWriterLease(ctx, lease.Token), pageID, def, inputs, options, result)
	if releaseErr := s.ReleaseWriterLease(pageID, lease.Token); releaseErr != nil && taskErr == nil {
		taskErr = releaseErr
	}
	return result, taskErr
}

func (s *Session) runWorkflowLeased(ctx context.Context, pageID string, def WorkflowDefinition, inputs map[string]WorkflowInput, options WorkflowRunOptions, result WorkflowRunResult) (WorkflowRunResult, *TaskError) {
	if options.Journal == "" {
		return s.runWorkflowSteps(ctx, pageID, def, inputs, options, result, nil, 0)
	}
	journal, records, err := openWorkflowJournal(options.Journal, def)
	if err != nil {
		return result, newTaskError(TaskErrorInvalidInput, "workflow_journal", err)
	}
	states, err := journalSteps(records, len(def.Steps))
	if err != nil {
		return result, newTaskError(TaskErrorInvalidInput, "workflow_journal", err)
	}
	start, stopped, taskErr := s.resumePoint(ctx, pageID, def, states, &result, journal)
	if taskErr != nil || stopped {
		return result, taskErr
	}
	return s.runWorkflowSteps(ctx, pageID, def, inputs, options, result, journal, start)
}

// resumePoint replays the journal: verified steps are kept, an interrupted
// step is verified through its declared outcome; an unverifiable destructive
// step is uncertain and stops the run instead of repeating its effect.
func (s *Session) resumePoint(ctx context.Context, pageID string, def WorkflowDefinition, states []journalStep, result *WorkflowRunResult, journal *workflowJournal) (int, bool, *TaskError) {
	lastURL := ""
	for index, state := range states {
		receipt := &result.Steps[index]
		switch state.status {
		case WorkflowStepVerified:
			receipt.Status, lastURL = WorkflowStepVerified, state.url
			continue
		case WorkflowStepUncertain:
			receipt.Status, receipt.Error = WorkflowStepUncertain, "outcome of the interrupted step is unknown; resolve it before resuming"
			return index, true, nil
		case "pending":
			step := def.Steps[index]
			if step.Outcome != nil && s.verifyWorkflowOutcome(ctx, pageID, step.Outcome) == nil {
				pageURL, taskErr := s.PageURL(ctx, pageID)
				if taskErr != nil {
					return index, true, taskErr
				}
				if err := journal.append(journalRecord{Kind: journalReceipt, Step: index, Status: WorkflowStepVerified, URL: pageURL}); err != nil {
					return index, true, classifyTaskError("workflow_journal", err)
				}
				receipt.Status, lastURL = WorkflowStepVerified, pageURL
				continue
			}
			if destructiveActions[step.Action] {
				if err := journal.append(journalRecord{Kind: journalReceipt, Step: index, Status: WorkflowStepUncertain}); err != nil {
					return index, true, classifyTaskError("workflow_journal", err)
				}
				receipt.Status, receipt.Error = WorkflowStepUncertain, "interrupted destructive step cannot be verified; it is not repeated"
				return index, true, nil
			}
		}
		return index, false, s.alignWorkflowPage(ctx, pageID, def, lastURL)
	}
	result.Success = true
	return len(states), true, nil
}

// alignWorkflowPage returns the page to the URL the last verified step
// reached before execution continues from the journal.
func (s *Session) alignWorkflowPage(ctx context.Context, pageID string, def WorkflowDefinition, lastURL string) *TaskError {
	if lastURL == "" {
		return nil
	}
	current, taskErr := s.PageURL(ctx, pageID)
	if taskErr != nil || current == lastURL {
		return taskErr
	}
	if !containsString(def.Origins, workflowOrigin(lastURL)) {
		return newTaskError(TaskErrorPolicyDenied, "workflow_resume", fmt.Errorf("journaled URL %q is outside the workflow origins", lastURL))
	}
	outcome, taskErr := s.ActChromiumPage(ctx, pageID, 0, actions.Request{Kind: actions.KindNavigate, URL: lastURL})
	if taskErr != nil {
		return taskErr
	}
	if !outcome.Success {
		return newTaskError(TaskErrorExecutionFailed, "workflow_resume", fmt.Errorf("restore %s: %s", lastURL, outcome.Error))
	}
	return nil
}

func (s *Session) runWorkflowSteps(ctx context.Context, pageID string, def WorkflowDefinition, inputs map[string]WorkflowInput, options WorkflowRunOptions, result WorkflowRunResult, journal *workflowJournal, start int) (WorkflowRunResult, *TaskError) {
	checker := security.NewIDPIChecker(security.DefaultIDPIConfig())
	var err error
	for index := start; index < len(def.Steps); index++ {
		step := def.Steps[index]
		if err := ctx.Err(); err != nil {
			return result, classifyTaskError("workflow", err)
		}
		receipt := &result.Steps[index]
		pageURL, evidenceNodes, epoch, taskErr := s.workflowView(ctx, pageID)
		if taskErr != nil {
			receipt.Status, receipt.Error = WorkflowStepFailed, taskErr.Error()
			return result, nil
		}
		if receipt.Trust, err = checker.Label(pageURL, "workflow", nil); err != nil {
			receipt.Status, receipt.Error = WorkflowStepFailed, err.Error()
			return result, nil
		}
		if !containsString(def.Origins, workflowOrigin(pageURL)) {
			receipt.Status, receipt.Error = WorkflowStepFailed, "page origin is outside the workflow origins"
			return result, nil
		}
		target, err := resolveWorkflowTarget(evidenceNodes, step.Target)
		if err != nil {
			receipt.Status, receipt.Error = WorkflowStepFailed, err.Error()
			return result, nil
		}
		receipt.Ref = target.Ref
		if options.Shadow {
			receipt.Status = WorkflowStepPlanned
			if step.Action == actions.KindNavigate || destructiveActions[step.Action] {
				for later := index + 1; later < len(result.Steps); later++ {
					result.Steps[later].Status = WorkflowStepDeferred
				}
				result.Success = true
				return result, nil
			}
			continue
		}
		if journal != nil {
			if err := journal.append(journalRecord{Kind: journalIntent, Step: index}); err != nil {
				return result, classifyTaskError("workflow_journal", err)
			}
		}
		outcome, err := s.dispatchWorkflowStep(ctx, pageID, epoch, target, step, inputs, options.Credentials)
		receipt.Effect = outcome.Evidence.Effect
		if err != nil || !outcome.Success {
			receipt.Status, receipt.Failure = WorkflowStepFailed, string(outcome.Failure)
			if destructiveActions[step.Action] && (outcome.Evidence.Effect == actions.EffectDispatched || outcome.Evidence.Effect == actions.EffectPossible) {
				receipt.Status = WorkflowStepUncertain
			}
			receipt.Error = workflowError(err, outcome)
			return result, writeJournalReceipt(journal, index, receipt.Status, "")
		}
		if err := s.awaitWorkflowOutcome(ctx, pageID, step.Outcome, workflowOutcomeSettle); err != nil {
			receipt.Status, receipt.Error = WorkflowStepFailed, "outcome not met: "+err.Error()
			return result, writeJournalReceipt(journal, index, receipt.Status, "")
		}
		// The step may have navigated: label the receipt with the origin the
		// page reached; a blocked origin fails the step.
		afterURL, taskErr := s.PageURL(ctx, pageID)
		if taskErr != nil {
			receipt.Status, receipt.Error = WorkflowStepFailed, taskErr.Error()
			return result, writeJournalReceipt(journal, index, WorkflowStepFailed, "")
		}
		if receipt.Trust, err = checker.Label(afterURL, "workflow", nil); err != nil {
			receipt.Status, receipt.Error = WorkflowStepFailed, err.Error()
			return result, writeJournalReceipt(journal, index, WorkflowStepFailed, "")
		}
		receipt.Status = WorkflowStepVerified
		if taskErr := writeJournalReceipt(journal, index, WorkflowStepVerified, afterURL); taskErr != nil {
			return result, taskErr
		}
	}
	result.Success = true
	return result, nil
}

func writeJournalReceipt(journal *workflowJournal, step int, status, pageURL string) *TaskError {
	if journal == nil {
		return nil
	}
	if err := journal.append(journalRecord{Kind: journalReceipt, Step: step, Status: status, URL: pageURL}); err != nil {
		return classifyTaskError("workflow_journal", err)
	}
	return nil
}

func hasSensitiveParam(def WorkflowDefinition) bool {
	for _, param := range def.Params {
		if param.Sensitive {
			return true
		}
	}
	return false
}

func workflowError(err error, outcome actions.Outcome) string {
	if err != nil {
		return err.Error()
	}
	return outcome.Error
}

// workflowView observes the page and returns its URL, interactive nodes with
// page-bound refs and the current document epoch.
func (s *Session) workflowView(ctx context.Context, pageID string) (string, []bridgeobserve.Node, uint64, *TaskError) {
	evidence, taskErr := s.ObserveChromiumPage(ctx, pageID, bridgeobserve.ModeFull, "")
	if taskErr != nil {
		return "", nil, 0, taskErr
	}
	pageURL, taskErr := s.PageURL(ctx, pageID)
	if taskErr != nil {
		return "", nil, 0, taskErr
	}
	return pageURL, evidence.Snapshot.Nodes, evidence.Snapshot.Epoch, nil
}

// resolveWorkflowTarget finds exactly one node for target; zero or several
// matches are drift and fail closed. A nil target resolves to no node.
func resolveWorkflowTarget(nodes []bridgeobserve.Node, target *WorkflowTarget) (bridgeobserve.Node, error) {
	if target == nil {
		return bridgeobserve.Node{}, nil
	}
	var matches []bridgeobserve.Node
	for _, node := range nodes {
		if node.Ref == "" || !strings.EqualFold(node.Role, target.Role) || !strings.EqualFold(strings.TrimSpace(node.Name), strings.TrimSpace(target.Name)) {
			continue
		}
		if target.ID != "" && node.Attributes["id"] != target.ID {
			continue
		}
		matches = append(matches, node)
	}
	switch len(matches) {
	case 0:
		return bridgeobserve.Node{}, fmt.Errorf("selector drift: no %s %q on the page", target.Role, target.Name)
	case 1:
		if matches[0].Disabled {
			return bridgeobserve.Node{}, fmt.Errorf("target %s %q is disabled", target.Role, target.Name)
		}
		return matches[0], nil
	default:
		return bridgeobserve.Node{}, fmt.Errorf("selector drift: %d matches for %s %q", len(matches), target.Role, target.Name)
	}
}

// dispatchWorkflowStep builds the canonical action and runs it. Credential
// values exist only inside the lease callback.
func (s *Session) dispatchWorkflowStep(ctx context.Context, pageID string, epoch uint64, target bridgeobserve.Node, step WorkflowStep, inputs map[string]WorkflowInput, store *profile.CredentialStore) (actions.Outcome, error) {
	request := actions.Request{Kind: step.Action, Ref: target.Ref, URL: step.URL, Key: step.Key}
	if target.Ref == "" {
		epoch = 0
	}
	if step.Action == actions.KindSelect {
		request.Value = step.Value
	} else {
		request.Text = step.Value
	}
	input, hasInput := inputs[step.Param]
	if step.Param == "" || !hasInput {
		outcome, taskErr := s.ActChromiumPage(ctx, pageID, epoch, request)
		return outcome, taskErrorOrNil(taskErr)
	}
	if input.Credential == nil {
		if step.Action == actions.KindSelect {
			request.Value = input.Value
		} else {
			request.Text = input.Value
		}
		outcome, taskErr := s.ActChromiumPage(ctx, pageID, epoch, request)
		return outcome, taskErrorOrNil(taskErr)
	}
	reference := input.Credential
	if err := s.verifyWorkflowCredentialTarget(ctx, pageID, target, reference.Domain); err != nil {
		return actions.Outcome{Failure: actions.FailurePolicy}, err
	}
	lease, err := store.AcquireCredential(reference.Profile, reference.Domain)
	if err != nil {
		return actions.Outcome{Failure: actions.FailurePolicy}, fmt.Errorf("credential %s/%s: %w", reference.Profile, reference.Domain, err)
	}
	defer lease.Close()
	var outcome actions.Outcome
	var taskErr *TaskError
	dispatch := func(secret string) error {
		request.Text = secret
		outcome, taskErr = s.ActChromiumPage(ctx, pageID, epoch, request)
		request.Text = ""
		outcome.Value = nil
		return nil
	}
	if reference.Field == "username" {
		record := lease.Record()
		if record == nil {
			return actions.Outcome{Failure: actions.FailurePolicy}, errors.New("credential lease closed")
		}
		if err := dispatch(record.Username); err != nil {
			return outcome, err
		}
	} else if err := lease.WithPassword(dispatch); err != nil {
		return outcome, err
	}
	return outcome, taskErrorOrNil(taskErr)
}

func taskErrorOrNil(taskErr *TaskError) error {
	if taskErr == nil {
		return nil
	}
	return taskErr
}

// workflowOutcomeSettle bounds how long a dispatched step's outcome may take
// to appear: navigations and asynchronous updates settle after the action.
const workflowOutcomeSettle = 5 * time.Second

// awaitWorkflowOutcome polls the outcome until it holds or settle elapses.
func (s *Session) awaitWorkflowOutcome(ctx context.Context, pageID string, outcome *WorkflowOutcome, settle time.Duration) error {
	deadline := time.Now().Add(settle)
	for {
		err := s.verifyWorkflowOutcome(ctx, pageID, outcome)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}

func (s *Session) verifyWorkflowOutcome(ctx context.Context, pageID string, outcome *WorkflowOutcome) error {
	if outcome == nil {
		return nil
	}
	pageURL, nodes, _, taskErr := s.workflowView(ctx, pageID)
	if taskErr != nil {
		return taskErr
	}
	switch outcome.Type {
	case "url_contains":
		if !strings.Contains(pageURL, outcome.Value) {
			return fmt.Errorf("url %q lacks %q", pageURL, outcome.Value)
		}
	case "text_present":
		text, taskErr := s.DumpChromiumPage(ctx, pageID, "text")
		if taskErr != nil {
			return taskErr
		}
		if body, ok := text.(string); !ok || !strings.Contains(body, outcome.Value) {
			return fmt.Errorf("text %q not present", outcome.Value)
		}
	case "element_present":
		if _, err := resolveWorkflowTarget(nodes, outcome.Node); err != nil {
			return err
		}
	}
	return nil
}

// verifyWorkflowCredentialTarget applies the login credential rule to the
// workflow field: same host, secure scheme, top-level document and
// same-origin form targets.
func (s *Session) verifyWorkflowCredentialTarget(ctx context.Context, pageID string, field bridgeobserve.Node, domain string) error {
	selector := ""
	switch {
	case field.Attributes["id"] != "":
		selector = "[id=" + strconv.Quote(field.Attributes["id"]) + "]"
	case field.Attributes["name"] != "":
		selector = "[name=" + strconv.Quote(field.Attributes["name"]) + "]"
	default:
		return errors.New("credential field needs an id or name to verify its form target")
	}
	s.chromiumMu.Lock()
	page := s.chromiumPages[pageID]
	s.chromiumMu.Unlock()
	if page == nil {
		return fmt.Errorf("unknown page %q", pageID)
	}
	evaluate := func(ctx context.Context, expression string, output any) error {
		var result struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			} `json:"result"`
			ExceptionDetails *struct {
				Text string `json:"text"`
			} `json:"exceptionDetails"`
		}
		if err := page.page.Call(ctx, "Runtime.evaluate", probeEvaluateParams{Expression: expression, ReturnByValue: true}, &result); err != nil {
			return err
		}
		if result.ExceptionDetails != nil {
			return errors.New(result.ExceptionDetails.Text)
		}
		return json.Unmarshal(result.Result.Value, output)
	}
	return profile.VerifyCredentialTarget(ctx, evaluate, domain, selector)
}
