# TASK 012 (Phase A3): AbortController + DOMException + global_event_handlers

## Why

Three small but ubiquitous WebAPI surfaces. AbortController is everywhere in modern fetch code. DOMException is what real browsers throw - libraries do `instanceof DOMException`. `<button onclick="...">` inline handlers light up the static HTML.

## Acceptance

- `AbortController` constructor; `.signal` (AbortSignal); `.abort(reason)` flips signal.
- `AbortSignal`: `.aborted`, `.reason`, `.throwIfAborted()`, `addEventListener('abort', fn)`, `dispatchEvent`.
- `DOMException` constructor `new DOMException(message, name)`; properties `.message`, `.name`, `.code`. Inherits Error.
- `<element on<event>="...">` HTML attributes registered as listeners at Context init: walks the document at install time, compiles each `onclick`/`onload`/etc. attribute as a function, registers via the JS-side listener Map. Newly created elements with on* attributes set after init are NOT auto-registered (deferred).
- All green.

## Done sub-tasks

- [x] AbortController + AbortSignal in extras bootstrap
- [x] DOMException class
- [x] init-time on* attribute walker
- [x] tests
