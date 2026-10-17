package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

const (
	traceEventBuffer      = 1024
	traceDebugEntryLimit  = 1024
	traceSourceLimit      = 64
	traceSourceBytesLimit = 512 * 1024
	traceTextLimit        = 4096
)

// TraceDebugEvidence is the bounded debug projection captured from the same
// target as the trace artifacts.
type TraceDebugEvidence struct {
	Target           TargetIdentity      `json:"target"`
	Console          []TraceConsoleEntry `json:"console,omitempty"`
	PageErrors       []TracePageError    `json:"page_errors,omitempty"`
	Network          []TraceNetworkEntry `json:"network,omitempty"`
	Truncated        bool                `json:"truncated,omitempty"`
	SourcesTruncated bool                `json:"sources_truncated,omitempty"`
}

// TraceConsoleEntry is one target-bound Runtime.consoleAPICalled event.
type TraceConsoleEntry struct {
	Type      string    `json:"type"`
	Text      string    `json:"text"`
	URL       string    `json:"url,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TracePageError is one target-bound Runtime.exceptionThrown event.
type TracePageError struct {
	Message   string    `json:"message"`
	Name      string    `json:"name,omitempty"`
	Stack     string    `json:"stack,omitempty"`
	URL       string    `json:"url,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TraceNetworkEntry is a target-bound request lifecycle projection.
type TraceNetworkEntry struct {
	RequestID    string    `json:"request_id"`
	FrameID      string    `json:"frame_id,omitempty"`
	LoaderID     string    `json:"loader_id,omitempty"`
	Method       string    `json:"method"`
	URL          string    `json:"url"`
	ResourceType string    `json:"resource_type,omitempty"`
	Status       int       `json:"status,omitempty"`
	OK           bool      `json:"ok"`
	FailureText  string    `json:"failure_text,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// traceMetadata is the stable archive manifest written beside artifacts.
type traceMetadata struct {
	StartedAt   time.Time         `json:"started_at"`
	StoppedAt   time.Time         `json:"stopped_at"`
	Target      *TargetIdentity   `json:"target,omitempty"`
	Screenshots int               `json:"screenshots"`
	Snapshots   int               `json:"snapshots"`
	Sources     int               `json:"sources"`
	Debug       traceDebugSummary `json:"debug"`
}

type traceDebugSummary struct {
	Console          int  `json:"console"`
	PageErrors       int  `json:"page_errors"`
	Network          int  `json:"network"`
	Truncated        bool `json:"truncated"`
	SourcesTruncated bool `json:"sources_truncated"`
}

func newTraceDebugSummary(evidence TraceDebugEvidence) traceDebugSummary {
	return traceDebugSummary{
		Console: len(evidence.Console), PageErrors: len(evidence.PageErrors),
		Network: len(evidence.Network), Truncated: evidence.Truncated,
		SourcesTruncated: evidence.SourcesTruncated,
	}
}

func cloneTraceDebugEvidence(evidence TraceDebugEvidence) TraceDebugEvidence {
	evidence.Console = append([]TraceConsoleEntry(nil), evidence.Console...)
	evidence.PageErrors = append([]TracePageError(nil), evidence.PageErrors...)
	evidence.Network = append([]TraceNetworkEntry(nil), evidence.Network...)
	return evidence
}

// safeTraceName creates a deterministic archive component from a resource URL.
func safeTraceName(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil {
		raw = parsed.Host + parsed.Path
	}
	name := strings.Trim(strings.NewReplacer("/", "_", "\\", "_", ":", "_", "?", "_", "#", "_").Replace(raw), "_.")
	if name == "" {
		return "resource"
	}
	if len(name) > 96 {
		name = name[len(name)-96:]
	}
	return name
}

// TracePage is the minimal real CDP page contract needed by BrowserTraceRecorder.
// bridge.Page satisfies it without exposing bridge internals to the recorder.
type TracePage interface {
	TargetID() string
	SessionID() string
	BrowserContextID() string
	Call(context.Context, string, any, any) error
	SubscribeBrowserEvents(int) (*bridge.CDPSubscription, error)
}

// BrowserTraceRecorder binds Playwright-style trace artifacts and debug events
// to one concrete Chromium target/context.
type BrowserTraceRecorder struct {
	mu           sync.Mutex
	page         TracePage
	identity     TargetIdentity
	config       TraceRecordConfig
	recorder     *TraceRecorder
	subscription *bridge.CDPSubscription
	collector    *traceEventCollector
	workers      sync.WaitGroup
	active       bool
}

// NewBrowserTraceRecorder creates an idle recorder bound to page identity.
func NewBrowserTraceRecorder(page TracePage, config TraceRecordConfig) (*BrowserTraceRecorder, error) {
	if page == nil {
		return nil, fmt.Errorf("browser trace page is required")
	}
	identity := TargetIdentity{TargetID: page.TargetID(), SessionID: page.SessionID(), BrowserContextID: page.BrowserContextID()}
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	recorder, err := NewTraceRecorderWithTarget(config, identity)
	if err != nil {
		return nil, err
	}
	return &BrowserTraceRecorder{page: page, identity: identity, config: config, recorder: recorder}, nil
}

// Start enables the target domains and begins bounded event capture.
func (r *BrowserTraceRecorder) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("browser trace start: context is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active {
		return fmt.Errorf("trace already running; stop the current trace before starting")
	}
	for _, method := range []string{"Page.enable", "Runtime.enable", "Network.enable", "Log.enable"} {
		if err := r.page.Call(ctx, method, nil, nil); err != nil {
			return fmt.Errorf("browser trace enable %s: %w", method, err)
		}
	}
	subscription, err := r.page.SubscribeBrowserEvents(traceEventBuffer)
	if err != nil {
		return fmt.Errorf("browser trace subscribe: %w", err)
	}
	if subscription == nil {
		return fmt.Errorf("browser trace subscribe: nil subscription")
	}
	if err := r.recorder.Start(); err != nil {
		subscription.Close()
		return err
	}
	r.subscription = subscription
	r.collector = newTraceEventCollector(r.identity)
	r.active = true
	r.workers.Add(1)
	go r.consumeEvents(subscription, r.collector)
	return nil
}

// Stop captures real page artifacts, drains debug events and atomically writes
// the archive. It returns the archive path even when one optional capture fails.
// DiscardArchive retains the same drain/join/error contract without persistence.
func (r *BrowserTraceRecorder) Stop(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("browser trace stop: context is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return "", fmt.Errorf("no active trace; start a trace before stopping")
	}
	var captureErr error
	if r.config.Screenshots {
		data, err := captureTraceScreenshot(ctx, r.page)
		if err != nil {
			captureErr = errors.Join(captureErr, err)
		} else {
			captureErr = errors.Join(captureErr, r.recorder.AddScreenshot(data))
		}
	}
	if r.config.Snapshots {
		data, err := captureTraceSnapshot(ctx, r.page)
		if err != nil {
			captureErr = errors.Join(captureErr, err)
		} else {
			captureErr = errors.Join(captureErr, r.recorder.AddSnapshot(data))
		}
	}
	if r.config.Sources {
		sources, truncated, err := captureTraceSources(ctx, r.page)
		if err != nil {
			captureErr = errors.Join(captureErr, err)
		}
		for _, source := range sources {
			captureErr = errors.Join(captureErr, r.recorder.AddNamedSource(source))
		}
		if r.collector != nil {
			r.collector.setSourcesTruncated(truncated)
		}
	}
	if r.subscription != nil {
		r.subscription.Close()
	}
	r.workers.Wait()

	evidence := TraceDebugEvidence{Target: r.identity}
	if r.collector != nil {
		evidence = r.collector.snapshot()
		captureErr = errors.Join(captureErr, r.collector.err())
	}
	if err := r.recorder.SetDebugEvidence(evidence); err != nil {
		captureErr = errors.Join(captureErr, err)
	}
	path, stopErr := r.recorder.Stop()
	r.active = false
	r.subscription = nil
	r.collector = nil
	return path, errors.Join(captureErr, stopErr)
}

// IsActive reports whether a target-bound trace is currently recording.
func (r *BrowserTraceRecorder) IsActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// DocumentResponse returns only an observed HTTP response belonging to the
// exact navigation frame/loader in this recorder's owned target session.
// A pending or omitted response returns found=false, never a guessed status.
func (r *BrowserTraceRecorder) DocumentResponse(frameID, loaderID string) (entry TraceNetworkEntry, found bool, err error) {
	if r == nil || frameID == "" || loaderID == "" {
		return entry, false, fmt.Errorf("trace response requires recorder, frame and loader identity")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active || r.collector == nil {
		return entry, false, fmt.Errorf("trace response requires an active recorder")
	}
	c := r.collector
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errValue != nil {
		return entry, false, c.errValue
	}
	for index := len(c.network) - 1; index >= 0; index-- {
		observed := c.network[index]
		if current, ok := c.networkByID[observed.RequestID]; !ok || current != index {
			continue
		}
		if observed.FrameID != frameID || observed.LoaderID != loaderID || observed.ResourceType != "Document" {
			continue
		}
		if observed.FailureText != "" {
			return entry, false, fmt.Errorf("trace document request failed: %s", observed.FailureText)
		}
		if observed.Status >= 100 && observed.Status <= 599 {
			if observed.URL == "" {
				return entry, false, fmt.Errorf("trace document response has no retained URL evidence")
			}
			return observed, true, nil
		}
		return entry, false, nil
	}
	if c.truncated {
		return entry, false, fmt.Errorf("trace document response unavailable: event evidence limit reached")
	}
	return entry, false, nil
}

func (r *BrowserTraceRecorder) consumeEvents(sub *bridge.CDPSubscription, collector *traceEventCollector) {
	defer r.workers.Done()
	events := sub.Events
	errorsChannel := sub.Errors
	for events != nil || errorsChannel != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if event.SessionID == collector.identity.SessionID {
				collector.record(event)
			}
		case eventErr, ok := <-errorsChannel:
			if !ok {
				errorsChannel = nil
				continue
			}
			if eventErr != nil {
				collector.setErr(eventErr)
			}
		}
	}
}

type traceEventCollector struct {
	mu               sync.Mutex
	identity         TargetIdentity
	console          []TraceConsoleEntry
	pageErrors       []TracePageError
	network          []TraceNetworkEntry
	networkByID      map[string]int
	truncated        bool
	errValue         error
	sourcesTruncated bool
	debugBytes       int
}

func (c *traceEventCollector) reserveDebugStringsLocked(values ...string) bool {
	bytes := 0
	for _, value := range values {
		if len(value) > traceTextLimit || len(value) > traceDebugBytesLimit-c.debugBytes-bytes {
			c.truncated = true
			return false
		}
		bytes += len(value)
	}
	c.debugBytes += bytes
	return true
}

func newTraceEventCollector(identity TargetIdentity) *traceEventCollector {
	return &traceEventCollector{identity: identity, networkByID: make(map[string]int)}
}

func (c *traceEventCollector) record(event bridge.CDPEvent) {
	switch event.Method {
	case "Runtime.consoleAPICalled":
		c.recordConsole(event.Params)
	case "Runtime.exceptionThrown":
		c.recordPageError(event.Params)
	case "Network.requestWillBeSent":
		c.recordRequest(event.Params)
	case "Network.responseReceived":
		c.recordResponse(event.Params)
	case "Network.loadingFinished":
		c.recordFinished(event.Params)
	case "Network.loadingFailed":
		c.recordFailed(event.Params)
	}
}

func (c *traceEventCollector) recordConsole(raw json.RawMessage) {
	var payload struct {
		Type      string  `json:"type"`
		Timestamp float64 `json:"timestamp"`
		Args      []struct {
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
		} `json:"args"`
		StackTrace struct {
			CallFrames []struct {
				URL string `json:"url"`
			} `json:"callFrames"`
		} `json:"stackTrace"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	parts := make([]string, 0, len(payload.Args))
	for _, arg := range payload.Args {
		if value := traceRawText(arg.Value, arg.Description); value != "" {
			parts = append(parts, value)
		}
	}
	text := strings.Join(parts, " ")
	entry := TraceConsoleEntry{Type: payload.Type, Text: truncateTraceText(text), Timestamp: traceTimestamp(payload.Timestamp)}
	if len(payload.StackTrace.CallFrames) > 0 {
		entry.URL = sanitizeTraceURL(payload.StackTrace.CallFrames[0].URL)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(strings.TrimSpace(text)) > traceTextLimit {
		c.truncated = true
	}
	if len(payload.StackTrace.CallFrames) > 0 && payload.StackTrace.CallFrames[0].URL != "" && entry.URL == "" {
		c.truncated = true
	}
	if len(c.console) >= traceDebugEntryLimit {
		c.truncated = true
		return
	}
	if !c.reserveDebugStringsLocked(entry.Type, entry.Text, entry.URL) {
		return
	}
	c.console = append(c.console, entry)
}

func (c *traceEventCollector) recordPageError(raw json.RawMessage) {
	var payload struct {
		Timestamp        float64 `json:"timestamp"`
		ExceptionDetails struct {
			Text      string `json:"text"`
			URL       string `json:"url"`
			Exception struct {
				Description string          `json:"description"`
				Name        string          `json:"name"`
				Value       json.RawMessage `json:"value"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	message := payload.ExceptionDetails.Text
	if message == "" {
		message = payload.ExceptionDetails.Exception.Description
	}
	if message == "" {
		message = traceRawText(payload.ExceptionDetails.Exception.Value, "")
	}
	entry := TracePageError{
		Message: truncateTraceText(message), Name: truncateTraceText(payload.ExceptionDetails.Exception.Name),
		Stack: truncateTraceText(payload.ExceptionDetails.Exception.Description),
		URL:   sanitizeTraceURL(payload.ExceptionDetails.URL), Timestamp: traceTimestamp(payload.Timestamp),
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(strings.TrimSpace(message)) > traceTextLimit || len(strings.TrimSpace(payload.ExceptionDetails.Exception.Name)) > traceTextLimit || len(strings.TrimSpace(payload.ExceptionDetails.Exception.Description)) > traceTextLimit {
		c.truncated = true
	}
	if payload.ExceptionDetails.URL != "" && entry.URL == "" {
		c.truncated = true
	}
	if len(c.pageErrors) >= traceDebugEntryLimit {
		c.truncated = true
		return
	}
	if !c.reserveDebugStringsLocked(entry.Message, entry.Name, entry.Stack, entry.URL) {
		return
	}
	c.pageErrors = append(c.pageErrors, entry)
}

func (c *traceEventCollector) recordRequest(raw json.RawMessage) {
	var payload struct {
		RequestID string  `json:"requestId"`
		FrameID   string  `json:"frameId"`
		LoaderID  string  `json:"loaderId"`
		Timestamp float64 `json:"timestamp"`
		Type      string  `json:"type"`
		Request   struct {
			URL    string `json:"url"`
			Method string `json:"method"`
		} `json:"request"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.RequestID == "" {
		return
	}
	entry := TraceNetworkEntry{RequestID: payload.RequestID, FrameID: payload.FrameID, LoaderID: payload.LoaderID, Method: payload.Request.Method, URL: sanitizeTraceURL(payload.Request.URL), ResourceType: payload.Type, Timestamp: time.Now().UTC()}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.networkByID, payload.RequestID)
	if len(c.network) >= traceDebugEntryLimit {
		c.truncated = true
		return
	}
	if (entry.URL == "" && payload.Request.URL != "") || !c.reserveDebugStringsLocked(entry.RequestID, entry.FrameID, entry.LoaderID, entry.Method, entry.URL, entry.ResourceType) {
		c.truncated = true
		return
	}
	c.networkByID[payload.RequestID] = len(c.network)
	c.network = append(c.network, entry)
}

func (c *traceEventCollector) recordResponse(raw json.RawMessage) {
	var payload struct {
		RequestID string  `json:"requestId"`
		FrameID   string  `json:"frameId"`
		LoaderID  string  `json:"loaderId"`
		Type      string  `json:"type"`
		Timestamp float64 `json:"timestamp"`
		Response  struct {
			URL    string `json:"url"`
			Status int    `json:"status"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	index, ok := c.networkByID[payload.RequestID]
	if !ok || index >= len(c.network) {
		return
	}
	entry := &c.network[index]
	if (payload.FrameID != "" && entry.FrameID != "" && payload.FrameID != entry.FrameID) || (payload.LoaderID != "" && entry.LoaderID != "" && payload.LoaderID != entry.LoaderID) || (payload.Type != "" && entry.ResourceType != "" && payload.Type != entry.ResourceType) {
		if c.errValue == nil {
			c.errValue = fmt.Errorf("trace response navigation identity changed")
		}
		return
	}
	c.network[index].Status = payload.Response.Status
	c.network[index].OK = payload.Response.Status >= 200 && payload.Response.Status < 400
	url := sanitizeTraceURL(payload.Response.URL)
	c.debugBytes -= len(entry.URL)
	entry.URL = ""
	if !c.reserveDebugStringsLocked(url) {
		return
	}
	entry.URL = url
	if payload.Response.URL != "" && url == "" {
		c.truncated = true
	}
}

func (c *traceEventCollector) recordFinished(raw json.RawMessage) {
	var payload struct {
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if index, ok := c.networkByID[payload.RequestID]; ok && index < len(c.network) && c.network[index].Status == 0 {
		c.network[index].OK = true
	}
}

func (c *traceEventCollector) recordFailed(raw json.RawMessage) {
	var payload struct {
		RequestID string  `json:"requestId"`
		ErrorText string  `json:"errorText"`
		Timestamp float64 `json:"timestamp"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if index, ok := c.networkByID[payload.RequestID]; ok && index < len(c.network) {
		failure := truncateTraceText(payload.ErrorText)
		if failure == "" {
			failure = "network loading failed"
		}
		if len(strings.TrimSpace(payload.ErrorText)) > traceTextLimit {
			c.truncated = true
		}
		if !c.reserveDebugStringsLocked(failure) {
			if c.errValue == nil {
				c.errValue = fmt.Errorf("trace failed-request evidence exceeds byte limit")
			}
			return
		}
		c.network[index].OK = false
		c.network[index].FailureText = failure
		if c.network[index].Timestamp.IsZero() {
			c.network[index].Timestamp = time.Now().UTC()
		}
	}
}

func (c *traceEventCollector) setErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errValue == nil {
		c.errValue = err
	}
}

func (c *traceEventCollector) setSourcesTruncated(truncated bool) {
	c.mu.Lock()
	c.sourcesTruncated = truncated
	c.mu.Unlock()
}

func (c *traceEventCollector) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.errValue
}

func (c *traceEventCollector) snapshot() TraceDebugEvidence {
	c.mu.Lock()
	defer c.mu.Unlock()
	return TraceDebugEvidence{
		Target: c.identity, Console: append([]TraceConsoleEntry(nil), c.console...),
		PageErrors: append([]TracePageError(nil), c.pageErrors...), Network: append([]TraceNetworkEntry(nil), c.network...),
		Truncated: c.truncated, SourcesTruncated: c.sourcesTruncated,
	}
}

func traceRawText(raw json.RawMessage, description string) string {
	if description != "" {
		return description
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return strings.Trim(string(raw), "\"")
}

func truncateTraceText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > traceTextLimit {
		value = value[:traceTextLimit]
	}
	return strings.Clone(value)
}

func traceTimestamp(milliseconds float64) time.Time {
	if milliseconds <= 0 {
		return time.Now().UTC()
	}
	return time.Unix(0, int64(milliseconds*float64(time.Millisecond))).UTC()
}

func sanitizeTraceURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if len(parsed.Scheme)+len(parsed.Host)+len(parsed.Path)+len(parsed.RawPath)+len(parsed.Opaque) > traceTextLimit {
		return ""
	}
	sanitized := parsed.String()
	if len(sanitized) > traceTextLimit {
		return ""
	}
	return sanitized
}

type screenshotParams struct {
	Format string `json:"format"`
}

func captureTraceScreenshot(ctx context.Context, page TracePage) ([]byte, error) {
	var response struct {
		Data string `json:"data"`
	}
	if err := page.Call(ctx, "Page.captureScreenshot", screenshotParams{Format: "png"}, &response); err != nil {
		return nil, fmt.Errorf("capture trace screenshot: %w", err)
	}
	if len(response.Data) > base64.StdEncoding.EncodedLen(TraceEntryBytesLimit) {
		return nil, fmt.Errorf("trace screenshot exceeds byte limit")
	}
	data, err := base64.StdEncoding.DecodeString(response.Data)
	if err != nil {
		return nil, fmt.Errorf("decode trace screenshot: %w", err)
	}
	if len(data) == 0 || len(data) > TraceEntryBytesLimit {
		return nil, fmt.Errorf("capture trace screenshot: empty data or byte limit exceeded")
	}
	return data, nil
}

type evaluateTraceParams struct {
	Expression    string `json:"expression"`
	ReturnByValue bool   `json:"returnByValue"`
}

func captureTraceSnapshot(ctx context.Context, page TracePage) ([]byte, error) {
	var response struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := page.Call(ctx, "Runtime.evaluate", evaluateTraceParams{
		Expression:    "document.documentElement ? document.documentElement.outerHTML : ''",
		ReturnByValue: true,
	}, &response); err != nil {
		return nil, fmt.Errorf("capture trace snapshot: %w", err)
	}
	var html string
	if err := json.Unmarshal(response.Result.Value, &html); err != nil {
		return nil, fmt.Errorf("decode trace snapshot: %w", err)
	}
	if html == "" {
		return nil, fmt.Errorf("capture trace snapshot: empty document")
	}
	if len(html) > TraceEntryBytesLimit {
		return nil, fmt.Errorf("trace snapshot exceeds byte limit")
	}
	return []byte(html), nil
}

type traceResourceFrame struct {
	Frame struct {
		ID string `json:"id"`
	} `json:"frame"`
	Resources   []traceResource      `json:"resources"`
	ChildFrames []traceResourceFrame `json:"childFrames"`
}

type traceResource struct {
	URL  string `json:"url"`
	Type string `json:"type"`
}

type traceResourceTreeResponse struct {
	FrameTree traceResourceFrame `json:"frameTree"`
}

type traceResourceContentParams struct {
	FrameID string `json:"frameId"`
	URL     string `json:"url"`
}

func captureTraceSources(ctx context.Context, page TracePage) ([]TraceSource, bool, error) {
	var tree traceResourceTreeResponse
	if err := page.Call(ctx, "Page.getResourceTree", nil, &tree); err != nil {
		return nil, false, fmt.Errorf("capture trace resource tree: %w", err)
	}
	frames, truncated := flattenTraceResourceFrames(tree.FrameTree)
	sources := make([]TraceSource, 0, len(frames))
	seen := make(map[string]struct{})
	var captureErr error
collect:
	for _, frame := range frames {
		for _, resource := range frame.Resources {
			if resource.URL == "" {
				continue
			}
			if _, ok := seen[resource.URL]; ok {
				continue
			}
			if len(seen) >= traceSourceLimit {
				truncated = true
				break collect
			}
			if len(resource.URL) > traceTextLimit {
				truncated = true
				captureErr = errors.Join(captureErr, fmt.Errorf("trace source URL exceeds byte limit"))
				break collect
			}
			seen[resource.URL] = struct{}{}
			var content struct {
				Content       string `json:"content"`
				Base64Encoded bool   `json:"base64Encoded"`
			}
			if err := page.Call(ctx, "Page.getResourceContent", traceResourceContentParams{FrameID: frame.Frame.ID, URL: resource.URL}, &content); err != nil {
				captureErr = errors.Join(captureErr, fmt.Errorf("capture trace source %s: %w", resource.URL, err))
				continue
			}
			data, sourceTruncated, err := decodeTraceSource(content.Content, content.Base64Encoded)
			if err != nil {
				captureErr = errors.Join(captureErr, fmt.Errorf("decode trace source %s: %w", resource.URL, err))
				continue
			}
			truncated = truncated || sourceTruncated
			sources = append(sources, TraceSource{URL: resource.URL, Data: data})
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].URL < sources[j].URL })
	return sources, truncated, captureErr
}

func decodeTraceSource(content string, encoded bool) ([]byte, bool, error) {
	if !encoded {
		truncated := len(content) > traceSourceBytesLimit
		if truncated {
			content = content[:traceSourceBytesLimit]
		}
		return []byte(content), truncated, nil
	}
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(content))
	data, err := io.ReadAll(io.LimitReader(decoder, traceSourceBytesLimit+1))
	if err != nil {
		return nil, false, err
	}
	if _, err := io.Copy(io.Discard, decoder); err != nil {
		return nil, false, err
	}
	truncated := len(data) > traceSourceBytesLimit
	if truncated {
		data = data[:traceSourceBytesLimit]
	}
	return data, truncated, nil
}

func flattenTraceResourceFrames(root traceResourceFrame) ([]traceResourceFrame, bool) {
	var frames []traceResourceFrame
	pending := []traceResourceFrame{root}
	truncated := false
	for len(pending) > 0 {
		frame := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		frames = append(frames, frame)
		children := frame.ChildFrames
		remaining := traceDebugEntryLimit - len(frames) - len(pending)
		if len(children) > remaining {
			children = children[:remaining]
			truncated = true
		}
		for index := len(children) - 1; index >= 0; index-- {
			pending = append(pending, children[index])
		}
	}
	return frames, truncated
}
