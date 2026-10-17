package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/tabs"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/security"
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

// defaultGracefulCloseTimeout bounds how long an owned browser is given to exit
// on its own after the CDP Browser.close request before the process-level
// SIGTERM/SIGKILL teardown takes over. A generous window lets the browser reclaim
// its own macOS code-sign clone under load instead of being force-killed and
// leaving the clone orphaned.
const defaultGracefulCloseTimeout = 10 * time.Second

// defaultContextAdmissionTimeout bounds Target.createBrowserContext, a
// millisecond control command. Missing its answer means the browser UI thread
// is wedged (observed on macOS: main thread blocked acquiring a RunningBoard
// App Nap assertion), so admission fails closed instead of hanging callers.
const defaultContextAdmissionTimeout = 15 * time.Second

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

type browserTargetInfo struct {
	TargetID         string `json:"targetId"`
	Type             string `json:"type"`
	BrowserContextID string `json:"browserContextId"`
}

type createTargetParams struct {
	URL              string `json:"url"`
	BrowserContextID string `json:"browserContextId,omitempty"`
	NewWindow        bool   `json:"newWindow,omitempty"`
}

type attachTargetParams struct {
	TargetID string `json:"targetId"`
	Flatten  bool   `json:"flatten"`
}

type navigateParams struct {
	URL      string `json:"url"`
	Referrer string `json:"referrer,omitempty"`
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
	// Emulation carries CDP-native overrides (Emulation.setUserAgentOverride,
	// setLocaleOverride, setTimezoneOverride). They are applied before the
	// first navigation so request headers (Accept-Language, Sec-CH-UA-*) and
	// navigator.userAgentData stay consistent without JS-side artifacts.
	Emulation EmulationOverrides
	// Referrer is sent on the initial navigation only — a believable entry
	// point (e.g. search) instead of a bare direct hit.
	Referrer string
}

// EmulationOverrides describes native CDP emulation for a page target.
type EmulationOverrides struct {
	UserAgent       string
	AcceptLanguage  string
	Locale          string
	TimezoneID      string
	Platform        string // client-hints platform, e.g. "macOS"
	PlatformVersion string
	Architecture    string
	ChromeVersion   string // full Chrome version for uaFullVersion
	Mobile          bool
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
	TargetID       string
	State          TargetState
	Message        string
	Compensated    bool
	OutcomeUnknown bool
	Err            error
}

func (e *TargetError) Error() string {
	if e.OutcomeUnknown {
		targetID := e.TargetID
		if targetID == "" {
			targetID = "unknown"
		}
		message := fmt.Sprintf("artemis target %s outcome is unknown: %s", targetID, e.Message)
		if e.Err != nil {
			return fmt.Sprintf("%s: %v", message, e.Err)
		}
		return message
	}
	targetID := e.TargetID
	if targetID == "" {
		targetID = "unknown"
	}
	message := fmt.Sprintf("artemis target %s is %s: %s", targetID, e.State, e.Message)
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", message, e.Err)
	}
	return message
}

func (e *TargetError) Unwrap() error { return e.Err }

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
	livePageLimit func() int
	pageSlots     int
	monitor       *CDPSubscription
	monitorDone   chan struct{}
	processDone   chan struct{}
	policy        *network.Policy
	policyProxy   *chromiumPolicyProxy
	targetScripts TargetScriptConfig
	closed        bool
	stopped       bool
	closeWarnings []string
	terminal      error
	closeOnce     sync.Once
	closeErr      error
	// admissionTimeout overrides defaultContextAdmissionTimeout when positive.
	admissionTimeout time.Duration
}

// ConnectChromium validates an external browser endpoint without taking process ownership.
func ConnectChromium(ctx context.Context, endpoint string) (*ChromiumBrowser, error) {
	return ConnectChromiumWithPolicy(ctx, endpoint, network.PolicyConfig{})
}

// ConnectChromiumWithPolicy validates an external browser endpoint and applies
// the supplied egress policy to every page created from the connection.
func ConnectChromiumWithPolicy(ctx context.Context, endpoint string, config network.PolicyConfig) (*ChromiumBrowser, error) {
	policy, err := network.NewPolicy(config, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create browser network policy: %w", err)
	}
	policy.SetRequestFilter(security.NewAdBlocker())
	return connectChromium(ctx, endpoint, nil, policy, nil)
}

// cloneSweepOnce runs the macOS code-sign clone janitor a single time per
// process, before the first owned browser launch, so repeated launch/teardown
// cycles cannot accumulate orphaned clone directories. It is off the per-launch
// latency path (guarded by sync.Once) and a no-op on non-darwin platforms.
var cloneSweepOnce sync.Once

// LaunchChromium launches and validates an owned Chromium process.
func LaunchChromium(ctx context.Context, config browserprocess.LaunchConfig) (*ChromiumBrowser, error) {
	cloneSweepOnce.Do(browserprocess.SweepOrphanCodeSignClones)
	policyConfig := network.PolicyConfig{
		AllowedPorts: config.AllowedPorts, AllowPrivateNetworks: config.AllowPrivateNetworks,
	}
	if config.NetworkPolicy != nil {
		policyConfig = *config.NetworkPolicy
	}
	policy, err := network.NewPolicy(policyConfig, nil, config.PolicyDecisionSink)
	if err != nil {
		return nil, fmt.Errorf("create browser network policy: %w", err)
	}
	if !config.DisableAdBlock {
		filter := config.AdBlock
		if filter == nil {
			filter = security.NewAdBlocker()
		}
		policy.SetRequestFilter(filter)
	}
	proxy, err := newChromiumPolicyProxy(ctx, policy)
	if err != nil {
		return nil, err
	}
	config.PolicyProxyURL = proxy.URL()
	processOwner, err := browserprocess.Launch(ctx, config)
	if err != nil {
		_ = proxy.Close()
		return nil, err
	}
	browser, err := connectChromium(ctx, processOwner.Endpoint(), processOwner, policy, proxy)
	if err != nil {
		_ = processOwner.Close()
		_ = proxy.Close()
		return nil, err
	}
	return browser, nil
}

func connectChromium(ctx context.Context, endpoint string, processOwner *browserprocess.Browser, policy *network.Policy, proxy *chromiumPolicyProxy) (*ChromiumBrowser, error) {
	if policy == nil {
		return nil, &CDPError{Code: CDPErrorInvalidConfig, Op: "connect browser", Err: fmt.Errorf("network policy required")}
	}
	// A just-launched browser can accept the WebSocket upgrade and then
	// close before the DevTools broker is fully wired — attaching to it
	// must not fail on that transient EOF. Bounded retries keep the
	// attach fast in the common case and resilient under load.
	var transport *CDPTransport
	var version BrowserVersion
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(50*attempt) * time.Millisecond):
			}
		}
		transport, err = DialCDPTransport(ctx, CDPTransportConfig{URL: endpoint})
		if err != nil {
			continue
		}
		if versionErr := transport.Call(ctx, "Browser.getVersion", nil, &version); versionErr != nil {
			_ = transport.Close()
			transport = nil
			err = fmt.Errorf("validate browser identity: %w", versionErr)
			continue
		}
		err = nil
		break
	}
	if transport == nil {
		return nil, err
	}
	browser := &ChromiumBrowser{
		transport: transport, process: processOwner, endpoint: endpoint, owned: processOwner != nil,
		contexts: make(map[string]*BrowserContext), pages: make(map[string]*Page), sessionMap: make(map[string]*Page),
		maxPages: defaultChromiumMaxPages, policy: policy, policyProxy: proxy,
	}
	browser.version = version
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
	browser.monitorDone = make(chan struct{})
	go func() {
		defer close(browser.monitorDone)
		browser.monitorTargets(monitor)
	}()
	if processOwner != nil {
		browser.processDone = make(chan struct{})
		go func() {
			defer close(browser.processDone)
			browser.monitorProcess(processOwner)
		}()
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

// ProcessWarnings returns detached launch-policy and shutdown warning codes.
func (b *ChromiumBrowser) ProcessWarnings() []string {
	var warnings []string
	if b.process != nil {
		warnings = b.process.Warnings()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append(warnings, b.closeWarnings...)
}

// Stopped reports verified cleanup of this runtime's owned resources.
// An externally attached Chromium process remains outside that ownership.
func (b *ChromiumBrowser) Stopped() bool {
	if b == nil {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.stopped
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

// SetLivePageLimit installs an embedder authority consulted on every page
// admission. A positive result tightens the static SetMaxPages bound, so a
// pressure change refuses new pages at once while admitted pages keep
// running; zero or negative leaves the static bound. Nil removes it.
func (b *ChromiumBrowser) SetLivePageLimit(limit func() int) {
	b.mu.Lock()
	b.livePageLimit = limit
	b.mu.Unlock()
}

func (b *ChromiumBrowser) reservePage() error {
	b.mu.RLock()
	live := b.livePageLimit
	b.mu.RUnlock()
	liveLimit := 0
	if live != nil {
		liveLimit = live()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.terminal != nil {
		return &CDPError{Code: CDPErrorClosed, Op: "reserve page", Err: fmt.Errorf("browser is closed")}
	}
	limit := b.maxPages
	if liveLimit > 0 && liveLimit < limit {
		limit = liveLimit
	}
	if b.pageSlots >= limit {
		return &CDPError{Code: CDPErrorOverloaded, Op: "reserve page", Err: fmt.Errorf("page limit %d reached", limit)}
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

// transportErr prefers the terminal process failure (with Chrome output)
// over the bare transport-closed error so callers see why the browser died.
func (b *ChromiumBrowser) transportErr(err error) error {
	if b == nil || err == nil {
		return err
	}
	if termErr := b.Err(); termErr != nil {
		return termErr
	}
	return err
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
	if !b.closed && b.terminal == nil {
		b.terminal = processOwner.Err()
		if b.terminal == nil {
			b.terminal = &browserprocess.Error{Code: browserprocess.ErrorBrowserCrash, Op: "supervise running browser", Err: fmt.Errorf("process exited unexpectedly")}
		}
	}
	terminal := b.terminal
	b.mu.Unlock()
	if terminal != nil {
		_ = b.transport.Close()
		if b.policyProxy != nil {
			_ = b.policyProxy.Close()
		}
	}
}

// failUnresponsive records a wedged browser as terminal, fails every pending
// and future call through the closed transport and terminates an owned process.
// Recovery stays an explicit relaunch (ReconnectExplicitRelaunch).
func (b *ChromiumBrowser) failUnresponsive(op string, cause error) error {
	b.mu.Lock()
	if b.terminal == nil && !b.closed {
		b.terminal = &CDPError{Code: CDPErrorUnresponsive, Op: op, Err: cause}
	}
	terminal := b.terminal
	b.mu.Unlock()
	if terminal == nil {
		terminal = cause
	}
	result := errors.Join(terminal, b.transport.Close())
	if b.owned && b.process != nil {
		result = errors.Join(result, b.process.Close())
	}
	return result
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
	timeout := b.admissionTimeout
	if timeout <= 0 {
		timeout = defaultContextAdmissionTimeout
	}
	admissionCtx, cancel := context.WithTimeout(ctx, timeout)
	err := b.transport.Call(admissionCtx, "Target.createBrowserContext", createBrowserContextParams{}, &result)
	cancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			err = b.failUnresponsive("create browser context", err)
		}
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

func (b *ChromiumBrowser) closeTarget(ctx context.Context, targetID string) error {
	if ctx == nil {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "close target", Err: fmt.Errorf("context required")}
	}
	if targetID == "" {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "close target", Err: fmt.Errorf("target ID required")}
	}
	if b == nil || b.transport == nil {
		return &CDPError{Code: CDPErrorClosed, Op: "close target", Err: fmt.Errorf("browser transport unavailable")}
	}
	var result struct {
		Success bool `json:"success"`
	}
	if err := b.transport.Call(ctx, "Target.closeTarget", targetParams{TargetID: targetID}, &result); err != nil {
		return fmt.Errorf("close target %s: %w", targetID, err)
	}
	if !result.Success {
		return &CDPError{Code: CDPErrorProtocol, Op: "close target", Err: fmt.Errorf("browser returned success=false for target %s", targetID)}
	}
	return nil
}

func (b *ChromiumBrowser) closeTargetCleanup(targetID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCDPDialTimeout)
	closeErr := b.closeTarget(ctx, targetID)
	cancel()
	if closeErr == nil {
		return nil
	}
	exists, reconcileErr := b.targetExistsCleanup(targetID)
	if reconcileErr != nil {
		return errors.Join(closeErr, fmt.Errorf("reconcile target close: %w", reconcileErr))
	}
	if !exists {
		return nil
	}
	return closeErr
}

func (b *ChromiumBrowser) listTargetInfos(ctx context.Context) ([]browserTargetInfo, error) {
	if ctx == nil {
		return nil, &CDPError{Code: CDPErrorInvalidConfig, Op: "list targets", Err: fmt.Errorf("context required")}
	}
	if b == nil || b.transport == nil {
		return nil, &CDPError{Code: CDPErrorClosed, Op: "list targets", Err: fmt.Errorf("browser transport unavailable")}
	}
	var result struct {
		TargetInfos *[]browserTargetInfo `json:"targetInfos"`
	}
	if err := b.transport.Call(ctx, "Target.getTargets", nil, &result); err != nil {
		return nil, fmt.Errorf("list browser targets: %w", err)
	}
	if result.TargetInfos == nil {
		return nil, &CDPError{Code: CDPErrorProtocol, Op: "list targets", Err: fmt.Errorf("targetInfos missing")}
	}
	targets := append([]browserTargetInfo(nil), (*result.TargetInfos)...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].TargetID < targets[j].TargetID })
	for index, target := range targets {
		if target.TargetID == "" {
			return nil, &CDPError{Code: CDPErrorProtocol, Op: "list targets", Err: fmt.Errorf("targetInfos contains empty targetId")}
		}
		if index > 0 && targets[index-1].TargetID == target.TargetID {
			return nil, &CDPError{Code: CDPErrorProtocol, Op: "list targets", Err: fmt.Errorf("targetInfos repeats targetId %s", target.TargetID)}
		}
	}
	return targets, nil
}

func (b *ChromiumBrowser) targetExists(ctx context.Context, targetID string) (bool, error) {
	targets, err := b.listTargetInfos(ctx)
	if err != nil {
		return false, err
	}
	for _, target := range targets {
		if target.TargetID == targetID {
			return true, nil
		}
	}
	return false, nil
}

func (b *ChromiumBrowser) targetExistsCleanup(targetID string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCDPDialTimeout)
	defer cancel()
	return b.targetExists(ctx, targetID)
}

func (b *ChromiumBrowser) ensureOpen() error {
	if !b.Healthy() {
		return &CDPError{Code: CDPErrorClosed, Op: "browser", Err: fmt.Errorf("browser is closed")}
	}
	return nil
}

func (b *ChromiumBrowser) monitorTargets(subscription *CDPSubscription) {
	ctx, cancel := context.WithCancel(context.Background())
	jobs := make(chan popupCloseJob, 32)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		for job := range jobs {
			if ctx.Err() != nil {
				return
			}
			b.closePopup(ctx, job)
			b.closeFailedPopupContexts(ctx)
		}
	}()
	defer func() {
		cancel()
		close(jobs)
		<-workerDone
		cleanupCtx, finish := context.WithTimeout(context.Background(), 5*time.Second)
		defer finish()
		b.closeFailedPopupContexts(cleanupCtx)
	}()
	for {
		select {
		case event, ok := <-subscription.Events:
			if !ok {
				cancel()
				var cause error
				select {
				case cause = <-subscription.Errors:
				default:
				}
				b.failTargetMonitoring(cause)
				return
			}
			if job, popup := b.popupCloseForEvent(event); popup {
				select {
				case jobs <- job:
				default:
					job.owner.revokePopupAdmission(fmt.Errorf("popup %s close mailbox full", job.targetID))
				}
			}
			b.applyTargetEvent(event)
		case err := <-subscription.Errors:
			cancel()
			b.failTargetMonitoring(err)
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
	if len(event.Params) > 0 {
		if err := json.Unmarshal(event.Params, &payload); err != nil {
			return
		}
	}
	// Flattened session-scoped events (e.g. Inspector.targetCrashed) carry
	// the session id on the envelope, not inside params.
	sessionID := payload.SessionID
	if sessionID == "" {
		sessionID = event.SessionID
	}
	b.mu.RLock()
	page := b.pages[payload.TargetID]
	if page == nil && sessionID != "" {
		page = b.sessionMap[sessionID]
	}
	b.mu.RUnlock()
	if page == nil {
		return
	}
	switch event.Method {
	case "Target.targetCrashed", "Inspector.targetCrashed":
		page.setState(TargetStateCrashed, fmt.Sprintf("status=%s errorCode=%d", payload.Status, payload.ErrorCode))
	case "Target.detachedFromTarget":
		page.setState(TargetStateDetached, "target session detached")
	case "Target.targetDestroyed":
		page.setState(TargetStateClosed, "target destroyed")
	}
	destroyed := event.Method == "Target.targetDestroyed"
	if destroyed {
		_ = page.close(false)
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
	var contextErr error
	for _, browserContext := range contexts {
		contextErr = errors.Join(contextErr, browserContext.close(true))
	}
	if b.monitor != nil {
		b.monitor.Close()
	}
	if b.monitorDone != nil {
		<-b.monitorDone
	}
	var requestErr error
	var gracefulErr error
	var warning string
	if b.owned {
		closeCtx, cancel := context.WithTimeout(context.Background(), defaultCDPDialTimeout)
		err := b.transport.Call(closeCtx, "Browser.close", nil, nil)
		gracefulErr = err
		cancel()
		if errors.Is(err, context.DeadlineExceeded) {
			warning = "chromium_graceful_close_timed_out"
		} else if err != nil && !IsCDPError(err, CDPErrorClosed) {
			requestErr = fmt.Errorf("request browser close: %w", err)
		}
		if b.process != nil && err == nil {
			select {
			case <-b.process.Exited():
			case <-time.After(defaultGracefulCloseTimeout):
				warning = "chromium_graceful_exit_timed_out"
			}
		}
	}
	cleanupErr := b.transport.Close()
	if b.process != nil {
		processErr := b.process.Close()
		cleanupErr = errors.Join(cleanupErr, processErr)
		if processErr == nil && b.processDone != nil {
			<-b.processDone
		}
	}
	if b.policyProxy != nil {
		cleanupErr = errors.Join(cleanupErr, b.policyProxy.Close())
	}
	stopped := cleanupErr == nil && (b.owned || contextErr == nil)
	if !stopped && errors.Is(gracefulErr, context.DeadlineExceeded) {
		requestErr = errors.Join(requestErr, fmt.Errorf("request browser close: %w", gracefulErr))
	}
	b.mu.Lock()
	b.stopped = stopped
	if warning != "" {
		b.closeWarnings = append(b.closeWarnings, warning)
	}
	b.mu.Unlock()
	if warning != "" {
		slog.Warn("Chromium graceful shutdown required owned cleanup", slog.String("reason", warning), slog.Bool("stopped", stopped))
	}
	return errors.Join(contextErr, requestErr, cleanupErr)
}

// BrowserContext owns pages inside one isolated CDP browser context.
type BrowserContext struct {
	mu           sync.Mutex
	closeMu      sync.Mutex
	id           string
	browser      *ChromiumBrowser
	pages        map[string]*Page
	closed       bool
	disposed     bool
	executor     *tabs.TabExecutor
	popups       *tabs.PopupBlocker
	popupErr     error
	tabProcesses TabProcessConfig
	downloadSlot chan struct{}
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
		cleanupErr := c.browser.closeTargetCleanup(targetID)
		return nil, newTargetCreationError(targetID, TargetStateDetached, "attach failed", err, cleanupErr)
	}
	page, err := c.registerPage(targetID, sessionID, scripts)
	if err != nil {
		cleanupErr := c.browser.closeTargetCleanup(targetID)
		return nil, newTargetCreationError(targetID, TargetStateDetached, "page registration failed", err, cleanupErr)
	}
	// The registered page owns the slot, including every subsequent failure.
	keepSlot = true
	exists, err := c.browser.targetExists(ctx, targetID)
	if err != nil {
		cleanupErr := page.close(true)
		return nil, newTargetCreationError(targetID, page.State(), "target liveness could not be verified after registration", err, cleanupErr)
	}
	if !exists {
		cleanupErr := page.close(false)
		return nil, newTargetCreationError(targetID, TargetStateClosed, "target disappeared before page initialization", fmt.Errorf("target is absent from Target.getTargets"), cleanupErr)
	}
	if err := page.registerSupervisor(ctx); err != nil {
		cleanupErr := page.close(true)
		return nil, newTargetCreationError(targetID, page.State(), "tab supervision registration failed", err, cleanupErr)
	}
	if err := page.installPageScript(ctx); err != nil {
		cleanupErr := page.close(true)
		return nil, newTargetCreationError(targetID, page.State(), "page script setup failed", err, cleanupErr)
	}
	if err := page.applyEmulationOverrides(ctx); err != nil {
		cleanupErr := page.close(true)
		return nil, newTargetCreationError(targetID, page.State(), "page emulation setup failed", err, cleanupErr)
	}
	if err := page.enableFrameRouting(ctx); err != nil {
		cleanupErr := page.close(true)
		return nil, newTargetCreationError(targetID, page.State(), "page routing setup failed", err, cleanupErr)
	}
	if targetURL != "about:blank" {
		if _, _, err := page.Navigate(ctx, targetURL); err != nil {
			cleanupErr := page.close(true)
			return nil, newTargetCreationError(targetID, page.State(), "initial navigation failed", err, cleanupErr)
		}
	}
	if state := page.State(); state != TargetStateAttached {
		cleanupErr := page.close(state != TargetStateClosed)
		cause := &TargetError{TargetID: targetID, State: state, Message: "target stopped during page initialization"}
		return nil, newTargetCreationError(targetID, state, "target stopped during page initialization", cause, cleanupErr)
	}
	return page, nil
}

func newTargetCreationError(targetID string, state TargetState, message string, cause, cleanupErr error) *TargetError {
	compensated := cleanupErr == nil
	if compensated {
		state = TargetStateClosed
	} else {
		cause = errors.Join(cause, fmt.Errorf("target cleanup failed: %w", cleanupErr))
	}
	return &TargetError{TargetID: targetID, State: state, Message: message, Compensated: compensated, Err: cause}
}

func targetCreateOutcomeUnknown(err error) bool {
	var cdpErr *CDPError
	if !errors.As(err, &cdpErr) {
		return true
	}
	if cdpErr.Code == CDPErrorInvalidConfig || cdpErr.Code == CDPErrorOverloaded || cdpErr.Op == "reserve" || cdpErr.Op == "marshal command" || cdpErr.Op == "remote" {
		return false
	}
	return true
}

func (c *BrowserContext) createTarget(ctx context.Context, initialURL string) (string, error) {
	if ctx == nil {
		return "", &CDPError{Code: CDPErrorInvalidConfig, Op: "create target", Err: fmt.Errorf("context required")}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var target struct {
		ID string `json:"targetId"`
	}
	// Each page gets its own window: a page that is a background tab of a
	// shared window produces no frames, so input dispatched to it would
	// wait for an acknowledgement that never comes.
	params := createTargetParams{URL: initialURL, BrowserContextID: c.id, NewWindow: true}
	// Creation is an irreversible browser effect. Once dispatched, wait for its
	// bounded receipt so cancellation cannot discard the only target identity.
	commandCtx, cancel := context.WithTimeout(context.Background(), defaultCDPDialTimeout)
	defer cancel()
	if err := c.browser.transport.Call(commandCtx, "Target.createTarget", params, &target); err != nil {
		transportErr := c.browser.transportErr(err)
		if targetCreateOutcomeUnknown(err) {
			return "", &TargetError{Message: "create target response was lost; browser effect may have occurred without a target ID", OutcomeUnknown: true, Err: transportErr}
		}
		return "", fmt.Errorf("create target: %w", transportErr)
	}
	if target.ID == "" {
		return "", &TargetError{Message: "create target receipt omitted the target ID; browser effect may have occurred", OutcomeUnknown: true, Err: &CDPError{Code: CDPErrorProtocol, Op: "create target", Err: fmt.Errorf("empty targetId")}}
	}
	if err := ctx.Err(); err != nil {
		cleanupErr := c.browser.closeTargetCleanup(target.ID)
		return "", newTargetCreationError(target.ID, TargetStateAttached, "creation canceled after target creation", err, cleanupErr)
	}
	return target.ID, nil
}

func (c *BrowserContext) attachTarget(ctx context.Context, targetID string) (string, error) {
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.browser.transport.Call(ctx, "Target.attachToTarget", attachTargetParams{TargetID: targetID, Flatten: true}, &attached); err != nil {
		return "", fmt.Errorf("attach target: %w", c.browser.transportErr(err))
	}
	if attached.SessionID == "" {
		return "", &CDPError{Code: CDPErrorProtocol, Op: "attach target", Err: fmt.Errorf("empty sessionId")}
	}
	return attached.SessionID, nil
}

func (c *BrowserContext) registerPage(targetID, sessionID string, scripts TargetScriptConfig) (*Page, error) {
	lifetime, cancel := context.WithCancel(context.Background())
	page := &Page{
		targetID: targetID, sessionID: sessionID, owner: c, state: TargetStateAttached, createdAt: time.Now(),
		frameSessions: make(map[string]string), childSessions: make(map[string]string), targetScripts: scripts,
		lifetime: lifetime, cancelLifetime: cancel,
		dialogs:     tabs.NewDialogHandler(),
		cleanupDone: make(chan struct{}),
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		cancel()
		return nil, &CDPError{Code: CDPErrorClosed, Op: "create page", Err: fmt.Errorf("browser context closed concurrently")}
	}
	config := c.tabProcesses
	if config.Kind == "" {
		config.Kind = tabs.ProcessSpecStream
	}
	if config.Owner == "" {
		config.Owner = tabs.OwnerRefTurn
	}
	if config.PriorityLane == "" {
		config.PriorityLane = "P3"
	}
	page.processInfo = TabProcessInfo{ID: "browser_tab:" + c.id + ":" + targetID, TargetID: targetID, ContextID: c.id,
		Spec: tabs.DefaultProcessSpec(config.Kind, config.PriorityLane), Owner: config.Owner}
	page.processSupervisor = config.Supervisor
	page.lifecycle = tabs.NewTabLifecycle(page.processInfo.Spec, config.Owner, cancel)
	c.browser.mu.Lock()
	defer c.browser.mu.Unlock()
	if c.browser.closed || c.browser.terminal != nil {
		cancel()
		return nil, &CDPError{Code: CDPErrorClosed, Op: "create page", Err: fmt.Errorf("browser closed concurrently")}
	}
	c.pages[targetID] = page
	c.browser.pages[targetID] = page
	c.browser.sessionMap[sessionID] = page
	context.AfterFunc(lifetime, page.retireAfterDown)
	return page, nil
}

// Close disposes the context and all owned pages.
func (c *BrowserContext) Close() error {
	return c.close(true)
}

func (c *BrowserContext) close(remote bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCDPDialTimeout)
	defer cancel()
	return c.closeWithContext(ctx, remote)
}

func (c *BrowserContext) closeWithContext(ctx context.Context, remote bool) error {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !c.closeMu.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	defer c.closeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.closeContext(ctx, remote)
}

func (c *BrowserContext) closeContext(ctx context.Context, remote bool) error {
	c.mu.Lock()
	c.closed = true
	pages := make([]*Page, 0, len(c.pages))
	for _, page := range c.pages {
		pages = append(pages, page)
	}
	disposed := c.disposed
	c.mu.Unlock()
	sort.Slice(pages, func(i, j int) bool { return pages[i].TargetID() < pages[j].TargetID() })
	for _, page := range pages {
		if page.cancelLifetime != nil {
			page.cancelLifetime()
		}
	}
	var result error
	if remote && c.id != "" && !disposed {
		if err := c.browser.transport.Call(ctx, "Target.disposeBrowserContext", browserContextParams{BrowserContextID: c.id}, nil); err != nil {
			exists, reconcileErr := c.browser.browserContextExists(ctx, c.id)
			if reconcileErr != nil {
				result = errors.Join(fmt.Errorf("dispose browser context %s: %w", c.id, err), fmt.Errorf("reconcile browser context disposal: %w", reconcileErr))
			} else if exists {
				result = fmt.Errorf("dispose browser context %s: %w", c.id, err)
			}
			if result != nil {
				for _, page := range pages {
					page.setState(TargetStateDetached, "browser context disposal unconfirmed")
				}
				return result
			}
		}
		c.mu.Lock()
		c.disposed = true
		c.mu.Unlock()
	}
	for _, page := range pages {
		if remote && c.id == "" {
			result = errors.Join(result, page.closeWithContext(ctx, true))
		} else {
			result = errors.Join(result, page.closeWithContext(ctx, false))
		}
	}
	c.mu.Lock()
	remaining := len(c.pages)
	c.mu.Unlock()
	if remaining > 0 {
		return errors.Join(result, fmt.Errorf("browser context close retained %d page(s)", remaining))
	}
	c.browser.mu.Lock()
	delete(c.browser.contexts, c.id)
	c.browser.mu.Unlock()
	return result
}

func (b *ChromiumBrowser) browserContextExists(ctx context.Context, contextID string) (bool, error) {
	var result struct {
		BrowserContextIDs *[]string `json:"browserContextIds"`
	}
	if err := b.transport.Call(ctx, "Target.getBrowserContexts", nil, &result); err != nil {
		return false, fmt.Errorf("list browser contexts: %w", err)
	}
	if result.BrowserContextIDs == nil {
		return false, &CDPError{Code: CDPErrorProtocol, Op: "list browser contexts", Err: fmt.Errorf("browserContextIds missing")}
	}
	for _, id := range *result.BrowserContextIDs {
		if id == contextID {
			return true, nil
		}
	}
	return false, nil
}

// Page owns one target and flattened CDP session.
type Page struct {
	mu              sync.RWMutex
	lifecycleMu     sync.Mutex
	targetID        string
	sessionID       string
	owner           *BrowserContext
	state           TargetState
	stateErr        string
	targetScriptErr string
	removed         bool
	createdAt       time.Time
	lastActive      time.Time
	frameMu         sync.RWMutex
	frameSessions   map[string]string
	childSessions   map[string]string
	frameSub        *CDPSubscription
	frameCancel     context.CancelFunc
	frameDone       chan struct{}
	fetchMu         sync.RWMutex
	fetchHandler    FetchRequestHandler
	adSite          atomic.Pointer[string]
	adBypass        atomic.Bool
	adBlocked       atomic.Int64
	targetScripts   TargetScriptConfig
	navigated       bool
	lifetime        context.Context
	cancelLifetime  context.CancelFunc
	// executionCancels holds every admitted execution lease's cancellation
	// so target retirement can cancel them synchronously before DownSignal.
	executionMu       sync.Mutex
	executionSeq      uint64
	executionCancels  map[uint64]context.CancelFunc
	dialogs           *tabs.DialogHandler
	lifecycle         *tabs.TabLifecycle
	transitionMu      sync.Mutex
	cleanupMu         sync.Mutex
	cleanupOnce       sync.Once
	cleanups          []func() error
	resourcesRetired  bool
	cleanupDone       chan struct{}
	cleanupErr        error
	retirementErr     error
	processInfo       TabProcessInfo
	processSupervisor TabProcessSupervisor
	supervised        bool
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

// applyEmulationOverrides installs CDP-native identity overrides before the
// first caller-controlled navigation. Unlike the JS pre-script these change
// real browser behavior — request headers, navigator.userAgentData, Date/Intl
// timezone — so there is no script artifact to fingerprint.
func (p *Page) applyEmulationOverrides(ctx context.Context) error {
	e := p.targetScripts.Emulation
	if e.UserAgent != "" {
		params := map[string]any{"userAgent": e.UserAgent}
		// Chrome requires platformVersion + architecture + model as an
		// all-or-nothing trio — only send userAgentMetadata when the full
		// measured identity exists; a bare UA override still carries
		// acceptLanguage and drops the Headless token.
		if e.ChromeVersion != "" && e.Platform != "" && e.PlatformVersion != "" && e.Architecture != "" {
			major := e.ChromeVersion
			if idx := strings.IndexByte(major, '.'); idx >= 0 {
				major = major[:idx]
			}
			params["userAgentMetadata"] = map[string]any{
				"brands": []map[string]string{
					{"brand": "Not_A Brand", "version": "8"},
					{"brand": "Chromium", "version": major},
					{"brand": "Google Chrome", "version": major},
				},
				"fullVersionList": []map[string]string{
					{"brand": "Not_A Brand", "version": "8.0.0.0"},
					{"brand": "Chromium", "version": e.ChromeVersion},
					{"brand": "Google Chrome", "version": e.ChromeVersion},
				},
				"fullVersion":     e.ChromeVersion,
				"platform":        e.Platform,
				"platformVersion": e.PlatformVersion,
				"architecture":    e.Architecture,
				"model":           "",
				"mobile":          e.Mobile,
			}
		}
		if e.AcceptLanguage != "" {
			params["acceptLanguage"] = e.AcceptLanguage
		}
		if err := p.Call(ctx, "Emulation.setUserAgentOverride", params, nil); err != nil {
			return fmt.Errorf("apply user agent override: %w", err)
		}
	}
	if e.Locale != "" {
		if err := p.Call(ctx, "Emulation.setLocaleOverride", map[string]any{"locale": e.Locale}, nil); err != nil {
			return fmt.Errorf("apply locale override: %w", err)
		}
	}
	if e.TimezoneID != "" {
		if err := p.Call(ctx, "Emulation.setTimezoneOverride", map[string]any{"timezoneId": e.TimezoneID}, nil); err != nil {
			return fmt.Errorf("apply timezone override: %w", err)
		}
	}
	return nil
}

// TargetID returns the immutable CDP target ID.
func (p *Page) TargetID() string {
	return p.targetID
}

// CurrentURL returns the URL the browser reports for this target now.
func (p *Page) CurrentURL(ctx context.Context) (string, error) {
	var result struct {
		TargetInfo struct {
			URL string `json:"url"`
		} `json:"targetInfo"`
	}
	if err := p.CallBrowser(ctx, "Target.getTargetInfo", targetParams{TargetID: p.targetID}, &result); err != nil {
		return "", fmt.Errorf("target %s url: %w", p.targetID, err)
	}
	return result.TargetInfo.URL, nil
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

// LifecycleTimes reports only timestamps observed from page creation and
// successful explicit activation. Inspection alone does not count as activity.
func (p *Page) LifecycleTimes() (time.Time, time.Time) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.createdAt, p.lastActive
}

// RecordActivity advances lastActive after a completed browser operation.
func (p *Page) RecordActivity(at time.Time) bool {
	if p == nil || at.IsZero() {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.removed || p.state == TargetStateClosed {
		return false
	}
	if at.After(p.lastActive) {
		p.lastActive = at
	}
	return true
}

func (p *Page) setState(state TargetState, message string) {
	p.mu.Lock()
	if p.removed || p.state == TargetStateClosed {
		p.mu.Unlock()
		return
	}
	p.state = state
	p.stateErr = message
	p.mu.Unlock()
	if state != TargetStateAttached {
		next := tabs.TabLifecycleLost
		if state == TargetStateClosed {
			next = tabs.TabLifecycleClosed
		}
		// Retire execution before publishing the down transition: an observer
		// of DownSignal must never see a live execution lease on a dead target.
		if p.cancelLifetime != nil {
			p.cancelLifetime()
		}
		p.cancelExecutions()
		p.cancelFrameRouting()
		if err := p.transitionLifecycle(next); err != nil {
			p.mu.Lock()
			p.retirementErr = errors.Join(p.retirementErr, err)
			p.mu.Unlock()
		}
	}
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

// ContextPages returns a target-ID ordered snapshot of locally tracked sibling pages.
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

// ContextPagesContext reconciles locally tracked pages against one authoritative
// browser target snapshot before returning the surviving pages.
func (p *Page) ContextPagesContext(ctx context.Context) ([]*Page, error) {
	if ctx == nil {
		return nil, &CDPError{Code: CDPErrorInvalidConfig, Op: "list context pages", Err: fmt.Errorf("context required")}
	}
	if p == nil || p.owner == nil || p.owner.browser == nil {
		return nil, &TargetError{Message: "page or browser-context owner unavailable"}
	}
	owner := p.owner
	owner.mu.Lock()
	pages := make([]*Page, 0, len(owner.pages))
	for _, page := range owner.pages {
		pages = append(pages, page)
	}
	owner.mu.Unlock()

	targets, err := owner.browser.listTargetInfos(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]browserTargetInfo, len(targets))
	for _, target := range targets {
		byID[target.TargetID] = target
	}
	live := make([]*Page, 0, len(pages))
	for _, page := range pages {
		if page.State() == TargetStateClosed {
			continue
		}
		target, exists := byID[page.TargetID()]
		if !exists {
			if err := page.close(false); err != nil {
				return nil, fmt.Errorf("retire absent target %s: %w", page.TargetID(), err)
			}
			continue
		}
		if target.Type != "page" || target.BrowserContextID != owner.id {
			return nil, &TargetError{TargetID: page.TargetID(), State: page.State(), Message: "target listing does not match its owning browser context"}
		}
		live = append(live, page)
	}
	sort.Slice(live, func(i, j int) bool { return live[i].TargetID() < live[j].TargetID() })
	return live, nil
}

// Activate brings this page target to the foreground.
func (p *Page) Activate(ctx context.Context) error {
	if ctx == nil {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "activate target", Err: fmt.Errorf("context required")}
	}
	if p == nil {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "activate target", Err: fmt.Errorf("page required")}
	}
	if err := p.lockLifecycle(ctx); err != nil {
		return fmt.Errorf("wait for page lifecycle: %w", err)
	}
	defer p.lifecycleMu.Unlock()
	p.mu.RLock()
	if p.removed || p.state == TargetStateClosed {
		state := p.state
		p.mu.RUnlock()
		return &TargetError{TargetID: p.targetID, State: state, Message: "target is closed"}
	}
	p.mu.RUnlock()
	if err := p.CallBrowser(ctx, "Target.activateTarget", targetParams{TargetID: p.targetID}, &struct{}{}); err != nil {
		return err
	}
	p.RecordActivity(time.Now())
	return nil
}

func (p *Page) lockLifecycle(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.lifecycleMu.TryLock() {
		if err := ctx.Err(); err != nil {
			p.lifecycleMu.Unlock()
			return err
		}
		return nil
	}
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if p.lifecycleMu.TryLock() {
				if err := ctx.Err(); err != nil {
					p.lifecycleMu.Unlock()
					return err
				}
				return nil
			}
			timer.Reset(time.Millisecond)
		}
	}
}

func (p *Page) enableFrameRouting(ctx context.Context) error {
	sub, err := p.owner.browser.transport.Subscribe(128)
	if err != nil {
		return fmt.Errorf("subscribe frame targets: %w", err)
	}
	if err := p.startFrameMonitor(sub); err != nil {
		sub.Close()
		return err
	}
	if err := p.Call(ctx, "Page.enable", nil, nil); err != nil {
		p.stopFrameRouting()
		return fmt.Errorf("enable page lifecycle and dialog events: %w", err)
	}
	if err := p.enableSessionPolicy(ctx, p.sessionID); err != nil {
		p.stopFrameRouting()
		return fmt.Errorf("enable page network policy: %w", err)
	}
	params := map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}
	if err := p.Call(ctx, "Target.setAutoAttach", params, &struct{}{}); err != nil {
		p.stopFrameRouting()
		return fmt.Errorf("enable iframe auto-attach: %w", err)
	}
	return nil
}

func (p *Page) monitorFrameTargets(ctx context.Context, sub *CDPSubscription) {
	ctx, cancel := context.WithCancel(ctx)
	jobs := make(chan fetchRequestJob, policyRequestQueue)
	var workers sync.WaitGroup
	for range policyRequestWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			p.runPolicyRequestWorker(ctx, jobs)
		}()
	}
	defer func() {
		cancel()
		sub.Close()
		close(jobs)
		workers.Wait()
		for job := range jobs {
			p.abortPausedJob(job)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Events:
			if !ok || ctx.Err() != nil {
				return
			}
			if err := p.captureDialogEvent(event); err != nil {
				p.setState(TargetStateDetached, err.Error())
				return
			}
			if !p.handleFetchEvent(event, jobs) {
				return
			}
			if event.Method != "Target.attachedToTarget" && event.Method != "Target.detachedFromTarget" {
				continue
			}
			if !p.ownsPolicySession(event.SessionID) {
				continue
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
				if payload.SessionID != "" {
					p.childSessions[payload.SessionID] = payload.TargetInfo.Type
				}
				if payload.TargetInfo.Type == "iframe" && payload.TargetInfo.TargetID != "" && payload.SessionID != "" {
					p.frameSessions[payload.TargetInfo.TargetID] = payload.SessionID
				}
				p.frameMu.Unlock()
				p.initializeAttachedTarget(ctx, payload.SessionID, payload.TargetInfo.Type)
			case "Target.detachedFromTarget":
				p.dialogs.ClearSession(p.targetID, payload.SessionID)
				p.frameMu.Lock()
				delete(p.childSessions, payload.SessionID)
				for frameID, sessionID := range p.frameSessions {
					if sessionID == payload.SessionID {
						delete(p.frameSessions, frameID)
					}
				}
				p.frameMu.Unlock()
			}
		case err, ok := <-sub.Errors:
			if !ok {
				return
			}
			p.failNetworkEnforcement(fmt.Errorf("frame target subscription: %w", err))
			return
		}
	}
}

func (p *Page) initializeAttachedTarget(ctx context.Context, sessionID, targetType string) {
	if sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, policyRequestTimeout)
	defer cancel()
	workerTarget := strings.Contains(targetType, "worker")
	if !workerTarget {
		if err := p.enableSessionPolicy(ctx, sessionID); err != nil {
			p.failNetworkEnforcement(fmt.Errorf("initialize %s target policy: %w", targetType, err))
			return
		}
	}
	if targetType == "iframe" {
		params := map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}
		if err := p.owner.browser.transport.CallSession(ctx, sessionID, "Target.setAutoAttach", params, nil); err != nil {
			p.failNetworkEnforcement(fmt.Errorf("initialize nested target policy: %w", err))
			return
		}
	}
	config := p.targetScripts
	script := config.PageScript
	if workerTarget {
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

// CallFrame invokes method through the frame's owning renderer session.
func (p *Page) CallFrame(ctx context.Context, frameID, method string, params, result any) error {
	if frameID == "" {
		return p.Call(ctx, method, params, result)
	}
	if err := p.ensureAttached(); err != nil {
		return err
	}
	sessionID, err := p.frameSession(ctx, frameID)
	if err != nil {
		return err
	}
	return p.owner.browser.transport.CallSession(ctx, sessionID, method, params, result)
}

// Navigate loads url and returns CDP frame/loader identity.
func (p *Page) Navigate(ctx context.Context, targetURL string) (frameID, loaderID string, err error) {
	if ctx == nil {
		return "", "", &CDPError{Code: CDPErrorInvalidConfig, Op: "navigate", Err: fmt.Errorf("context required")}
	}
	if targetURL == "" {
		return "", "", fmt.Errorf("navigate: URL required")
	}
	if err := p.owner.browser.policy.ValidateRequest(ctx, targetURL, "GET", "", 0, network.TargetNavigation, p.sessionID); err != nil {
		return "", "", fmt.Errorf("navigate: %w", err)
	}
	var result struct {
		FrameID  string `json:"frameId"`
		LoaderID string `json:"loaderId"`
		Error    string `json:"errorText"`
	}
	params := navigateParams{URL: targetURL}
	p.mu.Lock()
	if p.targetScripts.Referrer != "" && !p.navigated {
		params.Referrer = p.targetScripts.Referrer
	}
	p.navigated = true
	p.mu.Unlock()
	if err := p.Call(ctx, "Page.navigate", params, &result); err != nil {
		return "", "", err
	}
	if result.Error != "" {
		return result.FrameID, result.LoaderID, fmt.Errorf("navigate: %s", result.Error)
	}
	return result.FrameID, result.LoaderID, nil
}

// Close closes the target exactly once.
func (p *Page) Close() error {
	return p.close(true)
}

func (p *Page) close(remote bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultCDPDialTimeout)
	defer cancel()
	return p.closeWithContext(ctx, remote)
}

// CloseContext closes the target using the caller's deadline and cancellation.
func (p *Page) CloseContext(ctx context.Context) error {
	return p.closeWithContext(ctx, true)
}

func (p *Page) closeWithContext(ctx context.Context, remote bool) (resultErr error) {
	if ctx == nil {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "close target", Err: fmt.Errorf("context required")}
	}
	if p == nil {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "close target", Err: fmt.Errorf("page required")}
	}
	if err := p.lockLifecycle(ctx); err != nil {
		return fmt.Errorf("wait for page lifecycle: %w", err)
	}
	defer func() {
		p.lifecycleMu.Unlock()
		p.mu.RLock()
		retired := p.removed
		p.mu.RUnlock()
		if !retired {
			return
		}
		if err := p.drainExecution(ctx); err != nil {
			resultErr = errors.Join(resultErr, err)
			return
		}
		resultErr = errors.Join(resultErr, p.retireResources())
	}()
	p.mu.RLock()
	if p.removed {
		p.mu.RUnlock()
		return nil
	}
	previous := p.state
	p.mu.RUnlock()
	if p.owner == nil || p.owner.browser == nil {
		return &TargetError{TargetID: p.targetID, State: previous, Message: "page has no browser owner"}
	}
	if remote && previous != TargetStateClosed {
		// Retire request routing first: the monitor fails every paused request
		// it still owns, so closing the target cannot release one to the network.
		p.stopFrameRouting()
		if err := p.owner.browser.closeTarget(ctx, p.targetID); err != nil {
			p.mu.RLock()
			destroyed := p.state == TargetStateClosed
			p.mu.RUnlock()
			if !destroyed {
				reconcileCtx, cancel := context.WithTimeout(ctx, defaultCDPDialTimeout)
				exists, reconcileErr := p.owner.browser.targetExists(reconcileCtx, p.targetID)
				cancel()
				if reconcileErr != nil {
					return errors.Join(err, fmt.Errorf("reconcile target close: %w", reconcileErr))
				}
				if exists {
					return fmt.Errorf("close target (request routing retired; further requests stay paused until close succeeds): %w", err)
				}
			}
		}
	}
	p.mu.Lock()
	if p.removed {
		p.mu.Unlock()
		return nil
	}
	p.removed = true
	p.state = TargetStateClosed
	p.stateErr = "closed by owner"
	p.mu.Unlock()
	if err := p.transitionLifecycle(tabs.TabLifecycleClosed); err != nil {
		p.mu.Lock()
		p.retirementErr = errors.Join(p.retirementErr, err)
		p.mu.Unlock()
	}
	if p.cancelLifetime != nil {
		p.cancelLifetime()
	}
	p.stopFrameRouting()
	p.frameMu.Lock()
	p.frameSessions = make(map[string]string)
	p.childSessions = make(map[string]string)
	p.frameMu.Unlock()
	p.fetchMu.Lock()
	p.fetchHandler = nil
	p.fetchMu.Unlock()
	p.owner.mu.Lock()
	delete(p.owner.pages, p.targetID)
	p.owner.mu.Unlock()
	p.owner.browser.mu.Lock()
	delete(p.owner.browser.pages, p.targetID)
	delete(p.owner.browser.sessionMap, p.sessionID)
	p.owner.browser.mu.Unlock()
	p.owner.browser.releasePage()
	return nil
}

// CloseTargetInContext closes a registered page or a partially attached target
// only after proving that the target belongs to this page's browser context.
func (p *Page) CloseTargetInContext(ctx context.Context, targetID string) error {
	if ctx == nil {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "close target in context", Err: fmt.Errorf("context required")}
	}
	if p == nil || targetID == "" || p.owner == nil || p.owner.browser == nil || p.owner.browser.transport == nil {
		return &TargetError{TargetID: targetID, State: TargetStateClosed, Message: "target or browser context unavailable"}
	}
	if targetID == p.targetID {
		return p.CloseContext(ctx)
	}
	p.owner.mu.Lock()
	page := p.owner.pages[targetID]
	p.owner.mu.Unlock()
	if page != nil {
		return page.CloseContext(ctx)
	}
	var result struct {
		TargetInfo struct {
			TargetID         string `json:"targetId"`
			BrowserContextID string `json:"browserContextId"`
		} `json:"targetInfo"`
	}
	if err := p.owner.browser.transport.Call(ctx, "Target.getTargetInfo", targetParams{TargetID: targetID}, &result); err != nil {
		return fmt.Errorf("verify target %s browser context: %w", targetID, err)
	}
	if result.TargetInfo.TargetID != targetID || result.TargetInfo.BrowserContextID != p.owner.id {
		return &TargetError{TargetID: targetID, State: TargetStateDetached, Message: "target is not owned by this browser context"}
	}
	return p.owner.browser.closeTarget(ctx, targetID)
}
