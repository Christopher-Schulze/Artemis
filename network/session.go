package network

import (
	"context"
	"strings"
)

type sessionContextKey struct{}

// WithSessionID binds a runtime session identity to all nested network work.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil || strings.TrimSpace(sessionID) == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionContextKey{}, strings.TrimSpace(sessionID))
}

type robotsAgentContextKey struct{}

// WithRobotsAgent marks nested requests as robots-governed crawler traffic for
// userAgent (empty selects the default group): every redirect hop is admitted
// by the same robots.txt decision as the initial URL.
func WithRobotsAgent(ctx context.Context, userAgent string) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, robotsAgentContextKey{}, robotsAgentMark{agent: userAgent, active: true})
}

// withoutRobotsAgent clears the robots mark for requests that are not crawled
// pages themselves, such as the robots.txt fetch.
func withoutRobotsAgent(ctx context.Context) context.Context {
	return context.WithValue(ctx, robotsAgentContextKey{}, robotsAgentMark{})
}

type robotsAgentMark struct {
	agent  string
	active bool
}

func robotsAgent(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	mark, ok := ctx.Value(robotsAgentContextKey{}).(robotsAgentMark)
	return mark.agent, ok && mark.active
}

// SessionID returns the bound runtime identity or the supplied fallback.
func SessionID(ctx context.Context, fallback string) string {
	if ctx != nil {
		if value, ok := ctx.Value(sessionContextKey{}).(string); ok && value != "" {
			return value
		}
	}
	return fallback
}
