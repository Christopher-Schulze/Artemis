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

// SessionID returns the bound runtime identity or the supplied fallback.
func SessionID(ctx context.Context, fallback string) string {
	if ctx != nil {
		if value, ok := ctx.Value(sessionContextKey{}).(string); ok && value != "" {
			return value
		}
	}
	return fallback
}
