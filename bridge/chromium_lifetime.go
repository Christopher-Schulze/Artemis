package bridge

import (
	"context"
	"fmt"
)

func (p *Page) startFrameMonitor(sub *CDPSubscription) error {
	p.owner.mu.Lock()
	defer p.owner.mu.Unlock()
	if p.owner.closed {
		return &CDPError{Code: CDPErrorClosed, Op: "start frame monitor", Err: fmt.Errorf("owning context closed")}
	}
	p.owner.browser.mu.RLock()
	defer p.owner.browser.mu.RUnlock()
	if p.owner.browser.closed || p.owner.browser.terminal != nil {
		return &CDPError{Code: CDPErrorClosed, Op: "start frame monitor", Err: fmt.Errorf("owning browser closed")}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.removed || p.state != TargetStateAttached {
		return &TargetError{TargetID: p.targetID, State: p.state, Message: "page retired before frame monitor startup"}
	}
	p.frameMu.Lock()
	defer p.frameMu.Unlock()
	if p.frameDone != nil {
		return fmt.Errorf("frame monitor already started")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	p.frameSub, p.frameCancel, p.frameDone = sub, cancel, done
	go func() {
		defer close(done)
		defer cancel()
		p.monitorFrameTargets(ctx, sub)
	}()
	return nil
}

func (p *Page) cancelFrameRouting() {
	p.frameMu.RLock()
	cancel := p.frameCancel
	p.frameMu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

func (p *Page) stopFrameRouting() {
	p.frameMu.Lock()
	cancel, sub, done := p.frameCancel, p.frameSub, p.frameDone
	p.frameCancel, p.frameSub = nil, nil
	p.frameMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if sub != nil {
		sub.Close()
	}
	if done != nil {
		<-done
	}
}
