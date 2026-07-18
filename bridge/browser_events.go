package bridge

import (
	"fmt"

	"github.com/Christopher-Schulze/Artemis/network"
)

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
