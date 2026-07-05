package js

import (
	"sync"

	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

// nodeTable maps integer handles (used by JS) to Go *webapi.Node values.
// It is identity-stable: passing the same underlying html.Node returns
// the same handle. Each js.Context owns one nodeTable, freed at Close.
//
// Implementation note: sync.Mutex + map beats sync.Map for the typical
// single-goroutine-per-Context access pattern. sync.Map's atomic Read
// path costs more than the mutex in low-contention workloads (verified
// empirically: pooled 100-page bench moved from 22ms -> 25ms with
// sync.Map). Keep the mutex; revisit only if profiling shows lock
// contention as a hot spot.
type nodeTable struct {
	mu     sync.Mutex
	nextID uint32
	byID   map[uint32]*webapi.Node
	byNode map[*html.Node]uint32
}

func newNodeTable() *nodeTable {
	return &nodeTable{
		byID:   make(map[uint32]*webapi.Node),
		byNode: make(map[*html.Node]uint32),
	}
}

// Handle returns the handle for n, registering a new one on first sight.
// A nil node yields handle 0.
func (t *nodeTable) Handle(n *webapi.Node) uint32 {
	if n == nil || n.Raw() == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if id, ok := t.byNode[n.Raw()]; ok {
		return id
	}
	t.nextID++
	id := t.nextID
	t.byID[id] = n
	t.byNode[n.Raw()] = id
	return id
}

// Get returns the node bound to id, or nil.
func (t *nodeTable) Get(id uint32) *webapi.Node {
	if id == 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.byID[id]
}

// Len returns the number of distinct handles. Test helper.
func (t *nodeTable) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.byID)
}
