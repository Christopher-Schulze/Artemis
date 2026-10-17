package bridge

import (
	"fmt"

	"github.com/Christopher-Schulze/Artemis/network"
)

// SetFetchRequestHandler installs the application-owned resolver for requests
// that passed Artemis' network policy. Bounded page egress workers may invoke
// handlers concurrently. Handlers must honor the supplied cancellation and
// return promptly after registering a paused request. They must not close/join
// their own executing page/context/browser synchronously from the callback.
// Returning handled=true transfers the paused CDP request to the resolver;
// deferred resolution remains subject to the owning page's lifetime.
func (p *Page) SetFetchRequestHandler(handler FetchRequestHandler) error {
	if p == nil {
		return fmt.Errorf("set fetch request handler: page required")
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.removed || p.state != TargetStateAttached {
		return &TargetError{TargetID: p.targetID, State: p.state, Message: p.stateErr}
	}
	p.fetchMu.Lock()
	p.fetchHandler = handler
	p.fetchMu.Unlock()
	return nil
}

// SubscribeBrowserEvents opens a bounded subscription to browser-level CDP
// events for operations such as verified download lifecycle handling.
func (p *Page) SubscribeBrowserEvents(buffer int) (*CDPSubscription, error) {
	if err := p.ensureAttached(); err != nil {
		return nil, err
	}
	if p.owner == nil || p.owner.browser == nil || p.owner.browser.transport == nil {
		return nil, fmt.Errorf("subscribe browser events: page has no browser owner")
	}
	return p.owner.browser.transport.Subscribe(buffer)
}

// NetworkPolicy returns the immutable policy owner shared by browser egress
// and download validation.
func (p *Page) NetworkPolicy() (*network.Policy, error) {
	if err := p.ensureAttached(); err != nil {
		return nil, err
	}
	if p.owner == nil || p.owner.browser == nil || p.owner.browser.policy == nil {
		return nil, fmt.Errorf("page network policy unavailable")
	}
	return p.owner.browser.policy, nil
}
