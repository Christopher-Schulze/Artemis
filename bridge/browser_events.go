package bridge

import (
	"fmt"

	"github.com/Christopher-Schulze/Artemis/network"
)

// SetFetchRequestHandler installs the application-owned resolver for requests
// that passed Artemis' network policy. The handler is invoked by the single
// page egress worker, so callers must return quickly after registering a
// paused request. Returning handled=true transfers ownership of the paused
// CDP request to the supplied resolver.
func (p *Page) SetFetchRequestHandler(handler FetchRequestHandler) error {
	if p == nil {
		return fmt.Errorf("set fetch request handler: page required")
	}
	if err := p.ensureAttached(); err != nil {
		return err
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
