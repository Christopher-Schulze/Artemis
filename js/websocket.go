package js

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	v8 "github.com/tommie/v8go"
	"golang.org/x/net/http/httpguts"

	"github.com/Christopher-Schulze/Artemis/network"
)

const maxWebSocketMessageBytes = 64 << 10
const maxWebSocketConnections = 64
const maxWebSocketWorkers = 128

// wsEventKind identifies which JS-side handler should fire.
type wsEventKind int

const (
	wsOpen wsEventKind = iota + 1
	wsMessage
	wsError
	wsClose
)

// wsEvent is one event from the goroutine reader for a WebSocket.
type wsEvent struct {
	connID   uint32
	kind     wsEventKind
	data     []byte
	binary   bool
	code     int
	reason   string
	clean    bool
	protocol string
}

// wsConn is one open WebSocket. Each Context owns a registry.
type wsConn struct {
	id       uint32
	mu       sync.Mutex
	conn     *websocket.Conn
	ctx      context.Context // canceled when wsRegistry.closeAll runs
	cancel   context.CancelFunc
	state    atomic.Int32 // 0..3
	sendTail <-chan struct{}
}

func (c *wsConn) attach(conn *websocket.Conn) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil || c.state.Load() == 3 {
		_ = conn.CloseNow()
		return false
	}
	c.conn = conn
	return true
}

func (c *wsConn) current() *websocket.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

func (c *wsConn) closeNow() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.CloseNow()
		c.conn = nil
	}
	c.state.Store(3)
}

type wsRegistry struct {
	mu            sync.Mutex
	nextID        uint32
	conns         map[uint32]*wsConn
	events        chan wsEvent
	terminal      map[uint32]wsEvent
	closed        bool
	workers       sync.WaitGroup
	activeWorkers int
}

// newWSRegistry returns a registry with no allocated map or channel.
// The 256-slot channel and conns map are large per-Context allocations
// (~16KB+ together) and the vast majority of pages never open a
// WebSocket. ensureInit, called from put on first connect, allocates
// them on demand. Read paths (drain, get, remove) tolerate nil maps
// and nil channels (select fall-through, zero-value lookup).
func newWSRegistry() *wsRegistry { return &wsRegistry{} }

func (r *wsRegistry) ensureInit() {
	if r.conns == nil {
		r.conns = make(map[uint32]*wsConn)
		r.events = make(chan wsEvent, 256)
	}
}

func (r *wsRegistry) put(c *wsConn) uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(r.conns) >= maxWebSocketConnections || r.nextID >= math.MaxInt32 {
		return 0
	}
	r.ensureInit()
	r.nextID++
	c.id = r.nextID
	r.conns[c.id] = c
	return c.id
}

// start registers work before launch under the same owner that closes admission.
func (r *wsRegistry) start(operation func()) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.activeWorkers >= maxWebSocketWorkers {
		return false
	}
	r.workers.Add(1)
	r.activeWorkers++
	go func() {
		defer r.workers.Done()
		defer func() { r.mu.Lock(); r.activeWorkers--; r.mu.Unlock() }()
		operation()
	}()
	return true
}

func (r *wsRegistry) send(conn *wsConn, socket *websocket.Conn, kind websocket.MessageType, data []byte) bool {
	conn.mu.Lock()
	previous := conn.sendTail
	finished := make(chan struct{})
	conn.sendTail = finished
	conn.mu.Unlock()
	if r.start(func() {
		defer close(finished)
		if previous != nil {
			select {
			case <-previous:
			case <-conn.ctx.Done():
				return
			}
		}
		if err := socket.Write(conn.ctx, kind, data); err != nil {
			r.tryEvent(conn.ctx, wsEvent{connID: conn.id, kind: wsError, reason: err.Error()})
		}
	}) {
		return true
	}
	conn.mu.Lock()
	conn.sendTail = previous
	conn.mu.Unlock()
	close(finished)
	return false
}

func (r *wsRegistry) get(id uint32) *wsConn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conns[id]
}

func (r *wsRegistry) remove(id uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.conns, id)
	delete(r.terminal, id)
}

// tryEvent attempts to deliver an event into the buffered channel,
// giving up if the conn's ctx has been canceled. Read goroutines on
// closed Contexts use this so they can exit cleanly even when nothing
// is draining r.events anymore.
func (r *wsRegistry) tryEvent(ctx context.Context, ev wsEvent) bool {
	if r == nil || r.events == nil {
		return false
	}
	select {
	case r.events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// Terminal publication never waits for page execution or the transport context.
// One retained close per admitted connection survives a full message queue.
// It is dispatched only after earlier queued messages have drained.
func (r *wsRegistry) tryTerminalEvent(ev wsEvent) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.conns[ev.connID] == nil {
		return false
	}
	select {
	case r.events <- ev:
		return true
	default:
		if r.terminal == nil {
			r.terminal = make(map[uint32]wsEvent)
		}
		if _, exists := r.terminal[ev.connID]; !exists {
			r.terminal[ev.connID] = ev
		}
		return true
	}
}

func (r *wsRegistry) nextTerminalEvent() (wsEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) != 0 || len(r.terminal) == 0 {
		return wsEvent{}, false
	}
	var first uint32
	for id := range r.terminal {
		if first == 0 || id < first {
			first = id
		}
	}
	event := r.terminal[first]
	delete(r.terminal, first)
	return event, true
}

// closeAll cancels every WebSocket connection's context and closes the
// underlying conn, ensuring all per-conn read goroutines exit. Called
// from Context.Close so WS goroutines never outlive their owning
// *js.Context. Idempotent and safe under concurrent calls.
func (r *wsRegistry) closeAll() {
	r.mu.Lock()
	r.closed = true
	conns := make([]*wsConn, 0, len(r.conns))
	for _, c := range r.conns {
		conns = append(conns, c)
	}
	r.conns = nil
	r.mu.Unlock()
	for _, conn := range conns {
		conn.closeNow()
	}
	r.workers.Wait()
	r.mu.Lock()
	clear(r.terminal)
	for len(r.events) > 0 {
		<-r.events
	}
	r.mu.Unlock()
}

// drain pulls all currently queued WS events and fires the corresponding
// JS callbacks. Runs on the V8 thread.
func (r *wsRegistry) drain(c *Context) int {
	count := 0
	for count < cap(r.events) {
		if c.evalCtx.Err() != nil || c.async.ctx.Err() != nil {
			return count
		}
		select {
		case ev := <-r.events:
			r.fireEvent(c, ev)
			count++
		default:
			for range maxWebSocketConnections {
				if c.evalCtx.Err() != nil || c.async.ctx.Err() != nil {
					return count
				}
				terminal, exists := r.nextTerminalEvent()
				if !exists {
					return count
				}
				r.fireEvent(c, terminal)
				count++
			}
			return count
		}
	}
	return count
}

func (r *wsRegistry) fireEvent(c *Context, ev wsEvent) {
	conn := r.get(ev.connID)
	if conn == nil {
		return
	}
	switch ev.kind {
	case wsOpen:
		conn.state.Store(1)
		c.runWebSocketDispatch("__ws_dispatch(" + itoa(ev.connID) + ", 'open', null, false, 0, '', false, " + jsStringLit(ev.protocol) + ")")
	case wsMessage:
		if err := c.bindWSPayload(ev.data, ev.binary); err != nil {
			c.rt.execution.recordFailure(fmt.Errorf("WebSocket payload: %w", err))
			return
		}
		c.runWebSocketDispatch("__ws_dispatch(" + itoa(ev.connID) + ", 'message', __wsLastPayload, " + boolStr(ev.binary) + ", 0, '')")
	case wsError:
		c.runWebSocketDispatch("__ws_dispatch(" + itoa(ev.connID) + ", 'error', null, false, 0, " + jsStringLit(ev.reason) + ")")
	case wsClose:
		conn.state.Store(3)
		code, ok := checkedIntToUint32(ev.code)
		if !ok {
			code = 0
		}
		c.runWebSocketDispatch("__ws_dispatch(" + itoa(ev.connID) + ", 'close', null, false, " + itoa(code) + ", " + jsStringLit(ev.reason) + ", " + boolStr(ev.clean) + ")")
		r.remove(ev.connID)
	}
}

func (c *Context) runWebSocketDispatch(source string) {
	value, err := c.v8ctx.RunScript(source, "<artemis-ws-dispatch>")
	if value != nil {
		value.Release()
	}
	c.rt.execution.recordFailure(formatJSError(err))
}

// bindWSPayload sets globalThis.__wsLastPayload to the Go-side payload
// so the dispatch RunScript can reference it without escaping bytes.
func (c *Context) bindWSPayload(data []byte, binary bool) error {
	if len(data) > maxWebSocketMessageBytes {
		return errors.New("WebSocket message exceeds byte limit")
	}
	var payload *v8.Value
	var err error
	if binary {
		payload, err = c.v8ctx.NewArrayBuffer(data)
	} else {
		payload, err = v8.NewValue(c.rt.iso, string(data))
	}
	if err != nil {
		return err
	}
	defer payload.Release()
	global := c.v8ctx.Global()
	defer global.Release()
	return global.Set("__wsLastPayload", payload)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (c *Context) webSocketHTTPClient() *http.Client {
	c.wsClientOnce.Do(func() {
		if c.policy == nil {
			return
		}
		policyConfig := c.policy.Config()
		transport := &http.Transport{
			Proxy:                 nil,
			DialContext:           c.policy.DialContext,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
		if c.proxy != nil {
			transport.Proxy = http.ProxyURL(c.proxy)
		}
		c.wsClient = &http.Client{
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= policyConfig.MaxRedirects {
					return fmt.Errorf("%w: redirect_limit", network.ErrPolicyDenied)
				}
				return c.validateWebSocketTarget(
					req.Context(),
					req.URL.String(),
					req.Method,
					req.Header.Get("Content-Type"),
					req.ContentLength,
					network.TargetRedirect,
					c.sessionID,
				)
			},
		}
	})
	return c.wsClient
}

// validateWebSocketTarget keeps DNS with the upstream proxy when one routes
// the handshake (spec L4148); direct connections resolve and check locally.
func (c *Context) validateWebSocketTarget(ctx context.Context, rawURL, method, contentType string, contentLength int64, kind network.TargetKind, sessionID string) error {
	if c.proxy != nil {
		return c.policy.ValidateProxiedRequest(ctx, rawURL, method, contentType, contentLength, kind, sessionID)
	}
	return c.policy.ValidateRequest(ctx, rawURL, method, contentType, contentLength, kind, sessionID)
}

func (c *Context) closeWebSocketTransport() {
	if c == nil {
		return
	}
	c.wsClientOnce.Do(func() {})
	if c.wsClient == nil {
		return
	}
	if transport, ok := c.wsClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

// installWebSocket installs `__ws_open`, `__ws_send`, `__ws_close` and
// the JS-side WebSocket class.
// wsTemplates caches the 3 WebSocket trampolines at Runtime level.
type wsTemplates struct {
	open, send, closeFn *v8.FunctionTemplate
}

func webSocketProtocols(args []*v8.Value) ([]string, error) {
	if len(args) < 2 {
		return nil, nil
	}
	if !args[1].IsArray() {
		return nil, errors.New("WebSocket protocols require a normalized array")
	}
	array, err := args[1].AsObject()
	if err != nil {
		return nil, err
	}
	length, err := array.Get("length")
	if err != nil {
		return nil, err
	}
	count := length.Integer()
	length.Release()
	if count > 32 {
		return nil, errors.New("WebSocket protocol count exceeds limit")
	}
	protocols := make([]string, 0, count)
	for index := int64(0); index < count; index++ {
		value, err := array.GetIdx(uint32(index))
		if err != nil {
			return nil, err
		}
		protocol, err := value.StringChecked()
		value.Release()
		if err != nil {
			return nil, err
		}
		if len(protocol) > 256 || !httpguts.ValidHeaderFieldName(protocol) || slices.Contains(protocols, protocol) {
			return nil, errors.New("WebSocket protocol is invalid or duplicated")
		}
		protocols = append(protocols, protocol)
	}
	return protocols, nil
}

func (r *Runtime) ensureWSTemplates() *wsTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.wsTemplates != nil {
		return r.wsTemplates
	}
	iso := r.iso
	r.wsTemplates = &wsTemplates{
		open: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return r.callbackPrimitive(info, int32(0))
			}
			url, err := args[0].StringChecked()
			if err != nil {
				return r.responseError(info, err)
			}
			protocols, err := webSocketProtocols(args)
			if err != nil {
				return r.responseError(info, err)
			}
			ctx, cancel := context.WithCancel(c.async.ctx)
			conn := &wsConn{cancel: cancel, ctx: ctx}
			conn.state.Store(0) // CONNECTING
			id := c.ws.put(conn)
			ws := c.ws
			if id == 0 {
				cancel()
				return r.responseError(info, errors.New("WebSocket connection admission denied"))
			}
			started := ws.start(func() {
				defer conn.closeNow()
				if err := c.validateWebSocketTarget(ctx, url, http.MethodGet, "", 0, network.TargetWebSocket, c.sessionID); err != nil {
					ws.tryEvent(ctx, wsEvent{connID: id, kind: wsError, reason: err.Error()})
					ws.tryTerminalEvent(wsEvent{connID: id, kind: wsClose, code: 1006, reason: "policy denied"})
					return
				}
				client := c.webSocketHTTPClient()
				if client == nil {
					ws.tryEvent(ctx, wsEvent{connID: id, kind: wsError, reason: "network policy required"})
					ws.tryTerminalEvent(wsEvent{connID: id, kind: wsClose, code: 1006, reason: "policy denied"})
					return
				}
				wsConn, response, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: client, Subprotocols: protocols})
				if response != nil && response.Body != nil {
					if closeErr := response.Body.Close(); closeErr != nil && err == nil {
						err = fmt.Errorf("close WebSocket handshake response body: %w", closeErr)
					}
				}
				if err != nil {
					ws.tryEvent(ctx, wsEvent{connID: id, kind: wsError, reason: err.Error()})
					ws.tryTerminalEvent(wsEvent{connID: id, kind: wsClose, code: 1006, reason: "abnormal"})
					return
				}
				if len(protocols) > 0 && !slices.Contains(protocols, wsConn.Subprotocol()) {
					closeErr := wsConn.CloseNow()
					ws.tryEvent(ctx, wsEvent{connID: id, kind: wsError, reason: fmt.Sprint(errors.Join(errors.New("WebSocket peer did not select an offered protocol"), closeErr))})
					ws.tryTerminalEvent(wsEvent{connID: id, kind: wsClose, code: 1006})
					return
				}
				if !conn.attach(wsConn) {
					return
				}
				wsConn.SetReadLimit(min(int64(maxWebSocketMessageBytes), c.policy.Config().MaxResponseBodyBytes))
				ws.tryEvent(ctx, wsEvent{connID: id, kind: wsOpen, protocol: wsConn.Subprotocol()})
				for {
					typ, data, err := wsConn.Read(ctx)
					if err != nil {
						terminal := wsEvent{connID: id, kind: wsClose, code: 1006}
						var closed websocket.CloseError
						if errors.As(err, &closed) {
							terminal.code, terminal.reason = int(closed.Code), closed.Reason
							terminal.clean = closed.Code != websocket.StatusAbnormalClosure && closed.Code != websocket.StatusTLSHandshake
						} else if ctx.Err() == nil {
							ws.tryEvent(ctx, wsEvent{connID: id, kind: wsError, reason: err.Error()})
						}
						ws.tryTerminalEvent(terminal)
						return
					}
					if !ws.tryEvent(ctx, wsEvent{
						connID: id, kind: wsMessage, data: data,
						binary: typ == websocket.MessageBinary,
					}) {
						return // Context closed; abandon.
					}
				}
			})
			if !started {
				conn.closeNow()
				ws.remove(id)
				return r.responseError(info, errors.New("WebSocket worker admission denied"))
			}
			return r.callbackPrimitive(info, int32(id))
		}),
		send: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 2 {
				return v8.Null(iso)
			}
			id, ok := checkedInt64ToUint32(args[0].Integer())
			if !ok {
				return v8.Null(iso)
			}
			var data []byte
			var err error
			messageType := websocket.MessageText
			limit := int64(maxWebSocketMessageBytes)
			if c.policy != nil {
				limit = min(limit, c.policy.Config().MaxRequestBodyBytes)
			}
			if args[1].IsArrayBuffer() || args[1].IsArrayBufferView() || args[1].IsSharedArrayBuffer() {
				messageType = websocket.MessageBinary
				data, err = args[1].BufferBytes(int(limit))
			} else {
				var text string
				text, err = args[1].StringChecked()
				if int64(len(text)) > limit {
					err = errors.New("WebSocket send exceeds byte limit")
				}
				if err == nil {
					data = []byte(text)
				}
			}
			if err != nil {
				return r.responseError(info, err)
			}
			conn := c.ws.get(id)
			if conn == nil {
				return v8.Null(iso)
			}
			wsConn := conn.current()
			if wsConn == nil {
				return v8.Null(iso)
			}
			if !c.ws.send(conn, wsConn, messageType, data) {
				return r.responseError(info, errors.New("WebSocket worker admission denied"))
			}
			return v8.Null(iso)
		}),
		closeFn: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return v8.Null(iso)
			}
			id, ok := checkedInt64ToUint32(args[0].Integer())
			if !ok {
				return v8.Null(iso)
			}
			code := uint16(1000)
			reason := ""
			if len(args) >= 2 && !args[1].IsNullOrUndefined() {
				codeValue, valid := checkedInt64ToUint16(args[1].Integer())
				if !valid {
					return v8.Null(iso)
				}
				code = codeValue
			}
			if len(args) >= 3 {
				var err error
				reason, err = args[2].StringChecked()
				if err != nil {
					return r.responseError(info, err)
				}
			}
			if (code != 1000 && (code < 3000 || code > 4999)) || len(reason) > 123 {
				return r.responseError(info, errors.New("WebSocket close code or reason is invalid"))
			}
			conn := c.ws.get(id)
			if conn == nil {
				return v8.Null(iso)
			}
			conn.state.Store(2) // CLOSING
			if wsConn := conn.current(); wsConn != nil {
				conn.mu.Lock()
				pending := conn.sendTail
				conn.mu.Unlock()
				if !c.ws.start(func() {
					if pending != nil {
						select {
						case <-pending:
						case <-conn.ctx.Done():
							return
						}
					}
					if err := wsConn.Close(websocket.StatusCode(code), reason); err != nil && conn.ctx.Err() == nil {
						c.ws.tryEvent(conn.ctx, wsEvent{connID: id, kind: wsError, reason: err.Error()})
					}
					conn.cancel()
				}) {
					conn.closeNow()
					c.ws.tryTerminalEvent(wsEvent{connID: id, kind: wsClose, code: 1006, reason: "close worker admission denied"})
					return r.responseError(info, errors.New("WebSocket close worker admission denied"))
				}
				return v8.Undefined(iso)
			}
			conn.cancel()
			return v8.Null(iso)
		}),
	}
	return r.wsTemplates
}

func installWebSocket(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureWSTemplates()
	g := v8ctx.Global()
	defer g.Release()
	open := t.open.GetFunction(v8ctx)
	defer open.Release()
	send := t.send.GetFunction(v8ctx)
	defer send.Release()
	closeFn := t.closeFn.GetFunction(v8ctx)
	defer closeFn.Release()
	if err := g.Set("__ws_open", open); err != nil {
		return err
	}
	if err := g.Set("__ws_send", send); err != nil {
		return err
	}
	if err := g.Set("__ws_close", closeFn); err != nil {
		return err
	}
	c.registerBootstrap("artemis-websocket", websocketBootstrap)
	return nil
}

const websocketBootstrap = `
(() => {
  const addListener = document.addEventListener, removeListener = document.removeEventListener;
  const dispatch = document.dispatchEvent;
  const _wsRegistry = new Map(); // connId -> WebSocket instance
  Object.defineProperty(globalThis, '__ws_dispatch', {value: function(connId, kind, payload, binary, code, reason, clean, protocol) {
    const ws = _wsRegistry.get(connId);
    if (!ws) return;
    if (kind === 'open') {
      ws.protocol = protocol;
      ws.readyState = 1;
      ws.dispatchEvent(new Event('open'));
      return;
    }
    if (kind === 'message') {
      let data = payload;
      if (binary && ws.binaryType === 'arraybuffer') {
        data = payload;
      } else if (binary) {
        data = new Blob([new Uint8Array(payload)]);
      }
      const ev = new MessageEvent('message', {data});
      ws.dispatchEvent(ev);
      return;
    }
    if (kind === 'error') {
      const ev = new Event('error');
      ev.message = reason;
      ws.dispatchEvent(ev);
      return;
    }
    if (kind === 'close') {
      _wsRegistry.delete(connId);
      ws.readyState = 3;
      const ev = new CloseEvent('close', {code, reason, wasClean: !!clean});
      ws.dispatchEvent(ev);
    }
  }});

  class WebSocket {
    constructor(url, protocols) {
      const list = [], values = protocols === undefined ? [] : typeof protocols === 'string' ? [protocols] : protocols;
      for (const value of values) {
        if (typeof value === 'symbol') throw new TypeError('WebSocket protocol requires a string');
        const protocol = String(value);
        if (!/^[!#$%&'*+\-.^_\x60|~0-9A-Za-z]+$/.test(protocol) || list.includes(protocol)) throw new DOMException('Invalid WebSocket protocol', 'SyntaxError');
        if (list.length === 32 || protocol.length > 256) throw new RangeError('WebSocket protocol limit exceeded');
        list.push(protocol);
      }
      this.url = String(url);
      this.protocol = '';
      this.protocols = list;
      this.readyState = 0;
      this.binaryType = 'blob';
      this.bufferedAmount = 0;
      Object.defineProperty(this, '__id', {value: {}});
      this.onopen = null; this.onmessage = null; this.onerror = null; this.onclose = null;
      for (const kind of ['open', 'message', 'error', 'close']) {
        addListener.call(this, kind, event => {
          const callback = this['on' + kind];
          if (typeof callback === 'function') callback.call(this, event);
        });
      }
      this._id = __ws_open(this.url, list);
      _wsRegistry.set(this._id, this);
    }
    addEventListener(type, fn, options) {
      addListener.call(this, type, fn, options);
    }
    removeEventListener(type, fn, options) {
      removeListener.call(this, type, fn, options);
    }
    dispatchEvent(ev) {
      return dispatch.call(this, ev);
    }
    send(data) {
      if (this.readyState !== 1) throw new DOMException('not OPEN', 'InvalidStateError');
      __ws_send(this._id, data);
    }
    close(code, reason) {
      if (code === undefined) code = 1000;
      else {
        const number = +code;
        const clamped = Number.isNaN(number) ? 0 : Math.min(65535, Math.max(0, number));
        const lower = Math.floor(clamped), fraction = clamped - lower;
        code = fraction < 0.5 || (fraction === 0.5 && lower % 2 === 0) ? lower : lower + 1;
      }
      if (code !== 1000 && (code < 3000 || code > 4999)) throw new DOMException('Invalid WebSocket close code', 'InvalidAccessError');
      if (typeof reason === 'symbol') throw new TypeError('WebSocket close reason requires a string');
      reason = reason === undefined ? '' : String(reason);
      if (new TextEncoder().encode(reason).length > 123) throw new DOMException('WebSocket close reason exceeds 123 bytes', 'SyntaxError');
      if (this.readyState === 2 || this.readyState === 3) return;
      this.readyState = 2;
      __ws_close(this._id, code, reason);
    }
  }
  WebSocket.CONNECTING = 0;
  WebSocket.OPEN = 1;
  WebSocket.CLOSING = 2;
  WebSocket.CLOSED = 3;
  globalThis.WebSocket = WebSocket;
})();
`
