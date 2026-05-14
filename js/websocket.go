package js

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
	v8 "rogchap.com/v8go"
)

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
	connID uint32
	kind   wsEventKind
	data   []byte
	binary bool
	code   int
	reason string
}

// wsConn is one open WebSocket. Each Context owns a registry.
type wsConn struct {
	id     uint32
	conn   *websocket.Conn
	ctx    context.Context // canceled when wsRegistry.closeAll runs
	cancel context.CancelFunc
	state  atomic.Int32 // 0..3
	binary bool
}

type wsRegistry struct {
	mu     sync.Mutex
	nextID uint32
	conns  map[uint32]*wsConn
	events chan wsEvent
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
	r.ensureInit()
	r.nextID++
	c.id = r.nextID
	r.conns[c.id] = c
	return c.id
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

// closeAll cancels every WebSocket connection's context and closes the
// underlying conn, ensuring all per-conn read goroutines exit. Called
// from Context.Close so WS goroutines never outlive their owning
// *js.Context. Idempotent and safe under concurrent calls.
func (r *wsRegistry) closeAll() {
	r.mu.Lock()
	conns := make([]*wsConn, 0, len(r.conns))
	for _, c := range r.conns {
		conns = append(conns, c)
	}
	r.conns = nil
	r.mu.Unlock()
	for _, conn := range conns {
		if conn.cancel != nil {
			conn.cancel()
		}
		if conn.conn != nil {
			_ = conn.conn.CloseNow()
		}
		conn.state.Store(3) // CLOSED
	}
}

// drain pulls all currently queued WS events and fires the corresponding
// JS callbacks. Runs on the V8 thread.
func (r *wsRegistry) drain(c *Context) int {
	count := 0
	for {
		select {
		case ev := <-r.events:
			r.fireEvent(c, ev)
			count++
		default:
			return count
		}
	}
}

func (r *wsRegistry) fireEvent(c *Context, ev wsEvent) {
	conn := r.get(ev.connID)
	if conn == nil {
		return
	}
	switch ev.kind {
	case wsOpen:
		conn.state.Store(1)
		_, _ = c.v8ctx.RunScript("__ws_dispatch("+itoa(ev.connID)+", 'open', null, false, 0, '')", "<artemis-ws-dispatch>")
	case wsMessage:
		// Build payload: string for text frames, length-keyed numeric array for binary
		c.bindWSPayload(ev.connID, ev.data, ev.binary)
		_, _ = c.v8ctx.RunScript("__ws_dispatch("+itoa(ev.connID)+", 'message', __wsLastPayload, "+boolStr(ev.binary)+", 0, '')", "<artemis-ws-dispatch>")
	case wsError:
		_, _ = c.v8ctx.RunScript("__ws_dispatch("+itoa(ev.connID)+", 'error', null, false, 0, "+jsStringLit(ev.reason)+")", "<artemis-ws-dispatch>")
	case wsClose:
		conn.state.Store(3)
		_, _ = c.v8ctx.RunScript("__ws_dispatch("+itoa(ev.connID)+", 'close', null, false, "+itoa(uint32(ev.code))+", "+jsStringLit(ev.reason)+")", "<artemis-ws-dispatch>")
		r.remove(ev.connID)
	}
}

// bindWSPayload sets globalThis.__wsLastPayload to the Go-side payload
// so the dispatch RunScript can reference it without escaping bytes.
func (c *Context) bindWSPayload(connID uint32, data []byte, binary bool) {
	if binary {
		arr, err := v8.NewObjectTemplate(c.rt.iso).NewInstance(c.v8ctx)
		if err != nil {
			return
		}
		_ = arr.Set("length", int32(len(data)))
		_ = arr.Set("byteLength", int32(len(data)))
		for i, b := range data {
			_ = arr.SetIdx(uint32(i), int32(b))
		}
		_ = c.v8ctx.Global().Set("__wsLastPayload", arr)
		return
	}
	_ = c.v8ctx.Global().Set("__wsLastPayload", string(data))
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// installWebSocket installs `__ws_open`, `__ws_send`, `__ws_close` and
// the JS-side WebSocket class.
// wsTemplates caches the 3 WebSocket trampolines at Runtime level.
type wsTemplates struct {
	open, send, closeFn *v8.FunctionTemplate
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
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return mustValue(iso, int32(0))
			}
			url := args[0].String()
			ctx, cancel := context.WithCancel(context.Background())
			conn := &wsConn{cancel: cancel, ctx: ctx}
			conn.state.Store(0) // CONNECTING
			id := c.ws.put(conn)
			ws := c.ws
			go func() {
				wsConn, _, err := websocket.Dial(ctx, url, nil)
				if err != nil {
					ws.tryEvent(ctx, wsEvent{connID: id, kind: wsError, reason: err.Error()})
					ws.tryEvent(ctx, wsEvent{connID: id, kind: wsClose, code: 1006, reason: "abnormal"})
					return
				}
				conn.conn = wsConn
				ws.tryEvent(ctx, wsEvent{connID: id, kind: wsOpen})
				for {
					typ, data, err := wsConn.Read(ctx)
					if err != nil {
						ws.tryEvent(ctx, wsEvent{connID: id, kind: wsClose, code: 1000, reason: ""})
						return
					}
					if !ws.tryEvent(ctx, wsEvent{
						connID: id, kind: wsMessage, data: data,
						binary: typ == websocket.MessageBinary,
					}) {
						return // Context closed; abandon.
					}
				}
			}()
			return mustValue(iso, int32(id))
		}),
		send: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 2 {
				return v8.Null(iso)
			}
			id := uint32(args[0].Integer())
			data := []byte(args[1].String())
			conn := c.ws.get(id)
			if conn == nil || conn.conn == nil {
				return v8.Null(iso)
			}
			// Use the per-conn ctx so a Context.Close cancels in-flight sends.
			ctx := conn.ctx
			go func() {
				_ = conn.conn.Write(ctx, websocket.MessageText, data)
			}()
			return v8.Null(iso)
		}),
		closeFn: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			if c == nil || len(args) < 1 {
				return v8.Null(iso)
			}
			id := uint32(args[0].Integer())
			code := uint16(1000)
			reason := ""
			if len(args) >= 2 && !args[1].IsNullOrUndefined() {
				code = uint16(args[1].Integer())
			}
			if len(args) >= 3 {
				reason = args[2].String()
			}
			conn := c.ws.get(id)
			if conn == nil {
				return v8.Null(iso)
			}
			conn.state.Store(2) // CLOSING
			if conn.conn != nil {
				_ = conn.conn.Close(websocket.StatusCode(code), reason)
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
	if err := g.Set("__ws_open", t.open.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := g.Set("__ws_send", t.send.GetFunction(v8ctx)); err != nil {
		return err
	}
	if err := g.Set("__ws_close", t.closeFn.GetFunction(v8ctx)); err != nil {
		return err
	}
	c.registerBootstrap("artemis-websocket", websocketBootstrap)
	return nil
}

const websocketBootstrap = `
(() => {
  const _wsRegistry = new Map(); // connId -> WebSocket instance
  globalThis.__ws_dispatch = function(connId, kind, payload, binary, code, reason) {
    const ws = _wsRegistry.get(connId);
    if (!ws) return;
    if (kind === 'open') {
      ws.readyState = 1;
      ws.dispatchEvent(new Event('open'));
      return;
    }
    if (kind === 'message') {
      let data = payload;
      if (binary && ws.binaryType === 'arraybuffer') {
        // payload is a length-keyed numeric array; expose as ArrayBuffer-shaped
        data = payload;
      } else if (binary) {
        data = new Blob([payload]);
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
      ws.readyState = 3;
      const ev = new CloseEvent('close', {code, reason, wasClean: code === 1000});
      ws.dispatchEvent(ev);
      _wsRegistry.delete(connId);
    }
  };

  class WebSocket {
    constructor(url, protocols) {
      this.url = String(url);
      this.protocol = '';
      this.protocols = protocols || [];
      this.readyState = 0;
      this.binaryType = 'blob';
      this.bufferedAmount = 0;
      this._listeners = new Map();
      this.onopen = null; this.onmessage = null; this.onerror = null; this.onclose = null;
      this._id = __ws_open(this.url);
      _wsRegistry.set(this._id, this);
    }
    addEventListener(type, fn) {
      if (typeof fn !== 'function') return;
      let s = this._listeners.get(type);
      if (!s) { s = new Set(); this._listeners.set(type, s); }
      s.add(fn);
    }
    removeEventListener(type, fn) {
      const s = this._listeners.get(type);
      if (s) s.delete(fn);
    }
    dispatchEvent(ev) {
      ev.target = this;
      ev.currentTarget = this;
      const handler = this['on' + ev.type];
      if (typeof handler === 'function') { try { handler.call(this, ev); } catch (e) {} }
      const s = this._listeners.get(ev.type);
      if (s) { for (const fn of s) { try { fn.call(this, ev); } catch (e) {} } }
      return !ev.defaultPrevented;
    }
    send(data) {
      if (this.readyState !== 1) throw new DOMException('not OPEN', 'InvalidStateError');
      __ws_send(this._id, String(data));
    }
    close(code, reason) {
      if (this.readyState === 3) return;
      this.readyState = 2;
      __ws_close(this._id, code || 1000, reason || '');
    }
  }
  WebSocket.CONNECTING = 0;
  WebSocket.OPEN = 1;
  WebSocket.CLOSING = 2;
  WebSocket.CLOSED = 3;
  globalThis.WebSocket = WebSocket;
})();
`
