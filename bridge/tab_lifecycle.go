package bridge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/tabs"
)

// TabProcessInfo is the immutable native target identity admitted by its host.
type TabProcessInfo struct {
	ID        string
	TargetID  string
	ContextID string
	Spec      tabs.ProcessSpec
	Owner     tabs.OwnerRef
}

// TabProcessSupervisor connects Artemis lifetimes to an embedding host's
// existing process/state authorities without importing its private packages.
type TabProcessSupervisor interface {
	RegisterTab(context.Context, *Page) error
	TransitionTab(context.Context, TabProcessInfo, tabs.TabLifecycleState, tabs.TabLifecycleState) error
}

type TabProcessConfig struct {
	Kind         tabs.ProcessSpecKind
	Owner        tabs.OwnerRef
	PriorityLane string
	Supervisor   TabProcessSupervisor
}

// ConfigureTabProcesses binds host ownership before the context creates pages.
func (c *BrowserContext) ConfigureTabProcesses(config TabProcessConfig) error {
	if config.Kind != "" && config.Kind != tabs.ProcessSpecStream && config.Kind != tabs.ProcessSpecTemporary {
		return fmt.Errorf("tab process: unsupported process kind")
	}
	switch config.Owner {
	case "", tabs.OwnerRefTurn, tabs.OwnerRefSubagent, tabs.OwnerRefConnector, tabs.OwnerRefUI:
	default:
		return fmt.Errorf("tab process: unsupported owner")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || len(c.pages) != 0 {
		return fmt.Errorf("tab process: configure before creating pages")
	}
	c.tabProcesses = config
	return nil
}

func (p *Page) ProcessInfo() TabProcessInfo { return p.processInfo }

// SetExecutionSuspended pauses new scheduled work without closing the renderer.
func (p *Page) SetExecutionSuspended(ctx context.Context, suspended bool) error {
	if ctx == nil || p == nil || p.lifecycle == nil {
		return fmt.Errorf("tab suspension: context and initialized page required")
	}
	if err := p.ensureAttached(); err != nil {
		return err
	}
	if !suspended {
		return p.transitionLifecycle(tabs.TabLifecycleActive)
	}
	lease, err := p.AcquireExecution(ctx)
	if err != nil {
		return err
	}
	defer lease.Release()
	return p.transitionLifecycle(tabs.TabLifecycleSuspended)
}

func (p *Page) registerSupervisor(ctx context.Context) error {
	p.transitionMu.Lock()
	defer p.transitionMu.Unlock()
	if p.processSupervisor == nil {
		return nil
	}
	if err := p.lifetime.Err(); err != nil {
		return err
	}
	if err := p.processSupervisor.RegisterTab(ctx, p); err != nil {
		return err
	}
	p.supervised = true
	return nil
}

// DownSignal closes once when the real target closes, crashes or loses CDP.
func (p *Page) DownSignal() <-chan struct{} { return p.lifecycle.DownSignal() }

// LifecycleState reports the session-owned execution/lifecycle projection.
func (p *Page) LifecycleState() tabs.TabLifecycleState { return p.lifecycle.State() }

// CleanupDone closes after registered target resources have been joined/drained.
func (p *Page) CleanupDone() <-chan struct{} { return p.cleanupDone }

// AddCleanup transfers an acquired resource to this page's LIFO cleanup owner.
// Late registration disposes the incoming resource and reports target retirement.
func (p *Page) AddCleanup(cleanup func() error) error {
	if p == nil || cleanup == nil {
		return fmt.Errorf("tab cleanup: page and cleanup required")
	}
	p.cleanupMu.Lock()
	p.mu.RLock()
	attached := !p.removed && p.state == TargetStateAttached
	p.mu.RUnlock()
	if !p.resourcesRetired && attached && (p.lifetime == nil || p.lifetime.Err() == nil) {
		p.cleanups = append(p.cleanups, cleanup)
		p.cleanupMu.Unlock()
		return nil
	}
	p.cleanupMu.Unlock()
	return errors.Join(fmt.Errorf("tab cleanup: target resources already retired"), cleanup())
}

func (p *Page) retireResources() error {
	p.cleanupOnce.Do(func() {
		p.cleanupMu.Lock()
		p.resourcesRetired = true
		cleanups := p.cleanups
		p.cleanups = nil
		p.cleanupMu.Unlock()
		for i := len(cleanups) - 1; i >= 0; i-- {
			p.cleanupErr = errors.Join(p.cleanupErr, cleanups[i]())
		}
		if p.dialogs != nil {
			p.dialogs.Clear(p.targetID)
		}
		if p.cleanupDone != nil {
			close(p.cleanupDone)
		}
	})
	return p.cleanupErr
}

func (p *Page) retireAfterDown() {
	state := p.State()
	p.owner.mu.Lock()
	closing := p.owner.closed
	p.owner.mu.Unlock()
	if !closing && state != TargetStateAttached {
		if err := p.close(state != TargetStateClosed); err != nil {
			p.mu.Lock()
			p.retirementErr = err
			p.mu.Unlock()
			return
		}
	}
	p.mu.RLock()
	removed := p.removed
	p.mu.RUnlock()
	if !removed {
		return // Retain resource/ownership handles until native retirement succeeds.
	}
	if err := p.drainExecution(context.Background()); err != nil {
		p.mu.Lock()
		p.retirementErr = errors.Join(p.retirementErr, err)
		p.mu.Unlock()
		return
	}
	if err := p.retireResources(); err != nil {
		p.mu.Lock()
		p.retirementErr = errors.Join(p.retirementErr, err)
		p.mu.Unlock()
	}
}

func (p *Page) transitionLifecycle(next tabs.TabLifecycleState) error {
	if p.lifecycle == nil {
		return nil // Unregistered targets have no lifecycle projection.
	}
	p.transitionMu.Lock()
	defer p.transitionMu.Unlock()
	current := p.lifecycle.State()
	if current == next || current == tabs.TabLifecycleClosed || current == tabs.TabLifecycleLost {
		return nil
	}
	if current == tabs.TabLifecycleSuspended && next == tabs.TabLifecycleIdle {
		return nil
	}
	var hostErr error
	if p.supervised {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		hostErr = p.processSupervisor.TransitionTab(ctx, p.processInfo, current, next)
		cancel()
		if hostErr != nil && next != tabs.TabLifecycleClosed && next != tabs.TabLifecycleLost {
			return hostErr
		}
	}
	return errors.Join(hostErr, p.lifecycle.Transition(next))
}

// RetirementError preserves cleanup/remote-close failure evidence after down.
func (p *Page) RetirementError() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.retirementErr
}
