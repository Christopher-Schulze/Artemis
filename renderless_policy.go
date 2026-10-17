package artemis

import (
	"context"
	"errors"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/engine"
)

// Managed commands authorize the current page, including its final redirect URL.
// Page itself exposes a trusted low-level handle, not a managed command boundary.
func (s *Session) authorizeRenderlessPage(ctx context.Context, pageID, action string) (*engine.Page, *TaskError) {
	if ctx == nil || s.owner == nil {
		return nil, newTaskError(TaskErrorInvalidInput, "browser_policy", errors.New("session owner and context required"))
	}
	if !s.IsActive() {
		return nil, newTaskError(TaskErrorStaleReference, "browser_policy", errors.New("session closed"))
	}
	page := s.Page(pageID)
	if page == nil {
		return nil, newTaskError(TaskErrorPageNotFound, action, errors.New("page not found"))
	}
	if err := s.owner.authorizeBrowserRequest(ctx, bridge.PolicyRequest{Action: action, URL: page.URL()}); err != nil {
		return nil, classifyTaskError("browser_policy", err)
	}
	return page, nil
}
