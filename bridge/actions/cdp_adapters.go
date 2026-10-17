package actions

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/cdpops"
)

var _ cdpops.LifecycleWatcher = pageCaller{}

// pageCaller keeps navigation inside the bridge policy path while exposing
// the common typed CDP caller to cdpops.
type pageCaller struct {
	page *bridge.Page
}

func (c pageCaller) Call(ctx context.Context, method string, params, result any) error {
	if c.page == nil {
		return cdpops.ErrCallerRequired
	}
	return c.page.Call(ctx, method, params, result)
}

func (c pageCaller) Navigate(ctx context.Context, url, _ string, result *cdpops.NavigationResponse) error {
	if c.page == nil {
		return cdpops.ErrCallerRequired
	}
	frameID, loaderID, err := c.page.Navigate(ctx, url)
	if result != nil {
		result.FrameID = frameID
		result.LoaderID = loaderID
	}
	return err
}

// WatchLifecycle forwards Page.lifecycleEvent notifications of this page's own
// CDP session through the existing browser event subscription.
func (c pageCaller) WatchLifecycle() (<-chan cdpops.LifecycleEvent, func(), error) {
	if c.page == nil {
		return nil, nil, cdpops.ErrCallerRequired
	}
	subscription, err := c.page.SubscribeBrowserEvents(256)
	if err != nil {
		return nil, nil, err
	}
	sessionID := c.page.SessionID()
	events, done, finished := make(chan cdpops.LifecycleEvent, 16), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		defer close(events)
		for {
			select {
			case <-done:
				return
			case event, ok := <-subscription.Events:
				if !ok {
					return
				}
				if event.Method != "Page.lifecycleEvent" || event.SessionID != sessionID {
					continue
				}
				var lifecycle cdpops.LifecycleEvent
				if err := json.Unmarshal(event.Params, &lifecycle); err != nil {
					continue // A malformed notification cannot satisfy any wait.
				}
				select {
				case events <- lifecycle:
				case <-done:
					return
				}
			}
		}
	}()
	var once sync.Once
	return events, func() {
		once.Do(func() {
			close(done)
			subscription.Close()
			<-finished
		})
	}, nil
}
