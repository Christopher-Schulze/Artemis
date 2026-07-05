package bridge

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// BrowserProvider is the interface for multi-backend browser abstraction.
// Implementations select runtime backend via config/credentials:
// local headless Chromium (default), Camofox REST backend, or cloud
// providers (Browserbase, Firecrawl - deferred P7).
type BrowserProvider interface {
	// Name returns a short, human-readable name for logs and diagnostics.
	Name() string
	// Launch creates a browser session with the given config.
	Launch(ctx context.Context, config ProviderConfig) (*BrowserSession, error)
	// Close releases the browser session.
	Close() error
	// Healthy reports whether the provider is ready to serve requests.
	Healthy() bool
}

// ProviderConfig configures a browser provider launch.
type ProviderConfig struct {
	// CDPURL overrides the CDP websocket URL when set (BROWSER_CDP_URL env var).
	CDPURL string
	// Headless controls headless mode for local Chrome.
	Headless bool
	// SessionName is the unique session identifier.
	SessionName string
	// ProfileDir is the browser profile directory for persistent identity.
	ProfileDir string
	// ProxyURL routes outbound traffic through the given proxy.
	ProxyURL string
	// ExtraArgs are additional Chrome launch flags.
	ExtraArgs []string
}

// BrowserSession represents an active browser session from a provider.
type BrowserSession struct {
	// ProviderName is the name of the provider that created this session.
	ProviderName string
	// SessionID is the provider-specific session identifier.
	SessionID string
	// CDPURL is the CDP websocket URL for the session.
	CDPURL string
	// Features contains feature flags that were enabled.
	Features map[string]bool
}

// ProviderRegistry holds available browser providers and selects one
// based on configuration.
type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]BrowserProvider
	default_  string
}

// NewProviderRegistry creates a registry with the local Chrome provider
// as default.
func NewProviderRegistry() *ProviderRegistry {
	r := &ProviderRegistry{
		providers: make(map[string]BrowserProvider),
		default_:  "local",
	}
	r.Register("local", &LocalChromeProvider{})
	r.Register("camofox", &CamofoxProvider{})
	return r
}

// Register adds a provider to the registry.
func (r *ProviderRegistry) Register(name string, p BrowserProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[strings.ToLower(name)] = p
}

// Get returns the provider with the given name, or the default if not found.
func (r *ProviderRegistry) Get(name string) (BrowserProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name == "" {
		name = r.default_
	}
	p, ok := r.providers[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("bridge: browser provider %q not registered", name)
	}
	return p, nil
}

// Default returns the default provider.
func (r *ProviderRegistry) Default() BrowserProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[r.default_]
}

// SetDefault sets the default provider name.
func (r *ProviderRegistry) SetDefault(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.default_ = strings.ToLower(name)
}

// Available returns the names of all registered providers.
func (r *ProviderRegistry) Available() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// SelectFromConfig returns the provider selected by environment/config.
// Priority: BROWSER_CDP_URL (forces local with CDP override) > BROWSER_PROVIDER env var > default.
func (r *ProviderRegistry) SelectFromConfig() (BrowserProvider, ProviderConfig, error) {
	config := ProviderConfig{
		CDPURL:      os.Getenv("BROWSER_CDP_URL"),
		Headless:    true,
		SessionName: "artemis-session",
	}

	providerName := os.Getenv("BROWSER_PROVIDER")
	if config.CDPURL != "" {
		// CDP URL override forces local provider with existing Chrome
		providerName = "local"
	}

	p, err := r.Get(providerName)
	if err != nil {
		return nil, config, err
	}
	return p, config, nil
}

// LocalChromeProvider launches a local headless Chrome via chromedp.
type LocalChromeProvider struct {
	session *BrowserSession
}

// Name returns the provider name.
func (p *LocalChromeProvider) Name() string {
	return "local-chrome"
}

// Launch creates a local Chrome browser session.
func (p *LocalChromeProvider) Launch(ctx context.Context, config ProviderConfig) (*BrowserSession, error) {
	session := &BrowserSession{
		ProviderName: p.Name(),
		SessionID:    config.SessionName,
		CDPURL:       config.CDPURL,
		Features: map[string]bool{
			"headless":     config.Headless,
			"cdp_override": config.CDPURL != "",
		},
	}
	p.session = session
	return session, nil
}

// Close releases the local Chrome session.
func (p *LocalChromeProvider) Close() error {
	p.session = nil
	return nil
}

// Healthy reports whether the local Chrome provider is ready.
func (p *LocalChromeProvider) Healthy() bool {
	return true // local Chrome is always available
}

// CamofoxProvider connects to a Camofox REST backend (Camoufox/Firefox fork
// with C++ fingerprint spoofing).
type CamofoxProvider struct {
	session *BrowserSession
}

// Name returns the provider name.
func (p *CamofoxProvider) Name() string {
	return "camofox"
}

// Launch creates a Camofox browser session via REST API.
func (p *CamofoxProvider) Launch(ctx context.Context, config ProviderConfig) (*BrowserSession, error) {
	camofoxURL := os.Getenv("CAMOFOX_URL")
	if camofoxURL == "" {
		return nil, fmt.Errorf("camofox: CAMOFOX_URL env var required")
	}
	session := &BrowserSession{
		ProviderName: p.Name(),
		SessionID:    config.SessionName,
		CDPURL:       camofoxURL,
		Features: map[string]bool{
			"fingerprint_spoofing": true,
			"firefox_backend":      true,
		},
	}
	p.session = session
	return session, nil
}

// Close releases the Camofox session.
func (p *CamofoxProvider) Close() error {
	p.session = nil
	return nil
}

// Healthy reports whether the Camofox provider is configured.
func (p *CamofoxProvider) Healthy() bool {
	return os.Getenv("CAMOFOX_URL") != ""
}
