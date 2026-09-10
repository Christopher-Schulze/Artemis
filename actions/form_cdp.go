package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

const (
	formIntentStateName   = "__artemisFormIntentState"
	formIntentEventBuffer = 64
)

type FormCDPCaller interface {
	Call(context.Context, string, any, any) error
}

type FormCDPEvent struct {
	Method    string
	Params    json.RawMessage
	SessionID string
}

type FormCDPEventStream interface {
	Next(context.Context) (FormCDPEvent, error)
	Close()
}

type FormCDPSubscribe func(int) (FormCDPEventStream, error)

type CDPFormIntentConfig struct {
	Caller    FormCDPCaller
	SessionID string
	PageID    string
	Subscribe FormCDPSubscribe
}

type cdpFormIntentBackend struct {
	config      CDPFormIntentConfig
	bindingName string
	runtime     *FormIntentRuntime
	startMu     sync.Mutex
	stream      FormCDPEventStream
	monitorCtx  context.Context
	cancel      context.CancelFunc
	wait        sync.WaitGroup
	mu          sync.Mutex
	tokens      map[string]FormIdentity
	terminal    error
	closed      bool
}

func NewCDPFormIntentRuntime(config CDPFormIntentConfig, runtimeConfig FormIntentRuntimeConfig) (*FormIntentRuntime, error) {
	if config.Caller == nil || config.Subscribe == nil {
		return nil, errors.New("CDP form intent: caller and event subscription required")
	}
	if invalidFormIdentityPart(config.SessionID) || invalidFormIdentityPart(config.PageID) {
		return nil, errors.New("CDP form intent: session and page identity required")
	}
	backend := &cdpFormIntentBackend{
		config: config, bindingName: formBindingName(config.SessionID, config.PageID), tokens: make(map[string]FormIdentity),
	}
	runtime, err := NewFormIntentRuntime(backend, runtimeConfig)
	if err != nil {
		return nil, err
	}
	backend.runtime = runtime
	return runtime, nil
}

func (b *cdpFormIntentBackend) Prefetch(ctx context.Context, request FormPrefetchRequest) (FormPrefetchResult, error) {
	if err := b.validateIdentity(request.Identity); err != nil {
		return FormPrefetchResult{}, err
	}
	if err := b.ensureMonitor(ctx); err != nil {
		return FormPrefetchResult{}, err
	}
	if err := b.terminalError(); err != nil {
		return FormPrefetchResult{}, err
	}
	rootNodeID, err := b.resolveRoot(ctx, request.Identity.FormRoot)
	if err != nil {
		return FormPrefetchResult{}, err
	}
	token := formMutationToken(request.Identity, request.Generation)
	group := formResourceGroup(token, request.Generation)
	epoch, err := b.installObserver(ctx, request.Identity, token)
	if err != nil {
		return FormPrefetchResult{}, errors.Join(err, b.releaseObjectGroupBounded(group))
	}
	partial := FormPrefetchResult{ResourceGroup: group, MutationToken: token}
	accessible, err := b.queryAccessibleNodes(ctx, rootNodeID)
	if err != nil {
		return FormPrefetchResult{}, errors.Join(err, b.releasePrefetchBounded(partial))
	}
	fields, err := b.prefetchFields(ctx, rootNodeID, group, request.Fields, accessible)
	if err != nil {
		return FormPrefetchResult{}, errors.Join(err, b.releasePrefetchBounded(partial))
	}
	for index := range fields {
		fields[index].Epoch = epoch
	}
	return FormPrefetchResult{ResourceGroup: group, MutationToken: token, Fields: fields}, nil
}

func (b *cdpFormIntentBackend) Fill(ctx context.Context, request FormFillRequest) error {
	if err := b.validateIdentity(request.Identity); err != nil {
		return err
	}
	if err := b.terminalError(); err != nil {
		return err
	}
	value, err := encodeRuntimeString(request.Value)
	if err != nil {
		return err
	}
	token, err := encodeRuntimeString(formMutationToken(request.Identity, request.Generation))
	if err != nil {
		return err
	}
	epoch, err := encodeRuntimeUint64(request.Field.Epoch)
	if err != nil {
		return err
	}
	params := runtimeCallFunctionParams{
		ObjectID: request.Field.Ref, FunctionDeclaration: formFillFunction,
		Arguments:     []runtimeCallArgument{{Value: value}, {Value: token}, {Value: epoch}},
		ReturnByValue: true,
	}
	var result runtimeCallFunctionResult
	if err := b.config.Caller.Call(ctx, "Runtime.callFunctionOn", params, &result); err != nil {
		return fmt.Errorf("form intent fill %q: %w", request.Field.Selector, err)
	}
	if hasRuntimeException(result.ExceptionDetails) {
		return errors.New("form intent fill: page execution failed")
	}
	var status string
	if err := json.Unmarshal(result.Result.Value, &status); err != nil {
		return fmt.Errorf("form intent fill: decode status: %w", err)
	}
	switch status {
	case "filled":
		return nil
	case "stale":
		return ErrFormIntentStale
	default:
		return fmt.Errorf("form intent fill: unsupported element for %q", request.Field.Selector)
	}
}

func (b *cdpFormIntentBackend) Release(ctx context.Context, result FormPrefetchResult) error {
	var resultErr error
	if result.ResourceGroup != "" {
		resultErr = errors.Join(resultErr, b.releaseObjectGroup(ctx, result.ResourceGroup))
	}
	if result.MutationToken != "" {
		resultErr = errors.Join(resultErr, b.disconnectObserver(ctx, result.MutationToken))
		b.mu.Lock()
		delete(b.tokens, result.MutationToken)
		b.mu.Unlock()
	}
	return resultErr
}

func (b *cdpFormIntentBackend) Close() error {
	b.startMu.Lock()
	if b.closed {
		b.startMu.Unlock()
		return nil
	}
	b.closed = true
	if b.cancel != nil {
		b.cancel()
	}
	stream := b.stream
	b.startMu.Unlock()
	if stream == nil {
		return nil
	}
	stream.Close()
	b.wait.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), formIntentReleaseTimeout)
	defer cancel()
	return b.config.Caller.Call(ctx, "Runtime.removeBinding", runtimeBindingParams{Name: b.bindingName}, &struct{}{})
}

func (b *cdpFormIntentBackend) ensureMonitor(ctx context.Context) error {
	b.startMu.Lock()
	defer b.startMu.Unlock()
	if b.closed {
		return ErrFormIntentClosed
	}
	if b.stream != nil {
		return b.terminalError()
	}
	stream, err := b.config.Subscribe(formIntentEventBuffer)
	if err != nil {
		return fmt.Errorf("form intent mutation subscription: %w", err)
	}
	if err := b.config.Caller.Call(ctx, "Runtime.addBinding", runtimeBindingParams{Name: b.bindingName}, &struct{}{}); err != nil {
		stream.Close()
		return fmt.Errorf("form intent mutation binding: %w", err)
	}
	b.monitorCtx, b.cancel = context.WithCancel(context.Background())
	b.stream = stream
	b.wait.Add(1)
	go b.monitorMutations()
	return nil
}

func (b *cdpFormIntentBackend) monitorMutations() {
	defer b.wait.Done()
	for {
		event, err := b.stream.Next(b.monitorCtx)
		if err != nil {
			if b.monitorCtx.Err() == nil {
				b.failClosed(err)
			}
			return
		}
		b.handleMutationEvent(event)
	}
}

func (b *cdpFormIntentBackend) handleMutationEvent(event FormCDPEvent) {
	if event.Method != "Runtime.bindingCalled" || event.SessionID != "" && event.SessionID != b.config.SessionID {
		return
	}
	var payload runtimeBindingCalled
	if json.Unmarshal(event.Params, &payload) != nil || payload.Name != b.bindingName {
		return
	}
	b.mu.Lock()
	identity, ok := b.tokens[payload.Payload]
	b.mu.Unlock()
	if ok && b.runtime != nil {
		if err := b.runtime.Invalidate(identity); err != nil {
			b.recordTerminal(fmt.Errorf("form intent mutation invalidation: %w", err))
		}
	}
}

func (b *cdpFormIntentBackend) failClosed(err error) {
	b.mu.Lock()
	if b.terminal == nil {
		b.terminal = fmt.Errorf("form intent mutation stream: %w", err)
	}
	identities := make([]FormIdentity, 0, len(b.tokens))
	for _, identity := range b.tokens {
		identities = append(identities, identity)
	}
	b.mu.Unlock()
	for _, identity := range identities {
		if invalidateErr := b.runtime.Invalidate(identity); invalidateErr != nil {
			b.recordTerminal(fmt.Errorf("form intent fail-closed invalidation: %w", invalidateErr))
		}
	}
}

func (b *cdpFormIntentBackend) recordTerminal(err error) {
	b.mu.Lock()
	b.terminal = errors.Join(b.terminal, err)
	b.mu.Unlock()
}

func (b *cdpFormIntentBackend) terminalError() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.terminal
}

func (b *cdpFormIntentBackend) validateIdentity(identity FormIdentity) error {
	if identity.SessionID != b.config.SessionID || identity.PageID != b.config.PageID {
		return errors.New("CDP form intent: cross-session or cross-page identity denied")
	}
	return nil
}

func (b *cdpFormIntentBackend) resolveRoot(ctx context.Context, selector string) (int64, error) {
	var document domGetDocumentResult
	if err := b.config.Caller.Call(ctx, "DOM.getDocument", domGetDocumentParams{Depth: 1, Pierce: true}, &document); err != nil {
		return 0, fmt.Errorf("form intent document: %w", err)
	}
	var root domQuerySelectorResult
	if err := b.config.Caller.Call(ctx, "DOM.querySelector", domQuerySelectorParams{NodeID: document.Root.NodeID, Selector: selector}, &root); err != nil {
		return 0, fmt.Errorf("form intent root selector: %w", err)
	}
	if root.NodeID <= 0 {
		return 0, fmt.Errorf("form intent root %q not found", selector)
	}
	return root.NodeID, nil
}

func (b *cdpFormIntentBackend) queryAccessibleNodes(ctx context.Context, rootNodeID int64) (map[int64]struct{}, error) {
	var result accessibilityQueryResult
	if err := b.config.Caller.Call(ctx, "Accessibility.queryAXTree", accessibilityQueryParams{NodeID: rootNodeID}, &result); err != nil {
		return nil, fmt.Errorf("form intent accessibility query: %w", err)
	}
	nodes := make(map[int64]struct{}, len(result.Nodes))
	for _, node := range result.Nodes {
		if node.BackendDOMNodeID > 0 && !node.Ignored {
			nodes[node.BackendDOMNodeID] = struct{}{}
		}
	}
	return nodes, nil
}

func (b *cdpFormIntentBackend) prefetchFields(ctx context.Context, rootNodeID int64, group string, descriptors []FormFieldDescriptor, accessible map[int64]struct{}) ([]PrefetchedFormField, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fields := make([]PrefetchedFormField, len(descriptors))
	errs := make([]error, len(descriptors))
	var wait sync.WaitGroup
	for index, descriptor := range descriptors {
		wait.Add(1)
		go func(index int, descriptor FormFieldDescriptor) {
			defer wait.Done()
			fields[index], errs[index] = b.prefetchField(ctx, rootNodeID, group, descriptor, accessible)
			if errs[index] != nil {
				cancel()
			}
		}(index, descriptor)
	}
	wait.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return fields, nil
}

func (b *cdpFormIntentBackend) prefetchField(ctx context.Context, rootNodeID int64, group string, descriptor FormFieldDescriptor, accessible map[int64]struct{}) (PrefetchedFormField, error) {
	var selected domQuerySelectorResult
	if err := b.config.Caller.Call(ctx, "DOM.querySelector", domQuerySelectorParams{NodeID: rootNodeID, Selector: descriptor.Selector}, &selected); err != nil {
		return PrefetchedFormField{}, fmt.Errorf("form intent selector %q: %w", descriptor.Selector, err)
	}
	if selected.NodeID <= 0 {
		return PrefetchedFormField{}, fmt.Errorf("form intent selector %q not found", descriptor.Selector)
	}
	details, err := b.describeField(ctx, selected.NodeID, group)
	if err != nil {
		return PrefetchedFormField{}, fmt.Errorf("form intent selector %q: %w", descriptor.Selector, err)
	}
	if _, ok := accessible[details.backendNodeID]; !ok {
		return PrefetchedFormField{}, fmt.Errorf("form intent selector %q absent from accessible form tree", descriptor.Selector)
	}
	return PrefetchedFormField{Name: descriptor.Name, Selector: descriptor.Selector, Ref: details.objectID, Box: details.box}, nil
}

type prefetchedFieldDetails struct {
	backendNodeID int64
	objectID      string
	box           FormBoxModel
}

func (b *cdpFormIntentBackend) describeField(ctx context.Context, nodeID int64, group string) (prefetchedFieldDetails, error) {
	var described domDescribeNodeResult
	var resolved domResolveNodeResult
	var boxed domGetBoxModelResult
	errs := make([]error, 3)
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		errs[0] = b.config.Caller.Call(ctx, "DOM.describeNode", domDescribeNodeParams{NodeID: nodeID}, &described)
	}()
	go func() {
		defer wait.Done()
		errs[1] = b.config.Caller.Call(ctx, "DOM.resolveNode", domResolveNodeParams{NodeID: nodeID, ObjectGroup: group}, &resolved)
	}()
	go func() {
		defer wait.Done()
		errs[2] = b.config.Caller.Call(ctx, "DOM.getBoxModel", domGetBoxModelParams{NodeID: nodeID}, &boxed)
	}()
	wait.Wait()
	for _, err := range errs {
		if err != nil {
			return prefetchedFieldDetails{}, err
		}
	}
	if described.Node.BackendNodeID <= 0 || resolved.Object.ObjectID == "" {
		return prefetchedFieldDetails{}, errors.New("incomplete CDP node identity")
	}
	box, err := decodeFormBoxModel(boxed.Model)
	if err != nil {
		return prefetchedFieldDetails{}, err
	}
	return prefetchedFieldDetails{backendNodeID: described.Node.BackendNodeID, objectID: resolved.Object.ObjectID, box: box}, nil
}

func (b *cdpFormIntentBackend) installObserver(ctx context.Context, identity FormIdentity, token string) (uint64, error) {
	b.mu.Lock()
	b.tokens[token] = identity
	b.mu.Unlock()
	expression := formObserverExpression(identity.FormRoot, token, b.bindingName)
	var result runtimeEvaluateResult
	if err := b.config.Caller.Call(ctx, "Runtime.evaluate", runtimeEvaluateParams{Expression: expression, ReturnByValue: true}, &result); err != nil {
		b.removeToken(token)
		return 0, errors.Join(fmt.Errorf("form intent mutation observer: %w", err), b.disconnectObserverBounded(token))
	}
	if hasRuntimeException(result.ExceptionDetails) {
		b.removeToken(token)
		return 0, errors.Join(errors.New("form intent mutation observer setup failed"), b.disconnectObserverBounded(token))
	}
	var state formObserverState
	if err := json.Unmarshal(result.Result.Value, &state); err != nil {
		b.removeToken(token)
		return 0, errors.Join(fmt.Errorf("form intent mutation observer response: %w", err), b.disconnectObserverBounded(token))
	}
	if !state.OK {
		b.removeToken(token)
		return 0, errors.Join(fmt.Errorf("form intent mutation observer unavailable for %q", identity.FormRoot), b.disconnectObserverBounded(token))
	}
	return state.Epoch, nil
}

func (b *cdpFormIntentBackend) disconnectObserver(ctx context.Context, token string) error {
	expression := fmt.Sprintf(`(() => { const s=globalThis.%s; const e=s?.get(%s); if(e)e.observer.disconnect(); if(s)s.delete(%s); return true; })()`, formIntentStateName, jsLiteral(token), jsLiteral(token))
	var result runtimeEvaluateResult
	if err := b.config.Caller.Call(ctx, "Runtime.evaluate", runtimeEvaluateParams{Expression: expression, ReturnByValue: true}, &result); err != nil {
		return fmt.Errorf("form intent disconnect observer: %w", err)
	}
	if hasRuntimeException(result.ExceptionDetails) {
		return errors.New("form intent disconnect observer: page execution failed")
	}
	return nil
}

func (b *cdpFormIntentBackend) disconnectObserverBounded(token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), formIntentReleaseTimeout)
	defer cancel()
	return b.disconnectObserver(ctx, token)
}

func (b *cdpFormIntentBackend) releaseObjectGroup(ctx context.Context, group string) error {
	if group == "" {
		return nil
	}
	if err := b.config.Caller.Call(ctx, "Runtime.releaseObjectGroup", runtimeObjectGroupParams{ObjectGroup: group}, &struct{}{}); err != nil {
		return fmt.Errorf("form intent release object group: %w", err)
	}
	return nil
}

func (b *cdpFormIntentBackend) releaseObjectGroupBounded(group string) error {
	ctx, cancel := context.WithTimeout(context.Background(), formIntentReleaseTimeout)
	defer cancel()
	return b.releaseObjectGroup(ctx, group)
}

func (b *cdpFormIntentBackend) releasePrefetchBounded(result FormPrefetchResult) error {
	ctx, cancel := context.WithTimeout(context.Background(), formIntentReleaseTimeout)
	defer cancel()
	return b.Release(ctx, result)
}

func (b *cdpFormIntentBackend) removeToken(token string) {
	b.mu.Lock()
	delete(b.tokens, token)
	b.mu.Unlock()
}

func formBindingName(sessionID, pageID string) string {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + pageID))
	return "__artemis_form_intent_" + hex.EncodeToString(sum[:8])
}

func formMutationToken(identity FormIdentity, generation uint64) string {
	sum := sha256.Sum256([]byte(identity.SessionID + "\x00" + identity.PageID + "\x00" + identity.FormRoot + "\x00" + strconv.FormatUint(generation, 10)))
	return hex.EncodeToString(sum[:16])
}

func formResourceGroup(token string, generation uint64) string {
	return "artemis-form-" + token + "-" + strconv.FormatUint(generation, 10)
}

func formObserverExpression(root, token, binding string) string {
	return fmt.Sprintf(`(() => { const root=document.querySelector(%s); if(!root)return {ok:false,epoch:0}; const state=globalThis.%s||(globalThis.%s=new Map()); const prior=state.get(%s); if(prior)prior.observer.disconnect(); const entry={root:root,epoch:prior?.epoch||0}; entry.observer=new MutationObserver(() => { entry.epoch++; globalThis[%s](%s); }); entry.observer.observe(root,{attributes:true,childList:true,subtree:true}); state.set(%s,entry); return {ok:true,epoch:entry.epoch}; })()`, jsLiteral(root), formIntentStateName, formIntentStateName, jsLiteral(token), jsLiteral(binding), jsLiteral(token), jsLiteral(token))
}

func jsLiteral(value string) string {
	return strconv.Quote(value)
}

const formFillFunction = `function(value,token,expectedEpoch){
const state=globalThis.__artemisFormIntentState?.get(token);
if(!state||state.epoch!==expectedEpoch||!this.isConnected||!state.root.contains(this))return "stale";
if(this instanceof HTMLInputElement||this instanceof HTMLTextAreaElement){const proto=this instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;const setter=Object.getOwnPropertyDescriptor(proto,"value")?.set;if(setter)setter.call(this,value);else this.value=value;}
else if(this instanceof HTMLSelectElement){this.value=value;}
else if(this.isContentEditable){this.textContent=value;}
else{return "unsupported";}
this.dispatchEvent(new Event("input",{bubbles:true}));this.dispatchEvent(new Event("change",{bubbles:true}));return "filled";
}`

const formIntentReleaseTimeout = 2 * time.Second

type domGetDocumentParams struct {
	Depth  int  `json:"depth"`
	Pierce bool `json:"pierce"`
}

type domGetDocumentResult struct {
	Root struct {
		NodeID int64 `json:"nodeId"`
	} `json:"root"`
}

type domQuerySelectorParams struct {
	NodeID   int64  `json:"nodeId"`
	Selector string `json:"selector"`
}

type domQuerySelectorResult struct {
	NodeID int64 `json:"nodeId"`
}

type accessibilityQueryParams struct {
	NodeID int64 `json:"nodeId"`
}

type accessibilityQueryResult struct {
	Nodes []struct {
		BackendDOMNodeID int64 `json:"backendDOMNodeId"`
		Ignored          bool  `json:"ignored"`
	} `json:"nodes"`
}

type domDescribeNodeParams struct {
	NodeID int64 `json:"nodeId"`
}

type domDescribeNodeResult struct {
	Node struct {
		BackendNodeID int64 `json:"backendNodeId"`
	} `json:"node"`
}

type domResolveNodeParams struct {
	NodeID      int64  `json:"nodeId"`
	ObjectGroup string `json:"objectGroup"`
}

type domResolveNodeResult struct {
	Object struct {
		ObjectID string `json:"objectId"`
	} `json:"object"`
}

type domGetBoxModelParams struct {
	NodeID int64 `json:"nodeId"`
}

type domBoxModel struct {
	Content []float64 `json:"content"`
	Padding []float64 `json:"padding"`
	Border  []float64 `json:"border"`
	Margin  []float64 `json:"margin"`
	Width   int       `json:"width"`
	Height  int       `json:"height"`
}

type domGetBoxModelResult struct {
	Model domBoxModel `json:"model"`
}

type runtimeBindingParams struct {
	Name string `json:"name"`
}

type runtimeBindingCalled struct {
	Name    string `json:"name"`
	Payload string `json:"payload"`
}

type runtimeEvaluateParams struct {
	Expression    string `json:"expression"`
	ReturnByValue bool   `json:"returnByValue"`
}

type runtimeRemoteObject struct {
	Value json.RawMessage `json:"value"`
}

type runtimeEvaluateResult struct {
	Result           runtimeRemoteObject `json:"result"`
	ExceptionDetails json.RawMessage     `json:"exceptionDetails,omitempty"`
}

type runtimeCallArgument struct {
	Value json.RawMessage `json:"value"`
}

type runtimeCallFunctionParams struct {
	ObjectID            string                `json:"objectId"`
	FunctionDeclaration string                `json:"functionDeclaration"`
	Arguments           []runtimeCallArgument `json:"arguments"`
	ReturnByValue       bool                  `json:"returnByValue"`
}

type runtimeCallFunctionResult struct {
	Result           runtimeRemoteObject `json:"result"`
	ExceptionDetails json.RawMessage     `json:"exceptionDetails,omitempty"`
}

type runtimeObjectGroupParams struct {
	ObjectGroup string `json:"objectGroup"`
}

type formObserverState struct {
	OK    bool   `json:"ok"`
	Epoch uint64 `json:"epoch"`
}

func decodeFormBoxModel(model domBoxModel) (FormBoxModel, error) {
	content, err := formQuad(model.Content)
	if err != nil {
		return FormBoxModel{}, fmt.Errorf("content quad: %w", err)
	}
	padding, err := formQuad(model.Padding)
	if err != nil {
		return FormBoxModel{}, fmt.Errorf("padding quad: %w", err)
	}
	border, err := formQuad(model.Border)
	if err != nil {
		return FormBoxModel{}, fmt.Errorf("border quad: %w", err)
	}
	margin, err := formQuad(model.Margin)
	if err != nil {
		return FormBoxModel{}, fmt.Errorf("margin quad: %w", err)
	}
	return FormBoxModel{Content: content, Padding: padding, Border: border, Margin: margin, Width: model.Width, Height: model.Height}, nil
}

func formQuad(values []float64) ([8]float64, error) {
	var result [8]float64
	if len(values) != len(result) {
		return result, fmt.Errorf("expected 8 coordinates, got %d", len(values))
	}
	copy(result[:], values)
	return result, nil
}

func hasRuntimeException(value json.RawMessage) bool {
	return len(value) > 0 && string(value) != "null"
}

func encodeRuntimeString(value string) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("form intent fill: encode argument: %w", err)
	}
	return encoded, nil
}

func encodeRuntimeUint64(value uint64) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("form intent fill: encode argument: %w", err)
	}
	return encoded, nil
}
