package artemis

import (
	"context"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
)

func (s *Session) authorizeChromiumOperation(ctx context.Context, page *ownedChromiumPage, action, frameID string, refs ...string) *TaskError {
	request := actions.Request{Kind: actions.Kind(action), FrameID: frameID}
	if len(refs) > 0 {
		request.Ref = refs[0]
	}
	if len(refs) > 1 {
		request.TargetRef = refs[1]
	}
	policyRequest, err := page.actions.PolicyRequest(ctx, request)
	if err != nil {
		return classifyTaskError("browser_policy_context", err)
	}
	if err := s.owner.authorizeBrowserRequest(ctx, policyRequest); err != nil {
		return classifyTaskError("browser_policy", err)
	}
	return nil
}
