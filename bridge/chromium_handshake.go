package bridge

import (
	"context"
	"errors"
	"fmt"
	"time"

	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

const (
	// connectHandshakeBudget bounds how long an owned, still-running browser
	// may take to serve the DevTools endpoint it announced. Under host load a
	// fresh Chromium can write DevToolsActivePort and then refuse or drop the
	// first connections before its DevTools broker is fully wired.
	connectHandshakeBudget = 10 * time.Second
	// externalHandshakeAttempts keeps attaching to an endpoint without an
	// owned process short: there is no liveness signal to justify waiting.
	externalHandshakeAttempts = 5
	maxHandshakeBackoff       = 500 * time.Millisecond
)

// handshakeChromium dials the endpoint and completes the full attach
// handshake (identity, event subscription, target discovery) as one retryable
// unit. An owned browser is retried while it runs, within
// connectHandshakeBudget; its exit ends the attempt immediately as a typed
// crash carrying the exit status and bounded output.
func handshakeChromium(ctx context.Context, endpoint string, processOwner *browserprocess.Browser) (*CDPTransport, *CDPSubscription, BrowserVersion, error) {
	var exited <-chan struct{}
	if processOwner != nil {
		exited = processOwner.Exited()
	}
	deadline := time.Now().Add(connectHandshakeBudget)
	var lastErr error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if processOwner == nil && attempt >= externalHandshakeAttempts || processOwner != nil && time.Now().After(deadline) {
				break
			}
			backoff := min(time.Duration(50*attempt)*time.Millisecond, maxHandshakeBackoff)
			select {
			case <-ctx.Done():
				return nil, nil, BrowserVersion{}, errors.Join(ctx.Err(), lastErr)
			case <-exited:
				return nil, nil, BrowserVersion{}, browserExitedDuringHandshake(processOwner, lastErr)
			case <-time.After(backoff):
			}
		}
		transport, monitor, version, err := attemptChromiumHandshake(ctx, endpoint)
		if err == nil {
			return transport, monitor, version, nil
		}
		lastErr = err
		select {
		case <-exited:
			return nil, nil, BrowserVersion{}, browserExitedDuringHandshake(processOwner, lastErr)
		default:
		}
	}
	if processOwner != nil {
		return nil, nil, BrowserVersion{}, fmt.Errorf("%w; browser process still running after %s; output: %s", lastErr, connectHandshakeBudget, processOwner.Output())
	}
	return nil, nil, BrowserVersion{}, lastErr
}

func attemptChromiumHandshake(ctx context.Context, endpoint string) (*CDPTransport, *CDPSubscription, BrowserVersion, error) {
	var version BrowserVersion
	transport, err := DialCDPTransport(ctx, CDPTransportConfig{URL: endpoint})
	if err != nil {
		return nil, nil, version, err
	}
	fail := func(err error) (*CDPTransport, *CDPSubscription, BrowserVersion, error) {
		return nil, nil, version, errors.Join(err, transport.Close())
	}
	if err := transport.Call(ctx, "Browser.getVersion", nil, &version); err != nil {
		return fail(fmt.Errorf("validate browser identity: %w", err))
	}
	if version.ProtocolVersion == "" || version.Product == "" {
		return fail(&CDPError{Code: CDPErrorProtocol, Op: "validate browser identity", Err: fmt.Errorf("incomplete Browser.getVersion result")})
	}
	monitor, err := transport.Subscribe(256)
	if err != nil {
		return fail(err)
	}
	if err := transport.Call(ctx, "Target.setDiscoverTargets", setDiscoverTargetsParams{Discover: true}, nil); err != nil {
		monitor.Close()
		return fail(fmt.Errorf("enable target discovery: %w", err))
	}
	return transport, monitor, version, nil
}

func browserExitedDuringHandshake(processOwner *browserprocess.Browser, lastErr error) error {
	return &browserprocess.Error{
		Code: browserprocess.ErrorBrowserCrash, Op: "connect DevTools",
		Err: fmt.Errorf("process exited after announcing its endpoint: %v; last handshake error: %w; output: %s", processOwner.WaitError(), lastErr, processOwner.Output()),
	}
}
