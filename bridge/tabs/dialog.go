package tabs

import (
	"fmt"
	"sync"
	"unicode/utf8"
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
	SessionID     string       `json:"sessionId,omitempty"`
	Generation    uint64       `json:"generation"`
	Type          DialogType   `json:"type"`
	Message       string       `json:"message"`
	URL           string       `json:"url"`
	DefaultPrompt string       `json:"defaultPrompt,omitempty"`
	Action        DialogAction `json:"action"`
	PromptText    string       `json:"promptText,omitempty"`
	claimed       bool
}

// DialogHandler manages dialogs per tab
// (spec L4021: alert/confirm/prompt handling).
type DialogHandler struct {
	mu             sync.Mutex
	dialogs        map[string][]PendingDialog // tabID -> immutable pending values
	nextGeneration uint64
}

// NewDialogHandler creates a new DialogHandler
// (spec L4021: alert/confirm/prompt handling).
func NewDialogHandler() *DialogHandler {
	return &DialogHandler{
		dialogs: make(map[string][]PendingDialog),
	}
}

// Enqueue adds a pending dialog for a tab
// (spec L4021: alert/confirm/prompt handling).
func (h *DialogHandler) Enqueue(tabID string, dialog *PendingDialog) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if tabID == "" || dialog == nil || !IsValidDialogType(dialog.Type) {
		return fmt.Errorf("dialog: tab and valid dialog required")
	}
	if len(dialog.URL) > 8*1024 {
		return fmt.Errorf("dialog: URL exceeds policy identity budget")
	}
	if len(h.dialogs[tabID]) >= 32 {
		return fmt.Errorf("dialog: tab mailbox full")
	}
	copyDialog := *dialog
	h.nextGeneration++
	copyDialog.Generation, copyDialog.claimed = h.nextGeneration, false
	copyDialog.Message = boundedDialogText(copyDialog.Message)
	copyDialog.DefaultPrompt = boundedDialogText(copyDialog.DefaultPrompt)
	h.dialogs[tabID] = append(h.dialogs[tabID], copyDialog)
	return nil
}

func boundedDialogText(text string) string {
	if len(text) <= 8*1024 {
		return text
	}
	text = text[:8*1024]
	for !utf8.ValidString(text) && len(text) > 0 {
		text = text[:len(text)-1]
	}
	return text
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
	dialogs[0] = PendingDialog{}
	h.dialogs[tabID] = dialogs[1:]
	if len(h.dialogs[tabID]) == 0 {
		delete(h.dialogs, tabID)
	}
	return &dialog, true
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
	dialog := dialogs[0]
	return &dialog, true
}

// Claim reserves exactly the observed dialog for one explicit browser reply.
func (h *DialogHandler) Claim(tabID string, generation uint64) (PendingDialog, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	queue := h.dialogs[tabID]
	if len(queue) == 0 || queue[0].Generation != generation || queue[0].claimed {
		return PendingDialog{}, fmt.Errorf("dialog: observed pending generation unavailable")
	}
	queue[0].claimed = true
	return queue[0], nil
}

// Complete preserves a failed reply for retry and never removes a newer event.
func (h *DialogHandler) Complete(tabID string, generation uint64, handled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	queue := h.dialogs[tabID]
	if len(queue) == 0 || queue[0].Generation != generation {
		return
	}
	if !handled {
		queue[0].claimed = false
		return
	}
	queue[0] = PendingDialog{}
	if len(queue) == 1 {
		delete(h.dialogs, tabID)
	} else {
		h.dialogs[tabID] = queue[1:]
	}
}

// ClearSession removes only dialogs belonging to the renderer that closed them.
func (h *DialogHandler) ClearSession(tabID, sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	queue := h.dialogs[tabID]
	kept := queue[:0]
	for _, dialog := range queue {
		if dialog.SessionID != sessionID {
			kept = append(kept, dialog)
		}
	}
	clear(queue[len(kept):])
	if len(kept) == 0 {
		delete(h.dialogs, tabID)
	} else {
		h.dialogs[tabID] = kept
	}
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
	delete(h.dialogs, tabID)
	return count
}

// DismissAll dismisses all pending dialogs for a tab
// (spec L4021: alert/confirm/prompt handling).
func (h *DialogHandler) DismissAll(tabID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := len(h.dialogs[tabID])
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
