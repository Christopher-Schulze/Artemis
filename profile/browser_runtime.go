package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/Christopher-Schulze/Artemis/bridge"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/stealth"
)

type BrowserRuntime struct {
	mu       sync.Mutex
	manager  *RuntimeManager
	sessions map[SessionID]*ownedBrowserSession
	stealth  stealth.StealthPolicy
	creds    *CredentialStore
}

type ownedBrowserSession struct {
	browser *bridge.ChromiumBrowser
	context *bridge.BrowserContext
	pages   map[PageID]*bridge.Page
}

func NewBrowserRuntime(manager *RuntimeManager) (*BrowserRuntime, error) {
	if manager == nil {
		return nil, errors.New("browser runtime: session manager required")
	}
	policy, _, err := stealth.PolicyFromEnv()
	if err != nil {
		return nil, fmt.Errorf("browser runtime: %w", err)
	}
	return &BrowserRuntime{manager: manager, sessions: make(map[SessionID]*ownedBrowserSession), stealth: policy}, nil
}

// SetStealthPolicy configures the policy used when a new page is created.
// Existing pages retain their immutable target script contract.
func (r *BrowserRuntime) SetStealthPolicy(policy stealth.StealthPolicy) *BrowserRuntime {
	if r == nil {
		return r
	}
	r.mu.Lock()
	if policy.PublicDefault == "" {
		policy.PublicDefault = stealth.StealthDefault
	}
	r.stealth = policy
	r.mu.Unlock()
	return r
}

// SetCredentialStore binds the encrypted credential owner used by
// Authenticate. The runtime never persists plaintext credentials.
func (r *BrowserRuntime) SetCredentialStore(store *CredentialStore) *BrowserRuntime {
	if r == nil {
		return r
	}
	r.mu.Lock()
	r.creds = store
	r.mu.Unlock()
	return r
}

func (r *BrowserRuntime) Open(ctx context.Context, request OpenSessionRequest, launch browserprocess.LaunchConfig) (*RuntimeSession, error) {
	session, err := r.manager.Open(ctx, request)
	if err != nil {
		return nil, err
	}
	if session.Class != ProfileAttached {
		launch.UserDataDir = session.DataDir
	}
	browser, err := bridge.LaunchChromium(ctx, launch)
	if err != nil {
		cleanupErr := r.manager.Close(context.Background(), session.ID, session.OwnerUserRef)
		return nil, fmt.Errorf("browser runtime: launch: %w", errors.Join(err, cleanupErr))
	}
	var browserContext *bridge.BrowserContext
	if session.Class == ProfilePersistent {
		browserContext, err = browser.DefaultContext()
	} else {
		browserContext, err = browser.NewContext(ctx)
	}
	if err != nil {
		browserCloseErr := browser.Close()
		managerCloseErr := r.manager.Close(context.Background(), session.ID, session.OwnerUserRef)
		return nil, fmt.Errorf("browser runtime: context: %w", errors.Join(err, browserCloseErr, managerCloseErr))
	}
	r.mu.Lock()
	r.sessions[session.ID] = &ownedBrowserSession{browser: browser, context: browserContext, pages: make(map[PageID]*bridge.Page)}
	r.mu.Unlock()
	return session, nil
}

func (r *BrowserRuntime) Attach(ctx context.Context, request OpenSessionRequest, endpoint string) (*RuntimeSession, error) {
	request.Class = ProfileAttached
	session, err := r.manager.Open(ctx, request)
	if err != nil {
		return nil, err
	}
	browser, err := bridge.ConnectChromium(ctx, endpoint)
	if err != nil {
		cleanupErr := r.manager.Close(context.Background(), session.ID, session.OwnerUserRef)
		return nil, fmt.Errorf("browser runtime: attach: %w", errors.Join(err, cleanupErr))
	}
	browserContext, err := browser.DefaultContext()
	if err != nil {
		browserCloseErr := browser.Close()
		managerCloseErr := r.manager.Close(context.Background(), session.ID, session.OwnerUserRef)
		return nil, errors.Join(err, browserCloseErr, managerCloseErr)
	}
	r.mu.Lock()
	r.sessions[session.ID] = &ownedBrowserSession{browser: browser, context: browserContext, pages: make(map[PageID]*bridge.Page)}
	r.mu.Unlock()
	return session, nil
}

func (r *BrowserRuntime) NewPage(ctx context.Context, sessionID SessionID, owner, initialURL string) (PageID, *bridge.Page, error) {
	r.mu.Lock()
	owned := r.sessions[sessionID]
	r.mu.Unlock()
	if owned == nil {
		return "", nil, &RuntimeError{Class: FailureNotFound, Op: "new page", Msg: "browser session not active"}
	}
	if _, err := r.manager.Get(sessionID, owner); err != nil {
		return "", nil, err
	}
	scripts, err := r.targetScripts(ctx, owned.browser, owned.context, sessionID, initialURL)
	if err != nil {
		return "", nil, err
	}
	page, err := owned.context.NewPageWithScripts(ctx, initialURL, scripts)
	if err != nil {
		return "", nil, err
	}
	pageID, err := r.manager.RegisterPage(sessionID, owner, page.TargetID())
	if err != nil {
		return "", nil, errors.Join(err, page.Close())
	}
	r.mu.Lock()
	owned.pages[pageID] = page
	r.mu.Unlock()
	return pageID, page, nil
}

func (r *BrowserRuntime) targetScripts(ctx context.Context, browser *bridge.ChromiumBrowser, browserContext *bridge.BrowserContext, sessionID SessionID, initialURL string) (bridge.TargetScriptConfig, error) {
	if r == nil {
		return bridge.TargetScriptConfig{}, nil
	}
	r.mu.Lock()
	policy := r.stealth
	r.mu.Unlock()
	return PrepareTargetScripts(ctx, browser, browserContext, sessionID, initialURL, policy)
}

// PrepareTargetScripts derives the measured stealth pre-script contract for
// one navigation. It is shared between the runtime session path and callers
// (e.g. the CLI) that manage their own pages.
func PrepareTargetScripts(ctx context.Context, browser *bridge.ChromiumBrowser, browserContext *bridge.BrowserContext, sessionID SessionID, initialURL string, policy stealth.StealthPolicy) (bridge.TargetScriptConfig, error) {
	if browser == nil || strings.TrimSpace(initialURL) == "" || initialURL == "about:blank" {
		return bridge.TargetScriptConfig{}, nil
	}
	level, err := stealth.DetermineStealthLevel(initialURL, policy, net.LookupIP)
	if err != nil {
		return bridge.TargetScriptConfig{}, err
	}
	if level == stealth.StealthDefault {
		return bridge.TargetScriptConfig{}, nil
	}
	facts, err := measureEnvironment(ctx, browserContext)
	if err != nil {
		return bridge.TargetScriptConfig{}, fmt.Errorf("browser runtime: measure environment: %w", err)
	}
	version := browser.Version()
	chromeVersion := stealth.ParseChromeVersion(version.UserAgent)
	if chromeVersion == "" {
		chromeVersion = parseProductVersion(version.Product)
	}
	if chromeVersion == "" || version.UserAgent == "" {
		return bridge.TargetScriptConfig{}, fmt.Errorf("browser runtime: cannot derive measured Chrome identity")
	}
	facts.UserAgent = version.UserAgent
	facts.ChromeVersion = chromeVersion
	// Headless UA literally advertises automation — the single biggest bot
	// tell. Normalize the product token once so navigator.userAgent and the
	// Sec-CH-UA/UA request headers all say "Chrome/".
	facts.UserAgent = strings.ReplaceAll(facts.UserAgent, "HeadlessChrome/", "Chrome/")
	profile, err := stealth.NewEnvironmentProfile(string(sessionID), level, facts, level == stealth.StealthDefault || !policy.Ack.AcknowledgedAt.IsZero())
	if err != nil {
		return bridge.TargetScriptConfig{}, err
	}
	pageScript, err := stealth.NewDocumentScript(profile)
	if err != nil {
		return bridge.TargetScriptConfig{}, err
	}
	workerScript, err := stealth.NewWorkerScript(profile)
	if err != nil {
		return bridge.TargetScriptConfig{}, err
	}
	hash, err := profile.ScriptHash()
	if err != nil {
		return bridge.TargetScriptConfig{}, err
	}
	var referrer string
	if level == stealth.StealthParanoid {
		// Paranoid enters through a plausible search referrer instead of a
		// bare direct hit.
		referrer, _ = stealth.ReferrerForDomainContext(ctx, initialURL, nil)
	}
	return bridge.TargetScriptConfig{
		Version: hash, PageScript: pageScript, WorkerScript: workerScript, Referrer: referrer,
		Emulation: bridge.EmulationOverrides{
			UserAgent:       profile.UserAgent,
			AcceptLanguage:  strings.Join(profile.Languages, ","),
			Locale:          profile.Locale,
			TimezoneID:      profile.Timezone,
			Platform:        stealth.ClientHintsPlatform(profile.Platform),
			PlatformVersion: profile.PlatformVersion,
			Architecture:    profile.Architecture,
			ChromeVersion:   profile.ChromeVersion,
		},
	}, nil
}

type measuredEnvironment struct {
	UserAgent        string   `json:"user_agent"`
	Platform         string   `json:"platform"`
	PlatformVersion  string   `json:"platform_version"`
	Architecture     string   `json:"architecture"`
	Locale           string   `json:"locale"`
	Languages        []string `json:"languages"`
	Timezone         string   `json:"timezone"`
	ViewportWidth    int      `json:"viewport_width"`
	ViewportHeight   int      `json:"viewport_height"`
	DevicePixelRatio float64  `json:"device_pixel_ratio"`
	HardwareCores    int      `json:"hardware_cores"`
	DeviceMemoryGB   int      `json:"device_memory_gb"`
	WebGLVendor      string   `json:"webgl_vendor"`
	WebGLRenderer    string   `json:"webgl_renderer"`
	NetworkRTT       int      `json:"network_rtt_ms"`
}

func measureEnvironment(ctx context.Context, browserContext *bridge.BrowserContext) (facts stealth.EnvironmentFacts, returnErr error) {
	if browserContext == nil {
		return stealth.EnvironmentFacts{}, errors.New("browser context required")
	}
	page, err := browserContext.NewPageWithScripts(ctx, "about:blank", bridge.TargetScriptConfig{})
	if err != nil {
		return stealth.EnvironmentFacts{}, err
	}
	defer func() {
		if closeErr := page.Close(); closeErr != nil {
			facts = stealth.EnvironmentFacts{}
			returnErr = errors.Join(returnErr, fmt.Errorf("browser runtime: close environment page: %w", closeErr))
		}
	}()
	var result struct {
		ExceptionDetails json.RawMessage `json:"exceptionDetails,omitempty"`
		Result           struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	expression := `(async () => {
  const glCanvas = document.createElement("canvas");
  const gl = glCanvas.getContext("webgl") || glCanvas.getContext("experimental-webgl");
  let webglVendor = "", webglRenderer = "";
  if (gl) {
    const debug = gl.getExtension("WEBGL_debug_renderer_info");
    if (debug) { webglVendor = gl.getParameter(debug.UNMASKED_VENDOR_WEBGL) || ""; webglRenderer = gl.getParameter(debug.UNMASKED_RENDERER_WEBGL) || ""; }
  }
  let high = {};
  if (navigator.userAgentData && navigator.userAgentData.getHighEntropyValues) {
    try { high = await navigator.userAgentData.getHighEntropyValues(["platformVersion", "architecture"]); } catch (_) {}
  }
  return JSON.stringify({
    user_agent:navigator.userAgent, platform:navigator.platform, platform_version:high.platformVersion||"", architecture:high.architecture||"",
    locale:navigator.language, languages:Array.from(navigator.languages||[]), timezone:Intl.DateTimeFormat().resolvedOptions().timeZone,
    viewport_width:window.innerWidth, viewport_height:window.innerHeight, device_pixel_ratio:window.devicePixelRatio,
    hardware_cores:navigator.hardwareConcurrency, device_memory_gb:navigator.deviceMemory||0,
    webgl_vendor:webglVendor, webgl_renderer:webglRenderer, network_rtt_ms:navigator.connection?.rtt||0
  });
})()`
	if err := page.Call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true}, &result); err != nil {
		return stealth.EnvironmentFacts{}, err
	}
	if len(result.ExceptionDetails) > 0 && string(result.ExceptionDetails) != "null" {
		return stealth.EnvironmentFacts{}, errors.New("environment probe failed")
	}
	var measured measuredEnvironment
	var encoded string
	if err := json.Unmarshal(result.Result.Value, &encoded); err == nil {
		if err := json.Unmarshal([]byte(encoded), &measured); err != nil {
			return stealth.EnvironmentFacts{}, fmt.Errorf("decode environment probe: %w", err)
		}
	} else if err := json.Unmarshal(result.Result.Value, &measured); err != nil {
		return stealth.EnvironmentFacts{}, fmt.Errorf("decode environment probe: %w", err)
	}
	if measured.UserAgent == "" || measured.Platform == "" || measured.Locale == "" || len(measured.Languages) == 0 || measured.Timezone == "" || measured.ViewportWidth <= 0 || measured.ViewportHeight <= 0 || measured.DevicePixelRatio <= 0 || measured.HardwareCores <= 0 {
		return stealth.EnvironmentFacts{}, errors.New("environment probe returned incomplete measured values")
	}
	if (measured.WebGLVendor == "") != (measured.WebGLRenderer == "") {
		measured.WebGLVendor = ""
		measured.WebGLRenderer = ""
	}
	return stealth.EnvironmentFacts{
		UserAgent: measured.UserAgent, Platform: measured.Platform, PlatformVersion: measured.PlatformVersion, Architecture: measured.Architecture,
		Locale: measured.Locale, Languages: measured.Languages, Timezone: measured.Timezone,
		ViewportWidth: measured.ViewportWidth, ViewportHeight: measured.ViewportHeight, DevicePixelRatio: measured.DevicePixelRatio,
		HardwareConcurrency: measured.HardwareCores, DeviceMemoryGB: measured.DeviceMemoryGB,
		WebGLVendor: measured.WebGLVendor, WebGLRenderer: measured.WebGLRenderer, NetworkRTTMillis: measured.NetworkRTT, Measured: true,
	}, nil
}

// Authenticate runs the verified credential/MFA contract on an owned page.
// Page and profile ownership are checked before any credential is opened.
func (r *BrowserRuntime) Authenticate(ctx context.Context, sessionID SessionID, pageID PageID, owner string, request AuthenticationRequest, selectors LoginSelectors, policy AuthenticationPolicy, handoff MFAHandoff) (AuthenticationOutcome, error) {
	session, err := r.manager.Get(sessionID, owner)
	if err != nil {
		return AuthenticationOutcome{Status: AuthStatusDenied, Reason: "session_owner_denied"}, err
	}
	if request.ProfileName == "" {
		request.ProfileName = string(session.ProfileID)
	} else if request.ProfileName != string(session.ProfileID) {
		return AuthenticationOutcome{Status: AuthStatusDenied, Reason: "profile_session_mismatch", ProfileName: request.ProfileName, Domain: request.Domain}, &RuntimeError{Class: FailureDenied, Op: "authenticate", Msg: "profile does not belong to session"}
	}
	page, err := r.Page(sessionID, pageID, owner)
	if err != nil {
		return AuthenticationOutcome{Status: AuthStatusDenied, Reason: "page_owner_denied", ProfileName: request.ProfileName, Domain: request.Domain}, err
	}
	r.mu.Lock()
	store := r.creds
	r.mu.Unlock()
	if store == nil {
		return AuthenticationOutcome{Status: AuthStatusUnsupported, Reason: "credential_store_unavailable", ProfileName: request.ProfileName, Domain: request.Domain}, nil
	}
	executor, err := NewBrowserLoginExecutor(page, selectors)
	if err != nil {
		return AuthenticationOutcome{Status: AuthStatusFailed, Reason: "login_executor_unavailable", ProfileName: request.ProfileName, Domain: request.Domain}, err
	}
	auth := &Authenticator{Store: store, Executor: executor, Handoff: handoff, Policy: policy}
	outcome, authErr := auth.Authenticate(ctx, request)
	return outcome, errors.Join(authErr, executor.Close())
}

func parseProductVersion(product string) string {
	for _, field := range strings.Fields(product) {
		if strings.Count(field, ".") >= 1 && strings.IndexFunc(field, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
			return field
		}
	}
	return ""
}

func (r *BrowserRuntime) Page(sessionID SessionID, pageID PageID, owner string) (*bridge.Page, error) {
	if _, err := r.manager.ResolvePage(sessionID, pageID, owner); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	owned := r.sessions[sessionID]
	if owned == nil || owned.pages[pageID] == nil {
		return nil, &RuntimeError{Class: FailureNotFound, Op: "page", Msg: "page not active"}
	}
	return owned.pages[pageID], nil
}

func (r *BrowserRuntime) ClosePage(sessionID SessionID, pageID PageID, owner string) error {
	page, err := r.Page(sessionID, pageID, owner)
	if err != nil {
		return err
	}
	if err := page.Close(); err != nil {
		return err
	}
	r.mu.Lock()
	if owned := r.sessions[sessionID]; owned != nil {
		delete(owned.pages, pageID)
	}
	r.mu.Unlock()
	return r.manager.ClosePage(sessionID, pageID, owner)
}

func (r *BrowserRuntime) Close(ctx context.Context, sessionID SessionID, owner string) error {
	if _, err := r.manager.Get(sessionID, owner); err != nil {
		return err
	}
	r.mu.Lock()
	owned := r.sessions[sessionID]
	delete(r.sessions, sessionID)
	r.mu.Unlock()
	if owned != nil {
		browserErr := owned.browser.Close()
		managerErr := r.manager.Close(ctx, sessionID, owner)
		if browserErr != nil || managerErr != nil {
			return errors.Join(browserErr, managerErr)
		}
		return nil
	}
	return r.manager.Close(ctx, sessionID, owner)
}
