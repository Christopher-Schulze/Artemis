package renderless

import (
	"fmt"
	"sync"
)

// router.go (spec L4022: renderless/router.go - inline+external
// script execution router).
//
// In-process no-render JS browser path: inline+external script dispatch. The
// production engine owns execution; this router only dispatches caller-backed
// handlers and fails closed when no handler is bound.

// ScriptType enumerates script types
// (spec L4022: inline+external script execution).
type ScriptType string

const (
	ScriptTypeInline   ScriptType = "inline"
	ScriptTypeExternal ScriptType = "external"
	ScriptTypeModule   ScriptType = "module"
	ScriptTypeClassic  ScriptType = "classic"
)

// ScriptRequest represents a script execution request
// (spec L4022: inline+external script execution).
type ScriptRequest struct {
	Type    ScriptType `json:"type"`
	Source  string     `json:"source"` // inline code or external URL
	Async   bool       `json:"async"`
	Defer   bool       `json:"defer"`
	Timeout int        `json:"timeout,omitempty"` // milliseconds
}

// ScriptResult is the result of script execution
// (spec L4022: inline+external script execution).
type ScriptResult struct {
	Success  bool   `json:"success"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"durationMs"`
}

// ScriptRouter routes script execution requests
// (spec L4022: inline+external script execution).
type ScriptRouter struct {
	mu       sync.RWMutex
	handlers map[ScriptType]func(req ScriptRequest) ScriptResult
}

// NewScriptRouter creates a new ScriptRouter with fail-closed default handlers
// (spec L4022: inline+external script execution).
func NewScriptRouter() *ScriptRouter {
	r := &ScriptRouter{handlers: make(map[ScriptType]func(req ScriptRequest) ScriptResult)}
	r.RegisterHandler(ScriptTypeInline, r.defaultInlineHandler)
	r.RegisterHandler(ScriptTypeExternal, r.defaultExternalHandler)
	r.RegisterHandler(ScriptTypeModule, r.defaultModuleHandler)
	r.RegisterHandler(ScriptTypeClassic, r.defaultClassicHandler)
	return r
}

// RegisterHandler registers a handler for a script type
// (spec L4022: inline+external script execution).
func (r *ScriptRouter) RegisterHandler(t ScriptType, handler func(req ScriptRequest) ScriptResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if handler == nil {
		delete(r.handlers, t)
		return
	}
	r.handlers[t] = handler
}

// Execute routes and executes a script request
// (spec L4022: inline+external script execution).
func (r *ScriptRouter) Execute(req ScriptRequest) ScriptResult {
	r.mu.RLock()
	handler, ok := r.handlers[req.Type]
	r.mu.RUnlock()
	if !ok {
		return ScriptResult{
			Success: false,
			Error:   fmt.Sprintf("router: no handler for script type %q", req.Type),
		}
	}
	return handler(req)
}

func (r *ScriptRouter) defaultInlineHandler(req ScriptRequest) ScriptResult {
	return unavailableScriptResult(req, "inline")
}

func (r *ScriptRouter) defaultExternalHandler(req ScriptRequest) ScriptResult {
	return unavailableScriptResult(req, "external")
}

func (r *ScriptRouter) defaultModuleHandler(req ScriptRequest) ScriptResult {
	return unavailableScriptResult(req, "module")
}

func (r *ScriptRouter) defaultClassicHandler(req ScriptRequest) ScriptResult {
	return unavailableScriptResult(req, "classic")
}

func unavailableScriptResult(req ScriptRequest, kind string) ScriptResult {
	if req.Source == "" {
		return ScriptResult{Success: false, Error: kind + " script: empty source"}
	}
	return ScriptResult{Success: false, Error: "renderless: production page executor required"}
}

// IsValidScriptType reports whether a script type is valid
// (spec L4022: inline+external script execution).
func IsValidScriptType(t ScriptType) bool {
	switch t {
	case ScriptTypeInline, ScriptTypeExternal, ScriptTypeModule, ScriptTypeClassic:
		return true
	}
	return false
}

// String returns a diagnostic summary.
func (r ScriptRequest) String() string {
	return fmt.Sprintf("ScriptRequest{type:%s sourceLen:%d async:%v defer:%v}",
		r.Type, len(r.Source), r.Async, r.Defer)
}

// String returns a diagnostic summary.
func (r ScriptResult) String() string {
	return fmt.Sprintf("ScriptResult{success:%v outputLen:%d error:%q}",
		r.Success, len(r.Output), r.Error)
}
