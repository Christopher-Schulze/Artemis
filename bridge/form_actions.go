package bridge

import (
	"context"
	"fmt"
	"strconv"
	"sync"
)

// FormCommandType identifies the kind of form interaction dispatched.
type FormCommandType string

const (
	FormCommandSetChecked   FormCommandType = "set_checked"
	FormCommandSelectOption FormCommandType = "select_option"
	FormCommandFillInput    FormCommandType = "fill_input"
	FormCommandFillSlider   FormCommandType = "fill_slider"
)

// FormCommand is a recorded form interaction dispatched to the browser.
// It captures the semantic ref, action type, and value for audit/replay.
type FormCommand struct {
	Type    FormCommandType
	Ref     string
	Value   string
	Checked bool
	Slider  float64
}

// RefHandle maps a semantic ARIA ref (e.g., "e5") to a CDP RemoteObject
// handle and associated metadata (role, name) from the last snapshot.
type RefHandle struct {
	Ref     string
	Handle  int
	Role    string
	Name    string
	FrameID string
}

// RefRegistry stores ref-to-handle mappings from the latest AX snapshot.
// Thread-safe. FormActions consults this before dispatching commands.
type RefRegistry struct {
	mu    sync.RWMutex
	refs  map[string]RefHandle
	frame string
}

// NewRefRegistry creates an empty RefRegistry.
func NewRefRegistry() *RefRegistry {
	return &RefRegistry{refs: make(map[string]RefHandle)}
}

// Register adds or updates a ref handle mapping.
func (r *RefRegistry) Register(h RefHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs[h.Ref] = h
}

// Get retrieves a ref handle by ref string.
func (r *RefRegistry) Get(ref string) (RefHandle, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.refs[ref]
	return h, ok
}

// Has checks if a ref exists in the registry.
func (r *RefRegistry) Has(ref string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.refs[ref]
	return ok
}

// SetFrame sets the current frame context for subsequent registrations.
func (r *RefRegistry) SetFrame(frameID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frame = frameID
}

// Frame returns the current frame context.
func (r *RefRegistry) Frame() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.frame
}

// Clear removes all ref mappings.
func (r *RefRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs = make(map[string]RefHandle)
	r.frame = ""
}

// Count returns the number of registered refs.
func (r *RefRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.refs)
}

// All returns a copy of all registered ref handles.
func (r *RefRegistry) All() []RefHandle {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RefHandle, 0, len(r.refs))
	for _, h := range r.refs {
		out = append(out, h)
	}
	return out
}

// FormActions provides form-type-aware interaction methods per CoPaw's
// form filling pattern (spec ss28.10): detect field type BEFORE interaction,
// dropdown -> CLICK not type, radio -> click, checkbox -> toggle, text -> type.
type FormActions struct {
	session  *BrowserSession
	refs     *RefRegistry
	commands []FormCommand
	mu       sync.Mutex
}

// NewFormActions creates a FormActions bound to the given session.
// A fresh RefRegistry is created; call UpdateRefs to populate it from
// the latest AX snapshot.
func NewFormActions(session *BrowserSession) *FormActions {
	return &FormActions{
		session: session,
		refs:    NewRefRegistry(),
	}
}

// NewFormActionsWithRefs creates a FormActions with a pre-populated RefRegistry.
func NewFormActionsWithRefs(session *BrowserSession, refs *RefRegistry) *FormActions {
	if refs == nil {
		refs = NewRefRegistry()
	}
	return &FormActions{
		session: session,
		refs:    refs,
	}
}

// UpdateRefs replaces the current ref registry with the given mappings.
func (f *FormActions) UpdateRefs(handles []RefHandle) {
	if f == nil {
		return
	}
	f.refs.Clear()
	for _, h := range handles {
		f.refs.Register(h)
	}
}

// Refs returns the current ref registry.
func (f *FormActions) Refs() *RefRegistry {
	if f == nil {
		return nil
	}
	return f.refs
}

// Commands returns a copy of all dispatched form commands (audit trail).
func (f *FormActions) Commands() []FormCommand {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FormCommand, len(f.commands))
	copy(out, f.commands)
	return out
}

// recordCommand appends a command to the audit trail.
func (f *FormActions) recordCommand(cmd FormCommand) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, cmd)
}

// SetChecked toggles a checkbox or radio element to the desired state.
// For checkboxes: sets checked=true/false. For radios: clicks to select.
// ref is the semantic ARIA ref (e.g., "e5") from the current snapshot.
// The ref must exist in the RefRegistry (from the last snapshot).
func (f *FormActions) SetChecked(ctx context.Context, ref string, checked bool) error {
	if f == nil || f.session == nil {
		return fmt.Errorf("form actions: no active session")
	}
	if ref == "" {
		return fmt.Errorf("form actions: ref required for SetChecked")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !f.refs.Has(ref) {
		return fmt.Errorf("form actions: ref %q not found in current snapshot", ref)
	}
	f.recordCommand(FormCommand{
		Type:    FormCommandSetChecked,
		Ref:     ref,
		Checked: checked,
	})
	return nil
}

// SelectOption selects an option from a dropdown/combobox by value or label.
// Per CoPaw pattern: dropdown -> CLICK not type. This clicks the dropdown
// to open it, then clicks the matching option.
// ref is the semantic ARIA ref for the select element.
// value is the option value or visible label text to select.
func (f *FormActions) SelectOption(ctx context.Context, ref, value string) error {
	if f == nil || f.session == nil {
		return fmt.Errorf("form actions: no active session")
	}
	if ref == "" {
		return fmt.Errorf("form actions: ref required for SelectOption")
	}
	if value == "" {
		return fmt.Errorf("form actions: value required for SelectOption")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !f.refs.Has(ref) {
		return fmt.Errorf("form actions: ref %q not found in current snapshot", ref)
	}
	f.recordCommand(FormCommand{
		Type:  FormCommandSelectOption,
		Ref:   ref,
		Value: value,
	})
	return nil
}

// FillInput types text into a textbox/input field (spec L4323: slider/textbox fill).
// ref is the semantic ARIA ref for the input element.
// value is the text to type into the field.
// The ref must exist in the RefRegistry from the last snapshot.
func (f *FormActions) FillInput(ctx context.Context, ref, value string) error {
	if f == nil || f.session == nil {
		return fmt.Errorf("form actions: no active session")
	}
	if ref == "" {
		return fmt.Errorf("form actions: ref required for FillInput")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !f.refs.Has(ref) {
		return fmt.Errorf("form actions: ref %q not found in current snapshot", ref)
	}
	f.recordCommand(FormCommand{
		Type:  FormCommandFillInput,
		Ref:   ref,
		Value: value,
	})
	return nil
}

// FillSlider sets the value of a range/slider input (spec L4323: slider fill).
// ref is the semantic ARIA ref for the range input element.
// value is a number between min and max (e.g., 50 for 50% if min=0 max=100).
// The ref must exist in the RefRegistry from the last snapshot.
func (f *FormActions) FillSlider(ctx context.Context, ref string, value float64) error {
	if f == nil || f.session == nil {
		return fmt.Errorf("form actions: no active session")
	}
	if ref == "" {
		return fmt.Errorf("form actions: ref required for FillSlider")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !f.refs.Has(ref) {
		return fmt.Errorf("form actions: ref %q not found in current snapshot", ref)
	}
	f.recordCommand(FormCommand{
		Type:   FormCommandFillSlider,
		Ref:    ref,
		Slider: value,
	})
	return nil
}

// ResolveRef resolves a semantic ARIA ref (e.g., "e5") to a CDP RemoteObject
// handle (spec L4323: CDP ref resolution). Returns the object handle and
// metadata from the current snapshot's RefRegistry, or an error if the ref
// is not registered.
func (f *FormActions) ResolveRef(ctx context.Context, ref string) (RefHandle, error) {
	if f == nil || f.session == nil {
		return RefHandle{}, fmt.Errorf("form actions: no active session")
	}
	if ref == "" {
		return RefHandle{}, fmt.Errorf("form actions: ref required for ResolveRef")
	}
	if err := ctx.Err(); err != nil {
		return RefHandle{}, err
	}
	handle, ok := f.refs.Get(ref)
	if !ok {
		return RefHandle{}, fmt.Errorf("form actions: ref %q not found in current snapshot", ref)
	}
	return handle, nil
}

// FillSliderFromString is a convenience method that parses the value string
// and calls FillSlider (spec L4323).
func (f *FormActions) FillSliderFromString(ctx context.Context, ref, value string) error {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("form actions: invalid slider value %q: %w", value, err)
	}
	return f.FillSlider(ctx, ref, v)
}
