package network

import (
	"context"
	"testing"
)

func TestSessionIDContextOverridesFallback(t *testing.T) {
	ctx := WithSessionID(context.Background(), "runtime-session")
	if got := SessionID(ctx, "configured-session"); got != "runtime-session" {
		t.Fatalf("SessionID=%q", got)
	}
	if got := SessionID(context.Background(), "configured-session"); got != "configured-session" {
		t.Fatalf("fallback SessionID=%q", got)
	}
}
