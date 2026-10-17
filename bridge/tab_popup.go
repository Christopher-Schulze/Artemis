package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/tabs"
)

type popupCloseJob struct {
	owner    *BrowserContext
	targetID string
}

// SetPopupBlocking controls only popups whose opener belongs to this context.
func (c *BrowserContext) SetPopupBlocking(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.popups == nil {
		c.popups = tabs.NewPopupBlocker(enabled)
	} else if enabled {
		c.popups.Enable()
	} else {
		c.popups.Disable()
	}
}

// PopupStatus reports confirmed closes and any failure that revoked admission.
func (c *BrowserContext) PopupStatus() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.popups == nil {
		return 0, c.popupErr
	}
	return c.popups.BlockedCount(), c.popupErr
}

func (b *ChromiumBrowser) popupCloseForEvent(event CDPEvent) (popupCloseJob, bool) {
	if event.Method != "Target.targetCreated" {
		return popupCloseJob{}, false
	}
	var payload struct {
		TargetInfo struct {
			TargetID         string `json:"targetId"`
			Type             string `json:"type"`
			OpenerID         string `json:"openerId"`
			BrowserContextID string `json:"browserContextId"`
		} `json:"targetInfo"`
	}
	if json.Unmarshal(event.Params, &payload) != nil {
		return popupCloseJob{}, false
	}
	info := payload.TargetInfo
	if info.TargetID == "" || info.Type != "page" || info.OpenerID == "" {
		return popupCloseJob{}, false
	}
	b.mu.RLock()
	opener := b.pages[info.OpenerID]
	owner := b.contexts[info.BrowserContextID]
	b.mu.RUnlock()
	if opener != nil {
		if opener.owner == nil || opener.owner.id != info.BrowserContextID {
			return popupCloseJob{}, false
		}
		owner = opener.owner
	} else if info.BrowserContextID == "" || owner == nil {
		return popupCloseJob{}, false
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.popups == nil {
		owner.popups = tabs.NewPopupBlocker(true)
	}
	if !owner.popups.ShouldBlock(info.OpenerID, true) {
		return popupCloseJob{}, false
	}
	return popupCloseJob{owner: owner, targetID: info.TargetID}, true
}

func (b *ChromiumBrowser) closePopup(ctx context.Context, job popupCloseJob) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := b.closeTarget(ctx, job.targetID)
	if err == nil {
		err = b.confirmPopupAbsent(ctx, job.targetID)
	} else if exists, reconcileErr := b.targetExists(ctx, job.targetID); reconcileErr == nil && !exists {
		err = nil
	}
	job.owner.mu.Lock()
	if err == nil {
		job.owner.popups.RecordClosed()
	} else if !job.owner.closed {
		job.owner.popupErr = fmt.Errorf("popup target %s close unconfirmed: %w", job.targetID, err)
	}
	failure := job.owner.popupErr
	job.owner.mu.Unlock()
	if failure != nil {
		job.owner.revokePopupAdmission(failure)
	}
}

func (c *BrowserContext) revokePopupAdmission(cause error) {
	c.mu.Lock()
	c.popupErr = cause
	pages := make([]*Page, 0, len(c.pages))
	for _, page := range c.pages {
		pages = append(pages, page)
	}
	c.mu.Unlock()
	for _, page := range pages {
		if page.cancelLifetime != nil {
			page.cancelLifetime()
		}
	}
}

func (b *ChromiumBrowser) closeFailedPopupContexts(ctx context.Context) {
	b.mu.RLock()
	contexts := make([]*BrowserContext, 0, len(b.contexts))
	for _, owner := range b.contexts {
		contexts = append(contexts, owner)
	}
	b.mu.RUnlock()
	for _, owner := range contexts {
		if ctx.Err() != nil {
			return
		}
		owner.mu.Lock()
		failed := owner.popupErr != nil && !owner.closed
		owner.mu.Unlock()
		if failed {
			if err := owner.closeWithContext(ctx, true); err != nil {
				owner.mu.Lock()
				owner.popupErr = errors.Join(owner.popupErr, fmt.Errorf("popup context cleanup failed: %w", err))
				owner.mu.Unlock()
			}
		}
	}
}

func (b *ChromiumBrowser) confirmPopupAbsent(ctx context.Context, targetID string) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		exists, err := b.targetExists(ctx, targetID)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (b *ChromiumBrowser) failTargetMonitoring(cause error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	if cause == nil {
		cause = fmt.Errorf("target event stream ended unexpectedly")
	}
	if b.terminal == nil {
		b.terminal = fmt.Errorf("target lifecycle observation unavailable: %w", cause)
	}
	pages := make([]*Page, 0, len(b.pages))
	for _, page := range b.pages {
		pages = append(pages, page)
	}
	b.mu.Unlock()
	for _, page := range pages {
		page.setState(TargetStateDetached, "target lifecycle observation unavailable")
	}
}
