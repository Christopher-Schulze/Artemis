package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

const (
	policyRequestWorkers = 4
	policyRequestQueue   = 128
	policyRequestTimeout = 15 * time.Second
)

type fetchRequestPattern struct {
	URLPattern   string `json:"urlPattern"`
	RequestStage string `json:"requestStage"`
}

type fetchEnableParams struct {
	Patterns           []fetchRequestPattern `json:"patterns"`
	HandleAuthRequests bool                  `json:"handleAuthRequests"`
}

type fetchRequestPaused struct {
	RequestID           string `json:"requestId"`
	ResourceType        string `json:"resourceType"`
	RedirectedRequestID string `json:"redirectedRequestId"`
	Request             struct {
		URL         string         `json:"url"`
		Method      string         `json:"method"`
		Headers     map[string]any `json:"headers"`
		PostData    string         `json:"postData"`
		HasPostData bool           `json:"hasPostData"`
	} `json:"request"`
}

// FetchRequest is the bounded request metadata exposed to an application
// fetch-interception handler after network policy validation.
type FetchRequest struct {
	RequestID    string
	SessionID    string
	URL          string
	Method       string
	Headers      map[string]string
	PostData     string
	HasPostData  bool
	ResourceType string
}

// FetchHeader is one CDP request or response header entry.
type FetchHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// FetchResolutionKind identifies how an application resolves a paused
// request.
type FetchResolutionKind string

const (
	FetchResolutionContinue FetchResolutionKind = "continue"
	FetchResolutionFulfill  FetchResolutionKind = "fulfill"
	FetchResolutionFail     FetchResolutionKind = "fail"
	FetchResolutionTimeout  FetchResolutionKind = "timeout_auto_fail"
)

// FetchResolution is translated to one typed Fetch CDP command. Body is raw
// content and is base64 encoded only at the CDP boundary.
// PostData is a strict base64 request-body override. Nil Headers preserves
// the original headers; a non-nil empty slice explicitly removes them.
type FetchResolution struct {
	Kind            FetchResolutionKind
	URL             string
	Method          string
	Headers         []FetchHeader
	PostData        string
	StatusCode      int
	ResponseHeaders []FetchHeader
	Body            string
	ErrorReason     string
}

// FetchRequestResolver resolves one paused request. Resolution may happen
// after the worker context expires, so callers must supply a live bounded
// context for the CDP command.
// Each supplied resolver admits at most one terminal attempt. Overrides are
// revalidated through the page network policy before they can be dispatched.
type FetchRequestResolver func(context.Context, FetchResolution) error

// FetchRequestHandler receives policy-approved requests. handled=true means
// the handler owns the request and will invoke the resolver later; handled
// false lets Artemis continue it immediately.
type FetchRequestHandler func(context.Context, FetchRequest, FetchRequestResolver) (handled bool, err error)

type fetchRequestJob struct {
	sessionID    string
	payload      fetchRequestPaused
	authRequired bool
}

func (p *Page) enableSessionPolicy(ctx context.Context, sessionID string) error {
	if err := p.owner.browser.transport.CallSession(ctx, sessionID, "Network.enable", nil, nil); err != nil {
		return fmt.Errorf("enable Network domain: %w", err)
	}
	params := fetchEnableParams{Patterns: []fetchRequestPattern{{URLPattern: "*", RequestStage: "Request"}}, HandleAuthRequests: true}
	if err := p.owner.browser.transport.CallSession(ctx, sessionID, "Fetch.enable", params, nil); err != nil {
		return fmt.Errorf("enable Fetch interception: %w", err)
	}
	return nil
}

func (p *Page) handleFetchEvent(event CDPEvent, jobs chan<- fetchRequestJob) bool {
	if (event.Method != "Fetch.requestPaused" && event.Method != "Fetch.authRequired") || !p.ownsPolicySession(event.SessionID) {
		return true
	}
	if event.Method == "Fetch.authRequired" {
		return p.enqueueAuthChallenge(event, jobs)
	}
	var payload fetchRequestPaused
	if err := json.Unmarshal(event.Params, &payload); err != nil {
		p.failNetworkEnforcement(fmt.Errorf("decode Fetch.requestPaused: %w", err))
		return false
	}
	if payload.RequestID == "" {
		p.failNetworkEnforcement(fmt.Errorf("decode Fetch.requestPaused: requestId required"))
		return false
	}
	select {
	case jobs <- fetchRequestJob{sessionID: event.SessionID, payload: payload}:
		return true
	default:
		p.failNetworkEnforcement(fmt.Errorf("Fetch.requestPaused queue exhausted"))
		return false
	}
}

func (p *Page) runPolicyRequestWorker(ctx context.Context, jobs <-chan fetchRequestJob) {
	for {
		var job fetchRequestJob
		select {
		case <-ctx.Done():
			return
		case next, ok := <-jobs:
			if !ok || ctx.Err() != nil {
				return
			}
			job = next
		}
		if !p.ownsPolicySession(job.sessionID) {
			continue
		}
		requestCtx, cancel := context.WithTimeout(ctx, policyRequestTimeout)
		var err error
		if job.authRequired {
			err = p.cancelAuthChallenge(requestCtx, job.sessionID, job.payload.RequestID)
		} else {
			err = p.enforcePausedRequest(requestCtx, job)
		}
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				p.failNetworkEnforcement(err)
			}
			return
		}
	}
}

func (p *Page) enforcePausedRequest(ctx context.Context, job fetchRequestJob) error {
	request := job.payload.Request
	kind := p.policyTargetKind(job.sessionID, job.payload)
	ctx = p.adBlockContext(ctx, kind, job.sessionID, job.payload)
	contentType := fetchHeader(request.Headers, "content-type")
	contentLength := fetchContentLength(request.Headers, request.PostData, request.HasPostData)
	err := p.owner.browser.policy.ValidateRequest(ctx, request.URL, request.Method, contentType, contentLength, kind, job.sessionID)
	if err != nil {
		return p.resolveFetchRequest(ctx, job.sessionID, job.payload.RequestID, FetchResolution{
			Kind: FetchResolutionFail, ErrorReason: "BlockedByClient",
		})
	}
	if handler := p.fetchRequestHandler(); handler != nil {
		resolver, freezeErr := p.newFetchResolver(job)
		if freezeErr != nil {
			return p.resolveFetchRequest(ctx, job.sessionID, job.payload.RequestID, FetchResolution{Kind: FetchResolutionFail, ErrorReason: "BlockedByClient"})
		}
		handled, handlerErr := handler(ctx, resolver.requestCopy(), resolver.resolve)
		if handlerErr != nil {
			if resolver.resolved() && errors.Is(handlerErr, network.ErrPolicyDenied) {
				return nil
			}
			failErr := resolver.resolve(ctx, FetchResolution{
				Kind: FetchResolutionFail, ErrorReason: "Failed",
			})
			if failErr != nil && !errors.Is(failErr, ErrFetchAlreadyResolved) {
				return fmt.Errorf("fetch request handler: %w; fail paused request: %v", handlerErr, failErr)
			}
			return fmt.Errorf("fetch request handler: %w", handlerErr)
		}
		if handled || resolver.resolved() {
			return nil
		}
		return resolver.resolve(ctx, FetchResolution{Kind: FetchResolutionContinue})
	}
	if err := p.resolveFetchRequest(ctx, job.sessionID, job.payload.RequestID, FetchResolution{Kind: FetchResolutionContinue}); err != nil {
		return fmt.Errorf("resolve paused request %s: %w", job.payload.RequestID, err)
	}
	return nil
}

func (p *Page) fetchRequestHandler() FetchRequestHandler {
	p.fetchMu.RLock()
	defer p.fetchMu.RUnlock()
	return p.fetchHandler
}

func fetchRequest(job fetchRequestJob) FetchRequest {
	return FetchRequest{
		RequestID: job.payload.RequestID, SessionID: job.sessionID,
		URL: job.payload.Request.URL, Method: job.payload.Request.Method,
		Headers:  fetchHeaderStrings(job.payload.Request.Headers),
		PostData: job.payload.Request.PostData, HasPostData: job.payload.Request.HasPostData,
		ResourceType: job.payload.ResourceType,
	}
}

func fetchHeaderStrings(headers map[string]any) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	result := make(map[string]string, len(headers))
	for key, value := range headers {
		result[key] = fmt.Sprint(value)
	}
	return result
}

func (p *Page) resolveFetchRequest(ctx context.Context, sessionID, requestID string, resolution FetchResolution) error {
	if ctx == nil {
		return fmt.Errorf("resolve fetch request: context required")
	}
	if requestID == "" {
		return fmt.Errorf("resolve fetch request: request ID required")
	}
	if p == nil || p.owner == nil || p.owner.browser == nil {
		return fmt.Errorf("resolve fetch request: owning page required")
	}
	p.owner.mu.Lock()
	contextClosed := p.owner.closed
	p.owner.mu.Unlock()
	if contextClosed {
		return &TargetError{TargetID: p.targetID, State: TargetStateClosed, Message: "owning context closed"}
	}
	if err := p.ensureAttached(); err != nil {
		return err
	}
	if !p.ownsPolicySession(sessionID) {
		return fmt.Errorf("%w: unowned_fetch_session", network.ErrPolicyDenied)
	}
	method, params, err := fetchResolutionCommand(requestID, resolution)
	if err != nil {
		return err
	}
	if callErr := p.owner.browser.transport.CallSession(ctx, sessionID, method, params, nil); callErr != nil {
		return callErr
	}
	return nil
}

type fetchContinueRequestParams struct {
	RequestID string         `json:"requestId"`
	URL       string         `json:"url,omitempty"`
	Method    string         `json:"method,omitempty"`
	Headers   *[]FetchHeader `json:"headers,omitempty"`
	PostData  string         `json:"postData,omitempty"`
}

type fetchFulfillRequestParams struct {
	RequestID       string        `json:"requestId"`
	ResponseCode    int           `json:"responseCode"`
	ResponsePhrase  string        `json:"responsePhrase,omitempty"`
	ResponseHeaders []FetchHeader `json:"responseHeaders,omitempty"`
	Body            string        `json:"body,omitempty"`
}

type fetchFailRequestParams struct {
	RequestID   string `json:"requestId"`
	ErrorReason string `json:"errorReason"`
}

func fetchResolutionCommand(requestID string, resolution FetchResolution) (string, any, error) {
	switch resolution.Kind {
	case FetchResolutionContinue:
		return "Fetch.continueRequest", fetchContinueRequestParams{
			RequestID: requestID, URL: resolution.URL, Method: resolution.Method,
			Headers: fetchHeaderOverrides(resolution.Headers), PostData: resolution.PostData,
		}, nil
	case FetchResolutionFulfill:
		status := resolution.StatusCode
		if status == 0 {
			status = 200
		}
		if status < 100 || status > 599 {
			return "", nil, fmt.Errorf("resolve fetch request: status code %d outside 100..599", status)
		}
		return "Fetch.fulfillRequest", fetchFulfillRequestParams{
			RequestID: requestID, ResponseCode: status,
			ResponseHeaders: append([]FetchHeader(nil), resolution.ResponseHeaders...),
			Body:            base64.StdEncoding.EncodeToString([]byte(resolution.Body)),
		}, nil
	case FetchResolutionFail, FetchResolutionTimeout:
		reason := resolution.ErrorReason
		if resolution.Kind == FetchResolutionTimeout {
			reason = "TimedOut"
		}
		return "Fetch.failRequest", fetchFailRequestParams{RequestID: requestID, ErrorReason: validFetchErrorReason(reason)}, nil
	default:
		return "", nil, fmt.Errorf("resolve fetch request: unsupported resolution %q", resolution.Kind)
	}
}

func fetchHeaderOverrides(headers []FetchHeader) *[]FetchHeader {
	if headers == nil {
		return nil
	}
	copy := append([]FetchHeader{}, headers...)
	return &copy
}

func validFetchErrorReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "Aborted", "AccessDenied", "BlockedByClient", "BlockedByResponse", "ConnectionAborted", "ConnectionClosed", "ConnectionFailed", "ConnectionRefused", "ConnectionReset", "InternetDisconnected", "NameNotResolved", "TimedOut", "TunnelingFailed", "Failed", "Other":
		return strings.TrimSpace(reason)
	default:
		return "Failed"
	}
}

func (p *Page) policyTargetKind(sessionID string, paused fetchRequestPaused) network.TargetKind {
	if paused.RedirectedRequestID != "" {
		return network.TargetRedirect
	}
	resourceType := strings.ToLower(paused.ResourceType)
	if resourceType == "websocket" {
		return network.TargetWebSocket
	}
	if strings.Contains(resourceType, "worker") {
		return network.TargetWorker
	}
	if sessionID == p.sessionID && resourceType == "document" {
		return network.TargetNavigation
	}
	p.frameMu.RLock()
	targetType := strings.ToLower(p.childSessions[sessionID])
	p.frameMu.RUnlock()
	if strings.Contains(targetType, "worker") {
		return network.TargetWorker
	}
	if targetType == "iframe" && resourceType == "document" {
		return network.TargetSubframe
	}
	return network.TargetSubresource
}

func (p *Page) ownsPolicySession(sessionID string) bool {
	if sessionID == p.sessionID {
		return true
	}
	p.frameMu.RLock()
	defer p.frameMu.RUnlock()
	_, ok := p.childSessions[sessionID]
	return ok
}

func fetchHeader(headers map[string]any, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func fetchContentLength(headers map[string]any, postData string, hasPostData bool) int64 {
	if raw := fetchHeader(headers, "content-length"); raw != "" {
		if length, err := strconv.ParseInt(raw, 10, 64); err == nil && length >= int64(len(postData)) {
			return length
		}
		return -1
	}
	if hasPostData && postData == "" {
		return -1
	}
	if postData != "" {
		return int64(len(postData))
	}
	return 0
}

func (p *Page) failNetworkEnforcement(err error) {
	if err == nil || p.owner == nil || p.owner.browser == nil {
		return
	}
	browser := p.owner.browser
	p.owner.mu.Lock()
	if p.owner.closed {
		p.owner.mu.Unlock()
		return
	}
	browser.mu.Lock()
	p.mu.RLock()
	closing := p.removed || p.state == TargetStateClosed || browser.closed
	p.mu.RUnlock()
	if closing {
		browser.mu.Unlock()
		p.owner.mu.Unlock()
		return
	}
	if browser.terminal == nil && !browser.closed {
		browser.terminal = fmt.Errorf("browser network enforcement failed: %w", err)
	}
	browser.mu.Unlock()
	p.owner.mu.Unlock()
	if closeErr := browser.transport.Close(); closeErr != nil {
		browser.mu.Lock()
		browser.terminal = errors.Join(browser.terminal, fmt.Errorf("close failed network enforcement transport: %w", closeErr))
		browser.mu.Unlock()
	}
}
