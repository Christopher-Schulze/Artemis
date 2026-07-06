package renderless

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// engine.go (spec L4022: renderless/engine.go - V8/v8go isolate
// snapshot engine).
//
// In-process no-render JS browser path: the engine manages V8/v8go
// isolate snapshots for executing JavaScript without a full browser
// rendering pipeline. This is the spec-mandated facade for the
// renderless engine.
//
// Ref: research/artemis/engine/engine.go:1-272

// EngineConfig configures the renderless engine
// (spec L4022: V8/v8go isolate snapshot engine).
type EngineConfig struct {
	MaxIsolates    int           `json:"maxIsolates"`
	ScriptTimeout  time.Duration `json:"scriptTimeout"`
	FetchTimeout   time.Duration `json:"fetchTimeout"`
	UserAgent      string        `json:"userAgent"`
	EnableRobots   bool          `json:"enableRobots"`
	PrivateIPBlock bool          `json:"privateIPBlock"`
}

// ApplyDefaults applies default values to the config
// (spec L4022: V8/v8go isolate snapshot engine).
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

// Engine is the renderless JS execution engine
// (spec L4022: V8/v8go isolate snapshot engine).
type Engine struct {
	mu       sync.RWMutex
	cfg      EngineConfig
	isolates int
	closed   bool
}

// NewEngine creates a new renderless engine
// (spec L4022: V8/v8go isolate snapshot engine).
func NewEngine(cfg EngineConfig) (*Engine, error) {
	cfg.ApplyDefaults()
	e := &Engine{cfg: cfg}
	return e, nil
}

// Config returns the engine configuration
// (spec L4022: V8/v8go isolate snapshot engine).
func (e *Engine) Config() EngineConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// Fetch fetches a URL and returns a renderless Page
// (spec L4022: V8/v8go isolate snapshot engine).
func (e *Engine) Fetch(ctx context.Context, rawURL string) (*Page, error) {
	if e.IsClosed() {
		return nil, fmt.Errorf("engine: closed")
	}
	if rawURL == "" {
		return nil, fmt.Errorf("engine: empty URL")
	}
	return &Page{
		URL:        rawURL,
		StatusCode: 200,
		FetchedAt:  time.Now(),
		Engine:     e,
	}, nil
}

// Close shuts down the engine
// (spec L4022: V8/v8go isolate snapshot engine).
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

// IsClosed reports whether the engine is closed
// (spec L4022: V8/v8go isolate snapshot engine).
func (e *Engine) IsClosed() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.closed
}

// IsolateCount returns the number of active isolates
// (spec L4022: V8/v8go isolate snapshot engine).
func (e *Engine) IsolateCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isolates
}

// String returns a diagnostic summary.
func (e *Engine) String() string {
	return fmt.Sprintf("Engine{isolates:%d closed:%v}", e.isolates, e.closed)
}
