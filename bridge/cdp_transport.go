package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// cdpBufferPool reuses encode buffers for outbound CDP commands; buffers are
// returned after the synchronous write completes.
var cdpBufferPool = sync.Pool{New: func() any { return &bytes.Buffer{} }}

const (
	defaultCDPDialTimeout = 15 * time.Second
	defaultCDPMaxPending  = 256
	defaultCDPMaxMessage  = 8 * 1024 * 1024
)

// CDPErrorCode classifies transport and protocol failures.
type CDPErrorCode string

const (
	CDPErrorInvalidConfig CDPErrorCode = "invalid_config"
	CDPErrorDial          CDPErrorCode = "dial_failed"
	CDPErrorClosed        CDPErrorCode = "transport_closed"
	CDPErrorOverloaded    CDPErrorCode = "pending_limit"
	CDPErrorProtocol      CDPErrorCode = "protocol_error"
	CDPErrorEventOverflow CDPErrorCode = "event_overflow"
)

// CDPError is a stable typed transport failure.
type CDPError struct {
	Code CDPErrorCode
	Op   string
	Err  error
}

func (e *CDPError) Error() string {
	if e == nil {
		return "artemis CDP error"
	}
	return fmt.Sprintf("artemis CDP: %s: %s: %v", e.Op, e.Code, e.Err)
}

func (e *CDPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsCDPError reports whether err contains a CDPError with code.
func IsCDPError(err error, code CDPErrorCode) bool {
	var cdpErr *CDPError
	return errors.As(err, &cdpErr) && cdpErr.Code == code
}

// ProtocolError is the structured error object returned by Chromium.
type ProtocolError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return "unknown CDP protocol error"
	}
	return fmt.Sprintf("CDP protocol error %d: %s", e.Code, e.Message)
}

// CDPEvent is one unsolicited protocol event.
type CDPEvent struct {
	Method    string
	Params    json.RawMessage
	SessionID string
}

// CDPTransportConfig bounds one browser WebSocket transport.
type CDPTransportConfig struct {
	URL             string
	DialTimeout     time.Duration
	MaxPendingCalls int
	MaxMessageBytes int64
	HTTPClient      *http.Client
}

type cdpResponse struct {
	result json.RawMessage
	err    error
}

type cdpWireMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *ProtocolError  `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

type cdpCommand struct {
	ID        int64  `json:"id"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// CDPSubscription receives bounded protocol events and an overflow/close signal.
type CDPSubscription struct {
	Events <-chan CDPEvent
	Errors <-chan error
	id     uint64
	owner  *CDPTransport
	close  sync.Once
}

type cdpSubscriber struct {
	events chan CDPEvent
	errors chan error
}

// CDPTransport owns request correlation and event dispatch for one connection.
type CDPTransport struct {
	conn       *websocket.Conn
	cancel     context.CancelFunc
	done       chan struct{}
	finishOnce sync.Once
	writeMu    sync.Mutex
	mu         sync.Mutex
	pending    map[int64]chan cdpResponse
	subs       map[uint64]*cdpSubscriber
	terminal   error
	maxPending int
	nextID     atomic.Int64
	nextSubID  atomic.Uint64
}

// DialCDPTransport validates and connects a bounded CDP transport.
func DialCDPTransport(ctx context.Context, config CDPTransportConfig) (*CDPTransport, error) {
	if ctx == nil {
		return nil, &CDPError{Code: CDPErrorInvalidConfig, Op: "dial", Err: fmt.Errorf("context required")}
	}
	normalized, err := normalizeCDPTransportConfig(config)
	if err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, normalized.DialTimeout)
	defer cancel()
	conn, response, err := websocket.Dial(dialCtx, normalized.URL, &websocket.DialOptions{HTTPClient: normalized.HTTPClient})
	if response != nil && response.Body != nil {
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close CDP handshake response body: %w", closeErr)
		}
	}
	if err != nil {
		return nil, &CDPError{Code: CDPErrorDial, Op: "dial", Err: err}
	}
	conn.SetReadLimit(normalized.MaxMessageBytes)
	transportCtx, transportCancel := context.WithCancel(context.Background())
	transport := &CDPTransport{
		conn: conn, cancel: transportCancel, done: make(chan struct{}),
		pending: make(map[int64]chan cdpResponse), subs: make(map[uint64]*cdpSubscriber),
		maxPending: normalized.MaxPendingCalls,
	}
	go transport.readLoop(transportCtx)
	return transport, nil
}

func normalizeCDPTransportConfig(config CDPTransportConfig) (CDPTransportConfig, error) {
	parsed, err := url.Parse(config.URL)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Host == "" {
		return CDPTransportConfig{}, &CDPError{Code: CDPErrorInvalidConfig, Op: "validate", Err: fmt.Errorf("valid ws or wss URL required")}
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = defaultCDPDialTimeout
	}
	if config.MaxPendingCalls == 0 {
		config.MaxPendingCalls = defaultCDPMaxPending
	}
	if config.MaxMessageBytes == 0 {
		config.MaxMessageBytes = defaultCDPMaxMessage
	}
	if config.DialTimeout < 0 || config.MaxPendingCalls < 1 || config.MaxMessageBytes < 1024 {
		return CDPTransportConfig{}, &CDPError{Code: CDPErrorInvalidConfig, Op: "validate", Err: fmt.Errorf("positive bounds required")}
	}
	return config, nil
}

// Call invokes a browser-scoped CDP method.
func (t *CDPTransport) Call(ctx context.Context, method string, params any, result any) error {
	return t.CallSession(ctx, "", method, params, result)
}

// CallSession invokes a flattened session-scoped CDP method.
func (t *CDPTransport) CallSession(ctx context.Context, sessionID, method string, params any, result any) error {
	if ctx == nil {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "call", Err: fmt.Errorf("context required")}
	}
	if method == "" {
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "call", Err: fmt.Errorf("method required")}
	}
	id := t.nextID.Add(1)
	response := make(chan cdpResponse, 1)
	if err := t.reserve(id, response); err != nil {
		return err
	}
	defer t.release(id)
	buf := cdpBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(cdpCommand{ID: id, Method: method, Params: params, SessionID: sessionID}); err != nil {
		cdpBufferPool.Put(buf)
		return &CDPError{Code: CDPErrorInvalidConfig, Op: "marshal command", Err: err}
	}
	// Encoder writes a trailing newline; CDP frames tolerate it, trim anyway
	// so payloads stay byte-identical to the previous Marshal output.
	payload := buf.Bytes()
	if n := len(payload); n > 0 && payload[n-1] == '\n' {
		payload = payload[:n-1]
	}
	err := t.write(ctx, payload)
	cdpBufferPool.Put(buf)
	if err != nil {
		return err
	}
	select {
	case reply := <-response:
		if reply.err != nil {
			return reply.err
		}
		if result == nil || len(reply.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(reply.result, result); err != nil {
			return &CDPError{Code: CDPErrorProtocol, Op: "decode result", Err: err}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		return t.closeError("call")
	}
}

func (t *CDPTransport) reserve(id int64, response chan cdpResponse) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.terminal != nil {
		return t.closeErrorLocked("reserve")
	}
	if len(t.pending) >= t.maxPending {
		return &CDPError{Code: CDPErrorOverloaded, Op: "reserve", Err: fmt.Errorf("%d pending calls", len(t.pending))}
	}
	t.pending[id] = response
	return nil
}

func (t *CDPTransport) release(id int64) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

func (t *CDPTransport) write(ctx context.Context, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-t.done:
		return t.closeError("write")
	default:
	}
	writeCtx, cancel := context.WithTimeout(context.Background(), defaultCDPDialTimeout)
	defer cancel()
	if err := t.conn.Write(writeCtx, websocket.MessageText, payload); err != nil {
		t.finish(&CDPError{Code: CDPErrorClosed, Op: "write", Err: err})
		return t.closeError("write")
	}
	return nil
}

func (t *CDPTransport) readLoop(ctx context.Context) {
	for {
		messageType, payload, err := t.conn.Read(ctx)
		if err != nil {
			t.finish(&CDPError{Code: CDPErrorClosed, Op: "read", Err: err})
			return
		}
		if messageType != websocket.MessageText {
			t.finish(&CDPError{Code: CDPErrorProtocol, Op: "read", Err: fmt.Errorf("unexpected WebSocket message type %d", messageType)})
			return
		}
		var message cdpWireMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			t.finish(&CDPError{Code: CDPErrorProtocol, Op: "decode message", Err: err})
			return
		}
		if message.ID != 0 {
			t.dispatchResponse(message)
			continue
		}
		if message.Method == "" {
			t.finish(&CDPError{Code: CDPErrorProtocol, Op: "route message", Err: fmt.Errorf("message has neither id nor method")})
			return
		}
		t.dispatchEvent(CDPEvent{Method: message.Method, Params: message.Params, SessionID: message.SessionID})
	}
}

func (t *CDPTransport) dispatchResponse(message cdpWireMessage) {
	t.mu.Lock()
	response := t.pending[message.ID]
	t.mu.Unlock()
	if response == nil {
		return
	}
	reply := cdpResponse{result: message.Result}
	if message.Error != nil {
		reply = cdpResponse{err: &CDPError{Code: CDPErrorProtocol, Op: "remote", Err: message.Error}}
	}
	select {
	case response <- reply:
	default:
		t.finish(&CDPError{Code: CDPErrorProtocol, Op: "route response", Err: fmt.Errorf("duplicate response id %d", message.ID)})
	}
}

func (t *CDPTransport) dispatchEvent(event CDPEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, subscriber := range t.subs {
		select {
		case subscriber.events <- event:
		default:
			err := &CDPError{Code: CDPErrorEventOverflow, Op: "dispatch event", Err: fmt.Errorf("subscription %d overflow", id)}
			subscriber.errors <- err
			close(subscriber.events)
			close(subscriber.errors)
			delete(t.subs, id)
		}
	}
}

// Subscribe creates a bounded event subscription.
func (t *CDPTransport) Subscribe(buffer int) (*CDPSubscription, error) {
	if buffer < 1 {
		return nil, &CDPError{Code: CDPErrorInvalidConfig, Op: "subscribe", Err: fmt.Errorf("positive buffer required")}
	}
	id := t.nextSubID.Add(1)
	subscriber := &cdpSubscriber{events: make(chan CDPEvent, buffer), errors: make(chan error, 1)}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.terminal != nil {
		return nil, t.closeErrorLocked("subscribe")
	}
	t.subs[id] = subscriber
	return &CDPSubscription{Events: subscriber.events, Errors: subscriber.errors, id: id, owner: t}, nil
}

// Close removes the subscription and closes its channels.
func (s *CDPSubscription) Close() {
	if s == nil || s.owner == nil {
		return
	}
	s.close.Do(func() {
		s.owner.removeSubscription(s.id)
	})
}

func (t *CDPTransport) removeSubscription(id uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if subscriber := t.subs[id]; subscriber != nil {
		close(subscriber.events)
		close(subscriber.errors)
		delete(t.subs, id)
	}
}

func (t *CDPTransport) finish(err error) {
	t.finishOnce.Do(func() {
		t.cancel()
		_ = t.conn.CloseNow()
		t.mu.Lock()
		t.terminal = err
		for _, subscriber := range t.subs {
			subscriber.errors <- err
			close(subscriber.events)
			close(subscriber.errors)
		}
		t.subs = make(map[uint64]*cdpSubscriber)
		t.mu.Unlock()
		close(t.done)
	})
}

func (t *CDPTransport) closeError(op string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closeErrorLocked(op)
}

func (t *CDPTransport) closeErrorLocked(op string) error {
	if cdpErr, ok := t.terminal.(*CDPError); ok {
		return cdpErr
	}
	return &CDPError{Code: CDPErrorClosed, Op: op, Err: t.terminal}
}

// Done closes when the transport terminates.
func (t *CDPTransport) Done() <-chan struct{} {
	return t.done
}

// Err returns the terminal transport error after Done closes.
func (t *CDPTransport) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.terminal
}

// PendingCalls returns the current bounded request count.
func (t *CDPTransport) PendingCalls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}

// Close immediately closes the WebSocket and unblocks every waiter.
func (t *CDPTransport) Close() error {
	select {
	case <-t.done:
		return nil
	default:
	}
	t.finish(&CDPError{Code: CDPErrorClosed, Op: "close", Err: nil})
	return nil
}
