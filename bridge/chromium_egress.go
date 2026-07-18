package bridge

import (
	"context"
	"encoding/json"
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
	Patterns []fetchRequestPattern `json:"patterns"`
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

type fetchRequestJob struct {
	sessionID string
	payload   fetchRequestPaused
}

func (p *Page) enableSessionPolicy(ctx context.Context, sessionID string) error {
	if err := p.owner.browser.transport.CallSession(ctx, sessionID, "Network.enable", nil, nil); err != nil {
		return fmt.Errorf("enable Network domain: %w", err)
	}
	params := fetchEnableParams{Patterns: []fetchRequestPattern{{URLPattern: "*", RequestStage: "Request"}}}
	if err := p.owner.browser.transport.CallSession(ctx, sessionID, "Fetch.enable", params, nil); err != nil {
		return fmt.Errorf("enable Fetch interception: %w", err)
	}
	return nil
}

func (p *Page) handleFetchEvent(event CDPEvent, jobs chan<- fetchRequestJob) bool {
	if event.Method != "Fetch.requestPaused" || !p.ownsPolicySession(event.SessionID) {
		return true
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

func (p *Page) runPolicyRequestWorker(jobs <-chan fetchRequestJob) {
	for job := range jobs {
		ctx, cancel := context.WithTimeout(context.Background(), policyRequestTimeout)
		err := p.enforcePausedRequest(ctx, job)
		cancel()
		if err != nil {
			p.failNetworkEnforcement(err)
			return
		}
	}
}

func (p *Page) enforcePausedRequest(ctx context.Context, job fetchRequestJob) error {
	request := job.payload.Request
	kind := p.policyTargetKind(job.sessionID, job.payload)
	contentType := fetchHeader(request.Headers, "content-type")
	contentLength := fetchContentLength(request.Headers, request.PostData, request.HasPostData)
	err := p.owner.browser.policy.ValidateRequest(ctx, request.URL, request.Method, contentType, contentLength, kind, job.sessionID)
	method := "Fetch.continueRequest"
	params := map[string]string{"requestId": job.payload.RequestID}
	if err != nil {
		method = "Fetch.failRequest"
		params["errorReason"] = "BlockedByClient"
	}
	if callErr := p.owner.browser.transport.CallSession(ctx, job.sessionID, method, params, nil); callErr != nil {
		return fmt.Errorf("resolve paused request %s: %w", job.payload.RequestID, callErr)
	}
	return nil
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
		if length, err := strconv.ParseInt(raw, 10, 64); err == nil && length >= 0 {
			return length
		}
	}
	if hasPostData || postData != "" {
		return int64(len(postData))
	}
	return 0
}

func (p *Page) failNetworkEnforcement(err error) {
	if err == nil || p.owner == nil || p.owner.browser == nil {
		return
	}
	browser := p.owner.browser
	browser.mu.Lock()
	if browser.terminal == nil && !browser.closed {
		browser.terminal = fmt.Errorf("browser network enforcement failed: %w", err)
	}
	browser.mu.Unlock()
	_ = browser.transport.Close()
}
