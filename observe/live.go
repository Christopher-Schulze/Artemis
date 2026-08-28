package observe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

const (
	DefaultLiveEventBuffer  = 512
	DefaultMaxEvidenceBytes = 2 * 1024 * 1024
)

// BrowserEventSource exposes the browser-level CDP event stream used by the
// canonical observation owner. bridge.Page satisfies this interface.
type BrowserEventSource interface {
	SubscribeBrowserEvents(int) (*bridge.CDPSubscription, error)
}

// LiveConfig bounds the complete evidence surface for one page.
type LiveConfig struct {
	Observation      bridgeobserve.Config
	NetworkCapacity  int
	ConsoleCapacity  int
	EventBuffer      int
	MaxEvidenceBytes int
}

func DefaultLiveConfig() LiveConfig {
	return LiveConfig{
		Observation:      bridgeobserve.DefaultConfig(),
		NetworkCapacity:  DefaultNetworkRingCapacity,
		ConsoleCapacity:  DefaultConsoleBufferSize,
		EventBuffer:      DefaultLiveEventBuffer,
		MaxEvidenceBytes: DefaultMaxEvidenceBytes,
	}
}

// ObservationEvidence is the bounded, single-snapshot projection consumed by
// browser and route callers. Snapshot identity comes only from bridge/observe;
// the root buffers add the other protocol evidence without a second DOM owner.
type ObservationEvidence struct {
	Schema            string                 `json:"schema"`
	CapturedAt        time.Time              `json:"captured_at"`
	Snapshot          bridgeobserve.Snapshot `json:"snapshot"`
	Network           []NetworkEvent         `json:"network,omitempty"`
	Console           []ConsoleEntry         `json:"console,omitempty"`
	Metrics           PerformanceMetrics     `json:"metrics"`
	Truncated         bool                   `json:"truncated"`
	TruncationReasons []string               `json:"truncation_reasons,omitempty"`
	Warnings          []string               `json:"warnings,omitempty"`
}

// Validate enforces the canonical schema boundary before evidence crosses a
// route or stream contract.
func (e ObservationEvidence) Validate() error {
	if e.Schema != bridgeobserve.Schema || e.Snapshot.Schema != bridgeobserve.Schema {
		return fmt.Errorf("observation: unsupported schema %q", e.Schema)
	}
	return nil
}

// LiveCollector owns one bridge collector and the bounded protocol buffers
// associated with that collector. It is safe for concurrent event delivery and
// evidence capture.
type LiveCollector struct {
	snapshot         *bridgeobserve.Collector
	network          *NetworkMonitor
	console          *ConsoleCapture
	metrics          *MetricsCollector
	caller           bridgeobserve.Caller
	sessionID        string
	maxEvidenceBytes int
	pendingMu        sync.Mutex
	pending          map[string]*pendingNetwork
	errorMu          sync.RWMutex
	streamErr        error
	stream           *bridge.CDPSubscription
	cancel           context.CancelFunc
	closeOnce        sync.Once
	closed           atomic.Bool
	workers          sync.WaitGroup
}

type pendingNetwork struct {
	event NetworkEvent
}

// NewLiveCollector creates the canonical observation owner. When source is
// non-nil, browser events are subscribed before the caller can navigate.
func NewLiveCollector(caller bridgeobserve.Caller, source BrowserEventSource, config LiveConfig) (*LiveCollector, error) {
	defaults := DefaultLiveConfig()
	if config.NetworkCapacity <= 0 {
		config.NetworkCapacity = defaults.NetworkCapacity
	}
	if config.ConsoleCapacity <= 0 {
		config.ConsoleCapacity = defaults.ConsoleCapacity
	}
	if config.EventBuffer <= 0 {
		config.EventBuffer = defaults.EventBuffer
	}
	if config.MaxEvidenceBytes <= 0 {
		config.MaxEvidenceBytes = defaults.MaxEvidenceBytes
	}
	collector, err := bridgeobserve.NewCollector(caller, config.Observation)
	if err != nil {
		return nil, err
	}
	live := &LiveCollector{
		snapshot:         collector,
		network:          NewNetworkMonitor(config.NetworkCapacity),
		console:          NewConsoleCaptureWithCapacity(config.ConsoleCapacity),
		metrics:          NewMetricsCollector(),
		caller:           caller,
		maxEvidenceBytes: config.MaxEvidenceBytes,
		pending:          make(map[string]*pendingNetwork),
	}
	if page, ok := caller.(*bridge.Page); ok {
		live.sessionID = page.SessionID()
	}
	if source == nil {
		return live, nil
	}
	subscription, err := source.SubscribeBrowserEvents(config.EventBuffer)
	if err != nil {
		return nil, fmt.Errorf("observation events: %w", err)
	}
	if subscription == nil {
		return nil, errors.New("observation events: nil subscription")
	}
	live.stream = subscription
	streamCtx, cancel := context.WithCancel(context.Background())
	live.cancel = cancel
	live.workers.Add(1)
	go live.consumeEvents(streamCtx, subscription)
	return live, nil
}

// CaptureEvidence captures one bounded full observation and flushes in-flight
// network records so route consumers see a complete point-in-time projection.
func (c *LiveCollector) CaptureEvidence(ctx context.Context) (ObservationEvidence, error) {
	return c.Capture(ctx, bridgeobserve.ModeFull, "")
}

// Capture captures one bounded observation mode together with all protocol
// buffers owned by this collector.
func (c *LiveCollector) Capture(ctx context.Context, mode bridgeobserve.Mode, subtreeRef string) (ObservationEvidence, error) {
	if c == nil || c.snapshot == nil {
		return ObservationEvidence{}, errors.New("observation: live collector is nil")
	}
	if c.closed.Load() {
		return ObservationEvidence{}, errors.New("observation: live collector is closed")
	}
	snapshot, err := c.snapshot.Capture(ctx, mode, subtreeRef)
	if err != nil {
		return ObservationEvidence{}, err
	}
	c.flushPending()
	evidence := ObservationEvidence{
		Schema:     bridgeobserve.Schema,
		CapturedAt: time.Now().UTC(),
		Snapshot:   cloneSnapshot(snapshot),
		Network:    cloneNetworkEvents(c.network.Snapshot()),
		Console:    cloneConsoleEntries(c.console.Snapshot()),
		Metrics:    c.metrics.Current(),
	}
	if streamErr := c.eventError(); streamErr != nil {
		return ObservationEvidence{}, fmt.Errorf("observation events: %w", streamErr)
	}
	if err := c.captureMetrics(ctx, &evidence); err != nil {
		evidence.Warnings = append(evidence.Warnings, "performance metrics unavailable: "+err.Error())
	}
	return boundEvidence(evidence, c.maxEvidenceBytes), nil
}

// Resolve delegates stable-reference resolution to the sole bridge collector.
func (c *LiveCollector) Resolve(ctx context.Context, ref string) (bridgeobserve.Resolution, error) {
	if c == nil || c.snapshot == nil {
		return bridgeobserve.Resolution{}, errors.New("observation: live collector is nil")
	}
	return c.snapshot.Resolve(ctx, ref)
}

// LastSnapshot returns the latest stable snapshot from the canonical owner.
func (c *LiveCollector) LastSnapshot() bridgeobserve.Snapshot {
	if c == nil || c.snapshot == nil {
		return bridgeobserve.Snapshot{}
	}
	return c.snapshot.LastSnapshot()
}

// SnapshotCollector exposes the canonical stable-reference owner to action
// runtimes that require the concrete bridge collector.
func (c *LiveCollector) SnapshotCollector() *bridgeobserve.Collector {
	if c == nil {
		return nil
	}
	return c.snapshot
}

func (c *LiveCollector) RecordNetwork(event NetworkEvent) {
	if c == nil || c.closed.Load() {
		return
	}
	c.network.Push(event)
}

func (c *LiveCollector) RecordConsole(entry ConsoleEntry) {
	if c == nil || c.closed.Load() {
		return
	}
	c.console.Push(entry)
}

func (c *LiveCollector) SetMetrics(metrics PerformanceMetrics) {
	if c == nil || c.closed.Load() {
		return
	}
	c.metrics.SetMetrics(metrics)
}

// Close ends event delivery and waits for the owned worker to stop.
func (c *LiveCollector) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		if c.cancel != nil {
			c.cancel()
		}
		if c.stream != nil {
			c.stream.Close()
		}
		c.workers.Wait()
	})
	return nil
}

func (c *LiveCollector) consumeEvents(ctx context.Context, subscription *bridge.CDPSubscription) {
	defer c.workers.Done()
	if subscription == nil {
		return
	}
	events := subscription.Events
	errorsChannel := subscription.Errors
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				if errorsChannel == nil {
					return
				}
			}
			if ok {
				c.recordEvent(event)
			}
		case err, ok := <-errorsChannel:
			if !ok {
				errorsChannel = nil
				if events == nil {
					return
				}
				continue
			}
			if err != nil {
				c.errorMu.Lock()
				c.streamErr = err
				c.errorMu.Unlock()
				return
			}
		}
	}
}

func (c *LiveCollector) recordEvent(event bridge.CDPEvent) {
	if !c.eventBelongsToPage(event) {
		return
	}
	switch event.Method {
	case "Network.requestWillBeSent":
		c.recordNetworkRequest(event.Params)
	case "Network.responseReceived":
		c.recordNetworkResponse(event.Params)
	case "Network.loadingFinished":
		c.finishNetwork(event.Params, 0)
	case "Network.loadingFailed":
		c.finishNetwork(event.Params, 0)
	case "Runtime.consoleAPICalled":
		c.recordConsoleEvent(event.Params)
	}
}

func (c *LiveCollector) eventBelongsToPage(event bridge.CDPEvent) bool {
	if c == nil || c.sessionID == "" {
		return true
	}
	if event.SessionID == c.sessionID {
		return true
	}
	page, ok := c.caller.(*bridge.Page)
	if !ok {
		return true
	}
	for _, sessionID := range page.FrameSessions() {
		if event.SessionID == sessionID {
			return true
		}
	}
	return false
}

type requestWillBeSent struct {
	RequestID string `json:"requestId"`
	Request   struct {
		URL      string                     `json:"url"`
		Method   string                     `json:"method"`
		Headers  map[string]json.RawMessage `json:"headers"`
		PostData string                     `json:"postData"`
	} `json:"request"`
	Type string `json:"type"`
}

type responseReceived struct {
	RequestID string `json:"requestId"`
	Response  struct {
		URL            string                     `json:"url"`
		Status         int                        `json:"status"`
		MimeType       string                     `json:"mimeType"`
		Headers        map[string]json.RawMessage `json:"headers"`
		EncodedDataLen int64                      `json:"encodedDataLength"`
	} `json:"response"`
}

type loadingFinished struct {
	RequestID         string  `json:"requestId"`
	EncodedDataLength int64   `json:"encodedDataLength"`
	Timestamp         float64 `json:"timestamp"`
}

type consoleAPICalled struct {
	Type      string         `json:"type"`
	Args      []remoteObject `json:"args"`
	Timestamp float64        `json:"timestamp"`
	Stack     struct {
		Description string `json:"description"`
	} `json:"stackTrace"`
}

type remoteObject struct {
	Type        string          `json:"type"`
	Value       json.RawMessage `json:"value"`
	Description string          `json:"description"`
	ObjectID    string          `json:"objectId"`
}

func (c *LiveCollector) recordNetworkRequest(raw json.RawMessage) {
	var event requestWillBeSent
	if json.Unmarshal(raw, &event) != nil || strings.TrimSpace(event.RequestID) == "" {
		return
	}
	c.pendingMu.Lock()
	if previous := c.pending[event.RequestID]; previous != nil {
		c.network.Push(previous.event)
	}
	c.pending[event.RequestID] = &pendingNetwork{event: NetworkEvent{
		RequestID: event.RequestID, URL: event.Request.URL, Method: event.Request.Method,
		ResourceType: event.Type, Headers: rawHeaders(event.Request.Headers), PostData: event.Request.PostData,
		Start: time.Now().UTC(),
	}}
	c.pendingMu.Unlock()
}

func (c *LiveCollector) recordNetworkResponse(raw json.RawMessage) {
	var response responseReceived
	if json.Unmarshal(raw, &response) != nil || strings.TrimSpace(response.RequestID) == "" {
		return
	}
	c.pendingMu.Lock()
	entry := c.pending[response.RequestID]
	if entry == nil {
		entry = &pendingNetwork{event: NetworkEvent{RequestID: response.RequestID, Start: time.Now().UTC()}}
		c.pending[response.RequestID] = entry
	}
	entry.event.URL = firstNonEmpty(response.Response.URL, entry.event.URL)
	entry.event.Status = response.Response.Status
	entry.event.MimeType = response.Response.MimeType
	entry.event.ResponseHeaders = rawHeaders(response.Response.Headers)
	if response.Response.EncodedDataLen > 0 {
		entry.event.Bytes = int(response.Response.EncodedDataLen)
	}
	c.pendingMu.Unlock()
}

func (c *LiveCollector) finishNetwork(raw json.RawMessage, bytes int64) {
	var event loadingFinished
	if json.Unmarshal(raw, &event) != nil || strings.TrimSpace(event.RequestID) == "" {
		return
	}
	now := time.Now().UTC()
	c.pendingMu.Lock()
	entry := c.pending[event.RequestID]
	delete(c.pending, event.RequestID)
	c.pendingMu.Unlock()
	if entry == nil {
		return
	}
	if event.EncodedDataLength > 0 {
		bytes = event.EncodedDataLength
	}
	entry.event.Bytes = int(bytes)
	entry.event.End = now
	entry.event.DurationMS = now.Sub(entry.event.Start).Seconds() * 1000
	c.network.Push(entry.event)
}

func (c *LiveCollector) flushPending() {
	now := time.Now().UTC()
	c.pendingMu.Lock()
	entries := make([]NetworkEvent, 0, len(c.pending))
	for requestID, entry := range c.pending {
		delete(c.pending, requestID)
		entry.event.End = now
		entry.event.DurationMS = now.Sub(entry.event.Start).Seconds() * 1000
		entries = append(entries, entry.event)
	}
	c.pendingMu.Unlock()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Start.Before(entries[j].Start)
	})
	for _, entry := range entries {
		c.network.Push(entry)
	}
}

func (c *LiveCollector) recordConsoleEvent(raw json.RawMessage) {
	var event consoleAPICalled
	if json.Unmarshal(raw, &event) != nil {
		return
	}
	args := make([]string, 0, len(event.Args))
	for _, arg := range event.Args {
		value := strings.TrimSpace(arg.Description)
		if value == "" && len(arg.Value) > 0 && string(arg.Value) != "null" {
			value = strings.TrimSpace(string(arg.Value))
		}
		if value == "" {
			value = arg.ObjectID
		}
		args = append(args, truncateString(value, MaxPostDataLen))
	}
	timestamp := time.Now().UTC()
	c.console.Push(ConsoleEntry{Level: NormalizeLevel(event.Type), Args: args, Timestamp: timestamp, StackTrace: truncateString(event.Stack.Description, MaxPostDataLen)})
}

func (c *LiveCollector) captureMetrics(ctx context.Context, evidence *ObservationEvidence) error {
	var response struct {
		Metrics []struct {
			Name  string  `json:"name"`
			Value float64 `json:"value"`
		} `json:"metrics"`
	}
	if err := c.caller.Call(ctx, "Performance.getMetrics", map[string]any{}, &response); err != nil {
		return err
	}
	metrics := evidence.Metrics
	for _, metric := range response.Metrics {
		switch metric.Name {
		case "DomContentLoaded":
			metrics.DOMContentLoaded = secondsDuration(metric.Value)
		case "LoadEvent":
			metrics.LoadEvent = secondsDuration(metric.Value)
		case "FirstMeaningfulPaint", "FirstPaint":
			metrics.FirstPaint = secondsDuration(metric.Value)
		case "FirstContentfulPaint":
			metrics.FirstContentfulPaint = secondsDuration(metric.Value)
		case "LargestContentfulPaint":
			metrics.LargestContentfulPaint = secondsDuration(metric.Value)
		case "InteractiveTime":
			metrics.TimeToInteractive = secondsDuration(metric.Value)
		case "TaskDuration":
			metrics.TotalBlockingTime = secondsDuration(metric.Value)
		case "LayoutShift":
			metrics.CumulativeLayoutShift = metric.Value
		case "TransferSize":
			metrics.TransferSize = int64(metric.Value)
		case "EncodedBodySize":
			metrics.EncodedSize = int64(metric.Value)
		case "DecodedBodySize":
			metrics.DecodedSize = int64(metric.Value)
		case "ResourceCount":
			metrics.RequestCount = int(metric.Value)
		}
	}
	c.metrics.SetMetrics(metrics)
	evidence.Metrics = metrics
	return nil
}

func (c *LiveCollector) eventError() error {
	c.errorMu.RLock()
	defer c.errorMu.RUnlock()
	return c.streamErr
}

func rawHeaders(raw map[string]json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "authorization" || lower == "cookie" || lower == "set-cookie" || lower == "proxy-authorization" {
			continue
		}
		var value string
		if json.Unmarshal(raw[key], &value) != nil {
			value = strings.Trim(string(raw[key]), `"`)
		}
		result[key] = value
	}
	return result
}

func secondsDuration(seconds float64) time.Duration {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func boundEvidence(evidence ObservationEvidence, maxBytes int) ObservationEvidence {
	if maxBytes <= 0 {
		return evidence
	}
	evidence.Snapshot = cloneSnapshot(evidence.Snapshot)
	evidence.Network = cloneNetworkEvents(evidence.Network)
	evidence.Console = cloneConsoleEntries(evidence.Console)
	for encodedEvidenceSize(evidence) > maxBytes && len(evidence.Network) > 0 {
		evidence.Network = evidence.Network[1:]
		evidence.Truncated = true
		appendReason(&evidence, "max_evidence_bytes")
	}
	for encodedEvidenceSize(evidence) > maxBytes && len(evidence.Console) > 0 {
		evidence.Console = evidence.Console[1:]
		evidence.Truncated = true
		appendReason(&evidence, "max_evidence_bytes")
	}
	if encodedEvidenceSize(evidence) > maxBytes {
		evidence.Snapshot.Nodes = nil
		evidence.Snapshot.TotalNodes = 0
		evidence.Snapshot.Truncated = true
		appendReason(&evidence, "max_evidence_bytes")
	}
	return evidence
}

func encodedEvidenceSize(evidence ObservationEvidence) int {
	raw, err := json.Marshal(evidence)
	if err != nil {
		return math.MaxInt
	}
	return len(raw)
}

func appendReason(evidence *ObservationEvidence, reason string) {
	for _, existing := range evidence.TruncationReasons {
		if existing == reason {
			return
		}
	}
	evidence.TruncationReasons = append(evidence.TruncationReasons, reason)
}

func cloneSnapshot(snapshot bridgeobserve.Snapshot) bridgeobserve.Snapshot {
	clone := snapshot
	clone.Nodes = make([]bridgeobserve.Node, len(snapshot.Nodes))
	copy(clone.Nodes, snapshot.Nodes)
	for i := range clone.Nodes {
		clone.Nodes[i].Attributes = cloneStringMap(clone.Nodes[i].Attributes)
		clone.Nodes[i].ShadowPath = append([]string(nil), clone.Nodes[i].ShadowPath...)
		if clone.Nodes[i].Box != nil {
			box := *clone.Nodes[i].Box
			clone.Nodes[i].Box = &box
		}
	}
	clone.TruncationReasons = append([]string(nil), snapshot.TruncationReasons...)
	clone.Warnings = append([]string(nil), snapshot.Warnings...)
	return clone
}

func cloneNetworkEvents(events []NetworkEvent) []NetworkEvent {
	result := make([]NetworkEvent, len(events))
	copy(result, events)
	for i := range result {
		result[i].Headers = cloneStringMap(result[i].Headers)
		result[i].ResponseHeaders = cloneStringMap(result[i].ResponseHeaders)
	}
	return result
}

func cloneConsoleEntries(entries []ConsoleEntry) []ConsoleEntry {
	result := make([]ConsoleEntry, len(entries))
	copy(result, entries)
	for i := range result {
		result[i].Args = append([]string(nil), entries[i].Args...)
	}
	return result
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
