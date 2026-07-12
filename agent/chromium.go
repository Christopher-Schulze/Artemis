package agent

import (
	"context"
	"errors"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

// ChromiumAgent exposes the canonical observation and action contracts to agent callers.
type ChromiumAgent struct {
	runtime  *actions.Runtime
	observer *bridgeobserve.Collector
}

func NewChromiumAgent(runtime *actions.Runtime, observer *bridgeobserve.Collector) (*ChromiumAgent, error) {
	if runtime == nil {
		return nil, errors.New("chromium agent: action runtime required")
	}
	if observer == nil {
		return nil, errors.New("chromium agent: observer required")
	}
	return &ChromiumAgent{runtime: runtime, observer: observer}, nil
}
func (a *ChromiumAgent) Observe(ctx context.Context, mode bridgeobserve.Mode, subtreeRef string) (bridgeobserve.Snapshot, error) {
	return a.observer.Capture(ctx, mode, subtreeRef)
}
func (a *ChromiumAgent) Act(ctx context.Context, request actions.Request) actions.Outcome {
	return a.runtime.Execute(ctx, request)
}
