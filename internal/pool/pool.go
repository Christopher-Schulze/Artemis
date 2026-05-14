// Package pool provides a process-shared sync.Pool of strings.Builder
// instances. Hot paths in agent (Markdown, Text, HTML dump) and js
// helpers reuse builders rather than allocate fresh ones each call.
package pool

import (
	"strings"
	"sync"
)

var builderPool = sync.Pool{
	New: func() any { return &strings.Builder{} },
}

// GetBuilder returns a reset *strings.Builder.
func GetBuilder() *strings.Builder {
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	return b
}

// PutBuilder returns a *strings.Builder to the pool. Callers must NOT
// retain references to b's String() result if it was returned to the
// pool - the underlying byte buffer will be reused. Use the result
// before returning.
func PutBuilder(b *strings.Builder) {
	if b == nil {
		return
	}
	// Cap retained capacity to avoid keeping monstrous buffers around.
	if b.Cap() > 1<<20 {
		return
	}
	builderPool.Put(b)
}
