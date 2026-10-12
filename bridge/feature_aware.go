package bridge

import (
	"fmt"
	"strings"
	"sync"
)

// BrowserFeature identifies a capability the browser bridge may expose
// (spec L4284).
type BrowserFeature string

const (
	FeatureWebSocket   BrowserFeature = "websocket"
	FeatureTracing     BrowserFeature = "tracing"
	FeatureHAR         BrowserFeature = "har"
	FeatureAnnotations BrowserFeature = "annotations"
	FeatureStreaming   BrowserFeature = "streaming"
	FeatureDevTools    BrowserFeature = "devtools"
)

// FeatureStatus records the availability and version of a feature.
type FeatureStatus struct {
	Feature   BrowserFeature `json:"feature"`
	Available bool           `json:"available"`
	Version   string         `json:"version,omitempty"`
}

// FeatureRegistry tracks which browser features are available so the bridge
// can emit feature-aware warnings when a required capability is missing.
type FeatureRegistry struct {
	mu       sync.RWMutex
	features map[BrowserFeature]FeatureStatus
}

// NewFeatureRegistry builds an empty FeatureRegistry.
func NewFeatureRegistry() *FeatureRegistry {
	return &FeatureRegistry{
		features: make(map[BrowserFeature]FeatureStatus),
	}
}

// RegisterFeature records the availability and version of a feature.
func (r *FeatureRegistry) RegisterFeature(feature BrowserFeature, available bool, version string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.features[feature] = FeatureStatus{
		Feature:   feature,
		Available: available,
		Version:   version,
	}
}

// IsAvailable reports whether a feature is registered and available.
func (r *FeatureRegistry) IsAvailable(feature BrowserFeature) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	st, ok := r.features[feature]
	if !ok {
		return false
	}
	return st.Available
}

// GetFeature returns the status for a feature, or false if not registered.
func (r *FeatureRegistry) GetFeature(feature BrowserFeature) (FeatureStatus, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	st, ok := r.features[feature]
	return st, ok
}

// GetAllFeatures returns the status of every registered feature.
func (r *FeatureRegistry) GetAllFeatures() []FeatureStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]FeatureStatus, 0, len(r.features))
	for _, st := range r.features {
		out = append(out, st)
	}
	return out
}

// CheckWarning returns a non-empty warning string if any of the required
// features are unavailable. Returns an empty string when all required
// features are available.
func (r *FeatureRegistry) CheckWarning(required []BrowserFeature) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var missing []string
	for _, f := range required {
		st, ok := r.features[f]
		if !ok || !st.Available {
			missing = append(missing, string(f))
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("Required browser features unavailable: %s. Some capabilities may be degraded.",
		strings.Join(missing, ", "))
}

// RegisterAll registers a batch of feature statuses at once.
func (r *FeatureRegistry) RegisterAll(statuses []FeatureStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, st := range statuses {
		r.features[st.Feature] = st
	}
}

// Unregister removes a feature from the registry.
func (r *FeatureRegistry) Unregister(feature BrowserFeature) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.features, feature)
}

// Count returns the number of registered features.
func (r *FeatureRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.features)
}

// AvailableCount returns the number of registered features marked available.
func (r *FeatureRegistry) AvailableCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, st := range r.features {
		if st.Available {
			count++
		}
	}
	return count
}
