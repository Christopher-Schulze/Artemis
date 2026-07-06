package bridge

import (
	"fmt"
	"sync"
)

// FrameRegistry maps frame_id to CDP session info for cross-origin iframe
// support (spec ss28.10: frame_selector for iframe routing).
type FrameRegistry struct {
	mu     sync.RWMutex
	frames map[string]FrameInfo
}

// FrameInfo describes a registered iframe.
type FrameInfo struct {
	FrameID    string
	ParentID   string
	URL        string
	Name       string
	CDPSession string
}

// NewFrameRegistry creates an empty frame registry.
func NewFrameRegistry() *FrameRegistry {
	return &FrameRegistry{frames: make(map[string]FrameInfo)}
}

// Register adds or updates a frame in the registry.
func (r *FrameRegistry) Register(info FrameInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames[info.FrameID] = info
}

// Unregister removes a frame from the registry.
func (r *FrameRegistry) Unregister(frameID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.frames, frameID)
}

// Get returns frame info by frame_id.
func (r *FrameRegistry) Get(frameID string) (FrameInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.frames[frameID]
	return info, ok
}

// All returns all registered frames.
func (r *FrameRegistry) All() []FrameInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]FrameInfo, 0, len(r.frames))
	for _, info := range r.frames {
		out = append(out, info)
	}
	return out
}

// FrameSelector routes actions to iframes by frame_selector (ref/selector).
// Per CoPaw pattern: empty frame_selector means main page; non-empty means
// use page.frame_locator(frameSelector) to target the iframe.
type FrameSelector struct {
	registry *FrameRegistry
}

// NewFrameSelector creates a FrameSelector with the given registry.
func NewFrameSelector(registry *FrameRegistry) *FrameSelector {
	return &FrameSelector{registry: registry}
}

// ResolveFrame determines which frame to route an action to.
// frameSelector is a CSS selector or ref identifying the iframe.
// Returns the FrameInfo for the target frame, or an error if not found.
// Empty frameSelector means the main page (no iframe).
func (s *FrameSelector) ResolveFrame(frameSelector string) (*FrameInfo, error) {
	if frameSelector == "" || (len(frameSelector) > 0 && frameSelector[0] == 0) {
		return nil, nil // main page, no iframe
	}
	// Try to match by frame_id first
	if info, ok := s.registry.Get(frameSelector); ok {
		return &info, nil
	}
	// Try to match by frame name
	for _, info := range s.registry.All() {
		if info.Name == frameSelector {
			return &info, nil
		}
	}
	return nil, fmt.Errorf("frame selector: frame %q not found", frameSelector)
}

// IsMainFrame returns true when the frame selector targets the main page
// (empty selector).
func (s *FrameSelector) IsMainFrame(frameSelector string) bool {
	return frameSelector == ""
}
