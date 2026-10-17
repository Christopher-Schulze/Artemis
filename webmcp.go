package artemis

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/security"
)

// webmcp.go adapts Chrome's experimental WebMCP CDP domain (verified against
// the /json/protocol schema of Chrome 155: WebMCP.enable|disable|invokeTool|
// cancelInvocation, events toolsAdded|toolsRemoved|toolInvoked|toolResponded).
// Website tools are untrusted input: names, descriptions, schemas and
// annotations are bounded and validated, annotations can only escalate the
// policy action, never relax it, and every invocation is bound to the
// catalog revision, frame and origin it was discovered with.

const (
	maxWebMCPTools          = 128
	maxWebMCPDescription    = 4 << 10
	maxWebMCPSchemaBytes    = 64 << 10
	maxWebMCPInputBytes     = 64 << 10
	maxWebMCPOutputBytes    = 1 << 20
	defaultWebMCPInvocation = 30 * time.Second
	maxWebMCPInvocation     = 5 * time.Minute
)

var webMCPToolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// WebMCP policy actions; the consequential annotation escalates to the
// second, it never downgrades an invocation.
const (
	WebMCPActionInvoke        = "webmcp_invoke"
	WebMCPActionConsequential = "webmcp_consequential"
)

// WebMCPAnnotations are the website's hints. They are reported, never trusted.
type WebMCPAnnotations struct {
	ReadOnly         bool `json:"readOnly,omitempty"`
	UntrustedContent bool `json:"untrustedContent,omitempty"`
	Consequential    bool `json:"consequential,omitempty"`
	Autosubmit       bool `json:"autosubmit,omitempty"`
}

// WebMCPTool is one validated website tool bound to its frame and origin.
// Description is untrusted website text.
type WebMCPTool struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	InputSchema   json.RawMessage   `json:"inputSchema"`
	Annotations   WebMCPAnnotations `json:"annotations"`
	FrameID       string            `json:"frameId"`
	Origin        string            `json:"origin"`
	BackendNodeID int64             `json:"backendNodeId,omitempty"`
	PolicyAction  string            `json:"policyAction"`
}

// WebMCPCatalog is the page's validated tool catalog at Revision.
type WebMCPCatalog struct {
	PageID   string       `json:"pageId"`
	URL      string       `json:"url"`
	Revision string       `json:"revision"`
	Tools    []WebMCPTool `json:"tools"`
	// Rejected names tools refused by validation, with the reason.
	Rejected []string `json:"rejected,omitempty"`
}

// WebMCPInvocation calls one catalog tool.
type WebMCPInvocation struct {
	Revision string          `json:"revision"`
	FrameID  string          `json:"frameId"`
	ToolName string          `json:"toolName"`
	Input    json.RawMessage `json:"input"`
	Timeout  time.Duration   `json:"timeout,omitempty"`
}

// WebMCPResult is a tool's response. Output is untrusted website content.
type WebMCPResult struct {
	InvocationID string                `json:"invocationId"`
	Status       string                `json:"status"`
	Output       json.RawMessage       `json:"output,omitempty"`
	ErrorText    string                `json:"errorText,omitempty"`
	Trust        security.ContentTrust `json:"trust"`
}

type webMCPWireTool struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	InputSchema   json.RawMessage   `json:"inputSchema"`
	Annotations   WebMCPAnnotations `json:"annotations"`
	FrameID       string            `json:"frameId"`
	BackendNodeID int64             `json:"backendNodeId"`
}

type webMCPToolsAdded struct {
	Tools []webMCPWireTool `json:"tools"`
}

type webMCPInvokeParams struct {
	FrameID  string          `json:"frameId"`
	ToolName string          `json:"toolName"`
	Input    json.RawMessage `json:"input"`
}

type webMCPInvokeResult struct {
	InvocationID string `json:"invocationId"`
}

type webMCPResponded struct {
	InvocationID string          `json:"invocationId"`
	Status       string          `json:"status"`
	Output       json.RawMessage `json:"output"`
	ErrorText    string          `json:"errorText"`
}

type webMCPCancelParams struct {
	InvocationID string `json:"invocationId"`
}

type webMCPEmpty struct{}

type webMCPState struct {
	catalog WebMCPCatalog
	schemas map[string]*jsonschema.Schema
}

// WebMCPTools enables the page's WebMCP domain, collects every registered
// tool, validates it and stores the catalog as the page's current one.
func (s *Session) WebMCPTools(ctx context.Context, pageID string) (WebMCPCatalog, *TaskError) {
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return WebMCPCatalog{}, taskErr
	}
	defer done()
	page, lease, taskErr := s.leaseChromiumPage(execCtx, pageID)
	if taskErr != nil {
		return WebMCPCatalog{}, taskErr
	}
	defer lease.Release()
	execCtx = lease.Context()
	if taskErr := s.authorizeChromiumOperation(execCtx, page, "observe", ""); taskErr != nil {
		return WebMCPCatalog{}, taskErr
	}
	state, taskErr := discoverWebMCP(execCtx, page.page, pageID)
	if taskErr != nil {
		return WebMCPCatalog{}, taskErr
	}
	s.chromiumMu.Lock()
	page.webmcp = state
	s.chromiumMu.Unlock()
	return state.catalog, nil
}

// InvokeWebMCPTool invokes a tool of the page's current catalog after
// rediscovering the catalog (drift fails stale), checking the frame origin,
// validating the input against the tool schema and authorizing the policy
// action. Cancellation of ctx cancels the browser invocation.
func (s *Session) InvokeWebMCPTool(ctx context.Context, pageID string, invocation WebMCPInvocation) (WebMCPResult, *TaskError) {
	if invocation.Timeout < 0 || invocation.Timeout > maxWebMCPInvocation {
		return WebMCPResult{}, newTaskError(TaskErrorInvalidInput, "webmcp_invoke", fmt.Errorf("timeout must be 0..%s", maxWebMCPInvocation))
	}
	if invocation.Timeout == 0 {
		invocation.Timeout = defaultWebMCPInvocation
	}
	input, err := webMCPInput(invocation.Input)
	if err != nil {
		return WebMCPResult{}, newTaskError(TaskErrorInvalidInput, "webmcp_invoke", err)
	}
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return WebMCPResult{}, taskErr
	}
	defer done()
	page, lease, taskErr := s.leaseChromiumPage(execCtx, pageID)
	if taskErr != nil {
		return WebMCPResult{}, taskErr
	}
	defer lease.Release()
	execCtx = lease.Context()
	if taskErr := s.authorizeWrite(ctx, page, false); taskErr != nil {
		return WebMCPResult{}, taskErr
	}
	s.chromiumMu.Lock()
	known := page.webmcp
	s.chromiumMu.Unlock()
	if known == nil || known.catalog.Revision != invocation.Revision {
		return WebMCPResult{}, newTaskError(TaskErrorStaleReference, "webmcp_invoke", errors.New("catalog revision is not the page's current WebMCP catalog; list tools again"))
	}
	current, taskErr := discoverWebMCP(execCtx, page.page, pageID)
	if taskErr != nil {
		return WebMCPResult{}, taskErr
	}
	if current.catalog.Revision != known.catalog.Revision {
		s.chromiumMu.Lock()
		page.webmcp = nil
		s.chromiumMu.Unlock()
		return WebMCPResult{}, newTaskError(TaskErrorStaleReference, "webmcp_invoke", errors.New("the page's WebMCP tools, frames or origins changed since listing; list tools again"))
	}
	tool, schema := current.tool(invocation.FrameID, invocation.ToolName)
	if tool == nil {
		return WebMCPResult{}, newTaskError(TaskErrorInvalidInput, "webmcp_invoke", fmt.Errorf("tool %q is not in frame %q of the catalog", invocation.ToolName, invocation.FrameID))
	}
	if err := schema.Validate(input); err != nil {
		return WebMCPResult{}, newTaskError(TaskErrorInvalidInput, "webmcp_invoke", fmt.Errorf("input does not match the tool schema: %w", err))
	}
	if err := s.owner.authorizeBrowserRequest(execCtx, bridge.PolicyRequest{Action: tool.PolicyAction, URL: tool.Origin + "/", Method: "POST"}); err != nil {
		return WebMCPResult{}, classifyTaskError("webmcp_policy", err)
	}
	return invokeWebMCP(execCtx, page.page, *tool, invocation)
}

func (state *webMCPState) tool(frameID, name string) (*WebMCPTool, *jsonschema.Schema) {
	for index := range state.catalog.Tools {
		tool := &state.catalog.Tools[index]
		if tool.FrameID == frameID && tool.Name == name {
			return tool, state.schemas[frameID+"\x00"+name]
		}
	}
	return nil, nil
}

func webMCPInput(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > maxWebMCPInputBytes {
		return nil, fmt.Errorf("input exceeds %d bytes", maxWebMCPInputBytes)
	}
	input, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("input must be a JSON object")
	}
	if _, ok := input.(map[string]any); !ok {
		return nil, errors.New("input must be a JSON object")
	}
	return input, nil
}

// denySchemaReferences refuses every schema not embedded in the tool's own
// input schema: website schemas never load files or network resources.
type denySchemaReferences struct{}

func (denySchemaReferences) Load(location string) (any, error) {
	return nil, fmt.Errorf("schema reference %s is not allowed", location)
}

const webMCPSchemaID = "urn:artemis:webmcp:input-schema"

func compileWebMCPSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	object, ok := document.(map[string]any)
	if !ok || object["type"] != "object" {
		return nil, errors.New("input schema must describe an object")
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(denySchemaReferences{})
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource(webMCPSchemaID, document); err != nil {
		return nil, err
	}
	return compiler.Compile(webMCPSchemaID)
}

// discoverWebMCP collects the toolsAdded bursts that WebMCP.enable emits in
// the page session and every attached frame session. A trailing no-op
// command orders the stream: events sent before its response are queued.
func discoverWebMCP(ctx context.Context, page *bridge.Page, pageID string) (state *webMCPState, taskErr *TaskError) {
	subscription, err := page.SubscribeBrowserEvents(256)
	if err != nil {
		return nil, classifyTaskError("webmcp", err)
	}
	defer subscription.Close()
	if err := setWebMCP(ctx, page, "WebMCP.enable"); err != nil {
		if unsupportedCDPMethod(err) {
			return nil, newTaskError(TaskErrorCapabilityUnavailable, "webmcp", errors.New("this Chrome build has no WebMCP domain"))
		}
		return nil, classifyTaskError("webmcp", err)
	}
	defer func() {
		disableCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if err := setWebMCP(disableCtx, page, "WebMCP.disable"); err != nil && taskErr == nil {
			state, taskErr = nil, classifyTaskError("webmcp", fmt.Errorf("disable after discovery: %w", err))
		}
	}()
	pageURL, err := page.CurrentURL(ctx)
	if err != nil {
		return nil, classifyTaskError("webmcp", err)
	}
	if err := page.Call(ctx, "Runtime.evaluate", webMCPFlush{Expression: "0"}, nil); err != nil {
		return nil, classifyTaskError("webmcp", err)
	}
	sessions := webMCPSessions(page)
	var wire []webMCPWireTool
	for {
		select {
		case event := <-subscription.Events:
			if event.Method != "WebMCP.toolsAdded" || !sessions[event.SessionID] {
				continue
			}
			var added webMCPToolsAdded
			if err := json.Unmarshal(event.Params, &added); err != nil {
				return nil, newTaskError(TaskErrorExecutionFailed, "webmcp", fmt.Errorf("WebMCP.toolsAdded: %w", err))
			}
			if wire = append(wire, added.Tools...); len(wire) > 4*maxWebMCPTools {
				return nil, newTaskError(TaskErrorResourceLimit, "webmcp", fmt.Errorf("page registered more than %d tools", 4*maxWebMCPTools))
			}
		case err := <-subscription.Errors:
			if err != nil {
				return nil, classifyTaskError("webmcp", err)
			}
		default:
			return buildWebMCPCatalog(ctx, page, pageID, pageURL, wire), nil
		}
	}
}

type webMCPFlush struct {
	Expression string `json:"expression"`
}

// setWebMCP enables or disables the domain in the page session and in every
// attached frame session, so out-of-process frames report their tools.
func setWebMCP(ctx context.Context, page *bridge.Page, method string) error {
	if err := page.Call(ctx, method, webMCPEmpty{}, nil); err != nil {
		return err
	}
	frames := make([]string, 0)
	for frameID := range page.FrameSessions() {
		frames = append(frames, frameID)
	}
	sort.Strings(frames)
	for _, frameID := range frames {
		if err := page.CallFrame(ctx, frameID, method, webMCPEmpty{}, nil); err != nil {
			return fmt.Errorf("frame %s %s: %w", frameID, method, err)
		}
	}
	return nil
}

func webMCPSessions(page *bridge.Page) map[string]bool {
	sessions := map[string]bool{page.SessionID(): true}
	for _, session := range page.FrameSessions() {
		sessions[session] = true
	}
	return sessions
}

func unsupportedCDPMethod(err error) bool {
	message := err.Error()
	return strings.Contains(message, "wasn't found") || strings.Contains(message, "-32601")
}

// buildWebMCPCatalog validates every wire tool and binds it to its frame's
// origin. Invalid tools are rejected with a reason; duplicates in one frame
// are all rejected because the identity would be ambiguous.
func buildWebMCPCatalog(ctx context.Context, page *bridge.Page, pageID, pageURL string, wire []webMCPWireTool) *webMCPState {
	state := &webMCPState{catalog: WebMCPCatalog{PageID: pageID, URL: pageURL, Tools: []WebMCPTool{}}, schemas: map[string]*jsonschema.Schema{}}
	counts := map[string]int{}
	for _, tool := range wire {
		counts[tool.FrameID+"\x00"+tool.Name]++
	}
	origins := map[string]string{}
	for _, wireTool := range wire {
		key := wireTool.FrameID + "\x00" + wireTool.Name
		tool, schema, err := validateWebMCPTool(wireTool, counts[key])
		if err == nil {
			tool.Origin, err = webMCPFrameOrigin(ctx, page, wireTool.FrameID, origins)
		}
		if err == nil && len(state.catalog.Tools) == maxWebMCPTools {
			err = fmt.Errorf("catalog exceeds %d tools", maxWebMCPTools)
		}
		if err != nil {
			state.catalog.Rejected = append(state.catalog.Rejected, fmt.Sprintf("%.128s: %v", wireTool.Name, err))
			continue
		}
		state.catalog.Tools = append(state.catalog.Tools, tool)
		state.schemas[key] = schema
	}
	sort.Slice(state.catalog.Tools, func(i, j int) bool {
		left, right := state.catalog.Tools[i], state.catalog.Tools[j]
		return left.FrameID < right.FrameID || left.FrameID == right.FrameID && left.Name < right.Name
	})
	sort.Strings(state.catalog.Rejected)
	encoded, err := json.Marshal(struct {
		URL      string       `json:"url"`
		Tools    []WebMCPTool `json:"tools"`
		Rejected []string     `json:"rejected"`
	}{pageURL, state.catalog.Tools, state.catalog.Rejected})
	if err == nil {
		sum := sha256.Sum256(encoded)
		state.catalog.Revision = hex.EncodeToString(sum[:16])
	}
	return state
}

func validateWebMCPTool(wire webMCPWireTool, sameIdentity int) (WebMCPTool, *jsonschema.Schema, error) {
	if !webMCPToolName.MatchString(wire.Name) {
		return WebMCPTool{}, nil, errors.New("tool name must be 1-128 of [A-Za-z0-9_.-]")
	}
	if wire.FrameID == "" {
		return WebMCPTool{}, nil, errors.New("tool has no frame")
	}
	if sameIdentity > 1 {
		return WebMCPTool{}, nil, errors.New("duplicate tool name in one frame")
	}
	if len(wire.InputSchema) == 0 {
		wire.InputSchema = json.RawMessage(`{"type":"object"}`)
	}
	if len(wire.InputSchema) > maxWebMCPSchemaBytes {
		return WebMCPTool{}, nil, fmt.Errorf("input schema exceeds %d bytes", maxWebMCPSchemaBytes)
	}
	resolved, err := compileWebMCPSchema(wire.InputSchema)
	if err != nil {
		return WebMCPTool{}, nil, fmt.Errorf("input schema: %w", err)
	}
	description := strings.ToValidUTF8(wire.Description, "")
	if len(description) > maxWebMCPDescription {
		description = description[:maxWebMCPDescription]
	}
	action := WebMCPActionInvoke
	if wire.Annotations.Consequential {
		action = WebMCPActionConsequential
	}
	return WebMCPTool{Name: wire.Name, Description: description, InputSchema: wire.InputSchema, Annotations: wire.Annotations, FrameID: wire.FrameID, BackendNodeID: wire.BackendNodeID, PolicyAction: action}, resolved, nil
}

func webMCPFrameOrigin(ctx context.Context, page *bridge.Page, frameID string, cache map[string]string) (string, error) {
	if origin, ok := cache[frameID]; ok {
		return origin, nil
	}
	frameURL, err := page.FramePolicyURL(ctx, frameID)
	if err != nil {
		return "", fmt.Errorf("frame origin: %w", err)
	}
	parsed, err := url.Parse(frameURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("frame %s has no network origin", frameID)
	}
	origin := parsed.Scheme + "://" + parsed.Host
	cache[frameID] = origin
	return origin, nil
}

// invokeWebMCP dispatches the invocation and waits for its response; a
// cancelled or expired wait cancels the browser invocation.
func invokeWebMCP(ctx context.Context, page *bridge.Page, tool WebMCPTool, invocation WebMCPInvocation) (result WebMCPResult, taskErr *TaskError) {
	subscription, err := page.SubscribeBrowserEvents(256)
	if err != nil {
		return WebMCPResult{}, classifyTaskError("webmcp_invoke", err)
	}
	defer subscription.Close()
	if err := setWebMCP(ctx, page, "WebMCP.enable"); err != nil {
		return WebMCPResult{}, classifyTaskError("webmcp_invoke", err)
	}
	defer func() {
		disableCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if err := setWebMCP(disableCtx, page, "WebMCP.disable"); err != nil && taskErr == nil {
			taskErr = classifyTaskError("webmcp_invoke", fmt.Errorf("disable after invocation: %w", err))
		}
	}()
	input := invocation.Input
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	var started webMCPInvokeResult
	if err := page.CallFrame(ctx, webMCPFrameRoute(page, tool.FrameID), "WebMCP.invokeTool", webMCPInvokeParams{FrameID: tool.FrameID, ToolName: tool.Name, Input: input}, &started); err != nil {
		return WebMCPResult{}, classifyTaskError("webmcp_invoke", err)
	}
	if started.InvocationID == "" {
		return WebMCPResult{}, newTaskError(TaskErrorExecutionFailed, "webmcp_invoke", errors.New("browser returned no invocation id"))
	}
	checker := security.NewIDPIChecker(security.DefaultIDPIConfig())
	trust, err := checker.Label(tool.Origin+"/", "webmcp", nil)
	if err != nil {
		return WebMCPResult{}, classifyTaskError("webmcp_invoke", err)
	}
	result = WebMCPResult{InvocationID: started.InvocationID, Trust: trust}
	sessions := webMCPSessions(page)
	timer := time.NewTimer(invocation.Timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return result, cancelWebMCP(ctx, page, tool.FrameID, started.InvocationID, classifyTaskError("webmcp_invoke", ctx.Err()))
		case <-timer.C:
			return result, cancelWebMCP(ctx, page, tool.FrameID, started.InvocationID, newTaskError(TaskErrorTimeout, "webmcp_invoke", fmt.Errorf("tool did not respond within %s", invocation.Timeout)))
		case err := <-subscription.Errors:
			if err != nil {
				return result, classifyTaskError("webmcp_invoke", err)
			}
		case event := <-subscription.Events:
			if event.Method != "WebMCP.toolResponded" || !sessions[event.SessionID] {
				continue
			}
			var responded webMCPResponded
			if err := json.Unmarshal(event.Params, &responded); err != nil {
				return result, newTaskError(TaskErrorExecutionFailed, "webmcp_invoke", fmt.Errorf("WebMCP.toolResponded: %w", err))
			}
			if responded.InvocationID != started.InvocationID {
				continue
			}
			if len(responded.Output) > maxWebMCPOutputBytes {
				return result, newTaskError(TaskErrorResourceLimit, "webmcp_invoke", fmt.Errorf("tool output exceeds %d bytes", maxWebMCPOutputBytes))
			}
			result.Status, result.Output, result.ErrorText = responded.Status, responded.Output, responded.ErrorText
			if result.Trust, err = checker.Label(tool.Origin+"/", "webmcp", responded.Output); err != nil {
				return result, classifyTaskError("webmcp_invoke", err)
			}
			return result, nil
		}
	}
}

// webMCPFrameRoute returns the frame whose session owns frameID: an attached
// out-of-process frame routes to its own session, others to the page.
func webMCPFrameRoute(page *bridge.Page, frameID string) string {
	if page.FrameSessions()[frameID] != "" {
		return frameID
	}
	return ""
}

func cancelWebMCP(ctx context.Context, page *bridge.Page, frameID, invocationID string, cause *TaskError) *TaskError {
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := page.CallFrame(cancelCtx, webMCPFrameRoute(page, frameID), "WebMCP.cancelInvocation", webMCPCancelParams{InvocationID: invocationID}, nil); err != nil {
		return newTaskError(cause.Code, cause.Op, errors.Join(cause.Cause, fmt.Errorf("cancel invocation: %w", err)))
	}
	return cause
}
