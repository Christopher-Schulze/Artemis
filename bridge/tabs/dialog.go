package tabs

import (
	"fmt"
	"sync"
)

// dialog.go (spec L4021: bridge/tabs/dialog.go - alert/confirm/prompt
// handling).
//
// Multi-tab management: alert/confirm/prompt dialog handling.
// Each tab can have pending dialogs that need to be accepted or
// dismissed.

// DialogType enumerates JavaScript dialog types
// (spec L4021: alert/confirm/prompt handling).
type DialogType string

const (
	DialogTypeAlert        DialogType = "alert"
	DialogTypeConfirm      DialogType = "confirm"
	DialogTypePrompt       DialogType = "prompt"
	DialogTypeBeforeUnload DialogType = "beforeunload"
)

// DialogAction enumerates actions to take on a dialog
// (spec L4021: alert/confirm/prompt handling).
type DialogAction string

const (
	DialogActionAccept  DialogAction = "accept"
	DialogActionDismiss DialogAction = "dismiss"
)

// PendingDialog represents a pending JavaScript dialog on a tab
// (spec L4021: alert/confirm/prompt handling).
type PendingDialog struct {
	Type          DialogType   `json:"type"`
	Message       string       `json:"message"`
	URL           string       `json:"url"`
	DefaultPrompt string       `json:"defaultPrompt,omitempty"`
	Action        DialogAction `json:"action"`
	PromptText    string       `json:"promptText,omitempty"`
}

// DialogHandler manages dialogs per tab
// (spec L4021: alert/confirm/prompt handling).
type DialogHandler struct {
	mu      sync.Mutex
	dialogs map[string][]*PendingDialog // tabID -> pending dialogs
}

// NewDialogHandler creates a new DialogHandler
// (spec L4021: alert/confirm/prompt handling).
func NewDialogHandler() *DialogHandler {
	return &DialogHandler{
		dialogs: make(map[string][]*PendingDialog),
	}
}

// Enqueue adds a pending dialog for a tab
// (spec L4021: alert/confirm/prompt handling).
func (h *DialogHandler) Enqueue(tabID string, dialog *PendingDialog) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dialogs[tabID] = append(h.dialogs[tabID], dialog)
}

// Dequeue removes and returns the next pending dialog for a tab
// (spec L4021: alert/confirm/prompt handling).
func (h *DialogHandler) Dequeue(tabID string) (*PendingDialog, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	dialogs := h.dialogs[tabID]
	if len(dialogs) == 0 {
		return nil, false
	}
	dialog := dialogs[0]
	h.dialogs[tabID] = dialogs[1:]
	if len(h.dialogs[tabID]) == 0 {
		delete(h.dialogs, tabID)
	}
	return dialog, true
}

// Peek returns the next pending dialog without removing it
// (spec L4021: alert/confirm/prompt handling).
func (h *DialogHandler) Peek(tabID string) (*PendingDialog, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	dialogs := h.dialogs[tabID]
	if len(dialogs) == 0 {
		return nil, false
	}
	return dialogs[0], true
}

// Pending returns the number of pending dialogs for a tab
// (spec L4021: alert/confirm/prompt handling).
func (h *DialogHandler) Pending(tabID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.dialogs[tabID])
}

// Clear removes all pending dialogs for a tab
// (spec L4021: alert/confirm/prompt handling).
func (h *DialogHandler) Clear(tabID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := len(h.dialogs[tabID])
	delete(h.dialogs, tabID)
	return count
}

// AcceptAll accepts all pending dialogs for a tab
// (spec L4021: alert/confirm/prompt handling).
func (h *DialogHandler) AcceptAll(tabID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := len(h.dialogs[tabID])
	for _, d := range h.dialogs[tabID] {
		d.Action = DialogActionAccept
	}
	delete(h.dialogs, tabID)
	return count
}

// DismissAll dismisses all pending dialogs for a tab
// (spec L4021: alert/confirm/prompt handling).
func (h *DialogHandler) DismissAll(tabID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := len(h.dialogs[tabID])
	for _, d := range h.dialogs[tabID] {
		d.Action = DialogActionDismiss
	}
	delete(h.dialogs, tabID)
	return count
}

// IsValidDialogType reports whether a dialog type is valid
// (spec L4021: alert/confirm/prompt handling).
func IsValidDialogType(dt DialogType) bool {
	switch dt {
	case DialogTypeAlert, DialogTypeConfirm, DialogTypePrompt, DialogTypeBeforeUnload:
		return true
	}
	return false
}

// IsValidDialogAction reports whether a dialog action is valid
// (spec L4021: alert/confirm/prompt handling).
func IsValidDialogAction(da DialogAction) bool {
	switch da {
	case DialogActionAccept, DialogActionDismiss:
		return true
	}
	return false
}

// NewAlertDialog creates a new alert dialog
// (spec L4021: alert/confirm/prompt handling).
func NewAlertDialog(message, url string) *PendingDialog {
	return &PendingDialog{
		Type:    DialogTypeAlert,
		Message: message,
		URL:     url,
		Action:  DialogActionAccept,
	}
}

// NewConfirmDialog creates a new confirm dialog
// (spec L4021: alert/confirm/prompt handling).
func NewConfirmDialog(message, url string) *PendingDialog {
	return &PendingDialog{
		Type:    DialogTypeConfirm,
		Message: message,
		URL:     url,
		Action:  DialogActionDismiss,
	}
}

// NewPromptDialog creates a new prompt dialog
// (spec L4021: alert/confirm/prompt handling).
func NewPromptDialog(message, url, defaultPrompt string) *PendingDialog {
	return &PendingDialog{
		Type:          DialogTypePrompt,
		Message:       message,
		URL:           url,
		DefaultPrompt: defaultPrompt,
		Action:        DialogActionAccept,
	}
}

// String returns a diagnostic summary.
func (d PendingDialog) String() string {
	return fmt.Sprintf("PendingDialog{type:%s msg:%q action:%s}", d.Type, d.Message, d.Action)
}
