// Typed command parameters and results for the versioned steering protocol.
package serve

import (
	"encoding/json"
	"fmt"

	"github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/observe"
	"github.com/Christopher-Schulze/Artemis/scraper"
	"github.com/Christopher-Schulze/Artemis/security"
)

// ProtocolVersion is the wire-protocol version this server implements.
// Clients MUST check that they speak a compatible version. The version
// is bumped on any breaking change to the envelope shape or to a
// command's parameter/result schema.
const ProtocolVersion = "2.4.0"

// Command names every operation the steering server accepts.
type Command string

const (
	CmdSessionNew           Command = "session.new"
	CmdSessionClose         Command = "session.close"
	CmdSessionList          Command = "session.list"
	CmdPageOpen             Command = "page.open"
	CmdPageClose            Command = "page.close"
	CmdPageEval             Command = "page.eval"
	CmdPageDump             Command = "page.dump"
	CmdPageClickByText      Command = "page.click_by_text"
	CmdPageType             Command = "page.type"
	CmdPageWaitIdle         Command = "page.wait_idle"
	CmdPageAssert           Command = "page.assert"
	CmdChromiumAct          Command = "chromium.act"
	CmdChromiumOpen         Command = "chromium.open"
	CmdChromiumObserve      Command = "chromium.observe"
	CmdChromiumQuery        Command = "chromium.query"
	CmdChromiumClose        Command = "chromium.close"
	CmdStream               Command = "stream"
	CmdCancel               Command = "cancel"
	CmdTokenRotate          Command = "token.rotate"
	CmdHeartbeat            Command = "heartbeat"
	CmdCapabilities         Command = "capabilities"
	CmdVersion              Command = "version"
	CmdAdBlockWhitelist     Command = "adblock.whitelist"
	CmdWorkflowRecord       Command = "workflow.record"
	CmdWorkflowRun          Command = "workflow.run"
	CmdPageLease            Command = "page.lease"
	CmdPageHandoff          Command = "page.handoff"
	CmdChromiumVisual       Command = "chromium.visual"
	CmdChromiumVisualAct    Command = "chromium.visual_act"
	CmdChromiumVisualGround Command = "chromium.visual_ground"
	CmdPageExtract          Command = "page.extract"
)

// Validate rejects commands outside this versioned protocol before dispatch.
func (command Command) Validate() error {
	switch command {
	case CmdSessionNew, CmdSessionClose, CmdSessionList, CmdPageOpen, CmdPageClose,
		CmdPageEval, CmdPageDump, CmdPageClickByText, CmdPageType, CmdPageWaitIdle,
		CmdPageAssert, CmdChromiumAct, CmdChromiumOpen, CmdChromiumObserve,
		CmdChromiumQuery, CmdChromiumClose, CmdStream, CmdCancel, CmdTokenRotate,
		CmdHeartbeat, CmdCapabilities, CmdVersion, CmdAdBlockWhitelist, CmdWorkflowRecord, CmdWorkflowRun,
		CmdPageLease, CmdPageHandoff, CmdChromiumVisual, CmdChromiumVisualAct, CmdChromiumVisualGround, CmdPageExtract:
		return nil
	default:
		return fmt.Errorf("unknown steering command %q", command)
	}
}

// ErrCode is the canonical error taxonomy. Clients can branch on
// these codes programmatically; messages are human-readable and may
// change between versions.
type ErrCode string

const (
	ErrBadRequest            ErrCode = "bad_request"
	ErrBadParams             ErrCode = "bad_params"
	ErrBadMode               ErrCode = "bad_mode"
	ErrBadFormat             ErrCode = "bad_format"
	ErrUnknownCmd            ErrCode = "unknown_cmd"
	ErrNoSession             ErrCode = "no_session"
	ErrNoPage                ErrCode = "no_page"
	ErrStaleReference        ErrCode = "stale_reference"
	ErrFetchFailed           ErrCode = "fetch_failed"
	ErrEvalFailed            ErrCode = "eval_failed"
	ErrTypeFailed            ErrCode = "type_failed"
	ErrWaitFailed            ErrCode = "wait_failed"
	ErrClickFailed           ErrCode = "click_failed"
	ErrAssertFailed          ErrCode = "assert_failed"
	ErrNotFound              ErrCode = "not_found"
	ErrCapabilityUnavailable ErrCode = "capability_unavailable"
	ErrVersionMismatch       ErrCode = "version_mismatch"
	ErrRateExceeded          ErrCode = "rate_exceeded"
	ErrOwnershipDenied       ErrCode = "ownership_denied"
	ErrCancelled             ErrCode = "cancelled"
	ErrSessionLimit          ErrCode = "session_limit"
	ErrResourceLimit         ErrCode = "resource_limit"
	ErrExecutionFailed       ErrCode = "execution_failed"
	ErrContentBlocked        ErrCode = "content_blocked"
)

// DumpFormat enumerates the page.dump formats the server accepts.
type DumpFormat string

const (
	DumpHTML       DumpFormat = "html"
	DumpMarkdown   DumpFormat = "markdown"
	DumpText       DumpFormat = "text"
	DumpTitle      DumpFormat = "title"
	DumpLinks      DumpFormat = "links"
	DumpStructured DumpFormat = "structured"
	DumpSemantic   DumpFormat = "semantic"
)

// AssertMode enumerates the page.assert modes.
type AssertMode string

const (
	AssertSelectorExists AssertMode = "selector_exists"
	AssertTitleContains  AssertMode = "title_contains"
	AssertTextContains   AssertMode = "text_contains"
	AssertURLContains    AssertMode = "url_contains"
	AssertStatusEq       AssertMode = "status_eq"
	AssertEvalTruthy     AssertMode = "eval_truthy"
)

// --- Typed parameter structs ---

// SessionNewParams creates a fresh session with server-assigned identity.
// OwnerUserRef is retained for wire compatibility and, when supplied, must
// equal the authenticated client's server-issued owner reference.
type SessionNewParams struct {
	ProfileID    string `json:"profileId"`
	OwnerUserRef string `json:"ownerUserRef"`
	Class        string `json:"class"`
}

// SessionCloseParams closes an owned session and all its pages. OwnerUserRef is
// retained for wire compatibility; ownership is derived from the authenticated
// client capability rather than trusted from this field.
type SessionCloseParams struct {
	SessionID    string `json:"sessionId"`
	OwnerUserRef string `json:"ownerUserRef"`
}

// PageOpenParams opens a URL in a new page within the session.
type PageOpenParams struct {
	SessionID  string `json:"sessionId"`
	URL        string `json:"url"`
	RunScripts bool   `json:"runScripts"`
	AsyncFetch bool   `json:"asyncFetch,omitempty"`
}

// PageCloseParams closes a single page.
type PageCloseParams struct {
	SessionID string `json:"sessionId"`
	PageID    string `json:"pageId"`
}

// PageEvalParams evaluates a JavaScript expression in the page's JS
// context and returns the result as a string.
type PageEvalParams struct {
	SessionID string `json:"sessionId"`
	PageID    string `json:"pageId"`
	Expr      string `json:"expr"`
}

// PageDumpParams dumps the page in the requested format.
type PageDumpParams struct {
	SessionID string `json:"sessionId"`
	PageID    string `json:"pageId"`
	Format    string `json:"format"`
}

// PageClickByTextParams clicks the first button/anchor/input matching
// the given text (case-insensitive).
type PageClickByTextParams struct {
	SessionID string `json:"sessionId"`
	PageID    string `json:"pageId"`
	Text      string `json:"text"`
}

// PageTypeParams types text into the input/textarea matching the
// selector. This is a value mutation only; input/change events are not
// dispatched. For React-style controlled inputs, dispatch the event
// from JS via page.eval after Type.
type PageTypeParams struct {
	SessionID string `json:"sessionId"`
	PageID    string `json:"pageId"`
	Selector  string `json:"selector"`
	Text      string `json:"text"`
}

// PageWaitIdleParams blocks until in-flight async fetches have settled.
type PageWaitIdleParams struct {
	SessionID string `json:"sessionId"`
	PageID    string `json:"pageId"`
}

// PageAssertParams evaluates an assertion against the current page
// state. Want defaults to true if nil.
type PageAssertParams struct {
	SessionID string `json:"sessionId"`
	PageID    string `json:"pageId"`
	Mode      string `json:"mode"`
	Selector  string `json:"selector"`
	Want      *bool  `json:"want"`
	Substring string `json:"substring"`
	Status    int    `json:"status"`
	Expr      string `json:"expr"`
}

type ChromiumActParams struct {
	SessionID     string          `json:"sessionId"`
	PageID        string          `json:"pageId"`
	SnapshotEpoch uint64          `json:"snapshotEpoch"`
	Request       actions.Request `json:"request"`
	// LeaseToken authorizes the action on a page under a writer lease.
	LeaseToken string `json:"leaseToken,omitempty"`
}

type ChromiumOpenParams struct {
	SessionID string `json:"sessionId"`
	URL       string `json:"url"`
}

type ChromiumObserveParams struct {
	SessionID  string `json:"sessionId"`
	PageID     string `json:"pageId"`
	Mode       string `json:"mode,omitempty"`
	SubtreeRef string `json:"subtreeRef,omitempty"`
}

type ChromiumQueryParams struct {
	SessionID string                     `json:"sessionId"`
	PageID    string                     `json:"pageId"`
	Request   bridgeobserve.QueryRequest `json:"request"`
}

// WorkflowRecordParams starts or stops recording on a session Chromium page.
type WorkflowRecordParams struct {
	SessionID string `json:"sessionId"`
	PageID    string `json:"pageId"`
	Op        string `json:"op"` // start | stop
	Name      string `json:"name,omitempty"`
	// Origins, on stop, converts the recording into a reusable definition.
	Origins []string `json:"origins,omitempty"`
}

// WorkflowRecordResult carries the finished recording and its definition.
type WorkflowRecordResult struct {
	Recording  *observe.WorkflowRecording  `json:"recording,omitempty"`
	Definition *artemis.WorkflowDefinition `json:"definition,omitempty"`
}

// WorkflowRunParams runs a definition on a session Chromium page. Sensitive
// inputs need a host credential store and are refused over the wire.
type WorkflowRunParams struct {
	SessionID  string                           `json:"sessionId"`
	PageID     string                           `json:"pageId"`
	Definition artemis.WorkflowDefinition       `json:"definition"`
	Inputs     map[string]artemis.WorkflowInput `json:"inputs,omitempty"`
	Shadow     bool                             `json:"shadow,omitempty"`
	// RunID journals the run durably under Opts.WorkflowJournalDir; a
	// repeated RunID resumes it without repeating verified effects.
	RunID string `json:"runId,omitempty"`
}

// PageExtractParams runs source-grounded extraction: Schema on the page
// document (following Pagination when set) or JSON on a same-origin JSON
// resource of a Chromium page. Exactly one of Schema and JSON is required.
type PageExtractParams struct {
	SessionID  string                       `json:"sessionId"`
	PageID     string                       `json:"pageId"`
	Schema     *scraper.StructuredSchema    `json:"schema,omitempty"`
	Pagination *artemis.ExtractPagination   `json:"pagination,omitempty"`
	JSON       *artemis.JSONResponseRequest `json:"json,omitempty"`
}

// ChromiumVisualParams captures a bound visual observation of a page.
type ChromiumVisualParams struct {
	SessionID string                       `json:"sessionId"`
	PageID    string                       `json:"pageId"`
	Options   artemis.VisualCaptureOptions `json:"options"`
}

// ChromiumVisualActParams acts at a point of the page's current visual
// observation; LeaseToken authorizes the action on a leased page.
type ChromiumVisualActParams struct {
	SessionID  string                      `json:"sessionId"`
	PageID     string                      `json:"pageId"`
	Request    artemis.VisualActionRequest `json:"request"`
	LeaseToken string                      `json:"leaseToken,omitempty"`
}

// ChromiumVisualGroundParams asks the host grounding model for a point.
type ChromiumVisualGroundParams struct {
	SessionID     string `json:"sessionId"`
	PageID        string `json:"pageId"`
	ObservationID string `json:"observationId"`
	Instruction   string `json:"instruction"`
}

// PageLeaseParams acquires or releases ("acquire" | "release") the exclusive
// writer lease of a session Chromium page.
type PageLeaseParams struct {
	SessionID string `json:"sessionId"`
	PageID    string `json:"pageId"`
	Op        string `json:"op"`
	Holder    string `json:"holder,omitempty"`
	TTLMillis int64  `json:"ttlMs,omitempty"`
	Token     string `json:"token,omitempty"`
}

// PageHandoffParams hands a page to a human ("begin"), reveals its inspector
// once ("open", Token = open token) or returns it ("end", Token = lease token).
type PageHandoffParams struct {
	SessionID string `json:"sessionId"`
	PageID    string `json:"pageId"`
	Op        string `json:"op"`
	TTLMillis int64  `json:"ttlMs,omitempty"`
	Token     string `json:"token,omitempty"`
}

// PageHandoffResult carries the begin ticket or the opened inspector URL.
type PageHandoffResult struct {
	Ticket       *artemis.HandoffTicket `json:"ticket,omitempty"`
	InspectorURL string                 `json:"inspectorUrl,omitempty"`
}

// AdBlockWhitelistParams lists ("list"), adds or removes whitelisted sites.
type AdBlockWhitelistParams struct {
	Op     string `json:"op"`
	Domain string `json:"domain,omitempty"`
}

// AdBlockWhitelistResult is the whitelist after the operation.
type AdBlockWhitelistResult struct {
	Domains []string `json:"domains"`
}

type ChromiumActResult struct {
	Outcome actions.Outcome `json:"outcome"`
}

// --- Typed result structs ---

// SessionNewResult is the value returned by session.new.
type SessionNewResult struct {
	SessionID    string `json:"sessionId"`
	OwnerUserRef string `json:"ownerUserRef"`
}

// PageOpenResult is the value returned by page.open.
type PageOpenResult struct {
	PageID string `json:"pageId"`
	URL    string `json:"url"`
	Status int    `json:"status"`
	Title  string `json:"title"`
	// AdBlock is the Layer 2 adblocker-wall finding (spec L4208), if any.
	AdBlock *security.AdBlockFinding `json:"adblock,omitempty"`
}

// PageEvalResult is the value returned by page.eval.
type PageEvalResult struct {
	Value string `json:"value"`
}

// PageDumpResult is the value returned by page.dump. Data is the
// serialized page content in the requested format.
type PageDumpResult struct {
	Data any `json:"data"`
}

// AssertResult is the value returned by page.assert.
type AssertResult struct {
	Pass bool   `json:"pass"`
	Got  string `json:"got"`
}

// EmptyResult is the value returned by commands that have no payload
// (page.close, session.close, page.type, page.wait_idle,
// page.click_by_text on success).
type EmptyResult struct{}

// --- Typed helpers ---

// TypedRequest is a convenience wrapper that pairs a command with its
// typed parameter struct. Clients can use MarshalTyped to build the
// wire Request without manually constructing json.RawMessage.
type TypedRequest struct {
	ID     string      `json:"id"`
	Cmd    Command     `json:"cmd"`
	Params interface{} `json:"params,omitempty"`
}

// MarshalTyped serializes a TypedRequest into the wire Request format.
func MarshalTyped(id string, cmd Command, params interface{}) (Request, error) {
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return Request{}, fmt.Errorf("marshal params for %s: %w", cmd, err)
		}
		raw = data
	}
	return Request{ID: id, Cmd: string(cmd), Params: raw}, nil
}

// DecodeParams unmarshals a Request's params into the target struct.
func DecodeParams(req *Request, target interface{}) error {
	if len(req.Params) == 0 {
		return nil
	}
	if err := json.Unmarshal(req.Params, target); err != nil {
		return fmt.Errorf("decode params for %s: %w", req.Cmd, err)
	}
	return nil
}

// DecodeTypedResult unmarshals a Response's value into the target
// struct. Returns an error if the response is not OK.
func DecodeTypedResult(resp *Response, target interface{}) error {
	if !resp.OK {
		if resp.Error != nil {
			return resp.Error
		}
		return fmt.Errorf("response not OK (no error detail)")
	}
	if target == nil || resp.Value == nil {
		return nil
	}
	data, err := json.Marshal(resp.Value)
	if err != nil {
		return fmt.Errorf("re-marshal value: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode value: %w", err)
	}
	return nil
}

// VersionResponse is returned by the version handshake (a client
// sends cmd="version" and receives this).
type VersionResponse struct {
	Protocol     string               `json:"protocol"`
	Server       string               `json:"server"`
	InstanceID   string               `json:"instanceId"`
	ProcessID    int                  `json:"processId"`
	Capabilities []artemis.Capability `json:"capabilities"`
	Health       artemis.Health       `json:"health"`
}

// SessionInfo describes a single session for session.list.
type SessionInfo struct {
	SessionID    string `json:"sessionId"`
	OwnerUserRef string `json:"ownerUserRef"`
	Active       bool   `json:"active"`
	TabCount     int    `json:"tabCount"`
}

// SessionListResult is the value returned by session.list.
type SessionListResult struct {
	Sessions []SessionInfo `json:"sessions"`
}

// StreamParams starts or resumes a streaming page.open.
type StreamParams struct {
	SessionID  string `json:"sessionId"`
	URL        string `json:"url"`
	RunScripts bool   `json:"runScripts"`
	StreamID   string `json:"streamId,omitempty"`
	ResumeFrom int64  `json:"resumeFrom,omitempty"`
}

// StreamResult is the value returned by stream.
type StreamResult struct {
	StreamID string `json:"streamId"`
	Resumed  bool   `json:"resumed,omitempty"`
	Complete bool   `json:"complete,omitempty"`
}

// StreamProgress is a server-pushed stream event payload.
type StreamProgress struct {
	StreamID string `json:"streamId"`
	Stage    string `json:"stage"`
}

// CancelParams cancels an in-flight request.
type CancelParams struct {
	RequestID string `json:"requestId"`
}

// CancelResult is the value returned by cancel.
type CancelResult struct {
	Cancelled bool `json:"cancelled"`
}

// TokenRotateResult is the value returned by token.rotate.
type TokenRotateResult struct {
	Token string `json:"token"`
}

// HeartbeatResult is the value returned by heartbeat.
type HeartbeatResult struct {
	Now int64 `json:"now"`
}
