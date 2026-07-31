package actions

import (
	"context"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/cdpops"
)

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
