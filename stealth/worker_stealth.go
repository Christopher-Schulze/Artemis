package stealth

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// worker_stealth.go (spec L4058: Worker Thread Stealth Parity).
//
// When a worker thread (service worker, shared worker, dedicated worker)
// is attached via target.EventAttachedToTarget, the same UA/emulation
// patches applied to the main page must also be applied to the worker
// context. Without this, a worker can be detected via navigator
// properties that differ from the main page (e.g. navigator.userAgent,
// navigator.platform, navigator.webdriver=true in the worker but false
// in the main page).
//
// Reference: research/webstack/pinchtab-main/internal/bridge/worker_stealth.go:16-96

// WorkerTargetType is the CDP target type for a worker context
// (spec L4058: worker contexts).
type WorkerTargetType string

const (
	WorkerTypeServiceWorker   WorkerTargetType = "service_worker"
	WorkerTypeSharedWorker    WorkerTargetType = "shared_worker"
	WorkerTypeDedicatedWorker WorkerTargetType = "dedicated_worker"
	WorkerTypeOther           WorkerTargetType = "worker"
)

// IsWorkerTarget checks if a CDP target type string indicates a worker
// context (spec L4058: prevents detection via worker navigator properties).
// The CDP target types are "service_worker", "shared_worker", and
// "worker" (dedicated workers are reported as "worker" in CDP).
func IsWorkerTarget(targetType string) bool {
	if targetType == "" {
		return false
	}
	return strings.Contains(targetType, "worker")
}

// WorkerTargetInfo carries the minimal info needed from
// target.EventAttachedToTarget for worker stealth injection.
type WorkerTargetInfo struct {
	TargetID   string `json:"target_id"`
	TargetType string `json:"target_type"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url,omitempty"`
}

// MarshalJSON returns safe JSON for worker target info (spec L4058:
// safe JSON marshaling). Uses json.Marshal which escapes HTML entities
// and validates the output is well-formed JSON.
func (w WorkerTargetInfo) MarshalJSON() ([]byte, error) {
	type alias WorkerTargetInfo
	return json.Marshal(alias(w))
}

// WorkerStealthTracker tracks which worker targets have already received
// stealth patches, preventing duplicate injection (spec L4058:
// same UA/emulation as main page, applied once per worker).
type WorkerStealthTracker struct {
	mu      sync.RWMutex
	applied map[string]struct{}
}

// NewWorkerStealthTracker creates a new tracker.
func NewWorkerStealthTracker() *WorkerStealthTracker {
	return &WorkerStealthTracker{
		applied: make(map[string]struct{}),
	}
}

// ShouldApply returns true if stealth patches have not yet been applied
// to the given target ID, and marks it as applied.
func (t *WorkerStealthTracker) ShouldApply(targetID string) bool {
	if targetID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.applied[targetID]; ok {
		return false
	}
	t.applied[targetID] = struct{}{}
	return true
}

// IsApplied checks if stealth patches have been applied to a target.
func (t *WorkerStealthTracker) IsApplied(targetID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.applied[targetID]
	return ok
}

// Reset clears all tracked targets (for testing).
func (t *WorkerStealthTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.applied = make(map[string]struct{})
}

// Count returns the number of tracked targets.
func (t *WorkerStealthTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.applied)
}

// WorkerStealthScript returns the JavaScript stealth patch script for a
// worker context (spec L4058: same UA/emulation as main page).
//
// In a worker context, `self.navigator` is the navigator object (there
// is no `window`). The script patches:
//   - navigator.userAgent (matches main page UA)
//   - navigator.platform (matches main page platform)
//   - navigator.language (matches main page language)
//   - navigator.languages (matches main page languages array)
//   - navigator.webdriver = false (matches main page)
//
// The script uses Object.defineProperty on both the prototype and the
// instance for maximum compatibility across worker types. All JSON
// values are safely marshaled to prevent injection.
func WorkerStealthScript(p Profile) string {
	if p.UserAgent == "" {
		d := Defaults()
		p.UserAgent = d.UserAgent
		p.Vendor = d.Vendor
		p.Platform = d.Platform
		p.Languages = d.Languages
	}

	languagesJSON := SafeJSONStringArray(
		parseLanguages(p.Languages),
		`["en-US","en"]`,
	)

	language := firstLanguage(p.Languages)
	if language == "" {
		language = "en-US"
	}

	return fmt.Sprintf(`(() => {
  try {
    const nav = self.navigator;
    if (!nav) return;
    const target = Object.getPrototypeOf(nav) || nav;
    const define = (name, getter) => {
      try { Object.defineProperty(target, name, { get: getter, configurable: true }); } catch (e) {}
      try { Object.defineProperty(nav, name, { get: getter, configurable: true }); } catch (e) {}
    };
    const ua = %q || nav.userAgent || '';
    const platform = %q || nav.platform || '';
    const language = %q || nav.language || 'en-US';
    const languages = JSON.parse(%q);
    if (ua) define('userAgent', () => ua);
    if (platform) define('platform', () => platform);
    define('language', () => language);
    define('languages', () => languages.slice());
    define('webdriver', () => false);
  } catch (e) {}
})()`, p.UserAgent, p.Platform, language, languagesJSON)
}

// WorkerStealthHandler processes target.EventAttachedToTarget events
// and applies stealth patches to worker contexts (spec L4058).
type WorkerStealthHandler struct {
	tracker *WorkerStealthTracker
	profile Profile
}

// NewWorkerStealthHandler creates a new handler with the given profile.
func NewWorkerStealthHandler(profile Profile) *WorkerStealthHandler {
	return &WorkerStealthHandler{
		tracker: NewWorkerStealthTracker(),
		profile: profile,
	}
}

// HandleAttachedToTarget processes a target.EventAttachedToTarget event.
// Returns the worker target info and stealth script if the target is a
// worker that needs stealth patches, or nil if the target is not a worker
// or has already been patched.
//
// The caller is responsible for actually injecting the script into the
// worker context via the CDP protocol (chromedp.Evaluate or
// Page.addScriptToEvaluateOnNewDocument).
func (h *WorkerStealthHandler) HandleAttachedToTarget(targetID, targetType string) (*WorkerTargetInfo, string, bool) {
	if !IsWorkerTarget(targetType) {
		return nil, "", false
	}
	if !h.tracker.ShouldApply(targetID) {
		return nil, "", false
	}

	info := WorkerTargetInfo{
		TargetID:   targetID,
		TargetType: targetType,
	}
	script := WorkerStealthScript(h.profile)
	return &info, script, true
}

// Tracker returns the underlying tracker (for testing/diagnostics).
func (h *WorkerStealthHandler) Tracker() *WorkerStealthTracker {
	return h.tracker
}

// Profile returns the stealth profile used for worker patches.
func (h *WorkerStealthHandler) Profile() Profile {
	return h.profile
}

// SafeJSONStringArray marshals a string slice to JSON and validates the
// output is a JSON array of strings (spec L4058: safe JSON marshaling).
// Returns the fallback on any error.
func SafeJSONStringArray(values []string, fallback string) string {
	data, err := json.Marshal(values)
	if err != nil {
		return fallback
	}
	// Round-trip to verify output is a valid string array.
	var check []string
	if json.Unmarshal(data, &check) != nil {
		return fallback
	}
	return string(data)
}

// parseLanguages splits a comma-separated language list into a slice.
func parseLanguages(languages string) []string {
	if languages == "" {
		return []string{"en-US", "en"}
	}
	parts := strings.Split(languages, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return []string{"en-US", "en"}
	}
	return result
}

// firstLanguage returns the first language from a comma-separated list.
func firstLanguage(languages string) string {
	parts := parseLanguages(languages)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
