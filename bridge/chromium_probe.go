package bridge

import (
	"context"
	"fmt"
)

// Probe checks the same live CDP transport that executes page actions. It
// neither starts another browser nor changes any session or page state.
func (b *ChromiumBrowser) Probe(ctx context.Context) error {
	if b == nil || ctx == nil {
		return fmt.Errorf("browser probe: browser and context are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.RLock()
	initialized := b.transport != nil
	b.mu.RUnlock()
	if !initialized {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "probe browser", Err: fmt.Errorf("browser transport is not initialized")}
	}
	if !b.Healthy() {
		return &CDPError{Code: CDPErrorClosed, Op: "probe browser", Err: b.Err()}
	}
	probeCtx, cancel := context.WithTimeout(ctx, PiggybackBudget)
	defer cancel()
	var version BrowserVersion
	if err := b.transport.Call(probeCtx, "Browser.getVersion", nil, &version); err != nil {
		return fmt.Errorf("browser probe: %w", b.transportErr(err))
	}
	if version.Product == "" || version.ProtocolVersion == "" {
		return &CDPError{Code: CDPErrorProtocol, Op: "probe browser", Err: fmt.Errorf("incomplete browser version")}
	}
	return nil
}
