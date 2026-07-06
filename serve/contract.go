// Package serve contract.go — typed protocol envelopes for the Artemis
// steering API. These types are the published, versioned contract that
// external agents (Hermes, OpenClaw, and any third-party client) code
// against. The skeletal Request/Response/Err/Event types in
// protocol.go remain as the wire envelope; this file adds typed
// per-command parameter and result structs plus helpers so clients and
// the server share one canonical schema instead of ad-hoc
// json.RawMessage plumbing.
package serve

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is the wire-protocol version this server implements.
// Clients MUST check that they speak a compatible version. The version
// is bumped on any breaking change to the envelope shape or to a
// command's parameter/result schema.
const ProtocolVersion = "1.0.0"

// Command names every operation the steering server accepts.
type Command string

const (
	CmdSessionNew      Command = "session.new"
	CmdSessionClose    Command = "session.close"
	CmdPageOpen        Command = "page.open"
	CmdPageClose       Command = "page.close"
	CmdPageEval        Command = "page.eval"
	CmdPageDump        Command = "page.dump"
	CmdPageClickByText Command = "page.click_by_text"
	CmdPageType        Command = "page.type"
	CmdPageWaitIdle    Command = "page.wait_idle"
	CmdPageAssert      Command = "page.assert"
)

// ErrCode is the canonical error taxonomy. Clients can branch on
// these codes programmatically; messages are human-readable and may
// change between versions.
type ErrCode string

const (
	ErrBadRequest   ErrCode = "bad_request"
	ErrBadParams    ErrCode = "bad_params"
	ErrBadMode      ErrCode = "bad_mode"
	ErrBadFormat    ErrCode = "bad_format"
	ErrUnknownCmd   ErrCode = "unknown_cmd"
	ErrNoSession    ErrCode = "no_session"
	ErrNoPage       ErrCode = "no_page"
	ErrFetchFailed  ErrCode = "fetch_failed"
	ErrEvalFailed   ErrCode = "eval_failed"
	ErrTypeFailed   ErrCode = "type_failed"
	ErrWaitFailed   ErrCode = "wait_failed"
	ErrClickFailed  ErrCode = "click_failed"
	ErrAssertFailed ErrCode = "assert_failed"
	ErrNotFound     ErrCode = "not_found"
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

// SessionNewParams has no parameters; session.new creates a fresh
// session with server-assigned IDs.
type SessionNewParams struct{}

// SessionCloseParams closes an open session and all its pages.
type SessionCloseParams struct {
	SessionID string `json:"sessionId"`
}

// PageOpenParams opens a URL in a new page within the session.
type PageOpenParams struct {
	SessionID  string `json:"sessionId"`
	URL        string `json:"url"`
	RunScripts bool   `json:"runScripts"`
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

// --- Typed result structs ---

// SessionNewResult is the value returned by session.new.
type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// PageOpenResult is the value returned by page.open.
type PageOpenResult struct {
	PageID string `json:"pageId"`
	URL    string `json:"url"`
	Status int    `json:"status"`
	Title  string `json:"title"`
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
			return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
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
	Protocol string `json:"protocol"`
	Server   string `json:"server"`
}

// CmdVersion is a meta-command that returns the protocol version. It
// is handled inline by the server without touching the engine.
const CmdVersion Command = "version"
