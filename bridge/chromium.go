package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

// TargetState describes the legal lifecycle of a CDP target.
type TargetState string

const (
	TargetStateAttached TargetState = "attached"
	TargetStateDetached TargetState = "detached"
	TargetStateCrashed  TargetState = "crashed"
	TargetStateClosed   TargetState = "closed"
)

// ReconnectPolicy declares how a dead browser transport is recovered.
type ReconnectPolicy string

const (
	// ReconnectExplicitRelaunch prevents hidden state resurrection after a browser crash.
	ReconnectExplicitRelaunch ReconnectPolicy = "explicit_relaunch"
)

const defaultChromiumMaxPages = 32

// BrowserVersion is the validated Browser.getVersion result.
type BrowserVersion struct {
	ProtocolVersion string `json:"protocolVersion"`
	Product         string `json:"product"`
	Revision        string `json:"revision"`
	UserAgent       string `json:"userAgent"`
	JSVersion       string `json:"jsVersion"`
}

type setDiscoverTargetsParams struct {
	Discover bool `json:"discover"`
}

type createBrowserContextParams struct {
	DisposeOnDetach bool `json:"disposeOnDetach"`
}

type browserContextParams struct {
	BrowserContextID string `json:"browserContextId"`
}

type targetParams struct {
	TargetID string `json:"targetId"`
}

type createTargetParams struct {
	URL              string `json:"url"`
	BrowserContextID string `json:"browserContextId,omitempty"`
}

type attachTargetParams struct {
	TargetID string `json:"targetId"`
	Flatten  bool   `json:"flatten"`
}

type navigateParams struct {
	URL string `json:"url"`
}

// TargetScriptConfig is the browser-owned pre-script contract. PageScript is
// installed with Page.addScriptToEvaluateOnNewDocument; WorkerScript is
// evaluated while a worker target is paused before it resumes. Keeping the
// shape in bridge avoids a package cycle while allowing stealth to own the
// script contents and versioning.
type TargetScriptConfig struct {
	Version      string
	PageScript   string
	WorkerScript string
}

type targetScriptResult struct {
	Identifier string `json:"identifier"`
}

type targetScriptEvaluateParams struct {
	Expression    string `json:"expression"`
	ReturnByValue bool   `json:"returnByValue"`
}

// TargetError reports target-specific lifecycle failure.
type TargetError struct {
	TargetID string
	State    TargetState
	Message  string
}

func (e *TargetError) Error() string {
	return fmt.Sprintf("artemis target %s is %s: %s", e.TargetID, e.State, e.Message)
}

// ChromiumBrowser owns one CDP transport and optionally its local process.
type ChromiumBrowser struct {
	mu            sync.RWMutex
	transport     *CDPTransport
	process       *browserprocess.Browser
	endpoint      string
	owned         bool
	version       BrowserVersion
	contexts      map[string]*BrowserContext
	pages         map[string]*Page
	sessionMap    map[string]*Page
	maxPages      int
	pageSlots     int
	monitor       *CDPSubscription
	targetScripts TargetScriptConfig
	closed        bool
	terminal      error
	closeOnce     sync.Once
	closeErr      error
}

// ConnectChromium validates an external browser endpoint without taking process ownership.
func ConnectChromium(ctx context.Context, endpoint string) (*ChromiumBrowser, error) {
	return connectChromium(ctx, endpoint, nil)
}

// LaunchChromium launches and validates an owned Chromium process.
func LaunchChromium(ctx context.Context, config browserprocess.LaunchConfig) (*ChromiumBrowser, error) {
	processOwner, err := browserprocess.Launch(ctx, config)
	if err != nil {
		return nil, err
	}
	browser, err := connectChromium(ctx, processOwner.Endpoint(), processOwner)
	if err != nil {
		_ = processOwner.Close()
		return nil, err
	}
	return browser, nil
}

func connectChromium(ctx context.Context, endpoint string, processOwner *browserprocess.Browser) (*ChromiumBrowser, error) {
	transport, err := DialCDPTransport(ctx, CDPTransportConfig{URL: endpoint})
	if err != nil {
		return nil, err
	}
	browser := &ChromiumBrowser{
		transport: transport, process: processOwner, endpoint: endpoint, owned: processOwner != nil,
		contexts: make(map[string]*BrowserContext), pages: make(map[string]*Page), sessionMap: make(map[string]*Page),
		maxPages: defaultChromiumMaxPages,
	}
	if err := transport.Call(ctx, "Browser.getVersion", nil, &browser.version); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("validate browser identity: %w", err)
	}
	if browser.version.ProtocolVersion == "" || browser.version.Product == "" {
		_ = transport.Close()
		return nil, &CDPError{Code: CDPErrorProtocol, Op: "validate browser identity", Err: fmt.Errorf("incomplete Browser.getVersion result")}
	}
	monitor, err := transport.Subscribe(256)
	if err != nil {
		_ = transport.Close()
		return nil, err
	}
	browser.monitor = monitor
	if err := transport.Call(ctx, "Target.setDiscoverTargets", setDiscoverTargetsParams{Discover: true}, nil); err != nil {
		monitor.Close()
		_ = transport.Close()
		return nil, fmt.Errorf("enable target discovery: %w", err)
	}
	go browser.monitorTargets(monitor)
	if processOwner != nil {
		go browser.monitorProcess(processOwner)
	}
	return browser, nil
}

// Version returns the immutable validated browser identity.
func (b *ChromiumBrowser) Version() BrowserVersion {
	return b.version
}

// Endpoint returns the active CDP endpoint.
func (b *ChromiumBrowser) Endpoint() string {
	return b.endpoint
}

// Owned reports whether Artemis owns and may terminate the browser process.
func (b *ChromiumBrowser) Owned() bool {
	return b.owned
}

// ReconnectPolicy reports the fail-closed recovery contract.
func (b *ChromiumBrowser) ReconnectPolicy() ReconnectPolicy {
	return ReconnectExplicitRelaunch
}

// ProfileDir returns the owned process profile directory, or an empty string for external attachment.
func (b *ChromiumBrowser) ProfileDir() string {
	if b.process == nil {
		return ""
	}
	return b.process.ProfileDir()
}

// Transport exposes the typed CDP transport for advanced browser-scoped calls.
func (b *ChromiumBrowser) Transport() *CDPTransport {
	return b.transport
}

// ConfigureTargetScripts installs the immutable script contract used for
// subsequently created pages and attached child targets. Existing pages are
// intentionally not mutated: callers must configure before opening pages so
// the first document script runs before page code.
func (b *ChromiumBrowser) ConfigureTargetScripts(config TargetScriptConfig) error {
	if b == nil {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "configure target scripts", Err: fmt.Errorf("browser required")}
	}
	if config.PageScript == "" && config.WorkerScript == "" {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.closed || b.terminal != nil {
			return &CDPError{Code: CDPErrorClosed, Op: "configure target scripts", Err: fmt.Errorf("browser is closed")}
		}
		if b.targetScripts.PageScript != "" || b.targetScripts.WorkerScript != "" {
			return &CDPError{Code: CDPErrorInvalidConfig, Op: "configure target scripts", Err: fmt.Errorf("target scripts are immutable")}
		}
		b.targetScripts = TargetScriptConfig{}
		return nil
	}
	if strings.TrimSpace(config.Version) == "" {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "configure target scripts", Err: fmt.Errorf("script version required")}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.terminal != nil {
		return &CDPError{Code: CDPErrorClosed, Op: "configure target scripts", Err: fmt.Errorf("browser is closed")}
	}
	if b.targetScripts.PageScript != "" || b.targetScripts.WorkerScript != "" {
		if b.targetScripts != config {
			return &CDPError{Code: CDPErrorInvalidConfig, Op: "configure target scripts", Err: fmt.Errorf("target scripts already configured")}
		}
		return nil
	}
	b.targetScripts = config
	return nil
}

func (b *ChromiumBrowser) targetScriptConfig() TargetScriptConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.targetScripts
}

// SetMaxPages updates the positive runtime target bound before workload admission.
func (b *ChromiumBrowser) SetMaxPages(limit int) error {
	if limit < 1 {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "set page limit", Err: fmt.Errorf("positive limit required")}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit < b.pageSlots {
		return &CDPError{Code: CDPErrorOverloaded, Op: "set page limit", Err: fmt.Errorf("%d pages already admitted", b.pageSlots)}
	}
	b.maxPages = limit
	return nil
}

func (b *ChromiumBrowser) reservePage() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.terminal != nil {
		return &CDPError{Code: CDPErrorClosed, Op: "reserve page", Err: fmt.Errorf("browser is closed")}
	}
	if b.pageSlots >= b.maxPages {
		return &CDPError{Code: CDPErrorOverloaded, Op: "reserve page", Err: fmt.Errorf("page limit %d reached", b.maxPages)}
	}
	b.pageSlots++
	return nil
}

func (b *ChromiumBrowser) releasePage() {
	b.mu.Lock()
	if b.pageSlots > 0 {
		b.pageSlots--
	}
	b.mu.Unlock()
}

// Healthy reports whether the browser and transport are still usable.
func (b *ChromiumBrowser) Healthy() bool {
	b.mu.RLock()
	closed := b.closed
	terminal := b.terminal
	b.mu.RUnlock()
	if closed || terminal != nil {
		return false
	}
	select {
	case <-b.transport.Done():
		return false
	default:
		return true
	}
}

// Err returns the terminal browser/process failure, if one occurred.
func (b *ChromiumBrowser) Err() error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.terminal != nil {
		return b.terminal
	}
	select {
	case <-b.transport.Done():
		return b.transport.Err()
	default:
		return nil
	}
}

func (b *ChromiumBrowser) monitorProcess(processOwner *browserprocess.Browser) {
	<-processOwner.Done()
	b.mu.Lock()
	if !b.closed {
		b.terminal = processOwner.Err()
		if b.terminal == nil {
			b.terminal = &browserprocess.Error{Code: browserprocess.ErrorBrowserCrash, Op: "supervise running browser", Err: fmt.Errorf("process exited unexpectedly")}
		}
	}
	terminal := b.terminal
	b.mu.Unlock()
	if terminal != nil {
		_ = b.transport.Close()
	}
}

// NewContext creates one isolated CDP browser context.
func (b *ChromiumBrowser) NewContext(ctx context.Context) (*BrowserContext, error) {
	if ctx == nil {
		return nil, &CDPError{Code: CDPErrorInvalidConfig, Op: "create browser context", Err: fmt.Errorf("context required")}
	}
	if err := b.ensureOpen(); err != nil {
		return nil, err
	}
	var result struct {
		ID string `json:"browserContextId"`
	}
	if err := b.transport.Call(ctx, "Target.createBrowserContext", createBrowserContextParams{}, &result); err != nil {
		return nil, fmt.Errorf("create browser context: %w", err)
	}
	if result.ID == "" {
		return nil, &CDPError{Code: CDPErrorProtocol, Op: "create browser context", Err: fmt.Errorf("empty browserContextId")}
	}
	contextOwner := &BrowserContext{id: result.ID, browser: b, pages: make(map[string]*Page)}
	b.mu.Lock()
	if b.closed || b.terminal != nil {
		b.mu.Unlock()
		_ = b.callCleanup("Target.disposeBrowserContext", browserContextParams{BrowserContextID: result.ID})
		return nil, &CDPError{Code: CDPErrorClosed, Op: "create browser context", Err: fmt.Errorf("browser closed concurrently")}
	}
	b.contexts[result.ID] = contextOwner
	b.mu.Unlock()
	return contextOwner, nil
}

// DefaultContext returns the persistent Chromium profile context. Pages in
// this context use the launched process's user-data-dir and therefore retain
// cookies and origin storage across an explicit browser restart.
func (b *ChromiumBrowser) DefaultContext() (*BrowserContext, error) {
	if err := b.ensureOpen(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.terminal != nil {
		return nil, &CDPError{Code: CDPErrorClosed, Op: "open default context", Err: fmt.Errorf("browser closed concurrently")}
	}
	if contextOwner, ok := b.contexts[""]; ok {
		return contextOwner, nil
	}
	contextOwner := &BrowserContext{id: "", browser: b, pages: make(map[string]*Page)}
	b.contexts[""] = contextOwner
	return contextOwner, nil
}

func (b *ChromiumBrowser) callCleanup(method string, params any) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCDPDialTimeout)
	defer cancel()
	return b.transport.Call(ctx, method, params, nil)
}

func (b *ChromiumBrowser) ensureOpen() error {
	if !b.Healthy() {
		return &CDPError{Code: CDPErrorClosed, Op: "browser", Err: fmt.Errorf("browser is closed")}
	}
	return nil
}

func (b *ChromiumBrowser) monitorTargets(subscription *CDPSubscription) {
	for {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				return
			}
			b.applyTargetEvent(event)
		case _, ok := <-subscription.Errors:
			if !ok {
				return
			}
			return
		}
	}
}

func (b *ChromiumBrowser) applyTargetEvent(event CDPEvent) {
	var payload struct {
		TargetID  string `json:"targetId"`
		SessionID string `json:"sessionId"`
		Status    string `json:"status"`
		ErrorCode int    `json:"errorCode"`
	}
	if err := json.Unmarshal(event.Params, &payload); err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	page := b.pages[payload.TargetID]
	if page == nil && payload.SessionID != "" {
		page = b.sessionMap[payload.SessionID]
	}
	if page == nil {
		return
	}
	switch event.Method {
	case "Target.targetCrashed":
		page.setState(TargetStateCrashed, fmt.Sprintf("status=%s errorCode=%d", payload.Status, payload.ErrorCode))
	case "Target.detachedFromTarget":
		page.setState(TargetStateDetached, "target session detached")
	case "Target.targetDestroyed":
		page.setState(TargetStateClosed, "target destroyed")
	}
}

// Close closes runtime resources. External browsers are never terminated.
func (b *ChromiumBrowser) Close() error {
	b.closeOnce.Do(func() {
		b.closeErr = b.close()
	})
	return b.closeErr
}

func (b *ChromiumBrowser) close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	contexts := make([]*BrowserContext, 0, len(b.contexts))
	for _, browserContext := range b.contexts {
		contexts = append(contexts, browserContext)
	}
	b.mu.Unlock()
	var result error
	for _, browserContext := range contexts {
		result = errors.Join(result, browserContext.close(true))
	}
	if b.monitor != nil {
		b.monitor.Close()
	}
	if b.owned {
		closeCtx, cancel := context.WithTimeout(context.Background(), defaultCDPDialTimeout)
		err := b.transport.Call(closeCtx, "Browser.close", nil, nil)
		cancel()
		if err != nil && !IsCDPError(err, CDPErrorClosed) {
			result = errors.Join(result, fmt.Errorf("request browser close: %w", err))
		}
		if b.process != nil && err == nil {
			select {
			case <-b.process.Done():
			case <-time.After(2 * time.Second):
				result = errors.Join(result, fmt.Errorf("wait for graceful browser close: timeout"))
			}
		}
	}
	result = errors.Join(result, b.transport.Close())
	if b.process != nil {
		result = errors.Join(result, b.process.Close())
	}
	return result
}

// BrowserContext owns pages inside one isolated CDP browser context.
type BrowserContext struct {
	mu      sync.Mutex
	id      string
	browser *ChromiumBrowser
	pages   map[string]*Page
	closed  bool
}

// ID returns the CDP browserContextId.
func (c *BrowserContext) ID() string {
	return c.id
}

// NewPage creates and attaches one flattened CDP target session.
func (c *BrowserContext) NewPage(ctx context.Context, initialURL string) (*Page, error) {
	return c.newPage(ctx, initialURL, c.browser.targetScriptConfig())
}

// NewPageWithScripts creates a page with a caller-selected immutable target
// script contract. It is used when a navigation policy selects a different
// supported stealth level for the target while retaining the same profile.
func (c *BrowserContext) NewPageWithScripts(ctx context.Context, initialURL string, scripts TargetScriptConfig) (*Page, error) {
	return c.newPage(ctx, initialURL, scripts)
}

func (c *BrowserContext) newPage(ctx context.Context, initialURL string, scripts TargetScriptConfig) (*Page, error) {
	if ctx == nil {
		return nil, &CDPError{Code: CDPErrorInvalidConfig, Op: "create page", Err: fmt.Errorf("context required")}
	}
	if (scripts.PageScript != "" || scripts.WorkerScript != "") && strings.TrimSpace(scripts.Version) == "" {
		return nil, &CDPError{Code: CDPErrorInvalidConfig, Op: "create page", Err: fmt.Errorf("target script version required")}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, &CDPError{Code: CDPErrorClosed, Op: "create page", Err: fmt.Errorf("browser context closed")}
	}
	c.mu.Unlock()
	if err := c.browser.reservePage(); err != nil {
		return nil, err
	}
	keepSlot := false
	defer func() {
		if !keepSlot {
			c.browser.releasePage()
		}
	}()
	targetURL := initialURL
	if targetURL == "" {
		targetURL = "about:blank"
	}
	// Always create about:blank first. This gives the owner a chance to install
	// Page.addScriptToEvaluateOnNewDocument before any caller-controlled page
	// script executes.
	targetID, err := c.createTarget(ctx, "about:blank")
	if err != nil {
		return nil, err
	}
	sessionID, err := c.attachTarget(ctx, targetID)
	if err != nil {
		_ = c.browser.callCleanup("Target.closeTarget", targetParams{TargetID: targetID})
		return nil, err
	}
	page, err := c.registerPage(targetID, sessionID, scripts)
	if err != nil {
		_ = c.browser.callCleanup("Target.closeTarget", targetParams{TargetID: targetID})
		return nil, err
	}
	if err := page.installPageScript(ctx); err != nil {
		_ = page.close(true)
		return nil, err
	}
	if err := page.enableFrameRouting(ctx); err != nil {
		_ = page.close(true)
		return nil, err
	}
	if targetURL != "about:blank" {
		if _, _, err := page.Navigate(ctx, targetURL); err != nil {
			_ = page.close(true)
			return nil, err
		}
	}
	keepSlot = true
	return page, nil
}

func (c *BrowserContext) createTarget(ctx context.Context, initialURL string) (string, error) {
	var target struct {
		ID string `json:"targetId"`
	}
	params := createTargetParams{URL: initialURL, BrowserContextID: c.id}
	if err := c.browser.transport.Call(ctx, "Target.createTarget", params, &target); err != nil {
		return "", fmt.Errorf("create target: %w", err)
	}
	if target.ID == "" {
		return "", &CDPError{Code: CDPErrorProtocol, Op: "create target", Err: fmt.Errorf("empty targetId")}
	}
	return target.ID, nil
}

func (c *BrowserContext) attachTarget(ctx context.Context, targetID string) (string, error) {
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.browser.transport.Call(ctx, "Target.attachToTarget", attachTargetParams{TargetID: targetID, Flatten: true}, &attached); err != nil {
		return "", fmt.Errorf("attach target: %w", err)
	}
	if attached.SessionID == "" {
		return "", &CDPError{Code: CDPErrorProtocol, Op: "attach target", Err: fmt.Errorf("empty sessionId")}
	}
	return attached.SessionID, nil
}

func (c *BrowserContext) registerPage(targetID, sessionID string, scripts TargetScriptConfig) (*Page, error) {
	page := &Page{targetID: targetID, sessionID: sessionID, owner: c, state: TargetStateAttached, frameSessions: make(map[string]string), targetScripts: scripts}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, &CDPError{Code: CDPErrorClosed, Op: "create page", Err: fmt.Errorf("browser context closed concurrently")}
	}
	c.browser.mu.Lock()
	defer c.browser.mu.Unlock()
	if c.browser.closed || c.browser.terminal != nil {
		return nil, &CDPError{Code: CDPErrorClosed, Op: "create page", Err: fmt.Errorf("browser closed concurrently")}
	}
	c.pages[targetID] = page
	c.browser.pages[targetID] = page
	c.browser.sessionMap[sessionID] = page
	return page, nil
}

// Close disposes the context and all owned pages.
func (c *BrowserContext) Close() error {
	return c.close(true)
}

func (c *BrowserContext) close(remote bool) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	pages := make([]*Page, 0, len(c.pages))
	for _, page := range c.pages {
		pages = append(pages, page)
	}
	c.mu.Unlock()
	var result error
	if remote && c.id != "" {
		err := c.browser.callCleanup("Target.disposeBrowserContext", browserContextParams{BrowserContextID: c.id})
		result = errors.Join(result, err)
	}
	for _, page := range pages {
		result = errors.Join(result, page.close(false))
	}
	c.browser.mu.Lock()
	delete(c.browser.contexts, c.id)
	c.browser.mu.Unlock()
	return result
}

// Page owns one target and flattened CDP session.
type Page struct {
	mu              sync.RWMutex
	targetID        string
	sessionID       string
	owner           *BrowserContext
	state           TargetState
	stateErr        string
	targetScriptErr string
	closeOnce       sync.Once
	closeErr        error
	removed         bool
	frameMu         sync.RWMutex
	frameSessions   map[string]string
	frameSub        *CDPSubscription
	targetScripts   TargetScriptConfig
}

func (p *Page) installPageScript(ctx context.Context) error {
	config := p.targetScripts
	if config.PageScript == "" {
		return nil
	}
	if err := p.Call(ctx, "Page.enable", nil, nil); err != nil {
		return fmt.Errorf("enable page pre-script domain: %w", err)
	}
	var result targetScriptResult
	if err := p.Call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]string{"source": config.PageScript}, &result); err != nil {
		return fmt.Errorf("install page pre-script: %w", err)
	}
	if result.Identifier == "" {
		return &CDPError{Code: CDPErrorProtocol, Op: "install page pre-script", Err: fmt.Errorf("empty script identifier")}
	}
	return nil
}

// TargetID returns the immutable CDP target ID.
func (p *Page) TargetID() string {
	return p.targetID
}

// SessionID returns the immutable flattened CDP session ID.
func (p *Page) SessionID() string {
	return p.sessionID
}

// BrowserContextID returns the owning isolated CDP browser-context ID.
func (p *Page) BrowserContextID() string {
	if p.owner == nil {
		return ""
	}
	return p.owner.ID()
}

// State returns the target lifecycle state.
func (p *Page) State() TargetState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

func (p *Page) setState(state TargetState, message string) {
	p.mu.Lock()
	p.state = state
	p.stateErr = message
	p.mu.Unlock()
}

// TargetScriptStatus reports the immutable pre-script version and the first
// child-target injection failure, if any. A child target error is surfaced
// separately from target lifecycle state so callers cannot mistake a usable
// page transport for a fully covered stealth session.
func (p *Page) TargetScriptStatus() (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.targetScriptErr == "" {
		return p.targetScripts.Version, nil
	}
	return p.targetScripts.Version, fmt.Errorf("target script injection: %s", p.targetScriptErr)
}

func (p *Page) setTargetScriptError(targetType string, err error) {
	if err == nil {
		return
	}
	p.mu.Lock()
	if p.targetScriptErr == "" {
		p.targetScriptErr = fmt.Sprintf("%s: %v", targetType, err)
	}
	p.mu.Unlock()
}

func (p *Page) ensureAttached() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.state != TargetStateAttached {
		return &TargetError{TargetID: p.targetID, State: p.state, Message: p.stateErr}
	}
	return nil
}

// Call invokes a target-session CDP method after lifecycle validation.
func (p *Page) Call(ctx context.Context, method string, params any, result any) error {
	if ctx == nil {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "page call", Err: fmt.Errorf("context required")}
	}
	if err := p.ensureAttached(); err != nil {
		return err
	}
	return p.owner.browser.transport.CallSession(ctx, p.sessionID, method, params, result)
}

// CallBrowser invokes a browser-session CDP method through this page's owner.
func (p *Page) CallBrowser(ctx context.Context, method string, params any, result any) error {
	if ctx == nil {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "browser call", Err: fmt.Errorf("context required")}
	}
	if err := p.ensureAttached(); err != nil {
		return err
	}
	if p.owner == nil || p.owner.browser == nil {
		return &TargetError{TargetID: p.targetID, State: p.State(), Message: "page has no browser owner"}
	}
	return p.owner.browser.transport.Call(ctx, method, params, result)
}

// NewSibling creates another page in the same isolated browser context.
func (p *Page) NewSibling(ctx context.Context, initialURL string) (*Page, error) {
	if p.owner == nil {
		return nil, &TargetError{TargetID: p.targetID, State: p.State(), Message: "page has no browser-context owner"}
	}
	return p.owner.NewPage(ctx, initialURL)
}

// ContextPages returns a target-ID ordered snapshot of live sibling pages.
func (p *Page) ContextPages() []*Page {
	if p.owner == nil {
		return nil
	}
	p.owner.mu.Lock()
	defer p.owner.mu.Unlock()
	pages := make([]*Page, 0, len(p.owner.pages))
	for _, page := range p.owner.pages {
		pages = append(pages, page)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].TargetID() < pages[j].TargetID() })
	return pages
}

// Activate brings this page target to the foreground.
func (p *Page) Activate(ctx context.Context) error {
	return p.CallBrowser(ctx, "Target.activateTarget", targetParams{TargetID: p.targetID}, &struct{}{})
}

func (p *Page) enableFrameRouting(ctx context.Context) error {
	sub, err := p.owner.browser.transport.Subscribe(128)
	if err != nil {
		return fmt.Errorf("subscribe frame targets: %w", err)
	}
	p.frameMu.Lock()
	p.frameSub = sub
	p.frameMu.Unlock()
	config := p.targetScripts
	params := map[string]any{"autoAttach": true, "waitForDebuggerOnStart": config.WorkerScript != "" || config.PageScript != "", "flatten": true}
	if err := p.Call(ctx, "Target.setAutoAttach", params, &struct{}{}); err != nil {
		sub.Close()
		return fmt.Errorf("enable iframe auto-attach: %w", err)
	}
	go p.monitorFrameTargets(sub)
	return nil
}

func (p *Page) monitorFrameTargets(sub *CDPSubscription) {
	for {
		select {
		case event, ok := <-sub.Events:
			if !ok {
				return
			}
			var payload struct {
				SessionID  string `json:"sessionId"`
				TargetInfo struct {
					TargetID string `json:"targetId"`
					Type     string `json:"type"`
				} `json:"targetInfo"`
			}
			if json.Unmarshal(event.Params, &payload) != nil {
				continue
			}
			switch event.Method {
			case "Target.attachedToTarget":
				p.frameMu.Lock()
				if payload.TargetInfo.Type == "iframe" && payload.TargetInfo.TargetID != "" && payload.SessionID != "" {
					p.frameSessions[payload.TargetInfo.TargetID] = payload.SessionID
				}
				p.frameMu.Unlock()
				p.initializeAttachedTarget(payload.SessionID, payload.TargetInfo.Type)
			case "Target.detachedFromTarget":
				p.frameMu.Lock()
				for frameID, sessionID := range p.frameSessions {
					if sessionID == payload.SessionID {
						delete(p.frameSessions, frameID)
					}
				}
				p.frameMu.Unlock()
			}
		case _, ok := <-sub.Errors:
			if !ok {
				return
			}
			return
		}
	}
}

func (p *Page) initializeAttachedTarget(sessionID, targetType string) {
	if sessionID == "" {
		return
	}
	config := p.targetScripts
	if config.PageScript == "" && config.WorkerScript == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	script := config.PageScript
	if strings.Contains(targetType, "worker") {
		script = config.WorkerScript
	}
	if script != "" {
		method := "Page.addScriptToEvaluateOnNewDocument"
		params := any(map[string]string{"source": script})
		if strings.Contains(targetType, "worker") {
			method = "Runtime.evaluate"
			params = targetScriptEvaluateParams{Expression: script}
		} else if err := p.owner.browser.transport.CallSession(ctx, sessionID, "Page.enable", nil, nil); err != nil {
			p.setTargetScriptError(targetType, err)
			return
		}
		if err := p.owner.browser.transport.CallSession(ctx, sessionID, method, params, nil); err != nil {
			p.setTargetScriptError(targetType, err)
			return
		}
	}
	if err := p.owner.browser.transport.CallSession(ctx, sessionID, "Runtime.runIfWaitingForDebugger", nil, nil); err != nil {
		p.setTargetScriptError(targetType, err)
	}
}

// FrameSessions returns the current OOPIF frame-to-session routing table.
func (p *Page) FrameSessions() map[string]string {
	p.frameMu.RLock()
	defer p.frameMu.RUnlock()
	out := make(map[string]string, len(p.frameSessions))
	for frameID, sessionID := range p.frameSessions {
		out[frameID] = sessionID
	}
	return out
}

// CallFrame invokes method through an OOPIF session when frameID is attached.
func (p *Page) CallFrame(ctx context.Context, frameID, method string, params, result any) error {
	if frameID == "" {
		return p.Call(ctx, method, params, result)
	}
	p.frameMu.RLock()
	sessionID := p.frameSessions[frameID]
	p.frameMu.RUnlock()
	if sessionID == "" {
		return p.Call(ctx, method, params, result)
	}
	if err := p.ensureAttached(); err != nil {
		return err
	}
	return p.owner.browser.transport.CallSession(ctx, sessionID, method, params, result)
}

// Navigate loads url and returns CDP frame/loader identity.
func (p *Page) Navigate(ctx context.Context, targetURL string) (frameID, loaderID string, err error) {
	if targetURL == "" {
		return "", "", fmt.Errorf("navigate: URL required")
	}
	var result struct {
		FrameID  string `json:"frameId"`
		LoaderID string `json:"loaderId"`
		Error    string `json:"errorText"`
	}
	if err := p.Call(ctx, "Page.navigate", navigateParams{URL: targetURL}, &result); err != nil {
		return "", "", err
	}
	if result.Error != "" {
		return result.FrameID, result.LoaderID, fmt.Errorf("navigate: %s", result.Error)
	}
	return result.FrameID, result.LoaderID, nil
}

// Close closes the target exactly once.
func (p *Page) Close() error {
	p.closeOnce.Do(func() {
		p.closeErr = p.close(true)
	})
	return p.closeErr
}

func (p *Page) close(remote bool) error {
	p.mu.Lock()
	if p.removed {
		p.mu.Unlock()
		return nil
	}
	previous := p.state
	p.removed = true
	p.state = TargetStateClosed
	p.stateErr = "closed by owner"
	p.mu.Unlock()
	p.frameMu.Lock()
	if p.frameSub != nil {
		p.frameSub.Close()
		p.frameSub = nil
	}
	p.frameSessions = make(map[string]string)
	p.frameMu.Unlock()
	var err error
	if remote && previous == TargetStateAttached {
		err = p.owner.browser.callCleanup("Target.closeTarget", targetParams{TargetID: p.targetID})
	}
	p.owner.mu.Lock()
	delete(p.owner.pages, p.targetID)
	p.owner.mu.Unlock()
	p.owner.browser.mu.Lock()
	delete(p.owner.browser.pages, p.targetID)
	delete(p.owner.browser.sessionMap, p.sessionID)
	p.owner.browser.mu.Unlock()
	p.owner.browser.releasePage()
	return err
}
