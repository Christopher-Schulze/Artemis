package network

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"syscall"

	"github.com/Christopher-Schulze/Artemis/platform"
)

type fastOpenRequestKey struct{}

// httpDialContextFor confines delayed-connect Fast Open to HTTP transports.
// Generic DialContext keeps normal connect semantics for read-first protocols.
func (p *Policy) httpDialContextFor(sessionID string, caps platform.PlatformCapabilities, enabled bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, networkName, address string) (net.Conn, error) {
		if p == nil {
			return nil, fmt.Errorf("%w: policy required", ErrPolicyDenied)
		}
		if err := p.validateContext(ctx); err != nil {
			return nil, err
		}
		dialer := p.dialer
		eligible, _ := ctx.Value(fastOpenRequestKey{}).(bool)
		if enabled && eligible && caps.HasTCPFastOpen && caps.Evidence.TCPFastOpen.State == platform.StateAvailable {
			dialer.ControlContext = p.fastOpenControl(SessionID(ctx, sessionID))
		}
		return p.dialContextWithDialer(ctx, networkName, address, SessionID(ctx, sessionID), &dialer)
	}
}

func (p *Policy) fastOpenControl(sessionID string) func(context.Context, string, string, syscall.RawConn) error {
	return func(ctx context.Context, networkName, address string, socket syscall.RawConn) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, activationErr := activateTCPFastOpen(socket)
		if activationErr != nil {
			return activationErr
		}
		host, portText, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("Fast Open socket address: %w", err)
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			return err
		}
		// The ordinary socket admission is already audited before Control runs.
		// This additional receipt describes option activation, never saved RTTs.
		decision := Decision{Action: DecisionAllow, Kind: TargetSocket, Scheme: networkName, Host: host, Port: port, SessionID: sessionID, Reason: "tcp_fast_open_option", TCPFastOpen: &result}
		if err := p.emit(decision); err != nil {
			return fmt.Errorf("%w: %w", ErrPolicyDenied, err)
		}
		return ctx.Err()
	}
}
