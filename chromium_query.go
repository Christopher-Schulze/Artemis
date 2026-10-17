package artemis

import (
	"context"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

// QueryChromiumPage returns page-bound task observations and revision deltas.
func (s *Session) QueryChromiumPage(ctx context.Context, pageID string, request bridgeobserve.QueryRequest) (bridgeobserve.QueryResult, *TaskError) {
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return bridgeobserve.QueryResult{}, taskErr
	}
	defer done()
	page, release, taskErr := s.leaseChromiumPage(execCtx, pageID)
	if taskErr != nil {
		return bridgeobserve.QueryResult{}, taskErr
	}
	defer release.Release()
	execCtx = release.Context()
	for _, ref := range []*string{&request.Options.SubtreeRef, &request.Options.TargetRef} {
		if *ref == "" {
			continue
		}
		value, err := pageReference(pageID, *ref)
		if err != nil {
			return bridgeobserve.QueryResult{}, classifyTaskError("chromium_query_ref", err)
		}
		*ref = value
	}
	if taskErr := s.authorizeChromiumOperation(execCtx, page, "query", request.Options.FrameID, request.Options.SubtreeRef, request.Options.TargetRef); taskErr != nil {
		return bridgeobserve.QueryResult{}, taskErr
	}
	result, err := page.observer.QueryWithNamespace(execCtx, request, pageID+":")
	if err != nil {
		return bridgeobserve.QueryResult{}, classifyTaskError("chromium_query", err)
	}
	return result, nil
}
