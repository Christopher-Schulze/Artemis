# TASK 030: WebSocket client (browser-side)

## Why

Modern dashboards, chat, live data, trading UIs use `new WebSocket(...)`. The async-runtime infrastructure (TASK 014) unlocks this. We use `github.com/coder/websocket` as the underlying transport.

## Acceptance

- `WebSocket` JS class with constructor `new WebSocket(url, protocols?)`.
- Properties: `readyState` (0=CONNECTING, 1=OPEN, 2=CLOSING, 3=CLOSED), `url`, `protocol`, `binaryType` ('blob'|'arraybuffer'), `bufferedAmount`.
- Methods: `send(data)`, `close(code?, reason?)`.
- Events: `onopen`, `onmessage`, `onerror`, `onclose` plus `addEventListener` via dispatchEvent route.
- Each WebSocket runs on a goroutine that owns the coder/websocket Conn; events post to the Context's drain channel; the V8 thread drains and fires JS callbacks.
- All green; race-clean.

## Sub-Tasks

- [x] WS event channel on Context
- [x] native trampolines `__ws_open`, `__ws_send`, `__ws_close`
- [x] JS-side `WebSocket` class hooking the trampolines
- [x] drain WS events alongside fetch results in Eval/WaitIdle
- [x] tests
- [x] FLUSH
- [x] archive

## Notes

We deliberately use `binaryType='arraybuffer'` representation as a length-keyed numeric array (same shape as crypto.subtle.digest output) since we don't have proper ArrayBuffer construction yet from Go-side. Documented limitation.
