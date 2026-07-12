package bridge

import (
	"fmt"
	"sync"
)

// CDPContextUnit is one unit in the browser target hierarchy.
type CDPContextUnit struct {
	ID       string
	ParentID string
	Kind     ContextKind
}

// CDPContextTree stores parent/child context relationships for CDP routing.
type CDPContextTree struct {
	mu    sync.RWMutex
	units map[string]CDPContextUnit
}

func NewCDPContextTree() *CDPContextTree {
	return &CDPContextTree{units: make(map[string]CDPContextUnit)}
}

func (t *CDPContextTree) Attach(unit CDPContextUnit) error {
	if unit.ID == "" {
		return fmt.Errorf("cdp context: id required")
	}
	if !IsValidContextKind(unit.Kind) {
		return fmt.Errorf("cdp context: invalid kind %q", unit.Kind)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.units[unit.ID]; exists {
		return fmt.Errorf("cdp context: id %q already attached", unit.ID)
	}
	if err := t.validateParent(unit); err != nil {
		return err
	}
	t.units[unit.ID] = unit
	return nil
}

func (t *CDPContextTree) validateParent(unit CDPContextUnit) error {
	if unit.Kind == ContextKindAlloc {
		if unit.ParentID != "" {
			return fmt.Errorf("cdp context: allocation root cannot have parent")
		}
		for _, existing := range t.units {
			if existing.Kind == ContextKindAlloc {
				return fmt.Errorf("cdp context: allocation root already attached")
			}
		}
		return nil
	}
	parent, ok := t.units[unit.ParentID]
	if !ok {
		return fmt.Errorf("cdp context: parent %q missing", unit.ParentID)
	}
	if unit.Kind == ContextKindBrowser && parent.Kind != ContextKindAlloc {
		return fmt.Errorf("cdp context: browser parent must be allocation context")
	}
	if unit.Kind == ContextKindTab && parent.Kind != ContextKindBrowser {
		return fmt.Errorf("cdp context: tab parent must be browser context")
	}
	return nil
}

// Detach removes id and every descendant so no orphan route survives.
func (t *CDPContextTree) Detach(id string) ([]CDPContextUnit, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.units[id]; !ok {
		return nil, fmt.Errorf("cdp context: %q not found", id)
	}
	removed := make([]CDPContextUnit, 0)
	queue := []string{id}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for childID, unit := range t.units {
			if unit.ParentID == current {
				queue = append(queue, childID)
			}
		}
		removed = append(removed, t.units[current])
		delete(t.units, current)
	}
	return removed, nil
}

// Len returns the number of live context units.
func (t *CDPContextTree) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.units)
}

func (t *CDPContextTree) Hierarchy(id string) ([]CDPContextUnit, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var chain []CDPContextUnit
	cur, ok := t.units[id]
	if !ok {
		return nil, fmt.Errorf("cdp context: %q not found", id)
	}
	seen := make(map[string]struct{}, len(t.units))
	for {
		if _, duplicate := seen[cur.ID]; duplicate {
			return nil, fmt.Errorf("cdp context: cycle at %q", cur.ID)
		}
		seen[cur.ID] = struct{}{}
		chain = append([]CDPContextUnit{cur}, chain...)
		if cur.ParentID == "" {
			break
		}
		parent, ok := t.units[cur.ParentID]
		if !ok {
			return nil, fmt.Errorf("cdp context: broken parent %q", cur.ParentID)
		}
		cur = parent
	}
	return chain, nil
}

func (t *CDPContextTree) RootID(id string) (string, error) {
	chain, err := t.Hierarchy(id)
	if err != nil {
		return "", err
	}
	if len(chain) == 0 {
		return "", fmt.Errorf("cdp context: empty hierarchy")
	}
	return chain[0].ID, nil
}
