package renderless

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	artemisengine "github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
)

// engine.go (spec L4022: renderless/engine.go - production-backed
// renderless facade).
//
// In-process no-render JS browser path: this package is the public facade for
// the production engine.Engine network, parser and V8 execution owner. It
// does not maintain a second synthetic execution engine.
//
// Ref: research/artemis/engine/engine.go:1-272

// EngineConfig configures the production-backed renderless facade
// (spec L4022: renderless engine).
type EngineConfig struct {
	MaxIsolates    int           `json:"maxIsolates"`
	ScriptTimeout  time.Duration `json:"scriptTimeout"`
	FetchTimeout   time.Duration `json:"fetchTimeout"`
	UserAgent      string        `json:"userAgent"`
	EnableRobots   bool          `json:"enableRobots"`
	PrivateIPBlock bool          `json:"privateIPBlock"`
	// AllowPrivateNetworks is an explicit local/test opt-in. The zero value
	// remains fail-closed through the production engine's network policy.
	AllowPrivateNetworks bool `json:"allowPrivateNetworks"`
	// AllowedPorts narrows or extends the production network policy's port
	// allowlist. Empty keeps the secure 80/443 default.
	AllowedPorts []int `json:"allowedPorts,omitempty"`
}

// FetchOptions is the production engine request contract exposed by the
// renderless facade.
type FetchOptions = artemisengine.FetchOpts

// ApplyDefaults applies default values to the config
// (spec L4022: production-backed renderless facade).
func (c *EngineConfig) ApplyDefaults() {
	if c.MaxIsolates <= 0 {
		c.MaxIsolates = 4
	}
	if c.ScriptTimeout <= 0 {
		c.ScriptTimeout = 30 * time.Second
	}
	if c.FetchTimeout <= 0 {
		c.FetchTimeout = 30 * time.Second
	}
	if c.UserAgent == "" {
		c.UserAgent = "Omnimus/Renderless/1.0"
	}
}

// Engine is the renderless facade over the production JS execution engine
// (spec L4022: renderless engine).
type Engine struct {
	mu      sync.RWMutex
	cfg     EngineConfig
	backend *artemisengine.Engine
	closed  bool
}

// NewEngine creates a new production-backed renderless facade
// (spec L4022: renderless engine).
func NewEngine(cfg EngineConfig) (*Engine, error) {
	cfg.ApplyDefaults()
	backend, err := artemisengine.New(artemisengine.Config{
		UserAgent:         cfg.UserAgent,
		Timeout:           cfg.FetchTimeout,
		ObeyRobots:        cfg.EnableRobots,
		JSContextPoolSize: cfg.MaxIsolates,
		PolicyConfig: network.PolicyConfig{
			AllowPrivateNetworks: cfg.AllowPrivateNetworks,
			AllowedPorts:         append([]int(nil), cfg.AllowedPorts...),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("renderless: create production engine: %w", err)
	}
	return &Engine{cfg: cfg, backend: backend}, nil
}

// Config returns the engine configuration
// (spec L4022: renderless engine).
func (e *Engine) Config() EngineConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// Fetch fetches a URL through the production engine and returns its real
// parsed page. It never synthesizes a response.
func (e *Engine) Fetch(ctx context.Context, rawURL string) (*Page, error) {
	return e.FetchWithOptions(ctx, rawURL, artemisengine.FetchOpts{})
}

// FetchWithOptions is the renderless facade for the production engine's
// network, parser, JavaScript, cookie and policy contract.
func (e *Engine) FetchWithOptions(ctx context.Context, rawURL string, opts artemisengine.FetchOpts) (*Page, error) {
	if ctx == nil {
		return nil, errors.New("renderless: fetch context required")
	}
	e.mu.RLock()
	backend, closed, timeout := e.backend, e.closed, e.cfg.FetchTimeout
	e.mu.RUnlock()
	if closed || backend == nil {
		return nil, errors.New("renderless: engine closed")
	}
	if (opts.RunScripts || opts.RunInlineScripts) && e.cfg.ScriptTimeout > 0 && e.cfg.ScriptTimeout < timeout {
		timeout = e.cfg.ScriptTimeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	page, err := backend.Fetch(ctx, rawURL, opts)
	if err != nil {
		return nil, fmt.Errorf("renderless: fetch %s: %w", rawURL, err)
	}
	if page == nil {
		return nil, errors.New("renderless: production engine returned no page")
	}
	return newPageFromEngine(e, page), nil
}

// Close shuts down the engine
// (spec L4022: renderless engine).
func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	backend := e.backend
	e.mu.Unlock()
	if backend == nil {
		return nil
	}
	return backend.Close()
}

// IsClosed reports whether the engine is closed
// (spec L4022: renderless engine).
func (e *Engine) IsClosed() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.closed
}

// IsolateCount returns the number of active production page leases.
func (e *Engine) IsolateCount() int {
	e.mu.RLock()
	backend := e.backend
	e.mu.RUnlock()
	if backend == nil {
		return 0
	}
	return backend.SessionUsage().ActiveTabs
}

// String returns a diagnostic summary.
func (e *Engine) String() string {
	e.mu.RLock()
	closed, backend := e.closed, e.backend
	e.mu.RUnlock()
	active := 0
	if backend != nil {
		active = backend.SessionUsage().ActiveTabs
	}
	return fmt.Sprintf("Engine{activePages:%d closed:%v}", active, closed)
}

func (e *Engine) withScriptTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil || e == nil || e.cfg.ScriptTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, e.cfg.ScriptTimeout)
}
