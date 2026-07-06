package bridge

import (
	"context"
	"fmt"
	"time"
)

// DefaultWaitTimeout is the default timeout for wait operations (30s per spec).
const DefaultWaitTimeout = 30 * time.Second

// WaitForText waits until the given text appears on the page.
// Uses polling with the given timeout (default 30s per CoPaw spec).
// Returns nil if text is found, error on timeout or context cancellation.
func WaitForText(ctx context.Context, pageTextProvider func() (string, error), text string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = DefaultWaitTimeout
	}
	if pageTextProvider == nil {
		return fmt.Errorf("wait: page text provider required")
	}
	deadline := time.Now().Add(timeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait: timeout waiting for text %q", text)
		case <-ticker.C:
			pageText, err := pageTextProvider()
			if err != nil {
				return fmt.Errorf("wait: page text error: %w", err)
			}
			if contains(pageText, text) {
				return nil
			}
		}
	}
}

// WaitForTextDisappear waits until the given text is no longer present on the page.
// Uses polling with the given timeout (default 30s per CoPaw spec).
// Returns nil if text is gone, error on timeout or context cancellation.
func WaitForTextDisappear(ctx context.Context, pageTextProvider func() (string, error), text string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = DefaultWaitTimeout
	}
	if pageTextProvider == nil {
		return fmt.Errorf("wait: page text provider required")
	}
	deadline := time.Now().Add(timeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait: timeout waiting for text %q to disappear", text)
		case <-ticker.C:
			pageText, err := pageTextProvider()
			if err != nil {
				return fmt.Errorf("wait: page text error: %w", err)
			}
			if !contains(pageText, text) {
				return nil
			}
		}
	}
}

// contains is a simple substring check without importing strings.
func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
