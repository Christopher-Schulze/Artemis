package agent

import (
	"context"
	"errors"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/profile"
)

// ChromiumAgent exposes the canonical observation and action contracts to agent callers.
type ChromiumAgent struct {
	runtime  *actions.Runtime
	observer *bridgeobserve.Collector
	sessions *profile.BrowserRuntime
}

func (a *ChromiumAgent) WithSessions(sessions *profile.BrowserRuntime) *ChromiumAgent {
	a.sessions = sessions
	return a
}

func (a *ChromiumAgent) OpenSession(ctx context.Context, request profile.OpenSessionRequest, launch browserprocess.LaunchConfig) (*profile.RuntimeSession, error) {
	if a.sessions == nil {
		return nil, errors.New("chromium agent: profile runtime required")
	}
	return a.sessions.Open(ctx, request, launch)
}

func (a *ChromiumAgent) OpenPage(ctx context.Context, sessionID profile.SessionID, owner, url string) (profile.PageID, error) {
	if a.sessions == nil {
		return "", errors.New("chromium agent: profile runtime required")
	}
	pageID, _, err := a.sessions.NewPage(ctx, sessionID, owner, url)
	return pageID, err
}

func (a *ChromiumAgent) CloseSession(ctx context.Context, sessionID profile.SessionID, owner string) error {
	if a.sessions == nil {
		return errors.New("chromium agent: profile runtime required")
	}
	return a.sessions.Close(ctx, sessionID, owner)
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
