package js

import (
	"sync"

	v8 "rogchap.com/v8go"
)

// scriptCache holds compiled bootstrap scripts so the Runtime parses each
// JS-side bootstrap exactly once, regardless of how many Contexts are
// created. UnboundScript.Run skips the parse step on subsequent Contexts
// and is the cheapest cross-Context win we get without a V8 snapshot.
type scriptCache struct {
	mu      sync.Mutex
	scripts map[string]*v8.UnboundScript
}

func newScriptCache() *scriptCache {
	return &scriptCache{scripts: make(map[string]*v8.UnboundScript)}
}

// bootstrapEntry is one (name, source) pair queued by an install
// function. The Context flushes the entire queue as a single concatenated
// RunScript to minimise cgo crossings (one call instead of N).
type bootstrapEntry struct{ name, source string }

// run compiles source on first call (caching the UnboundScript on the
// Runtime's isolate) and Runs it against ctx. Subsequent calls reuse the
// compiled script.
func (s *scriptCache) run(iso *v8.Isolate, ctx *v8.Context, name, source string) (*v8.Value, error) {
	s.mu.Lock()
	us, ok := s.scripts[name]
	s.mu.Unlock()
	if !ok {
		var err error
		us, err = iso.CompileUnboundScript(source, name, v8.CompileOptions{})
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.scripts[name] = us
		s.mu.Unlock()
	}
	return us.Run(ctx)
}
