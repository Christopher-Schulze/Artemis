package bridge

import (
	"context"
	"errors"
	"fmt"
)

// AcquireDownloadLease serializes ownership of Browser.setDownloadBehavior
// within this page's browser context. The behavior and its download path are
// per context, so a concurrent download on a sibling page could otherwise
// redirect the file into a foreign stage or deny an in-flight transfer.
func (p *Page) AcquireDownloadLease(ctx context.Context) (release func(), err error) {
	if ctx == nil {
		return nil, errors.New("download lease: context required")
	}
	if p == nil || p.owner == nil {
		return nil, errors.New("download lease: page has no browser context")
	}
	slot := p.owner.downloadLeaseSlot()
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("download lease: %w", ctx.Err())
	}
}

func (c *BrowserContext) downloadLeaseSlot() chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.downloadSlot == nil {
		c.downloadSlot = make(chan struct{}, 1)
	}
	return c.downloadSlot
}

type frameTreeNode struct {
	Frame struct {
		ID string `json:"id"`
	} `json:"frame"`
	ChildFrames []frameTreeNode `json:"childFrames"`
}

// OwnsFrame reports whether frameID is this page's main frame, one of its
// same-process child frames or an attached out-of-process frame. Browser
// download events are browser-wide; this binds them to the initiating page.
func (p *Page) OwnsFrame(ctx context.Context, frameID string) (bool, error) {
	if frameID == "" {
		return false, nil
	}
	if frameID == p.targetID {
		return true, nil
	}
	if _, attached := p.FrameSessions()[frameID]; attached {
		return true, nil
	}
	var result struct {
		FrameTree frameTreeNode `json:"frameTree"`
	}
	if err := p.Call(ctx, "Page.getFrameTree", nil, &result); err != nil {
		return false, fmt.Errorf("frame ownership: %w", err)
	}
	pending := []frameTreeNode{result.FrameTree}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if node.Frame.ID == frameID {
			return true, nil
		}
		pending = append(pending, node.ChildFrames...)
	}
	return false, nil
}
