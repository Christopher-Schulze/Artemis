# TASK 004b (Phase 3b): Event / EventTarget

## Why

Modern web pages register listeners (`element.addEventListener('click', ...)`) and dispatch events. Agents that fill forms or click buttons need this to work end-to-end: a `click()` programmatically must fire the registered handler. With no event system, even simple `<button onclick=...>` markup is dead. This TASK lands the minimum: `Event`, `EventTarget` mixin on every Element + Document, `addEventListener`, `removeEventListener`, `dispatchEvent`, and a `click()` shortcut that dispatches a click event on an element.

## Acceptance

- `js.Context` keeps a per-context listener map: handle id -> event type -> ordered list of listener functions.
- JS surface on every element + on `document`: `addEventListener(type, fn)`, `removeEventListener(type, fn)`, `dispatchEvent(event)` returning `bool` (whether default was prevented).
- Element shortcut: `element.click()` dispatches a synthetic `Event('click')` against the element and any ancestors that have a `click` listener (capturing skipped, bubbling honored).
- `Event` exposed in JS as a constructable: `new Event('foo', {bubbles: true})` with `type`, `bubbles`, `defaultPrevented`, `target`, `currentTarget`, `preventDefault()`, `stopPropagation()`.
- `make build`, `make vet`, `make test`, `go test -race ./js ./engine` all green.

- [x] mark active, write detail
- [x] initial Go-side listener map + trampolines (dropped, see Deviations)
- [x] JS-side listener map (`Map<id, Map<type, Set<fn>>>`), dispatch + bubbling implemented in bootstrap
- [x] bootstrap JS: `ELEM_PROTO` gets `addEventListener` / `removeEventListener` / `dispatchEvent` / `click`; `Event` class with `preventDefault` / `stopPropagation`; `document` mirrors EventTarget
- [x] tests: addEventListener+dispatch (×2), removeEventListener, bubbles+default-no-bubble, stopPropagation, preventDefault, click() shortcut, `this`-binding in listener
- [x] all green: `make build`, `make vet`, `make test`, `go test -race ./js ./engine`
- [x] FLUSH `docs/documentation.md`
- [x] archive

## Notes

Listener storage holds `*v8.Function` references. v8go's GC tracks these since a `*Value` returned from JS lives as long as something on the Go side holds it. The listener map is a Go-side root keeping the v8 functions alive; closing the context drops the map and frees them.

For phase 3b we do not implement event capture phase, target retargeting, or the full DOM event listener options object (`{capture, once, passive, signal}`); only the basic `(type, callback)` form. The `once` and `signal` options arrive when MutationObserver lands in TASK 004d.

Reference: internal design notes.

Initial implementation stored listeners as `*v8.Function` pointers on the Go side. Test failure in `TestRemoveEventListener` exposed that v8go's `args[N].AsFunction()` returns a fresh wrapper struct each call, so two pointer values referring to the same JS function are not equal. Workaround would have required tracking by underlying handle which v8go does not expose.

Re-architected: listeners live entirely in a JS-side `Map<id, Map<type, Set<fn>>>`. Function reference equality works inside JS Sets (`SameValueZero`), so `removeEventListener(type, fn)` correctly removes the registered handler. Bubbling is implemented in JS by walking `parentNode` (which goes through `__node_get` to Go and back). `js/events.go` and the corresponding native trampolines were removed; they did not exist in the final code.

## Deviations

`js/events.go` removed in favor of JS-side listener storage. Documented above.
