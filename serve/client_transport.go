package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

const maxPendingControlCalls = 256
const controlWriteTimeout = 10 * time.Second

var (
	// ErrControlClosed is returned when the owner explicitly closes the client.
	ErrControlClosed = errors.New("control connection closed")
	// ErrControlTransport is returned when the connection can no longer exchange frames.
	ErrControlTransport = errors.New("control transport failed")
	// ErrControlProtocol marks malformed or uncorrelated peer responses.
	ErrControlProtocol = errors.New("invalid control response")
	// ErrControlBusy is a local refusal before dispatch.
	ErrControlBusy = errors.New("control connection has too many outstanding requests")
	// ErrControlOutcomeUnknown means a dispatched command has no observed outcome.
	ErrControlOutcomeUnknown = errors.New("control effect outcome unknown")
)

type controlCall struct {
	id      string
	data    []byte
	sent    bool
	outcome chan Response
}

func closeFailedHandshake(client *Client, err error) error {
	return errors.Join(err, client.Close())
}

func (c *Client) call(ctx context.Context, command Command, params any) (Response, error) {
	if ctx == nil {
		return Response{}, fmt.Errorf("control context required")
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	id := c.requestPrefix + "-" + strconv.FormatUint(c.sequence.Add(1), 10)
	request, err := MarshalTyped(id, command, params)
	if err != nil {
		return Response{}, err
	}
	request.Version = ProtocolVersion
	data, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	outcomes := make(chan Response, 1)
	call := &controlCall{id: id, data: data, outcome: outcomes}
	c.mu.Lock()
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return Response{}, err
	}
	if len(c.pending) >= maxPendingControlCalls || len(c.outgoing) == cap(c.outgoing) {
		c.mu.Unlock()
		return Response{}, ErrControlBusy
	}
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return Response{}, err
	}
	c.pending[id] = call
	c.outgoing <- call
	c.mu.Unlock()

	select {
	case outcome := <-outcomes:
		return outcome, nil
	case <-ctx.Done():
		c.mu.Lock()
		select {
		case outcome := <-outcomes:
			c.mu.Unlock()
			return outcome, nil
		default:
		}
		// A sent request owns its ID until its terminal response. Retain
		// the ID alone so its late outcome cannot affect another caller.
		err := ctx.Err()
		if call.sent {
			call.outcome = nil
			err = errors.Join(ErrControlOutcomeUnknown, err)
		} else {
			delete(c.pending, id)
			call.data = nil
		}
		c.mu.Unlock()
		return Response{}, err
	case <-c.done:
		select {
		case outcome := <-outcomes:
			return outcome, nil
		default:
			c.mu.Lock()
			sent := call.sent
			c.mu.Unlock()
			err := c.terminal
			if sent {
				err = errors.Join(ErrControlOutcomeUnknown, err)
			}
			return Response{}, fmt.Errorf("receive %s outcome: %w", command, err)
		}
	}
}

func (c *Client) stop(err error) {
	c.stopOnce.Do(func() {
		c.mu.Lock()
		c.terminal = err
		c.pending = nil
		for len(c.outgoing) > 0 {
			select {
			case <-c.outgoing:
			default:
			}
		}
		close(c.done)
		c.mu.Unlock()
		c.cancel()
		if closeErr := c.connection.CloseNow(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			c.closeErr = closeErr
		}
	})
}

func (c *Client) writeRequests(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case call := <-c.outgoing:
			c.mu.Lock()
			if c.pending[call.id] != call {
				c.mu.Unlock()
				continue
			}
			call.sent = true
			data := call.data
			call.data = nil
			c.mu.Unlock()
			writeCtx, cancel := context.WithTimeout(ctx, controlWriteTimeout)
			err := c.connection.Write(writeCtx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.stop(fmt.Errorf("%w: dispatch control request: %w", ErrControlTransport, err))
				return
			}
		}
	}
}

func (c *Client) readOutcomes(ctx context.Context) {
	for {
		kind, data, err := c.connection.Read(ctx)
		if err != nil {
			c.stop(fmt.Errorf("%w: read control response: %w", ErrControlTransport, err))
			return
		}
		if kind != websocket.MessageText {
			c.stop(fmt.Errorf("%w: expected a text frame", ErrControlProtocol))
			return
		}
		if err := c.routeOutcome(data); err != nil {
			c.stop(err)
			return
		}
	}
}

func (c *Client) routeOutcome(data []byte) error {
	var frame struct {
		Response
		OK    *bool           `json:"ok"`
		Value json.RawMessage `json:"value"`
		Event string          `json:"event"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return fmt.Errorf("%w: %w", ErrControlProtocol, err)
	}
	if frame.Event != "" && frame.ID == "" && frame.OK == nil && frame.Error == nil && frame.Value == nil {
		return nil
	}
	if frame.Event != "" || frame.OK == nil || *frame.OK == (frame.Error != nil) {
		return fmt.Errorf("%w: invalid response envelope", ErrControlProtocol)
	}
	if frame.ID == "" {
		if frame.Error != nil {
			return errors.Join(ErrControlProtocol, frame.Error)
		}
		return fmt.Errorf("%w: response ID is required", ErrControlProtocol)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	call := c.pending[frame.ID]
	if call == nil || !call.sent {
		return fmt.Errorf("%w: response ID has no dispatched request", ErrControlProtocol)
	}
	delete(c.pending, frame.ID)
	if call.outcome != nil {
		frame.Response.OK = *frame.OK
		if len(frame.Value) > 0 && string(frame.Value) != "null" {
			frame.Response.Value = frame.Value
		}
		call.outcome <- frame.Response
	}
	return nil
}
