package renderless

import (
	"fmt"
	"sync"
	"time"
)

// runtime.go (renderless/runtime.go - runtime context
// management).
//
// In-process no-render JS browser path: runtime context management
// for V8/v8go isolates. Each runtime context wraps an isolate with
// its own global scope and execution state.

// RuntimeContext represents a V8 runtime context
// (runtime context management).
type RuntimeContext struct {
	mu        sync.Mutex
	id        string
	isolateID int
	createdAt time.Time
	lastUsed  time.Time
	closed    bool
	globals   map[string]interface{}
}

// NewRuntimeContext creates a new runtime context
// (runtime context management).
func NewRuntimeContext(id string, isolateID int) *RuntimeContext {
	return &RuntimeContext{
		id:        id,
		isolateID: isolateID,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		globals:   make(map[string]interface{}),
	}
}

// ID returns the context ID
// (runtime context management).
func (c *RuntimeContext) ID() string {
	return c.id
}

// IsolateID returns the isolate ID
// (runtime context management).
func (c *RuntimeContext) IsolateID() int {
	return c.isolateID
}

// SetGlobal sets a global variable in the context
// (runtime context management).
func (c *RuntimeContext) SetGlobal(name string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.globals[name] = value
	c.lastUsed = time.Now()
}

// GetGlobal retrieves a global variable from the context
// (runtime context management).
func (c *RuntimeContext) GetGlobal(name string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	val, ok := c.globals[name]
	c.lastUsed = time.Now()
	return val, ok
}

// Globals returns all global variable names
// (runtime context management).
func (c *RuntimeContext) Globals() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var names []string
	for name := range c.globals {
		names = append(names, name)
	}
	return names
}

// Touch updates the last-used timestamp
// (runtime context management).
func (c *RuntimeContext) Touch() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastUsed = time.Now()
}

// LastUsed returns the last-used timestamp
// (runtime context management).
func (c *RuntimeContext) LastUsed() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastUsed
}

// Close closes the runtime context
// (runtime context management).
func (c *RuntimeContext) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.globals = nil
}

// IsClosed reports whether the context is closed
// (runtime context management).
func (c *RuntimeContext) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// String returns a diagnostic summary.
func (c *RuntimeContext) String() string {
	return fmt.Sprintf("RuntimeContext{id:%s isolate:%d closed:%v}", c.id, c.isolateID, c.closed)
}
