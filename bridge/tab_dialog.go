package bridge

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/bridge/tabs"
)

// PendingDialog returns a bounded copy of the next real renderer dialog.
func (p *Page) PendingDialog() (*tabs.PendingDialog, bool) {
	if p == nil || p.dialogs == nil {
		return nil, false
	}
	return p.dialogs.Peek(p.targetID)
}

func (p *Page) captureDialogEvent(event CDPEvent) error {
	if event.Method != "Page.javascriptDialogOpening" && event.Method != "Page.javascriptDialogClosed" {
		return nil
	}
	if !p.ownsPolicySession(event.SessionID) {
		return nil
	}
	if event.Method == "Page.javascriptDialogClosed" {
		p.dialogs.ClearSession(p.targetID, event.SessionID)
		return nil
	}
	var dialog struct {
		Type          tabs.DialogType `json:"type"`
		URL           string          `json:"url"`
		Message       string          `json:"message"`
		DefaultPrompt string          `json:"defaultPrompt"`
	}
	if err := json.Unmarshal(event.Params, &dialog); err != nil {
		return fmt.Errorf("decode renderer dialog: %w", err)
	}
	if len(dialog.URL) > 8*1024 {
		return fmt.Errorf("renderer dialog URL exceeds policy identity budget")
	}
	p.dialogs.ClearSession(p.targetID, event.SessionID)
	return p.dialogs.Enqueue(p.targetID, &tabs.PendingDialog{
		SessionID: event.SessionID, Type: dialog.Type, URL: dialog.URL,
		Message: dialog.Message, DefaultPrompt: dialog.DefaultPrompt,
	})
}

// HandlePendingDialog answers one observed generation in its exact renderer.
// It is a control operation that can unblock an ordinary admitted action.
func (p *Page) HandlePendingDialog(ctx context.Context, generation uint64, accept bool, promptText string) error {
	if ctx == nil || p == nil || p.dialogs == nil {
		return fmt.Errorf("dialog: context and initialized page required")
	}
	if err := p.ensureAttached(); err != nil {
		return err
	}
	p.owner.mu.Lock()
	closed, popupErr := p.owner.closed, p.owner.popupErr
	p.owner.mu.Unlock()
	if popupErr != nil {
		return popupErr
	}
	if closed || (p.lifetime != nil && p.lifetime.Err() != nil) {
		return fmt.Errorf("dialog: page owner retired")
	}
	dialog, err := p.dialogs.Claim(p.targetID, generation)
	if err != nil {
		return err
	}
	handled := false
	defer func() { p.dialogs.Complete(p.targetID, generation, handled) }()
	if !p.ownsPolicySession(dialog.SessionID) {
		return fmt.Errorf("dialog: renderer is no longer owned")
	}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if p.lifetime != nil {
		stop := context.AfterFunc(p.lifetime, cancel)
		defer stop()
		if err := p.lifetime.Err(); err != nil {
			return err
		}
	}
	if err := p.ensureAttached(); err != nil {
		return err
	}
	params := struct {
		Accept     bool   `json:"accept"`
		PromptText string `json:"promptText,omitempty"`
	}{Accept: accept, PromptText: promptText}
	if err := p.owner.browser.transport.CallSession(callCtx, dialog.SessionID, "Page.handleJavaScriptDialog", params, nil); err != nil {
		return fmt.Errorf("handle observed dialog: %w", err)
	}
	handled = true
	return nil
}
